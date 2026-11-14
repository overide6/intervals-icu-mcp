package auth

import (
	"errors"
	"sync"
	"time"
)

const (
	cleanupInterval  = 5 * time.Minute
	maxClients       = 1000
	maxAuthCodes     = 10000
	maxRefreshTokens = 10000
	clientTTL        = 30 * 24 * time.Hour

	// refreshTokenReuseGrace is how long a rotated refresh token stays usable after its
	// first rotation. It tolerates concurrent refreshes from several sessions and client
	// retries after a lost token response, without allowing indefinite reuse.
	refreshTokenReuseGrace = 60 * time.Second
)

var (
	errAuthCodeNotFound     = errors.New("authorization code not found")
	errAuthCodeExpired      = errors.New("authorization code has expired")
	errRefreshTokenNotFound = errors.New("refresh token not found")
	errClientNotFound       = errors.New("client not found")
	errMaxClientsReached    = errors.New("maximum number of registered clients reached")
	errMaxAuthCodesReached  = errors.New("maximum number of authorization codes reached")
	// ErrMaxRefreshTokensReached is returned when the refresh token cap is reached.
	ErrMaxRefreshTokensReached = errors.New("maximum number of refresh tokens reached")
)

// Code represents an OAuth 2.1 authorization code stored in memory.
type Code struct {
	Code                string
	ClientID            string
	RedirectURI         string
	CodeChallenge       string
	CodeChallengeMethod string
	GitHubUsername      string
	Scopes              []string
	ExpiresAt           time.Time
}

// RefreshToken represents an OAuth 2.1 refresh token stored in memory.
type RefreshToken struct {
	Token          string
	ClientID       string
	GitHubUsername string
	Scopes         []string
	ExpiresAt      time.Time
	// RotatedAt records when the token was first rotated. Zero means never rotated.
	// A rotated token remains usable until ExpiresAt, which rotation shortens to
	// RotatedAt + refreshTokenReuseGrace (never beyond the original expiry).
	RotatedAt time.Time
}

// RegisteredClient represents a dynamically registered OAuth client.
type RegisteredClient struct {
	ClientID     string
	RedirectURIs []string
	ClientName   string
	GrantTypes   []string
	CreatedAt    time.Time
	// LastUsedAt records the last successful refresh token rotation for this client.
	// The client expires clientTTL after the later of CreatedAt and LastUsedAt.
	LastUsedAt time.Time
}

// expiresAt returns the time after which the client registration is considered expired.
func (c *RegisteredClient) expiresAt() time.Time {
	last := c.CreatedAt
	if c.LastUsedAt.After(last) {
		last = c.LastUsedAt
	}

	return last.Add(clientTTL)
}

// Store is a thread-safe in-memory store for OAuth entities.
// It runs a periodic cleanup goroutine to evict expired auth codes, refresh tokens, and clients.
type Store struct {
	mu            sync.RWMutex
	authCodes     map[string]*Code
	refreshTokens map[string]*RefreshToken
	clients       map[string]*RegisteredClient
	stopCleanup   chan struct{}
	stopOnce      sync.Once
}

// NewStore creates a new empty Store.
func NewStore() *Store {
	return &Store{
		authCodes:     make(map[string]*Code),
		refreshTokens: make(map[string]*RefreshToken),
		clients:       make(map[string]*RegisteredClient),
		stopCleanup:   make(chan struct{}),
	}
}

// StartCleanup begins periodic eviction of expired auth codes and refresh tokens.
func (s *Store) StartCleanup() {
	go func() {
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.evictExpired(time.Now())
			case <-s.stopCleanup:
				return
			}
		}
	}()
}

// StopCleanup stops the periodic cleanup goroutine. It is safe to call multiple times.
func (s *Store) StopCleanup() {
	s.stopOnce.Do(func() {
		close(s.stopCleanup)
	})
}

// evictExpired removes all expired auth codes, refresh tokens, and clients.
func (s *Store) evictExpired(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.evictExpiredLocked(now)
}

