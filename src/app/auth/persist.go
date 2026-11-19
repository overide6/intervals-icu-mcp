package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	stateFileName     = "auth_state.json"
	jwtSecretFileName = "jwt_secret"
	stateFileVersion  = 1
	stateDirPerm      = 0o700
	stateFilePerm     = 0o600
)

var errUnsupportedStateVersion = errors.New("unsupported auth state file version")

// StateDir is the directory where the OAuth state (registered clients, hashed refresh
// tokens, and an auto-generated JWT secret) is persisted, injected from main.go via DI.
// Empty means in-memory only: all sessions are lost when the process restarts.
type StateDir string

// persistedState is the on-disk representation of the durable part of the Store.
// Authorization codes are short-lived and are not persisted.
type persistedState struct {
	Version       int                     `json:"version"`
	Clients       []persistedClient       `json:"clients"`
	RefreshTokens []persistedRefreshToken `json:"refresh_tokens"`
}

type persistedClient struct {
	ClientID     string    `json:"client_id"`
	RedirectURIs []string  `json:"redirect_uris"`
	ClientName   string    `json:"client_name"`
	GrantTypes   []string  `json:"grant_types"`
	CreatedAt    time.Time `json:"created_at"`
	LastUsedAt   time.Time `json:"last_used_at"`
}

// persistedRefreshToken stores only the SHA-256 hash of the token value, so a leaked
// state file does not reveal usable refresh tokens.
type persistedRefreshToken struct {
	TokenHash      string    `json:"token_hash"`
	ClientID       string    `json:"client_id"`
	GitHubUsername string    `json:"github_username"`
	Scopes         []string  `json:"scopes"`
	ExpiresAt      time.Time `json:"expires_at"`
	RotatedAt      time.Time `json:"rotated_at"`
}

// hashToken returns the hex-encoded SHA-256 hash of a refresh token value.
// Refresh tokens are keyed by this hash in memory and on disk.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))

	return hex.EncodeToString(sum[:])
}

