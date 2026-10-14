package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// The CORS origin policy is defined once, by resolveAllowedOrigins:
// configured list -> ANUBIS_CORS_ORIGINS -> localhost development defaults.
// These tests pin that a preflight reaches the same decision as the middleware
// for the same origin. They used to disagree: the router's preflight branch
// (which runs before route matching, so corsMiddleware never sees OPTIONS)
// stopped at the environment variable and had no development defaults, so with
// nothing configured a browser blocked every cross-origin request needing a
// preflight while the server permitted the actual request.

func corsTestServer(t *testing.T) *RESTServer {
	t.Helper()
	return NewRESTServer(
		core.ServerConfig{Host: "localhost", Port: 8080}, // no AllowedOrigins: the default deployment
		core.AuthConfig{Enabled: core.BoolPtr(true)},
		newMockStorage(), &mockProbeEngine{}, &mockAlertManager{}, &mockAuthenticator{},
		&mockClusterManager{}, nil, nil, nil, nil, newTestLogger(),
	)
}

func corsPreflightOrigin(t *testing.T, server *RESTServer, origin string) string {
	t.Helper()

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/souls", nil)
	req.Header.Set("Origin", origin)
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "Content-Type")
	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", w.Code, http.StatusNoContent)
	}
	return w.Header().Get("Access-Control-Allow-Origin")
}

func TestCORSPreflightAllowsDefaultDevelopmentOrigin(t *testing.T) {
	const devOrigin = "http://localhost:3000"

	server := corsTestServer(t)

	// The middleware allows this origin on a real request; the preflight must agree.
	server.router.Handle(http.MethodGet, "/cors-probe", func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/cors-probe", nil)
	req.Header.Set("Origin", devOrigin)
	server.router.ServeHTTP(w, req)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != devOrigin {
		t.Fatalf("the middleware no longer allows the default development origin on a real request (got %q)", got)
	}

	if got := corsPreflightOrigin(t, server, devOrigin); got != devOrigin {
		t.Errorf("preflight from the default development origin was refused (got %q): the decision must not depend on preflight-ness", got)
	}
}

func TestCORSPreflightRefusesUnknownOrigin(t *testing.T) {
	if got := corsPreflightOrigin(t, corsTestServer(t), "https://evil.example"); got != "" {
		t.Errorf("an unknown origin was allowed on preflight (got %q)", got)
	}
}

// TestCORSPreflightHonoursConfiguredOrigins is the boundary case: when origins are
// configured, the development defaults must not still apply.
func TestCORSPreflightHonoursConfiguredOrigins(t *testing.T) {
	const configured = "https://dashboard.example"
	t.Setenv("ANUBIS_CORS_ORIGINS", configured)

	server := corsTestServer(t)

	if got := corsPreflightOrigin(t, server, configured); got != configured {
		t.Errorf("configured origin refused on preflight (got %q)", got)
	}
	if got := corsPreflightOrigin(t, server, "http://localhost:3000"); got != "" {
		t.Errorf("a development default was allowed although origins are configured (got %q)", got)
	}
}