// evictExpiredLocked is the lock-free version of evictExpired.
// The caller must hold s.mu.
func (s *Store) evictExpiredLocked(now time.Time) {
	for key, ac := range s.authCodes {
		if now.After(ac.ExpiresAt) {
			delete(s.authCodes, key)
		}
	}

	for key, rt := range s.refreshTokens {
		if now.After(rt.ExpiresAt) {
			delete(s.refreshTokens, key)
		}
	}

	for key, client := range s.clients {
		if now.After(client.expiresAt()) {
			delete(s.clients, key)
		}
	}
}

// SaveAuthCode stores an authorization code. Returns an error if the maximum number
// of authorization codes has been reached. When the cap is hit, expired codes
// are evicted first before rejecting the request.
func (s *Store) SaveAuthCode(code *Code) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.authCodes) >= maxAuthCodes {
		s.evictExpiredLocked(time.Now())
	}

	if len(s.authCodes) >= maxAuthCodes {
		return errMaxAuthCodesReached
	}

	s.authCodes[code.Code] = code

	return nil
}

// GetAuthCode retrieves an authorization code without deleting it.
// Returns a copy to prevent callers from mutating store data.
// Returns an error if the code is not found or has expired.
func (s *Store) GetAuthCode(code string, now time.Time) (*Code, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ac, ok := s.authCodes[code]
	if !ok {
		return nil, errAuthCodeNotFound
	}

	if now.After(ac.ExpiresAt) {
		return nil, errAuthCodeExpired
	}

	cp := *ac
	cp.Scopes = cloneStrings(ac.Scopes)

	return &cp, nil
}

// DeleteAuthCode deletes an authorization code by key.
func (s *Store) DeleteAuthCode(code string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.authCodes, code)
}

// ValidateAndConsumeAuthCode atomically retrieves, validates, and deletes an authorization code.
// The validate function is called under the store's write lock. If validate returns an error,
// the code is NOT consumed (allowing retry with correct parameters).
// Returns an error if the code is not found, has expired, or fails validation.
func (s *Store) ValidateAndConsumeAuthCode(
	code string, now time.Time, validate func(*Code) error,
) (*Code, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ac, ok := s.authCodes[code]
	if !ok {
		return nil, errAuthCodeNotFound
	}

	if now.After(ac.ExpiresAt) {
		delete(s.authCodes, code)

		return nil, errAuthCodeExpired
	}

	if err := validate(ac); err != nil {
		return nil, err
	}

	delete(s.authCodes, code)

	cp := *ac
	cp.Scopes = cloneStrings(ac.Scopes)

	return &cp, nil
}

// SaveRefreshToken stores a refresh token. Returns an error if the maximum number
// of refresh tokens has been reached. When the cap is hit, expired tokens
// are evicted first before rejecting the request.
func (s *Store) SaveRefreshToken(token *RefreshToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.refreshTokens) >= maxRefreshTokens {
		s.evictExpiredLocked(time.Now())
	}

	if len(s.refreshTokens) >= maxRefreshTokens {
		return ErrMaxRefreshTokensReached
	}

	s.refreshTokens[token.Token] = token

	return nil
}

// ConsumeRefreshToken retrieves and deletes a refresh token (rotation).
// The caller is responsible for saving a new refresh token after consuming the old one.
// Returns an error if the token is not found, belongs to a different client, or has expired
// (including a rotated token whose reuse grace window has passed).
// The clientID check happens before deletion to prevent an attacker from revoking
// another client's refresh token by presenting it with a mismatched client_id.
func (s *Store) ConsumeRefreshToken(token, clientID string, now time.Time) (*RefreshToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rt, ok := s.refreshTokens[token]
	if !ok {
		return nil, errRefreshTokenNotFound
	}

	if rt.ClientID != clientID {
		return nil, errRefreshTokenNotFound
	}

	delete(s.refreshTokens, token)

	if now.After(rt.ExpiresAt) {
		return nil, errRefreshTokenNotFound
	}

	return rt, nil
}

