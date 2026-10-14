package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// A PUT to /api/v1/status-pages/{id} that omits "enabled" must keep the stored
// value. Binding the zero value disabled the page, and Storage's
// GetStatusPageBySlug filters out !Enabled pages, so the public /status/{slug} URL
// silently started 404ing for any client that did not echo the field. The create
// handler already peeked the raw body to tell "field omitted" from "explicit
// false"; the update handler now applies the same rule.

func statusPageEnabledServer(store *mockStorage) *Router {
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config:     core.ServerConfig{Host: "localhost", Port: 8080},
		authConfig: core.AuthConfig{Enabled: core.BoolPtr(true)},
		store:      store,
		router:     router,
		auth:       &mockAuthenticator{},
		logger:     newTestLogger(),
		cluster:    &mockClusterManager{},
	}
	router.Handle("PUT", "/api/v1/status-pages/:id", server.requireAuth(server.handleUpdateStatusPage))
	return router
}

func statusPageEnabledPut(t *testing.T, router *Router, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest("PUT", "/api/v1/status-pages/page-1", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func statusPageEnabledStore(enabled bool) *mockStorage {
	store := newMockStorage()
	store.statusPages["page-1"] = &core.StatusPage{
		ID: "page-1", WorkspaceID: "default", Name: "Enabled Regression", Slug: "enabled-regression",
		Enabled: enabled, Visibility: core.VisibilityPublic,
	}
	return store
}

func TestStatusPageUpdatePreservesOmittedEnabled(t *testing.T) {
	for _, tc := range []struct {
		name    string
		stored  bool
		body    string
		want    bool
		because string
	}{
		{
			name: "omitted keeps an enabled page enabled", stored: true,
			body: `{"name":"Enabled Regression","slug":"enabled-regression"}`, want: true,
			because: "an update that does not mention enabled must not disable the page",
		},
		{
			name: "omitted keeps a disabled page disabled", stored: false,
			body: `{"name":"Enabled Regression","slug":"enabled-regression"}`, want: false,
			because: "preserving means preserving either value, not defaulting to enabled",
		},
		{
			name: "explicit false still disables", stored: true,
			body: `{"name":"Enabled Regression","slug":"enabled-regression","enabled":false}`, want: false,
			because: "an explicit opt-out must keep working",
		},
		{
			name: "explicit true still enables", stored: false,
			body: `{"name":"Enabled Regression","slug":"enabled-regression","enabled":true}`, want: true,
			because: "an explicit opt-in must keep working",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := statusPageEnabledStore(tc.stored)
			rec := statusPageEnabledPut(t, statusPageEnabledServer(store), tc.body)
			if rec.Code != http.StatusOK {
				t.Fatalf("update status = %d, want 200: %s", rec.Code, rec.Body.String())
			}
			if stored := store.statusPages["page-1"]; stored.Enabled != tc.want {
				t.Errorf("stored enabled = %v, want %v: %s", stored.Enabled, tc.want, tc.because)
			}
		})
	}
}
