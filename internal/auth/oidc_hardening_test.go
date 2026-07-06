package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// TestParseIDToken_Hardening covers the OIDC ID-token validation hardening:
// azp enforcement for multi-audience tokens and rejection of unverified emails.
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
