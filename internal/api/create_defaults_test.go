package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// TestCreateHandlers_EnabledDefaultsToTrue: a create request that leaves out
// "enabled" stores an enabled object; an explicit false is kept.
func TestCreateHandlers_EnabledDefaultsToTrue(t *testing.T) {
	window := `"start_time":"2030-01-01T00:00:00Z","end_time":"2030-01-01T01:00:00Z"`
	cases := []struct {
		name   string
		call   func(*RESTServer, *Context) error
		body   string
		status int
	}{
		{"channel", (*RESTServer).handleCreateChannel, `{"name":"c","type":"webhook","config":{"url":"https://example.com/hook"}}`, http.StatusCreated},
		{"rule", (*RESTServer).handleCreateRule, `{"name":"r","channels":["c1"],"conditions":[{"type":"consecutive_failures","threshold":3}]}`, http.StatusCreated},
		{"journey", (*RESTServer).handleCreateJourney, `{"name":"j","steps":[{"name":"s","target":"https://example.com"}]}`, http.StatusCreated},
		{"maintenance", (*RESTServer).handleCreateMaintenanceWindow, `{"name":"m",` + window + `}`, http.StatusCreated},
	}
	for _, tc := range cases {
		for _, explicit := range []string{"", "false"} {
			body := tc.body
			if explicit != "" {
				body = strings.TrimSuffix(body, "}") + `,"enabled":` + explicit + `}`
			}
			server := newTestServerWithStorage(newMockStorage())
			rec := httptest.NewRecorder()
			ctx := &Context{
				Request:   httptest.NewRequest("POST", "/", strings.NewReader(body)),
				Response:  rec,
				Workspace: "default",
				User:      &User{ID: "u1"},
			}
			if err := tc.call(server, ctx); err != nil {
				t.Fatalf("%s: handler error: %v", tc.name, err)
			}
			if rec.Code != tc.status {
				t.Fatalf("%s (enabled=%q): status %d: %s", tc.name, explicit, rec.Code, rec.Body.String())
			}
			var got struct {
				Enabled bool `json:"enabled"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("%s: decode: %v", tc.name, err)
			}
			if want := explicit == ""; got.Enabled != want {
				t.Errorf("%s (enabled=%q): enabled = %v, want %v", tc.name, explicit, got.Enabled, want)
			}
		}
	}
}

func TestHandleUpdateRule_KeepsCreatedAt(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	alert := &mockAlertManager{rules: map[string]*core.AlertRule{
		"r1": {ID: "r1", WorkspaceID: "default", Name: "r", CreatedAt: created},
	}}
	server := NewRESTServer(core.ServerConfig{Port: 8080}, core.AuthConfig{Enabled: core.BoolPtr(true)}, newMockStorage(), &mockProbeEngine{}, alert, &mockAuthenticator{}, &mockClusterManager{}, nil, nil, nil, nil, newTestLogger())
	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:   httptest.NewRequest("PUT", "/", strings.NewReader(`{"name":"r2","enabled":true,"channels":["c1"],"conditions":[{"type":"consecutive_failures","threshold":3}]}`)),
		Response:  rec,
		Params:    map[string]string{"id": "r1"},
		Workspace: "default",
	}
	if err := server.handleUpdateRule(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got core.AlertRule
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.CreatedAt.Equal(created) {
		t.Errorf("created_at = %v, want %v", got.CreatedAt, created)
	}
}

func TestHandleUpdateWorkspace_NameRequiredAndDeletedAtServerManaged(t *testing.T) {
	store := newMockStorage()
	if err := store.SaveWorkspaceNoCtx(&core.Workspace{ID: "default", Name: "Default", Slug: "default"}); err != nil {
		t.Fatal(err)
	}
	server := newTestServerWithStorage(store)
	update := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		ctx := &Context{
			Request:   httptest.NewRequest("PUT", "/", strings.NewReader(body)),
			Response:  rec,
			Params:    map[string]string{"id": "default"},
			Workspace: "default",
		}
		if err := server.handleUpdateWorkspace(ctx); err != nil {
			t.Fatal(err)
		}
		return rec
	}
	if rec := update(`{"name":"  "}`); rec.Code != http.StatusBadRequest {
		t.Errorf("blank name: status %d, want 400", rec.Code)
	}
	rec := update(`{"name":"Renamed","deleted_at":"2026-01-01T00:00:00Z"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	ws, err := store.GetWorkspaceNoCtx("default")
	if err != nil {
		t.Fatal(err)
	}
	if ws.Name != "Renamed" || ws.DeletedAt != nil {
		t.Errorf("stored workspace name=%q deleted_at=%v; want Renamed, nil", ws.Name, ws.DeletedAt)
	}
}
