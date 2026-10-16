package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func newSoulCreateServer() (*RESTServer, *mockStorage, *mockProbeEngine, *Router) {
	storage := newMockStorage()
	probe := &mockProbeEngine{}
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config:     core.ServerConfig{Host: "localhost", Port: 8080},
		authConfig: core.AuthConfig{Enabled: core.BoolPtr(true)},
		store:      storage,
		probe:      probe,
		router:     router,
		auth:       &mockAuthenticator{},
		logger:     newTestLogger(),
		cluster:    &mockClusterManager{},
	}
	router.Handle("POST", "/api/v1/souls", server.requireRole(server.handleCreateSoul, "souls:*"))
	return server, storage, probe, router
}

func postSoul(router *Router, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/v1/souls", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// A minimal API soul used to be rejected (missing protocol block,
// valid_status) or silently created disabled (missing "enabled").
func TestCreateSoulAppliesDefaults(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		enabled bool
		check   func(*core.Soul) string
	}{
		{"minimal http", `{"name":"a","type":"http","target":"https://example.com"}`, true, func(s *core.Soul) string {
			if s.HTTP == nil || s.HTTP.Method != "GET" || len(s.HTTP.ValidStatus) != 1 || s.HTTP.ValidStatus[0] != 200 {
				return "http defaults not applied"
			}
			return ""
		}},
		{"minimal tcp", `{"name":"b","type":"tcp","target":"example.com:443"}`, true, func(s *core.Soul) string {
			if s.TCP == nil {
				return "tcp block missing"
			}
			return ""
		}},
		{"minimal dns", `{"name":"c","type":"dns","target":"example.com"}`, true, func(s *core.Soul) string {
			if s.DNS == nil || s.DNS.RecordType != "A" {
				return "dns defaults not applied"
			}
			return ""
		}},
		{"explicit disabled", `{"name":"d","type":"tcp","target":"example.com:443","enabled":false}`, false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, storage, probe, router := newSoulCreateServer()
			w := postSoul(router, tt.body)
			if w.Code != http.StatusCreated {
				t.Fatalf("status = %d: %s", w.Code, w.Body.String())
			}
			var created core.Soul
			if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
				t.Fatal(err)
			}
			stored := storage.souls[created.ID]
			if stored == nil {
				t.Fatal("soul not stored")
			}
			if stored.Enabled != tt.enabled {
				t.Fatalf("enabled = %v, want %v", stored.Enabled, tt.enabled)
			}
			if tt.enabled && len(probe.souls) != 1 {
				t.Fatalf("enabled soul not scheduled (probe souls %d)", len(probe.souls))
			}
			if tt.check != nil {
				if msg := tt.check(stored); msg != "" {
					t.Fatal(msg)
				}
			}
		})
	}
}

func TestCreateSoulErrorsNameFieldWithoutConfigIndex(t *testing.T) {
	_, _, _, router := newSoulCreateServer()
	w := postSoul(router, `{"name":"x","type":"tcp","target":"no-port"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "souls[0]") {
		t.Fatalf("API error leaks config-file index: %s", w.Body.String())
	}
}

// PUT without "enabled" used to decode as false and disable the soul.
func TestUpdateSoulKeepsEnabledWhenOmitted(t *testing.T) {
	server, storage, _, router := newSoulCreateServer()
	router.Handle("PUT", "/api/v1/souls/:id", server.requireRole(server.handleUpdateSoul, "souls:*"))
	w := postSoul(router, `{"name":"a","type":"tcp","target":"example.com:443"}`)
	var created core.Soul
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		body string
		want bool
	}{
		{`{"name":"a2","type":"tcp","target":"example.com:443"}`, true},
		{`{"name":"a3","type":"tcp","target":"example.com:443","enabled":false}`, false},
		{`{"name":"a4","type":"tcp","target":"example.com:443"}`, false},
	} {
		req := httptest.NewRequest("PUT", "/api/v1/souls/"+created.ID, bytes.NewBufferString(tt.body))
		req.Header.Set("Authorization", "Bearer valid-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT %s: %d %s", tt.body, rec.Code, rec.Body.String())
		}
		if got := storage.souls[created.ID].Enabled; got != tt.want {
			t.Fatalf("after PUT %s enabled = %v, want %v", tt.body, got, tt.want)
		}
	}
}
