package auth

import (
	"errors"
	"testing"
)

// A cluster that cannot prove quorum says nothing about the token. The
// failure must be marked unavailable (the API answers 503) so a valid session
// is not reported as invalid and dropped by its client; a token that really is
// invalid must stay a plain error (401).
func TestClusterAuthSeparatesUnavailableFromInvalid(t *testing.T) {
	s := &memoryClusterAuthStore{}
	a := newTestClusterAuth(t, s)
	token := clusterLogin(t, a, "TestPass1234!")

	setStore := func(f func()) {
		s.mu.Lock()
		defer s.mu.Unlock()
		f()
	}
	isUnavailable := func(err error) bool {
		var u *UnavailableError
		return errors.As(err, &u) && u.AuthUnavailable()
	}

	t.Run("a bad token with quorum is an ordinary error", func(t *testing.T) {
		_, err := a.Authenticate("not-a-token")
		if err == nil || isUnavailable(err) {
			t.Fatalf("invalid token error = %v, want a plain error", err)
		}
	})

	t.Run("a valid token without quorum is unavailable", func(t *testing.T) {
		setStore(func() { s.unavailable = true })
		defer setStore(func() { s.unavailable = false })
		if _, err := a.Authenticate(token); !isUnavailable(err) {
			t.Fatalf("Authenticate without quorum = %v, want an unavailable error", err)
		}
	})

	t.Run("login without quorum is unavailable, not invalid credentials", func(t *testing.T) {
		setStore(func() { s.unavailable = true })
		defer setStore(func() { s.unavailable = false })
		if _, _, err := a.Login("admin@example.com", "TestPass1234!"); !isUnavailable(err) {
			t.Fatalf("Login without quorum = %v, want an unavailable error", err)
		}
	})

	t.Run("a wrong password with quorum stays invalid credentials", func(t *testing.T) {
		_, _, err := a.Login("admin@example.com", "WrongPass1234!")
		if err == nil || isUnavailable(err) {
			t.Fatalf("wrong password error = %v, want a plain error", err)
		}
	})

	t.Run("a failed commit is unavailable", func(t *testing.T) {
		setStore(func() { s.failBefore = true })
		defer setStore(func() { s.failBefore = false })
		if err := a.Logout(token); !isUnavailable(err) {
			t.Fatalf("Logout with a failed commit = %v, want an unavailable error", err)
		}
	})

	t.Run("the token still works once quorum is back", func(t *testing.T) {
		if _, err := a.Authenticate(token); err != nil {
			t.Fatalf("valid token rejected after the outage: %v", err)
		}
	})
}
