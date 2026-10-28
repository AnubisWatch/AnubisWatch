package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func corsActualOrigin(t *testing.T, server *RESTServer, origin string) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/cors-env-regression", nil)
	req.Header.Set("Origin", origin)
	server.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("actual request status = %d, want %d", w.Code, http.StatusOK)
	}
	return w.Header().Get("Access-Control-Allow-Origin")
}

// ANUBIS_CORS_ORIGINS is a comma-separated list. Spaces used to format that
// list are delimiters, not part of an origin, and must not silently make every
// entry after the first unusable by browsers.
func TestCORSEnvironmentOriginsTrimDelimiterWhitespace(t *testing.T) {
	const (
		firstOrigin  = "https://first.example"
		secondOrigin = "https://second.example"
	)
	t.Setenv("ANUBIS_CORS_ORIGINS", firstOrigin+", "+secondOrigin)

	server := corsTestServer(t)
	server.router.Handle(http.MethodGet, "/cors-env-regression", func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	tests := []struct {
		name       string
		origin     string
		wantHeader string
	}{
		{name: "first origin control", origin: firstOrigin, wantHeader: firstOrigin},
		{name: "second origin after formatted comma", origin: secondOrigin, wantHeader: secondOrigin},
		{name: "unknown origin control", origin: "https://unknown.example", wantHeader: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := corsActualOrigin(t, server, tc.origin); got != tc.wantHeader {
				t.Fatalf("actual Access-Control-Allow-Origin = %q, want %q", got, tc.wantHeader)
			}
			if got := corsPreflightOrigin(t, server, tc.origin); got != tc.wantHeader {
				t.Fatalf("preflight Access-Control-Allow-Origin = %q, want %q", got, tc.wantHeader)
			}
		})
	}
}
