package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Regression: GET /api/v1/config reported hard-coded retention_days and
// storage_path, and PUT accepted any reported key into an in-memory map that
// was applied nowhere and lost on restart.

type memorySettingsStore struct {
	data    map[string][]byte
	saveErr error
}

func (m *memorySettingsStore) GetSystemConfig(_ context.Context, key string) ([]byte, error) {
	value, ok := m.data[key]
	if !ok {
		return nil, fmt.Errorf("key not found")
	}
	return value, nil
}

func (m *memorySettingsStore) SaveSystemConfig(_ context.Context, key string, value []byte) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.data[key] = append([]byte(nil), value...)
	return nil
}

func settingsRequest(t *testing.T, server *RESTServer, method string, body any) (int, map[string]any) {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	rec := httptest.NewRecorder()
	ctx := &Context{Request: httptest.NewRequest(method, "/api/v1/config", reader), Response: rec}
	var err error
	if method == http.MethodGet {
		err = server.handleGetConfig(ctx)
	} else {
		err = server.handleUpdateConfig(ctx)
	}
	if err != nil {
		t.Fatalf("%s handler error: %v", method, err)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestSettings_GetReportsRunningConfig(t *testing.T) {
	server := newTestServerWithStorage(newMockStorage())
	server.SetRuntimeConfig("/srv/anubis-data", 14*24*time.Hour)

	code, got := settingsRequest(t, server, http.MethodGet, nil)
	if code != http.StatusOK {
		t.Fatalf("GET status = %d", code)
	}
	if got["storage_path"] != "/srv/anubis-data" || got["retention_days"] != float64(14) {
		t.Fatalf("expected running storage_path and retention_days, got %v / %v", got["storage_path"], got["retention_days"])
	}
	readOnly, ok := got["read_only_keys"].([]any)
	if !ok {
		t.Fatalf("read_only_keys missing or not an array: %#v", got["read_only_keys"])
	}
	listed := map[string]bool{}
	for _, key := range readOnly {
		listed[key.(string)] = true
	}
	for _, key := range []string{"retention_days", "storage_path", "auth_enabled", "port", "tls_enabled"} {
		if !listed[key] {
			t.Errorf("read_only_keys should list %q: %v", key, readOnly)
		}
	}
	for key := range editableSettingDefaults {
		if listed[key] {
			t.Errorf("editable key %q must not be listed as read-only", key)
		}
		if _, ok := got[key]; !ok {
			t.Errorf("GET should include editable key %q", key)
		}
	}
}

func TestSettings_PutRejectsConfigFileKeys(t *testing.T) {
	cases := []struct {
		name    string
		body    map[string]any
		wantMsg string
	}{
		{"retention", map[string]any{"retention_days": 7}, "retention_days is set in the config file and cannot be changed at runtime"},
		{"storage path", map[string]any{"theme": "light", "storage_path": "/tmp/x"}, "storage_path is set in the config file and cannot be changed at runtime"},
		{"auth", map[string]any{"auth_enabled": false}, "auth_enabled is set in the config file and cannot be changed at runtime"},
		{"unknown", map[string]any{"bogus": "x"}, "bogus is not a known setting"},
		{"bad theme", map[string]any{"theme": "neon"}, "theme must be one of dark, light, system"},
		{"non-string", map[string]any{"instance_name": 5}, "instance_name must be a string or null"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &memorySettingsStore{data: map[string][]byte{}}
			server := newTestServerWithStorage(newMockStorage())
			server.SetSettingsStore(store)

			code, got := settingsRequest(t, server, http.MethodPut, tc.body)
			if code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (%v)", code, got)
			}
			if got["error"] != tc.wantMsg && got["message"] != tc.wantMsg {
				t.Fatalf("error body = %v, want %q", got, tc.wantMsg)
			}
			if len(store.data) != 0 {
				t.Fatalf("a rejected request must persist nothing, stored %v", store.data)
			}
			if _, after := settingsRequest(t, server, http.MethodGet, nil); after["theme"] != "dark" {
				t.Fatalf("a rejected request must not apply valid keys alongside the bad one, theme = %v", after["theme"])
			}
		})
	}
}

func TestSettings_PresentationKeysPersistAcrossRestart(t *testing.T) {
	store := &memorySettingsStore{data: map[string][]byte{}}
	server := newTestServerWithStorage(newMockStorage())
	server.SetSettingsStore(store)

	code, got := settingsRequest(t, server, http.MethodPut, map[string]any{
		"instance_name": "Prod Watch",
		"timezone":      "Europe/Istanbul",
		"language":      "tr",
		"theme":         "light",
	})
	if code != http.StatusOK {
		t.Fatalf("PUT status = %d (%v)", code, got)
	}
	if got["instance_name"] != "Prod Watch" || got["read_only_keys"] == nil {
		t.Fatalf("PUT should answer with the full GET payload, got %v", got)
	}
	if _, ok := store.data[settingsSystemConfigKey]; !ok {
		t.Fatalf("settings were not persisted under system config key %q", settingsSystemConfigKey)
	}

	// A fresh server over the same storage is a restart.
	restarted := newTestServerWithStorage(newMockStorage())
	restarted.SetSettingsStore(store)
	_, after := settingsRequest(t, restarted, http.MethodGet, nil)
	for key, want := range map[string]string{"instance_name": "Prod Watch", "timezone": "Europe/Istanbul", "language": "tr", "theme": "light"} {
		if after[key] != want {
			t.Errorf("after restart %s = %v, want %q", key, after[key], want)
		}
	}

	// null restores the default.
	if code, got := settingsRequest(t, restarted, http.MethodPut, map[string]any{"theme": nil}); code != http.StatusOK || got["theme"] != "dark" {
		t.Fatalf("null should reset theme to its default, got %d %v", code, got["theme"])
	}
}

func TestSettings_PersistFailureIsNotReportedAsSaved(t *testing.T) {
	store := &memorySettingsStore{data: map[string][]byte{}, saveErr: fmt.Errorf("disk full")}
	server := newTestServerWithStorage(newMockStorage())
	server.SetSettingsStore(store)

	if code, _ := settingsRequest(t, server, http.MethodPut, map[string]any{"theme": "light"}); code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 when the settings cannot be stored", code)
	}
	if _, got := settingsRequest(t, server, http.MethodGet, nil); got["theme"] != "dark" {
		t.Fatalf("an unsaved change must not be served, theme = %v", got["theme"])
	}
}

// GET used to serve the copy loaded at startup, so a change written by
// another cluster node never showed up.
func TestGetConfigReloadsStoredSettings(t *testing.T) {
	store := &memorySettingsStore{data: map[string][]byte{}}
	server := newTestServerWithStorage(newMockStorage())
	server.SetSettingsStore(store)
	store.data[settingsSystemConfigKey] = []byte(`{"instance_name":"From another node"}`)
	if _, got := settingsRequest(t, server, http.MethodGet, nil); got["instance_name"] != "From another node" {
		t.Fatalf("instance_name = %v, want the stored value", got["instance_name"])
	}
}
