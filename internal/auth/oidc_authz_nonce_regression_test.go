package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// oidcTestTokenBody is the token endpoint payload shape used by the fake
// providers below.
type oidcTestTokenBody struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	IDToken     string `json:"id_token"`
}

// TestOIDCAuthorizationRequestCarriesNonce pins the OIDC Core 1.0 §3.1.2.1
// requirement that the nonce of the Authentication Request is passed through
// unmodified into the ID Token, and §3.1.3.7's requirement that the client
// checks the ID token's nonce against "the value of the nonce parameter sent in
// the Authentication Request".
//
// OIDCCallback requires the ID token to echo the session nonce, so the
// authorization URL built by OIDCLoginURL must carry that nonce; otherwise no
// conformant provider can echo it and authentication always fails.
func TestOIDCAuthorizationRequestCarriesNonce(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	var idToken string

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q,"jwks_uri":%q}`,
				server.URL, server.URL+"/authorize", server.URL+"/exchange", server.URL+"/keys")
		case "/keys":
			_ = json.NewEncoder(w).Encode(createTestJWK(priv))
		case "/exchange":
			_ = json.NewEncoder(w).Encode(oidcTestTokenBody{
				AccessToken: "regression-access",
				TokenType:   "Bearer",
				IDToken:     idToken,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	auth := newTestOIDCAuth(t, core.OIDCAuth{
		Issuer:      server.URL,
		ClientID:    "regression-client-id",
		RedirectURL: "http://localhost:8080/api/v1/auth/oidc/callback",
	}, "", "admin@test.com", "TestPass1234!")

	loginURL, state, nonce, err := auth.OIDCLoginURL()
	if err != nil {
		t.Fatalf("OIDCLoginURL failed: %v", err)
	}
	if nonce == "" {
		t.Fatal("OIDCLoginURL returned an empty nonce")
	}

	authzURL, err := url.Parse(loginURL)
	if err != nil {
		t.Fatalf("authorization URL is not parseable: %v", err)
	}

	// The value a conformant provider receives, and therefore all it can echo.
	providerNonce := authzURL.Query().Get("nonce")
	if providerNonce != nonce {
		t.Fatalf("authorization request must carry the session nonce, got %q (URL: %s)",
			providerNonce, loginURL)
	}
	if got := authzURL.Query().Get("state"); got != state {
		t.Fatalf("authorization request must carry the signed state, got %q", got)
	}

	// The provider issues the ID token a conformant provider would issue.
	idToken = createTestJWT(t, priv, map[string]interface{}{
		"iss":            server.URL,
		"aud":            "regression-client-id",
		"sub":            "regression-subject",
		"email":          "regression@example.com",
		"email_verified": true,
		"name":           "Regression User",
		"exp":            time.Now().Add(time.Hour).Unix(),
		"nonce":          providerNonce,
	})

	user, token, err := auth.OIDCCallback("regression-code", state, nonce)
	if err != nil {
		t.Fatalf("OIDC login against a conformant provider failed: %v", err)
	}
	if user.Email != "regression@example.com" {
		t.Errorf("expected email regression@example.com, got %s", user.Email)
	}
	if token == "" {
		t.Error("expected a non-empty session token")
	}
}

// TestOIDCCallbackRejectsIDTokenWithoutNonce keeps the nonce check itself
// meaningful: a token that does not echo the session nonce must still be
// rejected, so the fix for the authorization request cannot be replaced by
// deleting the validation.
func TestOIDCCallbackRejectsIDTokenWithoutNonce(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	var idToken string

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q,"jwks_uri":%q}`,
				server.URL, server.URL+"/authorize", server.URL+"/exchange", server.URL+"/keys")
		case "/keys":
			_ = json.NewEncoder(w).Encode(createTestJWK(priv))
		case "/exchange":
			_ = json.NewEncoder(w).Encode(oidcTestTokenBody{
				AccessToken: "regression-access",
				TokenType:   "Bearer",
				IDToken:     idToken,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	auth := newTestOIDCAuth(t, core.OIDCAuth{
		Issuer:      server.URL,
		ClientID:    "regression-client-id",
		RedirectURL: "http://localhost:8080/api/v1/auth/oidc/callback",
	}, "", "admin@test.com", "TestPass1234!")

	_, state, nonce, err := auth.OIDCLoginURL()
	if err != nil {
		t.Fatalf("OIDCLoginURL failed: %v", err)
	}

	// A provider that drops the nonce claim must not authenticate.
	idToken = createTestJWT(t, priv, map[string]interface{}{
		"iss":   server.URL,
		"aud":   "regression-client-id",
		"sub":   "regression-subject",
		"email": "regression@example.com",
		"exp":   time.Now().Add(time.Hour).Unix(),
	})

	_, _, err = auth.OIDCCallback("regression-code", state, nonce)
	if err == nil {
		t.Fatal("expected an ID token without the session nonce to be rejected")
	}
	if !strings.Contains(err.Error(), "nonce") {
		t.Fatalf("expected a nonce validation error, got: %v", err)
	}
}
