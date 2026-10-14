package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestJanitorReclaimsExpiredResetTokens pins that the cleanup janitor reclaims
// expired password-reset tokens, not only expired sessions.
//
// resetToken carries a one-hour expiry, and loadSessions already drops expired
// ones, so they must not be retained in memory for the lifetime of the process:
// POST /api/v1/auth/reset-password is unauthenticated and issues a token whenever
// the caller supplies the configured admin address.
func TestJanitorReclaimsExpiredResetTokens(t *testing.T) {
	const (
		adminEmail    = "admin@example.com"
		adminPassword = "TestPass1234!"
	)

	oldInterval := cleanupInterval
	cleanupInterval = 2 * time.Millisecond
	t.Cleanup(func() { cleanupInterval = oldInterval })

	path := filepath.Join(t.TempDir(), "sessions.json")
	a := newTestLocalAuth(t, path, adminEmail, adminPassword)

	// The session doubles as the observation point for "a janitor cycle ran":
	// removing expired sessions is behaviour that already works.
	_, sessionToken, err := a.Login(adminEmail, adminPassword)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	const issued = 10
	for i := 0; i < issued; i++ {
		if _, err := a.RequestPasswordReset(adminEmail); err != nil {
			t.Fatalf("RequestPasswordReset #%d: %v", i, err)
		}
	}
	liveToken, err := a.RequestPasswordReset(adminEmail)
	if err != nil {
		t.Fatalf("RequestPasswordReset (live): %v", err)
	}

	expired := time.Now().Add(-time.Minute)
	a.mu.Lock()
	a.tokens[sessionToken].ExpiresAt = expired
	for token, rt := range a.resetTokens {
		if token != liveToken {
			rt.ExpiresAt = expired
		}
	}
	a.mu.Unlock()

	deadline := time.Now().Add(10 * time.Second)
	for {
		a.mu.RLock()
		_, sessionStillThere := a.tokens[sessionToken]
		remaining := len(a.resetTokens)
		a.mu.RUnlock()

		if !sessionStillThere || remaining <= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no janitor cycle reclaimed expired state within 10s (reset tokens retained: %d)", remaining)
		}
		time.Sleep(time.Millisecond)
	}

	a.mu.RLock()
	remaining := len(a.resetTokens)
	_, liveSurvived := a.resetTokens[liveToken]
	a.mu.RUnlock()

	if remaining != 1 {
		t.Errorf("%d expired reset tokens retained, want only the unexpired one", remaining-1)
	}
	if !liveSurvived {
		t.Error("the unexpired reset token was dropped; reclamation must not clear the map")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var persisted sessionData
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	if len(persisted.ResetTokens) != 1 {
		t.Errorf("persisted file holds %d reset token records, want 1", len(persisted.ResetTokens))
	}
}
