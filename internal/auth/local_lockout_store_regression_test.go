package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLocalAuthLockoutStoreOnlyTracksAuthenticatableAccount pins that the
// brute-force lockout store holds entries only for accounts that can
// authenticate.
//
// Login compares the supplied address against the configured admin address in
// constant time and fails every other address before any password check, so an
// attempt against another address can never affect a lockout decision. Recording
// it lets an unauthenticated caller (POST /api/v1/auth/login) grow both the
// in-memory store and the file every failed login rewrites.
func TestLocalAuthLockoutStoreOnlyTracksAuthenticatableAccount(t *testing.T) {
	const (
		email    = "admin@example.com"
		password = "TestPass1234!"
	)

	path := filepath.Join(t.TempDir(), "sessions.json")
	a := newTestLocalAuth(t, path, email, password)

	// A legitimate failure must still be recorded; it also writes the store that
	// the assertions below inspect.
	if _, _, err := a.Login(email, "wrong-password"); err == nil {
		t.Fatal("a wrong password was accepted for the admin account")
	}

	// Caller-chosen addresses, covering both the empty-password branch and the
	// password-failure branch.
	for i := 0; i < 25; i++ {
		if _, _, err := a.Login(fmt.Sprintf("attacker-%d@example.com", i), ""); err == nil {
			t.Fatalf("caller-supplied address %d unexpectedly authenticated", i)
		}
	}
	for i := 0; i < 2; i++ {
		if _, _, err := a.Login(fmt.Sprintf("other-%d@example.com", i), "wrong-password"); err == nil {
			t.Fatalf("caller-supplied address %d unexpectedly authenticated", i)
		}
	}

	a.attemptsMu.RLock()
	tracked := make([]string, 0, len(a.loginAttempts))
	for address := range a.loginAttempts {
		tracked = append(tracked, address)
	}
	a.attemptsMu.RUnlock()

	if len(tracked) != 1 || tracked[0] != email {
		t.Fatalf("lockout store tracks %v, want only the account that can authenticate (%q)", tracked, email)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var persisted sessionData
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	if len(persisted.Lockouts) != 1 || persisted.Lockouts[email] == nil {
		persistedAddresses := make([]string, 0, len(persisted.Lockouts))
		for address := range persisted.Lockouts {
			persistedAddresses = append(persistedAddresses, address)
		}
		t.Fatalf("persisted lockout store holds %v, want only %q", persistedAddresses, email)
	}
}

// TestLocalAuthLockoutStillLocksAdminAccount is the neighbouring control:
// restricting the store to the authenticatable account must not weaken the
// protection it exists for.
func TestLocalAuthLockoutStillLocksAdminAccount(t *testing.T) {
	const (
		email    = "admin@example.com"
		password = "TestPass1234!"
	)

	a := newTestLocalAuth(t, filepath.Join(t.TempDir(), "sessions.json"), email, password)

	for i := 0; i < maxLoginAttempts; i++ {
		if _, _, err := a.Login(email, "wrong-password"); err == nil {
			t.Fatalf("a wrong password was accepted at attempt %d", i+1)
		}
	}

	if err := a.checkBruteForceProtection(email); err == nil {
		t.Fatal("the admin account was not locked after maxLoginAttempts failures")
	}
	if _, _, err := a.Login(email, password); err == nil || !strings.Contains(err.Error(), "temporarily locked") {
		t.Fatalf("the correct password was not blocked while locked, got: %v", err)
	}
}

// TestLocalAuthLockoutClearedOnSuccessfulLogin pins the other half of the
// lifecycle: a successful login still clears the counter.
func TestLocalAuthLockoutClearedOnSuccessfulLogin(t *testing.T) {
	const (
		email    = "admin@example.com"
		password = "TestPass1234!"
	)

	a := newTestLocalAuth(t, filepath.Join(t.TempDir(), "sessions.json"), email, password)

	for i := 0; i < maxLoginAttempts-1; i++ {
		if _, _, err := a.Login(email, "wrong-password"); err == nil {
			t.Fatalf("a wrong password was accepted at attempt %d", i+1)
		}
	}
	if err := a.checkBruteForceProtection(email); err != nil {
		t.Fatalf("the account was locked before reaching maxLoginAttempts: %v", err)
	}

	if _, _, err := a.Login(email, password); err != nil {
		t.Fatalf("the correct password was rejected: %v", err)
	}

	a.attemptsMu.RLock()
	_, stillTracked := a.loginAttempts[email]
	a.attemptsMu.RUnlock()
	if stillTracked {
		t.Error("a successful login did not clear the failed-attempt counter")
	}
}
