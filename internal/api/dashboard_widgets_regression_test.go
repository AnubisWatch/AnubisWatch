package api

// A custom dashboard's widgets are a non-null array on the wire. A dashboard
// created or updated without a widgets array was stored and served as
// "widgets": null, and the dashboard pages crashed iterating it. Records stored
// that way before the fix must read back as [] too.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func dashboardWidgetsServer(store Storage) *RESTServer {
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
	router.Handle("GET", "/api/v1/dashboards", server.requireAuth(server.handleListDashboards))
	router.Handle("POST", "/api/v1/dashboards", server.requireAuth(server.handleCreateDashboard))
	router.Handle("GET", "/api/v1/dashboards/:id", server.requireAuth(server.handleGetDashboard))
	router.Handle("PUT", "/api/v1/dashboards/:id", server.requireAuth(server.handleUpdateDashboard))
	return server
}

func dashboardWidgetsRequest(t *testing.T, server *RESTServer, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.router.ServeHTTP(rec, req)
	return rec
}

// assertWidgetsEmptyArray decodes one dashboard object and requires
// "widgets" to be a JSON array (not null, not absent) with no items.
func assertWidgetsEmptyArray(t *testing.T, label string, raw json.RawMessage) {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("%s: decode: %v (%s)", label, err, raw)
	}
	widgets, ok := fields["widgets"]
	if !ok {
		t.Fatalf("%s: widgets is absent: %s", label, raw)
	}
	if got := strings.TrimSpace(string(widgets)); got != "[]" {
		t.Fatalf("%s: widgets = %s, want []", label, got)
	}
}

func TestDashboardWidgetsAreNeverNull(t *testing.T) {
	store := newMockStorage()
	server := dashboardWidgetsServer(store)

	created := dashboardWidgetsRequest(t, server, "POST", "/api/v1/dashboards", `{"name":"Empty"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create: status %d: %s", created.Code, created.Body.String())
	}
	assertWidgetsEmptyArray(t, "create response", created.Body.Bytes())
	var dash core.CustomDashboard
	if err := json.Unmarshal(created.Body.Bytes(), &dash); err != nil {
		t.Fatal(err)
	}
	if stored := store.dashboards[dash.ID]; stored == nil || stored.Widgets == nil {
		t.Fatalf("stored dashboard widgets are nil")
	}

	updated := dashboardWidgetsRequest(t, server, "PUT", "/api/v1/dashboards/"+dash.ID, `{"name":"Renamed","widgets":null}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("update: status %d: %s", updated.Code, updated.Body.String())
	}
	assertWidgetsEmptyArray(t, "update response", updated.Body.Bytes())

	// A record stored with nil widgets before the fix.
	store.dashboards["legacy"] = &core.CustomDashboard{
		ID: "legacy", Name: "Legacy", WorkspaceID: "default",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	got := dashboardWidgetsRequest(t, server, "GET", "/api/v1/dashboards/legacy", "")
	if got.Code != http.StatusOK {
		t.Fatalf("get legacy: status %d: %s", got.Code, got.Body.String())
	}
	assertWidgetsEmptyArray(t, "legacy get", got.Body.Bytes())
	if store.dashboards["legacy"].Widgets != nil {
		t.Errorf("reading a legacy record modified the stored dashboard")
	}

	list := dashboardWidgetsRequest(t, server, "GET", "/api/v1/dashboards", "")
	if list.Code != http.StatusOK {
		t.Fatalf("list: status %d: %s", list.Code, list.Body.String())
	}
	var items []json.RawMessage
	if err := json.Unmarshal(list.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("list returned %d dashboards, want 2", len(items))
	}
	for _, item := range items {
		assertWidgetsEmptyArray(t, "list item", item)
	}
}
