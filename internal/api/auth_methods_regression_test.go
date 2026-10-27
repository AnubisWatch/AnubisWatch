package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Regression: the login page had no way to learn that OIDC was configured,
// so the SSO flow at /api/v1/auth/oidc/login was unreachable from the UI.
func TestAuthMethods_PublicAndReflectsAuthenticator(t *testing.T) {
	cases := []struct {
		name     string
		authType string
		auth     Authenticator
		want     map[string]bool
	}{
		{"local", "local", &mockAuthenticator{}, map[string]bool{"local": true, "oidc": false, "ldap": false, "password_reset": false}},
		{"oidc keeps local admin login", "oidc", &mockOIDCAuth{}, map[string]bool{"local": true, "oidc": true, "ldap": false, "password_reset": false}},
		{"ldap", "ldap", &mockAuthenticator{}, map[string]bool{"local": false, "oidc": false, "ldap": true, "password_reset": false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := NewRESTServer(core.ServerConfig{Port: 8080}, core.AuthConfig{Enabled: core.BoolPtr(true), Type: tc.authType},
				newMockStorage(), &mockProbeEngine{}, &mockAlertManager{}, tc.auth, &mockClusterManager{}, nil, nil, nil, nil, newTestLogger())
			t.Cleanup(server.ws.Stop)

			// Through the router with no credentials: the login page calls
			// this before anyone is signed in.
			rec := httptest.NewRecorder()
			server.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/auth/methods", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 without auth: %s", rec.Code, rec.Body.String())
			}
			var got map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("response must hold only the method flags, got %v", got)
			}
			for key, want := range tc.want {
				if got[key] != want {
					t.Errorf("%s = %v, want %v", key, got[key], want)
				}
			}
		})
	}
}
