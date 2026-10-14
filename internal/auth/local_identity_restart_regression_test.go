package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/api"
)

// writePersistedState writes a sessions file in the production on-disk shape.
func writePersistedState(t *testing.T, path string, users map[string]*api.User, tokens map[string]*session) {
	t.Helper()
	data, err := json.Marshal(sessionData{Users: users, Tokens: tokens})
	if err != nil {
		t.Fatalf("marshal persisted state: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write persisted state: %v", err)
	}
}

// TestLocalAuthAccountIdentitySurvivesRestart pins that an account keeps its
// api.User.ID across a restart even when it has no live session.
//
// Workspace ownership is persisted as `ws.OwnerID = ctx.User.ID`
// (internal/api/rest.go) and authorized with `ws.OwnerID == ctx.User.ID`, so a
// new identity for the same account silently orphans every workspace it created:
// the workspaces drop out of GET /api/v1/workspaces and switching to them is
// denied.
func TestLocalAuthAccountIdentitySurvivesRestart(t *testing.T) {
	const (
		email    = "admin@example.com"
		password = "TestPass1234!"
	)

	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")

	// First boot through the real flow; Logout leaves the account record on disk
	// with no live session, which is the state a restart sees after a logout or
	// after the 24h session expires.
	a1 := newTestLocalAuth(t, path, email, password)

	u1, token, err := a1.Login(email, password)
	if err != nil {
		t.Fatalf("Login on first boot: %v", err)
	}
	if err := a1.Logout(token); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	a1.Shutdown()

	// Restart against the persisted state.
	a2 := newTestLocalAuth(t, path, email, password)

	u2, _, err := a2.Login(email, password)
	if err != nil {
		t.Fatalf("Login after restart: %v", err)
	}
	if u2.ID != u1.ID {
		t.Fatalf("account identity changed across restart: was %s, now %s; "+
			"workspaces with OwnerID=%s are orphaned", u1.ID, u2.ID, u1.ID)
	}
}

// TestLocalAuthNoDuplicateIdentityForPersistedAccount pins that a restart does
// not add a second identity for an account that already has persisted state,
// which would otherwise make identify-per-login depend on map iteration order.
func TestLocalAuthNoDuplicateIdentityForPersistedAccount(t *testing.T) {
	const (
		email    = "admin@example.com"
		password = "TestPass1234!"
	)

	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")

	a1 := newTestLocalAuth(t, path, email, password)

	u1, token, err := a1.Login(email, password)
	if err != nil {
		t.Fatalf("Login on first boot: %v", err)
	}
	// Keep the session live so the account is restored alongside a boot-seeded one.
	if _, err := a1.Authenticate(token); err != nil {
		t.Fatalf("Authenticate on first boot: %v", err)
	}
	a1.Shutdown()

	a2 := newTestLocalAuth(t, path, email, password)

	ids := make(map[string]int)
	for i := 0; i < 60; i++ {
		u, _, err := a2.Login(email, password)
		if err != nil {
			t.Fatalf("Login %d after restart: %v", i, err)
		}
		ids[u.ID]++
	}
	if len(ids) != 1 {
		t.Fatalf("one account resolved to %d distinct identities after restart: %v (persisted %s)",
			len(ids), ids, u1.ID)
	}
	if _, ok := ids[u1.ID]; !ok {
		t.Fatalf("fresh logins resolved to %v, none of which is the persisted identity %s", ids, u1.ID)
	}
}

// TestLocalAuthSessionIdentitySurvivesRestart is the neighbouring control: the
// session path resolves to the persisted identity and must keep doing so.
func TestLocalAuthSessionIdentitySurvivesRestart(t *testing.T) {
	const (
		email    = "admin@example.com"
		password = "TestPass1234!"
	)

	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")

	a1 := newTestLocalAuth(t, path, email, password)

	u1, token, err := a1.Login(email, password)
	if err != nil {
		t.Fatalf("Login on first boot: %v", err)
	}
	a1.Shutdown()

	a2 := newTestLocalAuth(t, path, email, password)

	u2, err := a2.Authenticate(token)
	if err != nil {
		t.Fatalf("live session not restored: %v", err)
	}
	if u2.ID != u1.ID {
		t.Fatalf("session resolved to %s, want %s", u2.ID, u1.ID)
	}
}

// TestLocalAuthAdminCreatedWhenNoPersistedAccount pins the boundary: persisted
// accounts for other emails must not suppress seeding the configured admin.
func TestLocalAuthAdminCreatedWhenNoPersistedAccount(t *testing.T) {
	const (
		email    = "admin@example.com"
		password = "TestPass1234!"
	)

	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")

	writePersistedState(t, path, map[string]*api.User{
		"other-id": {ID: "other-id", Email: "other@example.com", Role: "viewer", Workspace: "default"},
	}, nil)

	a := newTestLocalAuth(t, path, email, password)

	user, token, err := a.Login(email, password)
	if err != nil {
		t.Fatalf("admin login after restart with unrelated persisted accounts failed: %v", err)
	}
	if user.Email != email {
		t.Fatalf("expected admin account %s, got %s", email, user.Email)
	}
	if token == "" {
		t.Error("expected a non-empty session token")
	}

	// The unrelated persisted account must still be available.
	a.mu.RLock()
	_, ok := a.users["other-id"]
	a.mu.RUnlock()
	if !ok {
		t.Error("persisted account for another email was dropped")
	}
}
