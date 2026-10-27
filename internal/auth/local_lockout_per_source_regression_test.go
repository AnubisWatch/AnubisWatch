package auth

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// The lockout used to be keyed by the account alone, so anyone who knew the
// admin address could keep the only account locked out forever with five
// wrong passwords every fifteen minutes. Failures are now keyed by
// (account, client IP); the account as a whole only slows down, with an
// exponential backoff, after accountBackoffThreshold failures from anywhere,
// and never for an address it recently signed in from.

const (
	lockoutTestEmail    = "admin@example.com"
	lockoutTestPassword = "TestPass1234!"
)

func TestLockoutLocksTheGuessingAddressNotTheAccount(t *testing.T) {
	a := newTestLocalAuth(t, filepath.Join(t.TempDir(), "sessions.json"), lockoutTestEmail, lockoutTestPassword)

	for i := 0; i < maxLoginAttempts; i++ {
		if _, _, err := a.LoginFrom(lockoutTestEmail, "wrong-password", "198.51.100.66"); err == nil {
			t.Fatal("wrong password accepted")
		}
	}
	// The guessing address is locked, even for the right password.
	if _, _, err := a.LoginFrom(lockoutTestEmail, lockoutTestPassword, "198.51.100.66"); err == nil || !strings.Contains(err.Error(), "temporarily locked") {
		t.Fatalf("attacker address not locked: %v", err)
	}
	// The owner, elsewhere, is not.
	if _, _, err := a.LoginFrom(lockoutTestEmail, lockoutTestPassword, "203.0.113.5"); err != nil {
		t.Fatalf("legitimate user locked out by another address's failures: %v", err)
	}
	// Failures without an address still lock the address-less source
	// (callers that never pass one keep the old behaviour).
	for i := 0; i < maxLoginAttempts; i++ {
		_, _, _ = a.Login(lockoutTestEmail, "wrong-password")
	}
	if err := a.checkBruteForceProtection(lockoutTestEmail); err == nil {
		t.Fatal("address-less source not locked")
	}
	if err := a.checkBruteForceProtectionFrom(lockoutTestEmail, "203.0.113.5"); err != nil {
		t.Fatalf("address-less lockout leaked to a real address: %v", err)
	}
}

func TestDistributedGuessingTriggersAccountBackoffButNotForKnownAddresses(t *testing.T) {
	a := newTestLocalAuth(t, "", lockoutTestEmail, lockoutTestPassword)

	// The owner has signed in from 203.0.113.5 before.
	if _, _, err := a.LoginFrom(lockoutTestEmail, lockoutTestPassword, "203.0.113.5"); err != nil {
		t.Fatal(err)
	}

	// A botnet spreads guesses below the per-address limit.
	for i := 0; i < accountBackoffThreshold; i++ {
		ip := fmt.Sprintf("198.51.%d.%d", i/250, i%250+1)
		if _, _, err := a.LoginFrom(lockoutTestEmail, "wrong-password", ip); err == nil {
			t.Fatal("wrong password accepted")
		}
	}

	// A fresh address -- including the right password from it -- now waits.
	_, _, err := a.LoginFrom(lockoutTestEmail, lockoutTestPassword, "192.0.2.200")
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("account backoff not applied to a new address: %v", err)
	}
	// The owner's known address is exempt.
	if _, _, err := a.LoginFrom(lockoutTestEmail, lockoutTestPassword, "203.0.113.5"); err != nil {
		t.Fatalf("known address locked out by distributed guessing: %v", err)
	}
}

