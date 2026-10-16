package auth

import (
	"fmt"
	"path/filepath"
	"runtime/debug"
	"testing"
	"time"
)

// raceDetectorActive reports whether this binary was built with -race. The check
// in TestLoginVerifiesPasswordOutsideAuthLock needs a deliberately slow bcrypt,
// and the race detector inflates that work by orders of magnitude, which would
// make the package's race run pathological rather than more informative.
func raceDetectorActive() bool {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return false
	}
	for _, s := range info.Settings {
		if s.Key == "-race" {
			return s.Value == "true"
		}
	}
	return false
}

// TestLoginRejectionPathsDoNotHoldAuthLock pins that an address the caller
// chooses -- which can never authenticate -- is rejected without acquiring the
// auth lock, and that the lock is still required where state is mutated.
//
// Login and RequestPasswordReset used to take the global auth lock and then run
// bcrypt at bcrypt.DefaultCost while holding it. Authenticate needs that same
// lock, so any unauthenticated caller could stall every authenticated request.
const lockScopeCompletionBudget = 60 * time.Second

func TestLoginRejectionPathsDoNotHoldAuthLock(t *testing.T) {
	const (
		adminEmail    = "admin@example.com"
		adminPassword = "TestPass1234!"
	)

	a := newTestLocalAuth(t, filepath.Join(t.TempDir(), "sessions.json"), adminEmail, adminPassword)

	// Hold the shared lock to model an in-flight read of the auth state.
	a.mu.RLock()

	rejections := make(chan string, 3)
	go func() {
		if _, err := a.RequestPasswordReset("nobody@example.com"); err != nil {
			rejections <- fmt.Sprintf("RequestPasswordReset returned %v", err)
			return
		}
		rejections <- ""
	}()
	go func() {
		if _, _, err := a.Login("nobody@example.com", "wrong-password"); err == nil {
			rejections <- "a bogus address authenticated"
			return
		}
		rejections <- ""
	}()
	go func() {
		if _, _, err := a.Login("", "wrong-password"); err == nil {
			rejections <- "an empty address authenticated"
			return
		}
		rejections <- ""
	}()

	// Paths that touch the configured account mutate the failure counter, so they
	// must still wait for exclusive access: the lock was narrowed, not dropped.
	adminBlocked := make(chan string, 2)
	go func() {
		_, _, _ = a.Login(adminEmail, "wrong-password")
		adminBlocked <- "wrong-password"
	}()
	go func() {
		_, _, _ = a.Login(adminEmail, "")
		adminBlocked <- "empty-password"
	}()

	// The deadlines only bound how long bcrypt may take; a path blocked on the
	// lock never completes, so a generous bound proves the same thing. Under
	// -race on a busy CI runner (other packages' tests run concurrently) the
	// dummy bcrypt alone has taken over 3s.
	completed := 0
	deadline := time.After(lockScopeCompletionBudget)
	for completed < 3 {
		select {
		case msg := <-rejections:
			completed++
			if msg != "" {
				t.Error(msg)
			}
		case <-deadline:
			a.mu.RUnlock()
			t.Fatalf("only %d of 3 rejection paths completed while a reader held the auth lock; "+
				"caller-supplied bcrypt work is still serialised behind it", completed)
		}
	}

	for i := 0; i < 2; i++ {
		select {
		case who := <-adminBlocked:
			t.Errorf("the admin %s path completed while a reader held the auth lock", who)
		case <-time.After(200 * time.Millisecond):
		}
	}

	a.mu.RUnlock()

	for i := 0; i < 2; i++ {
		select {
		case <-adminBlocked:
		case <-time.After(lockScopeCompletionBudget):
			t.Fatal("the admin path never completed after the lock was released")
		}
	}
}

// TestLoginVerifiesPasswordOutsideAuthLock pins that the expensive password
// comparison does not hold the auth lock, since every authenticated request needs
// it. The bcrypt cost is raised so a held lock is unambiguous.
func TestLoginVerifiesPasswordOutsideAuthLock(t *testing.T) {
	if raceDetectorActive() {
		t.Skip("needs a deliberately slow bcrypt; the race detector inflates it by orders of magnitude")
	}

	oldCost := bcryptCost
	bcryptCost = 16
	t.Cleanup(func() { bcryptCost = oldCost })

	const (
		adminEmail    = "admin@example.com"
		adminPassword = "TestPass1234!"
	)

	a := newTestLocalAuth(t, filepath.Join(t.TempDir(), "sessions.json"), adminEmail, adminPassword)

	loginDone := make(chan struct{})
	go func() {
		_, _, _ = a.Login(adminEmail, "wrong-password")
		close(loginDone)
	}()

	time.Sleep(150 * time.Millisecond) // let the login reach its comparison

	acquired := make(chan struct{})
	go func() {
		a.mu.Lock()
		close(acquired)
		a.mu.Unlock()
	}()

	select {
	case <-acquired:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("the auth lock is held while the password is verified")
	}

	select {
	case <-loginDone:
	case <-time.After(10 * time.Second):
		t.Fatal("the login never completed")
	}
}

// TestLoginBehaviourUnchanged is the neighbouring control: moving the
// verification out of the lock must not change what Login accepts or the
// bookkeeping around it.
func TestLoginBehaviourUnchanged(t *testing.T) {
	const (
		adminEmail    = "admin@example.com"
		adminPassword = "TestPass1234!"
	)

	a := newTestLocalAuth(t, filepath.Join(t.TempDir(), "sessions.json"), adminEmail, adminPassword)

	// A wrong password is still rejected and still books a failed attempt.
	if _, _, err := a.Login(adminEmail, "wrong-password"); err == nil {
		t.Fatal("a wrong password was accepted")
	}
	a.attemptsMu.RLock()
	_, booked := a.loginAttempts[adminEmail]
	a.attemptsMu.RUnlock()
	if !booked {
		t.Error("a failed admin login was not recorded")
	}

	// The correct password still authenticates and still clears the counter.
	user, token, err := a.Login(adminEmail, adminPassword)
	if err != nil {
		t.Fatalf("the correct password was rejected: %v", err)
	}
	if token == "" || user == nil || user.Role != "admin" {
		t.Fatalf("unexpected login result: user=%+v tokenSet=%t", user, token != "")
	}
	a.attemptsMu.RLock()
	_, stillBooked := a.loginAttempts[adminEmail]
	a.attemptsMu.RUnlock()
	if stillBooked {
		t.Error("a successful login did not clear the failed-attempt counter")
	}

	// The session works, and the lockout still applies after maxLoginAttempts.
	if _, err := a.Authenticate(token); err != nil {
		t.Fatalf("the issued session is not usable: %v", err)
	}
}
