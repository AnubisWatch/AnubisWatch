package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type userinfoRegressionTokenBody struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	IDToken     string `json:"id_token"`
}

// runOIDCUserInfoFlow drives the real OIDCCallback against a provider whose ID
// token carries idTokenClaims (plus the session nonce) and whose userinfo
// endpoint returns userinfoBody.
func runOIDCUserInfoFlow(
	t *testing.T,
	priv *rsa.PrivateKey,
	idTokenClaims map[string]interface{},
	userinfoBody map[string]interface{},
) (*api.User, string, error) {
	t.Helper()
	return runOIDCUserInfoFlowWith(t, false, priv, idTokenClaims, userinfoBody)
}

// runOIDCUserInfoFlowWith is runOIDCUserInfoFlow with
// auth.oidc.allow_unverified_email set to allowUnverified.
func runOIDCUserInfoFlowWith(
	t *testing.T,
	allowUnverified bool,
	priv *rsa.PrivateKey,
	idTokenClaims map[string]interface{},
	userinfoBody map[string]interface{},
) (*api.User, string, error) {
	t.Helper()

	var idToken string

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"issuer":%q,"authorization_endpoint":%q,"token_endpoint":%q,"jwks_uri":%q,"userinfo_endpoint":%q}`,
				server.URL, server.URL+"/authorize", server.URL+"/exchange",
				server.URL+"/keys", server.URL+"/userinfo")
		case "/keys":
			_ = json.NewEncoder(w).Encode(createTestJWK(priv))
		case "/exchange":
			_ = json.NewEncoder(w).Encode(userinfoRegressionTokenBody{
				AccessToken: "regression-access",
				TokenType:   "Bearer",
				IDToken:     idToken,
			})
		case "/userinfo":
			_ = json.NewEncoder(w).Encode(userinfoBody)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	auth := newTestOIDCAuth(t, core.OIDCAuth{
		Issuer:      server.URL,
		ClientID:    "regression-client-id",
		RedirectURL: "http://localhost:8080/api/v1/auth/oidc/callback",

		AllowUnverifiedEmail: allowUnverified,
	}, "", "admin@test.com", "TestPass1234!")

	_, state, nonce, err := auth.OIDCLoginURL()
	if err != nil {
		t.Fatalf("OIDCLoginURL failed: %v", err)
	}

	claims := map[string]interface{}{
		"iss":   server.URL,
		"aud":   "regression-client-id",
		"sub":   "regression-subject",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"nonce": nonce,
	}
	for k, v := range idTokenClaims {
		claims[k] = v
	}
	idToken = createTestJWT(t, priv, claims)

	return auth.OIDCCallback("regression-code", state, nonce)
}

// TestOIDCCallbackRejectsExplicitlyUnverifiedUserInfoEmail pins the same
// invariant parseIDToken enforces for the ID token: an address the provider
// explicitly marks unverified must not establish identity, because the account
// is provisioned and looked up by email. Here the ID token carries no email
// claim, so the userinfo response is the only candidate identity.
func TestOIDCCallbackRejectsExplicitlyUnverifiedUserInfoEmail(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	user, token, err := runOIDCUserInfoFlow(
		t,
		priv,
		nil, // ID token deliberately carries no email claim
		map[string]interface{}{
			"email":          "victim@example.com",
			"email_verified": false,
		},
	)
	if err == nil {
		t.Fatalf("an explicitly unverified userinfo email established identity: user=%+v", user)
	}
	if user != nil {
		t.Errorf("expected no user on rejection, got %+v", user)
	}
	if token != "" {
		t.Error("expected no session token to be issued on rejection")
	}
}

// TestOIDCCallbackAcceptsVerifiedUserInfoEmail is the neighbouring valid-input
// control: a provider that does verify the address still authenticates.
func TestOIDCCallbackAcceptsVerifiedUserInfoEmail(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	user, token, err := runOIDCUserInfoFlow(
		t,
		priv,
		nil,
		map[string]interface{}{
			"email":          "verified@example.com",
			"email_verified": true,
		},
	)
	if err != nil {
		t.Fatalf("verified userinfo email was rejected: %v", err)
	}
	if user.Email != "verified@example.com" {
		t.Errorf("expected email verified@example.com, got %s", user.Email)
	}
	if token == "" {
		t.Error("expected a non-empty session token")
	}
}

// TestOIDCCallbackAcceptsUserInfoEmailWhenClaimOmitted pins the documented
// boundary: email_verified is optional in userinfo, so an omitted claim is left
// to the provider's policy rather than treated as unverified.
func TestOIDCCallbackAcceptsUserInfoEmailWhenClaimOmitted(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	// By default an omitted claim is not proof of ownership: accounts are
	// matched by email.
	if _, _, err := runOIDCUserInfoFlow(t, priv, nil, map[string]interface{}{"email": "claim-omitted@example.com"}); err == nil {
		t.Fatal("an email without email_verified signed in without auth.oidc.allow_unverified_email")
	}
	user, token, err := runOIDCUserInfoFlowWith(
		t,
		true,
		priv,
		nil,
		map[string]interface{}{
			"email": "claim-omitted@example.com",
		},
	)
	if err != nil {
		t.Fatalf("with allow_unverified_email, userinfo email without an email_verified claim was rejected: %v", err)
	}
	if user.Email != "claim-omitted@example.com" {
		t.Errorf("expected email claim-omitted@example.com, got %s", user.Email)
	}
	if token == "" {
		t.Error("expected a non-empty session token")
	}
}

// TestOIDCCallbackKeepsVerifiedIDTokenEmailOverUnverifiedUserInfoEmail pins the
// secondary branch: when the ID token already established a verified identity,
// an unverified userinfo address must not replace it.
func TestOIDCCallbackKeepsVerifiedIDTokenEmailOverUnverifiedUserInfoEmail(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	user, token, err := runOIDCUserInfoFlow(
		t,
		priv,
		map[string]interface{}{
			"email":          "real@example.com",
			"email_verified": true,
		},
		map[string]interface{}{
			"email":          "attacker@example.com",
			"email_verified": false,
		},
	)
	if err != nil {
		t.Fatalf("verified ID token identity was rejected: %v", err)
	}
	if user.Email != "real@example.com" {
		t.Errorf("unverified userinfo email replaced the verified ID token identity: got %s", user.Email)
	}
	if token == "" {
		t.Error("expected a non-empty session token")
	}
}