func TestAccountBackoffGrowsExponentiallyAndIsCapped(t *testing.T) {
	a := newTestLocalAuth(t, "", lockoutTestEmail, lockoutTestPassword)
	delayAfter := func(failures int) time.Duration {
		a.attemptsMu.Lock()
		delete(a.accountFailures, lockoutTestEmail)
		a.attemptsMu.Unlock()
		a.mu.Lock()
		for i := 0; i < failures; i++ {
			a.recordFailedAttemptFrom(lockoutTestEmail, fmt.Sprintf("198.51.100.%d", i%200))
		}
		a.mu.Unlock()
		a.attemptsMu.RLock()
		defer a.attemptsMu.RUnlock()
		acct := a.accountFailures[lockoutTestEmail]
		if acct.LockedUntil.IsZero() {
			return 0
		}
		return time.Until(acct.LockedUntil).Round(time.Second)
	}
	for _, tc := range []struct {
		failures int
		want     time.Duration
	}{
		{accountBackoffThreshold - 1, 0},
		{accountBackoffThreshold, accountBackoffBase},
		{accountBackoffThreshold + 3, 8 * accountBackoffBase},
		{accountBackoffThreshold + 40, accountBackoffMax},
	} {
		if got := delayAfter(tc.failures); got != tc.want {
			t.Errorf("after %d failures: backoff %v, want %v", tc.failures, got, tc.want)
		}
	}
}

func TestAccountBackoffAndKnownAddressesSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	a := newTestLocalAuth(t, path, lockoutTestEmail, lockoutTestPassword)
	if _, _, err := a.LoginFrom(lockoutTestEmail, lockoutTestPassword, "203.0.113.5"); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	for i := 0; i < accountBackoffThreshold+5; i++ {
		a.recordFailedAttemptFrom(lockoutTestEmail, fmt.Sprintf("198.51.100.%d", i))
	}
	a.mu.Unlock()
	a.Shutdown()

	b := newTestLocalAuth(t, path, lockoutTestEmail, lockoutTestPassword)
	if err := b.checkBruteForceProtectionFrom(lockoutTestEmail, "192.0.2.1"); err == nil {
		t.Error("account backoff lost on restart")
	}
	if err := b.checkBruteForceProtectionFrom(lockoutTestEmail, "203.0.113.5"); err != nil {
		t.Errorf("known address forgotten on restart: %v", err)
	}
}

func TestLoginSourceTableIsBounded(t *testing.T) {
	a := newTestLocalAuth(t, "", lockoutTestEmail, lockoutTestPassword)
	a.mu.Lock()
	for i := 0; i < maxTrackedLoginSources+100; i++ {
		a.recordFailedAttemptFrom(lockoutTestEmail, fmt.Sprintf("2001:db8::%x", i))
	}
	a.mu.Unlock()
	a.attemptsMu.RLock()
	defer a.attemptsMu.RUnlock()
	if n := len(a.loginAttempts); n > maxTrackedLoginSources {
		t.Fatalf("%d tracked sources, bound is %d", n, maxTrackedLoginSources)
	}
	if acct := a.accountFailures[lockoutTestEmail]; acct == nil || acct.Count != maxTrackedLoginSources+100 {
		t.Fatalf("account counter missed failures beyond the source table: %+v", acct)
	}
}

// The REST login route passes the resolved client address, so the lockout
// follows the caller rather than the account.
func TestLoginRouteKeysLockoutByClientAddress(t *testing.T) {
	a := newTestLocalAuth(t, "", lockoutTestEmail, lockoutTestPassword)
	srv := api.NewRESTServer(core.ServerConfig{Host: "127.0.0.1", Port: 8080}, core.AuthConfig{Enabled: core.BoolPtr(true)},
		nil, nil, nil, a, nil, nil, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { _ = srv.Stop(t.Context()) })
	login := func(remote, password string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":"`+lockoutTestEmail+`","password":"`+password+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = remote
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec.Code
	}
	for i := 0; i < maxLoginAttempts; i++ {
		login("198.51.100.66:1000", "wrong-password")
	}
	if code := login("198.51.100.66:1000", lockoutTestPassword); code != http.StatusUnauthorized {
		t.Fatalf("guessing address not locked: %d", code)
	}
	if code := login("203.0.113.5:1000", lockoutTestPassword); code != http.StatusOK {
		t.Fatalf("owner locked out from another address: %d", code)
	}
}
