package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Compatibility aliases keep working but point clients at the canonical route.
func TestDeprecatedAliasesAdvertiseSuccessor(t *testing.T) {
	server := newTestServerWithStorage(newMockStorage())
	t.Cleanup(func() { _ = server.Stop(context.Background()) })

	tests := []struct {
		path, successor string
	}{
		{"/api/v1/alerts/channels", "/api/v1/channels"},
		{"/api/v1/alerts/rules", "/api/v1/rules"},
		{"/api/v1/users/me", "/api/v1/auth/me"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			req.Header.Set("Authorization", "Bearer valid-token")
			rec := httptest.NewRecorder()
			server.router.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want the alias to keep working", rec.Code)
			}
			if got := rec.Header().Get("Deprecation"); got != "true" {
				t.Errorf("Deprecation = %q", got)
			}
			if want := "<" + tt.successor + `>; rel="successor-version"`; rec.Header().Get("Link") != want {
				t.Errorf("Link = %q, want %q", rec.Header().Get("Link"), want)
			}
		})
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/channels", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()
	server.router.ServeHTTP(rec, req)
	if rec.Header().Get("Deprecation") != "" {
		t.Error("canonical route marked deprecated")
	}
}

func TestAlertsAliasSuccessor(t *testing.T) {
	if got := alertsAliasSuccessor("/api/v1/alerts/rules/r1"); got != "/api/v1/rules/r1" {
		t.Fatalf("successor = %q", got)
	}
}