// NewPersistentStore creates a Store backed by a state file in dir. Existing, non-expired
// clients and refresh tokens are loaded from the file. An unreadable or corrupt file is
// logged and ignored (clients then have to reconnect), but a directory that cannot be
// created or written is an error, so a misconfiguration is detected at startup.
// An empty dir returns a purely in-memory Store.
func NewPersistentStore(dir StateDir) (*Store, error) {
	store := NewStore()

	if dir == "" {
		slog.Warn("no auth state directory configured: OAuth sessions are lost on restart")

		return store, nil
	}

	err := os.MkdirAll(string(dir), stateDirPerm)
	if err != nil {
		return nil, fmt.Errorf("creating auth state directory: %w", err)
	}

	store.statePath = filepath.Join(string(dir), stateFileName)

	now := time.Now()

	err = store.load(now)
	if err != nil {
		slog.Warn("ignoring unreadable auth state file", "path", store.statePath, "error", err)
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	// Write once at startup to drop expired entries and to verify the directory is writable.
	err = store.writeStateLocked()
	if err != nil {
		return nil, fmt.Errorf("writing auth state file: %w", err)
	}

	slog.Info("auth state loaded", "path", store.statePath,
		"clients", len(store.clients), "refresh_tokens", len(store.refreshTokens))

	return store, nil
}

// load reads the state file into the store, skipping expired entries.
// A missing file is not an error.
func (s *Store) load(now time.Time) error {
	data, err := os.ReadFile(s.statePath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("reading state file: %w", err)
	}

	var state persistedState

	err = json.Unmarshal(data, &state)
	if err != nil {
		return fmt.Errorf("decoding state file: %w", err)
	}

	if state.Version != stateFileVersion {
		return fmt.Errorf("%w: %d", errUnsupportedStateVersion, state.Version)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.restoreLocked(&state, now)

	return nil
}

// restoreLocked adds the non-expired entries of state to the store, respecting the caps.
// The caller must hold s.mu.
func (s *Store) restoreLocked(state *persistedState, now time.Time) {
	for _, pc := range state.Clients {
		client := &RegisteredClient{
			ClientID:     pc.ClientID,
			RedirectURIs: pc.RedirectURIs,
			ClientName:   pc.ClientName,
			GrantTypes:   pc.GrantTypes,
			CreatedAt:    pc.CreatedAt,
			LastUsedAt:   pc.LastUsedAt,
		}
		if pc.ClientID == "" || now.After(client.expiresAt()) || len(s.clients) >= maxClients {
			continue
		}

		s.clients[pc.ClientID] = client
	}

	for _, pt := range state.RefreshTokens {
		if pt.TokenHash == "" || now.After(pt.ExpiresAt) || len(s.refreshTokens) >= maxRefreshTokens {
			continue
		}

		s.refreshTokens[pt.TokenHash] = &RefreshToken{
			ClientID:       pt.ClientID,
			GitHubUsername: pt.GitHubUsername,
			Scopes:         pt.Scopes,
			ExpiresAt:      pt.ExpiresAt,
			RotatedAt:      pt.RotatedAt,
		}
	}
}

// persistLocked writes the durable state to disk if the store is file-backed.
// Failures are logged, not returned: the in-memory state stays authoritative and the
// next successful write catches up. The caller must hold s.mu.
func (s *Store) persistLocked() {
	if s.statePath == "" {
		return
	}

	err := s.writeStateLocked()
	if err != nil {
		slog.Error("persisting auth state failed", "path", s.statePath, "error", err)
	}
}

// writeStateLocked serializes clients and refresh tokens and atomically replaces the
// state file (write to a temp file in the same directory, fsync, rename).
// The caller must hold s.mu.
func (s *Store) writeStateLocked() error {
	state := persistedState{
		Version:       stateFileVersion,
		Clients:       make([]persistedClient, 0, len(s.clients)),
		RefreshTokens: make([]persistedRefreshToken, 0, len(s.refreshTokens)),
	}

	for _, c := range s.clients {
		state.Clients = append(state.Clients, persistedClient{
			ClientID:     c.ClientID,
			RedirectURIs: c.RedirectURIs,
			ClientName:   c.ClientName,
			GrantTypes:   c.GrantTypes,
			CreatedAt:    c.CreatedAt,
			LastUsedAt:   c.LastUsedAt,
		})
	}

	for hash, rt := range s.refreshTokens {
		state.RefreshTokens = append(state.RefreshTokens, persistedRefreshToken{
			TokenHash:      hash,
			ClientID:       rt.ClientID,
			GitHubUsername: rt.GitHubUsername,
			Scopes:         rt.Scopes,
			ExpiresAt:      rt.ExpiresAt,
			RotatedAt:      rt.RotatedAt,
		})
	}

	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encoding state: %w", err)
	}

	return writeFileAtomic(s.statePath, data)
}

// writeFileAtomic writes data to path with owner-only permissions, replacing any
// existing file atomically so a crash never leaves a truncated file behind.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}

	tmpName := tmp.Name()

	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename

	err = tmp.Chmod(stateFilePerm)
	if err == nil {
		_, err = tmp.Write(data)
	}

	if err == nil {
		err = tmp.Sync()
	}

	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}

	if err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}

	err = os.Rename(tmpName, path)
	if err != nil {
		return fmt.Errorf("replacing state file: %w", err)
	}

	return nil
}

// NewPersistentJWTSecret resolves the JWT signing secret. An explicitly configured
// secret always wins. Otherwise, with a state directory, the secret is read from (or
// generated once and written to) a file there, so access tokens stay valid across
// restarts. Without a state directory, a fresh random secret is generated.
func NewPersistentJWTSecret(raw RawJWTSecret, dir StateDir) (JWTSecret, error) {
	if strings.TrimSpace(string(raw)) != "" || dir == "" {
		return NewJWTSecret(raw)
	}

	err := os.MkdirAll(string(dir), stateDirPerm)
	if err != nil {
		return "", fmt.Errorf("creating auth state directory: %w", err)
	}

	path := filepath.Join(string(dir), jwtSecretFileName)

	data, err := os.ReadFile(path)
	if err == nil {
		secret, verr := NewJWTSecret(RawJWTSecret(strings.TrimSpace(string(data))))
		if verr != nil {
			return "", fmt.Errorf("invalid JWT secret in %s: %w", path, verr)
		}

		return secret, nil
	}

	if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("reading JWT secret file: %w", err)
	}

	secret, err := NewJWTSecret("")
	if err != nil {
		return "", err
	}

	err = writeFileAtomic(path, []byte(secret))
	if err != nil {
		return "", fmt.Errorf("writing JWT secret file: %w", err)
	}

	return secret, nil
}
