package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// TestOIDCDiscoveryHardening verifies discovery never expands the configured
// issuer trust boundary and rejects unbounded provider responses.
func TestOIDCDiscoveryHardening(t *testing.T) {
	t.Run("rejects non-loopback HTTP issuer before network access", func(t *testing.T) {
		auth := &OIDCAuthenticator{config: core.OIDCAuth{Issuer: "http://example.com"}}
		if _, err := auth.fetchOIDCConfig(); err == nil || !strings.Contains(err.Error(), "HTTPS") {
			t.Fatalf("expected HTTPS issuer error, got %v", err)
		}
	})

	t.Run("rejects issuer mismatch", func(t *testing.T) {
		var server *httptest.Server
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(oidcConfig{
				Issuer:   "https://attacker.example",
				AuthURL:  server.URL + "/auth",
				TokenURL: server.URL + "/token",
			})
		}))
		defer server.Close()

		auth := &OIDCAuthenticator{config: core.OIDCAuth{Issuer: server.URL}}
		if _, err := auth.fetchOIDCConfig(); err == nil || !strings.Contains(err.Error(), "does not match") {
			t.Fatalf("expected issuer mismatch, got %v", err)
		}
	})

	t.Run("accepts HTTPS endpoints on provider-declared origins", func(t *testing.T) {
		issuer, normalized, err := normalizedIssuer("https://issuer.example")
		if err != nil {
			t.Fatal(err)
		}
		if normalized != "https://issuer.example" {
			t.Fatalf("unexpected normalized issuer %q", normalized)
		}
		if _, err := validateOIDCEndpoint("https://tokens.example/token", "token", issuer); err != nil {
			t.Fatalf("OIDC permits HTTPS endpoints on a different provider origin: %v", err)
		}
	})

	t.Run("blocks discovery redirect outside issuer origin", func(t *testing.T) {
		var escaped atomic.Bool
		attacker := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { escaped.Store(true) }))
		defer attacker.Close()
		issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, attacker.URL+"/discovery", http.StatusFound)
		}))
		defer issuer.Close()

		auth := &OIDCAuthenticator{config: core.OIDCAuth{Issuer: issuer.URL}}
		if _, err := auth.fetchOIDCConfig(); err == nil {
			t.Fatal("expected cross-origin redirect to fail")
		}
		if escaped.Load() {
			t.Fatal("HTTP client followed redirect to untrusted origin")
		}
	})

	t.Run("bounds discovery response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Write([]byte(strings.Repeat("x", oidcMaxResponseBody+1)))
		}))
		defer server.Close()
		auth := &OIDCAuthenticator{config: core.OIDCAuth{Issuer: server.URL}}
		if _, err := auth.fetchOIDCConfig(); err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("expected response size error, got %v", err)
		}
	})
}

// TestParseIDToken_Hardening covers azp enforcement for multi-audience
// ID tokens and rejection of unverified email claims.
func TestParseIDToken_Hardening(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa key: %v", err)
	}
	jwks := createTestJWK(priv)
	auth := &OIDCAuthenticator{
		config:      core.OIDCAuth{Issuer: "https://idp.example.com", ClientID: "my-client"},
		jwks:        jwks,
		jwksFetched: time.Now(),
		jwksTTL:     time.Hour,
	}

	base := func() map[string]interface{} {
		return map[string]interface{}{
			"sub": "u1",
			"iss": "https://idp.example.com",
			"exp": time.Now().Add(time.Hour).Unix(),
		}
	}

	cases := []struct {
		name      string
		mutate    func(map[string]interface{})
		wantErr   bool
		errSubstr string
	}{
		{
			name:      "multi-aud without azp is rejected",
			mutate:    func(c map[string]interface{}) { c["aud"] = []interface{}{"other", "my-client"} },
			wantErr:   true,
			errSubstr: "azp",
		},
		{
			name: "multi-aud with correct azp is accepted",
			mutate: func(c map[string]interface{}) {
				c["aud"] = []interface{}{"other", "my-client"}
				c["azp"] = "my-client"
			},
			wantErr: false,
		},
		{
			name: "azp mismatch is rejected",
			mutate: func(c map[string]interface{}) {
				c["aud"] = "my-client"
				c["azp"] = "someone-else"
			},
			wantErr:   true,
			errSubstr: "azp",
		},
		{
			name: "explicit unverified email is rejected",
			mutate: func(c map[string]interface{}) {
				c["aud"] = "my-client"
				c["email"] = "victim@example.com"
				c["email_verified"] = false
			},
			wantErr:   true,
			errSubstr: "not verified",
		},
		{
			name: "verified email is accepted",
			mutate: func(c map[string]interface{}) {
				c["aud"] = "my-client"
				c["email"] = "user@example.com"
				c["email_verified"] = true
			},
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			claims := base()
			tc.mutate(claims)
			token := createTestJWT(t, priv, claims)
			_, err := auth.parseIDToken(token, "")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.errSubstr != "" && !strings.Contains(err.Error(), tc.errSubstr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
