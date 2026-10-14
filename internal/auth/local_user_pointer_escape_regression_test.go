package auth

import (
	"path/filepath"
	"sync"
	"testing"
)

// The authenticators store accounts in a map and SwitchWorkspace rewrites the
// stored account in place. Callers keep the *api.User they were given for the
// life of a request and read its fields with no lock held (internal/api/rest.go
// reads ctx.User in requireAuth and canAccessWorkspace), so handing out the
// stored pointer races with that write -- and Workspace, a two-word string, is
// what request scoping is derived from. These tests pin that every hand-out is
// an owned value.

func newEscapeTestAuth(t *testing.T) (*LocalAuthenticator, string) {
	t.Helper()

	const (
		adminEmail    = "admin@example.com"
		adminPassword = "TestPass1234!"
	)

	a := newTestLocalAuth(t, filepath.Join(t.TempDir(), "sessions.json"), adminEmail, adminPassword)
	_, token, err := a.Login(adminEmail, adminPassword)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	return a, token
}

// TestAuthenticateReturnsOwnedUserValue also carries the concurrency case: the
// handed-out value is read while the workspace is switched, which the race
// detector must see as ordered (a copy) rather than a write to shared memory.
func TestAuthenticateReturnsOwnedUserValue(t *testing.T) {
	a, token := newEscapeTestAuth(t)

	handedOut, err := a.Authenticate(token)
	if err != nil {
		t.Fatalf("authenticate failed: %v", err)
	}
	if handedOut.Workspace != "default" {
		t.Fatalf("unexpected starting workspace %q", handedOut.Workspace)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, err := a.SwitchWorkspace(token, "ws-switched"); err != nil {
			t.Errorf("switch failed: %v", err)
		}
	}()

	_ = handedOut.Workspace
	wg.Wait()

	// The switch still reaches the stored account, and every later caller.
	afterSwitch, err := a.Authenticate(token)
	if err != nil {
		t.Fatalf("post-switch authenticate failed: %v", err)
	}
	if afterSwitch.Workspace != "ws-switched" {
		t.Fatalf("the workspace switch did not take effect: got %q", afterSwitch.Workspace)
	}

	// ...but not the value handed out earlier.
	if handedOut.Workspace != "default" {
		t.Errorf("the *api.User from Authenticate aliases the stored account: it was rewritten in place to %q",
			handedOut.Workspace)
	}
}

func TestLoginReturnsOwnedUserValue(t *testing.T) {
	a, _ := newEscapeTestAuth(t)

	// A second session so the switch has a token to work with.
	_, token, err := a.Login("admin@example.com", "TestPass1234!")
	if err != nil {
		t.Fatalf("second login failed: %v", err)
	}
	loggedIn, _, err := a.Login("admin@example.com", "TestPass1234!")
	if err != nil {
		t.Fatalf("third login failed: %v", err)
	}

	if _, err := a.SwitchWorkspace(token, "ws-switched"); err != nil {
		t.Fatalf("switch failed: %v", err)
	}

	if loggedIn.Workspace != "default" {
		t.Errorf("the *api.User from Login aliases the stored account: it was rewritten in place to %q",
			loggedIn.Workspace)
	}
}

func TestSwitchWorkspaceReturnsOwnedUserValue(t *testing.T) {
	a, token := newEscapeTestAuth(t)

	returned, err := a.SwitchWorkspace(token, "ws-first")
	if err != nil {
		t.Fatalf("first switch failed: %v", err)
	}
	if returned.Workspace != "ws-first" {
		t.Fatalf("unexpected workspace after first switch: %q", returned.Workspace)
	}

	if _, err := a.SwitchWorkspace(token, "ws-second"); err != nil {
		t.Fatalf("second switch failed: %v", err)
	}

	current, err := a.Authenticate(token)
	if err != nil {
		t.Fatalf("authenticate failed: %v", err)
	}
	if current.Workspace != "ws-second" {
		t.Fatalf("the second switch did not take effect: got %q", current.Workspace)
	}
	if returned.Workspace != "ws-first" {
		t.Errorf("the *api.User from SwitchWorkspace aliases the stored account: it was rewritten in place to %q",
			returned.Workspace)
	}
}