// RotateRefreshToken atomically validates the old refresh token and saves a new one,
// carrying over the old token's identity fields (ClientID, GitHubUsername, Scopes).
//
// The old token is not deleted on its first rotation. Instead it is marked rotated and
// its expiry is shortened to now + refreshTokenReuseGrace (never extended), so that
// concurrent refreshes and client retries with the same token within the grace window
// also succeed, each receiving the supplied new token. After the grace window the old
// token is rejected with errRefreshTokenNotFound and removed.
//
// A client ID mismatch is rejected without mutating anything. When the token cap is
// reached (after evicting expired entries), a not-yet-rotated old token is replaced
// in place (deleted, freeing its slot) so the rotation still succeeds, but it loses its
// grace window; an already-rotated old token yields ErrMaxRefreshTokensReached and the
// store is left unchanged.
//
// On success the registered client's LastUsedAt is updated, extending its lifetime.
// The returned value is a snapshot of the old token carrying the identity fields.
func (s *Store) RotateRefreshToken(
	oldToken, clientID string, now time.Time, newToken *RefreshToken,
) (*RefreshToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rt, ok := s.refreshTokens[oldToken]
	if !ok || rt.ClientID != clientID {
		return nil, errRefreshTokenNotFound
	}

	if now.After(rt.ExpiresAt) {
		delete(s.refreshTokens, oldToken)

		return nil, errRefreshTokenNotFound
	}

	// The old token keeps its slot during the grace window, so the new token needs a
	// free slot of its own. Check the cap before mutating anything.
	if len(s.refreshTokens) >= maxRefreshTokens {
		s.evictExpiredLocked(now)
	}

	atCap := len(s.refreshTokens) >= maxRefreshTokens
	if atCap && !rt.RotatedAt.IsZero() {
		return nil, ErrMaxRefreshTokensReached
	}

	snapshot := *rt
	snapshot.Scopes = cloneStrings(rt.Scopes)

	if atCap {
		// No room for a grace entry: fall back to strict rotation, freeing the old slot.
		delete(s.refreshTokens, oldToken)
	} else {
		markRotated(rt, now)
	}

	newToken.ClientID = rt.ClientID
	newToken.GitHubUsername = rt.GitHubUsername
	newToken.Scopes = cloneStrings(rt.Scopes)

	s.refreshTokens[newToken.Token] = newToken

	s.touchClientLocked(rt.ClientID, now)

	return &snapshot, nil
}

// markRotated marks a refresh token as rotated at now (only on its first rotation) and
// shortens its expiry to the reuse grace window, never extending the original expiry.
func markRotated(rt *RefreshToken, now time.Time) {
	if !rt.RotatedAt.IsZero() {
		return
	}

	rt.RotatedAt = now

	graceEnd := now.Add(refreshTokenReuseGrace)
	if graceEnd.Before(rt.ExpiresAt) {
		rt.ExpiresAt = graceEnd
	}
}

// touchClientLocked records a use of the client at now, extending its sliding lifetime.
// Expired clients are not revived. The caller must hold s.mu for writing.
func (s *Store) touchClientLocked(clientID string, now time.Time) {
	client, ok := s.clients[clientID]
	if !ok || now.After(client.expiresAt()) {
		return
	}

	if now.After(client.LastUsedAt) {
		client.LastUsedAt = now
	}
}

// SaveClient stores a registered client. Returns an error if the maximum number
// of registered clients has been reached. When the cap is hit, expired clients
// are evicted first before rejecting the request.
func (s *Store) SaveClient(client *RegisteredClient) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.clients) >= maxClients {
		s.evictExpiredLocked(time.Now())
	}

	if len(s.clients) >= maxClients {
		return errMaxClientsReached
	}

	cp := *client
	cp.RedirectURIs = cloneStrings(client.RedirectURIs)
	cp.GrantTypes = cloneStrings(client.GrantTypes)

	s.clients[cp.ClientID] = &cp

	return nil
}

// GetClient retrieves a registered client by its client ID.
// Returns a deep copy to prevent callers from mutating store data.
// Returns an error if the client is not found or has expired.
func (s *Store) GetClient(clientID string, now time.Time) (*RegisteredClient, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	client, ok := s.clients[clientID]
	if !ok {
		return nil, errClientNotFound
	}

	if now.After(client.expiresAt()) {
		return nil, errClientNotFound
	}

	cp := *client
	cp.RedirectURIs = cloneStrings(client.RedirectURIs)
	cp.GrantTypes = cloneStrings(client.GrantTypes)

	return &cp, nil
}

func cloneStrings(src []string) []string {
	if src == nil {
		return nil
	}

	dst := make([]string, len(src))
	copy(dst, src)

	return dst
}
