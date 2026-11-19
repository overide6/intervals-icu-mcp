package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestPersistentStore(t *testing.T, dir string) *Store {
	t.Helper()

	store, err := NewPersistentStore(StateDir(dir))
	if err != nil {
		t.Fatalf("NewPersistentStore: %v", err)
	}

	return store
}

func TestPersistentStore_SurvivesRestart(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := time.Now()

	store := newTestPersistentStore(t, dir)

	if err := store.SaveClient(&RegisteredClient{
		ClientID:     "client-1",
		RedirectURIs: []string{"https://claude.ai/api/mcp/auth_callback"},
		GrantTypes:   []string{"authorization_code", "refresh_token"},
		CreatedAt:    now,
	}); err != nil {
		t.Fatalf("SaveClient: %v", err)
	}

	saveTestRefreshToken(t, store, &RefreshToken{
		Token:          "refresh-1",
		ClientID:       "client-1",
		GitHubUsername: "athlete",
		Scopes:         []string{"mcp"},
		ExpiresAt:      now.Add(time.Hour),
	})

	// Simulate a restart: a new store from the same directory.
	restarted := newTestPersistentStore(t, dir)

	client, err := restarted.GetClient("client-1", now)
	if err != nil {
		t.Fatalf("client lost after restart: %v", err)
	}

	if len(client.GrantTypes) != 2 || client.RedirectURIs[0] != "https://claude.ai/api/mcp/auth_callback" {
		t.Fatalf("client not restored correctly: %+v", client)
	}

	newToken := &RefreshToken{Token: "refresh-2", ExpiresAt: now.Add(time.Hour)}

	old, err := restarted.RotateRefreshToken("refresh-1", "client-1", now, newToken)
	if err != nil {
		t.Fatalf("refresh token lost after restart: %v", err)
	}

	if old.GitHubUsername != "athlete" || len(old.Scopes) != 1 || old.Scopes[0] != "mcp" {
		t.Fatalf("refresh token identity not restored: %+v", old)
	}

	// The rotated token must be usable after a second restart, too.
	again := newTestPersistentStore(t, dir)

	if _, err := again.RotateRefreshToken("refresh-2", "client-1", now,
		&RefreshToken{Token: "refresh-3", ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatalf("rotated refresh token lost after restart: %v", err)
	}
}

func TestPersistentStore_DoesNotStorePlainRefreshTokens(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := newTestPersistentStore(t, dir)

	saveTestRefreshToken(t, store, &RefreshToken{
		Token:     "very-secret-refresh-token-value",
		ClientID:  "client-1",
		ExpiresAt: time.Now().Add(time.Hour),
	})

	data, err := os.ReadFile(filepath.Join(dir, stateFileName))
	if err != nil {
		t.Fatalf("reading state file: %v", err)
	}

	if strings.Contains(string(data), "very-secret-refresh-token-value") {
		t.Fatal("state file contains a plain refresh token")
	}

	if !strings.Contains(string(data), hashToken("very-secret-refresh-token-value")) {
		t.Fatal("state file does not contain the token hash")
	}

	info, err := os.Stat(filepath.Join(dir, stateFileName))
	if err != nil {
		t.Fatalf("stat state file: %v", err)
	}

	if perm := info.Mode().Perm(); perm != stateFilePerm {
		t.Fatalf("state file permissions = %o, want %o", perm, stateFilePerm)
	}
}

func TestPersistentStore_SkipsExpiredEntriesOnLoad(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := time.Now()
	store := newTestPersistentStore(t, dir)

	saveTestRefreshToken(t, store, &RefreshToken{
		Token: "short-lived", ClientID: "c", ExpiresAt: now.Add(-time.Second),
	})

	if err := store.SaveClient(&RegisteredClient{
		ClientID: "old-client", CreatedAt: now.Add(-clientTTL - time.Hour),
	}); err != nil {
		t.Fatalf("SaveClient: %v", err)
	}

	restarted := newTestPersistentStore(t, dir)

	if len(restarted.refreshTokens) != 0 || len(restarted.clients) != 0 {
		t.Fatalf("expired entries restored: %d tokens, %d clients",
			len(restarted.refreshTokens), len(restarted.clients))
	}
}

func TestPersistentStore_CorruptFileStartsEmpty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, stateFileName), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("writing corrupt file: %v", err)
	}

	store := newTestPersistentStore(t, dir)

	if len(store.clients) != 0 || len(store.refreshTokens) != 0 {
		t.Fatal("corrupt file should yield an empty store")
	}

	// The corrupt file is replaced with a valid one.
	if _, err := NewPersistentStore(StateDir(dir)); err != nil {
		t.Fatalf("reopening after corrupt file: %v", err)
	}
}

func TestPersistentStore_UnwritableDirFails(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "not-a-dir")

	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatalf("creating file: %v", err)
	}

	if _, err := NewPersistentStore(StateDir(filepath.Join(file, "sub"))); err == nil {
		t.Fatal("expected an error for a state dir below a regular file")
	}
}

func TestPersistentStore_EmptyDirIsInMemory(t *testing.T) {
	t.Parallel()

	store := newTestPersistentStore(t, "")

	if store.statePath != "" {
		t.Fatalf("statePath = %q, want empty", store.statePath)
	}
}

func TestNewPersistentJWTSecret_StableAcrossRestarts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	first, err := NewPersistentJWTSecret("", StateDir(dir))
	if err != nil {
		t.Fatalf("first: %v", err)
	}

	second, err := NewPersistentJWTSecret("", StateDir(dir))
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	if first != second {
		t.Fatal("JWT secret changed across restarts")
	}

	if len(first) < jwtSecretLength {
		t.Fatalf("secret too short: %d", len(first))
	}
}

func TestNewPersistentJWTSecret_ExplicitSecretWins(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	explicit := RawJWTSecret("explicit-secret-that-is-at-least-32-characters")

	secret, err := NewPersistentJWTSecret(explicit, StateDir(dir))
	if err != nil {
		t.Fatalf("NewPersistentJWTSecret: %v", err)
	}

	if string(secret) != string(explicit) {
		t.Fatal("explicit secret not used")
	}

	if _, err := os.Stat(filepath.Join(dir, jwtSecretFileName)); err == nil {
		t.Fatal("secret file must not be written when an explicit secret is set")
	}
}

func TestNewPersistentJWTSecret_NoDirGeneratesEphemeral(t *testing.T) {
	t.Parallel()

	first, err := NewPersistentJWTSecret("", "")
	if err != nil {
		t.Fatalf("first: %v", err)
	}

	second, err := NewPersistentJWTSecret("", "")
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	if first == second {
		t.Fatal("expected different ephemeral secrets")
	}
}
