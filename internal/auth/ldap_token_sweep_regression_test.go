package auth

// Regression test: LDAPAuthenticator must bound its token map. Expired
// sessions were deleted only by Logout/SwitchWorkspace, so Authenticate
// rejected an expired token while leaving its entry in the map forever, and
// logins never repeated grew the map for the process lifetime. Matching the
// LocalAuthenticator's cleanupExpiredSessions and the OIDC state store's
// sweep-on-insert: Authenticate reclaims the entry it rejects, and Login
// sweeps expired sessions on insert.

import (
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/api"
)

func TestLDAPExpiredTokensReclaimed(t *testing.T) {
	l := &LDAPAuthenticator{
		users:  make(map[string]*api.User),
		tokens: make(map[string]*session),
		stopCh: make(chan struct{}),
	}
	l.users["u1"] = &api.User{ID: "u1"}
	now := time.Now()

	expired1 := "tok-expired-1"
	expired2 := "tok-expired-2"
	live := "tok-live"
	l.tokens[expired1] = &session{UserID: "u1", ExpiresAt: now.Add(-time.Hour)}
	l.tokens[expired2] = &session{UserID: "u1", ExpiresAt: now.Add(-time.Minute)}
	l.tokens[live] = &session{UserID: "u1", ExpiresAt: now.Add(time.Hour)}

	// Presenting an expired token must reject it AND reclaim the entry.
	if _, err := l.Authenticate(expired1); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("Authenticate(expired) = %v, want expiry error", err)
	}
	if _, still := l.tokens[expired1]; still {
		t.Fatalf("rejected expired token %q was not reclaimed", expired1)
	}

	// The login-time sweep drops every expired entry and keeps live ones.
	l.mu.Lock()
	l.sweepExpiredTokensLocked(now)
	l.mu.Unlock()
	if len(l.tokens) != 1 {
		t.Fatalf("sweep left %d entries, want 1 (the live session)", len(l.tokens))
	}
	if _, ok := l.tokens[live]; !ok {
		t.Fatalf("sweep removed the live session")
	}

	// Control: a live token still authenticates.
	if _, err := l.Authenticate(live); err != nil {
		t.Fatalf("live token rejected after sweep: %v", err)
	}
}
