package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// GET /api/v1/branding is public and serves dashboard.branding, with the
// defaults filled in when it is unset.
func TestBrandingIsPublicAndServesConfig(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  *core.DashboardBranding
		want core.DashboardBranding
	}{
		{"unset", nil, core.DashboardBranding{Title: "AnubisWatch", Theme: "auto"}},
		{"configured", &core.DashboardBranding{Title: "Acme Status", Logo: "https://cdn.example.com/acme.svg", Theme: "light"},
			core.DashboardBranding{Title: "Acme Status", Logo: "https://cdn.example.com/acme.svg", Theme: "light"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := NewRESTServer(core.ServerConfig{Port: 8080}, core.AuthConfig{Enabled: core.BoolPtr(true), Type: "local"},
				newMockStorage(), &mockProbeEngine{}, &mockAlertManager{}, &mockAuthenticator{}, &mockClusterManager{}, nil, nil, nil, nil, newTestLogger())
			t.Cleanup(server.ws.Stop)
			if tc.set != nil {
				server.SetBranding(*tc.set)
			}
			rec := httptest.NewRecorder()
			server.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/branding", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d without credentials, want 200: %s", rec.Code, rec.Body.String())
			}
			var got core.DashboardBranding
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("branding = %+v, want %+v", got, tc.want)
			}
		})
	}
}
