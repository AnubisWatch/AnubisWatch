package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// newStateReclaimProvider starts a discovery-only provider, which is all
// OIDCLoginURL needs to mint a login state.
func newStateReclaimProvider(t *testing.T) *httptest.Server {
	t.Helper()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q,"jwks_uri":%q}`,
			server.URL, server.URL+"/authorize", server.URL+"/exchange", server.URL+"/keys")
	}))
	t.Cleanup(server.Close)
	return server
}

func newStateReclaimAuth(t *testing.T, issuer string) *OIDCAuthenticator {
	t.Helper()
	return newTestOIDCAuth(t, core.OIDCAuth{
		Issuer:      issuer,
		ClientID:    "regression-client-id",
		RedirectURL: "http://localhost:8080/api/v1/auth/oidc/callback",
	}, "", "admin@test.com", "TestPass1234!")
}

// TestOIDCLoginReclaimsExpiredState pins that login states are time-limited in
// practice, not just in their recorded expiry.
//
// GET /api/v1/auth/oidc/login is reachable without credentials, and a state
// entry is otherwise removed only when that exact state is submitted to the
// callback. Without reclamation, abandoned or attacker-driven login starts grow
// the map for the lifetime of the process.
func TestOIDCLoginReclaimsExpiredState(t *testing.T) {
	server := newStateReclaimProvider(t)
	auth := newStateReclaimAuth(t, server.URL)

	const abandoned = 200
	for i := 0; i < abandoned; i++ {
		if _, _, _, err := auth.OIDCLoginURL(); err != nil {
			t.Fatalf("OIDCLoginURL #%d: %v", i, err)
		}
	}

	// Age every pending entry to model the CSRF window elapsing.
	expiredAt := time.Now().Add(-time.Minute)
	auth.mu.Lock()
	for _, st := range auth.state {
		st.ExpiresAt = expiredAt
	}
	auth.mu.Unlock()

	if _, _, _, err := auth.OIDCLoginURL(); err != nil {
		t.Fatalf("OIDCLoginURL after the window elapsed: %v", err)
	}

	auth.mu.RLock()
	retained := len(auth.state)
	auth.mu.RUnlock()

	if retained != 1 {
		t.Fatalf("%d login states retained after the CSRF window elapsed, want only the fresh entry", retained)
	}
}

// TestOIDCLoginKeepsUnexpiredState is the neighbouring control: reclamation must
// remove only expired entries, so logins that are legitimately in flight survive.
func TestOIDCLoginKeepsUnexpiredState(t *testing.T) {
	server := newStateReclaimProvider(t)
	auth := newStateReclaimAuth(t, server.URL)

	_, firstSigned, _, err := auth.OIDCLoginURL()
	if err != nil {
		t.Fatalf("OIDCLoginURL: %v", err)
	}
	_, secondSigned, _, err := auth.OIDCLoginURL()
	if err != nil {
		t.Fatalf("OIDCLoginURL: %v", err)
	}

	// OIDCLoginURL returns the HMAC-signed state; the store is keyed by the raw
	// state, which verifyState recovers.
	firstState, err := auth.verifyState(firstSigned)
	if err != nil {
		t.Fatalf("verifyState: %v", err)
	}
	secondState, err := auth.verifyState(secondSigned)
	if err != nil {
		t.Fatalf("verifyState: %v", err)
	}

	auth.mu.RLock()
	retained := len(auth.state)
	firstStillPending := auth.state[firstState] != nil
	secondStillPending := auth.state[secondState] != nil
	auth.mu.RUnlock()

	if retained != 2 || !firstStillPending || !secondStillPending {
		t.Fatalf("unexpired login states were dropped: retained=%d first=%t second=%t",
			retained, firstStillPending, secondStillPending)
	}
}

// TestOIDCLoginReclaimsMalformedState guards the defensive branch: an entry with
// no expiry record must be reclaimed rather than panic the next login.
func TestOIDCLoginReclaimsMalformedState(t *testing.T) {
	server := newStateReclaimProvider(t)
	auth := newStateReclaimAuth(t, server.URL)

	auth.mu.Lock()
	auth.state["malformed"] = nil
	auth.mu.Unlock()

	if _, _, _, err := auth.OIDCLoginURL(); err != nil {
		t.Fatalf("OIDCLoginURL with a malformed pending entry: %v", err)
	}

	auth.mu.RLock()
	_, malformedRetained := auth.state["malformed"]
	retained := len(auth.state)
	auth.mu.RUnlock()

	if malformedRetained {
		t.Error("malformed pending entry was retained")
	}
	if retained != 1 {
		t.Errorf("expected only the fresh entry to remain, got %d", retained)
	}
}
