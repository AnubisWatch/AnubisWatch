package main

import (
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func apiKeyRequest(t *testing.T, client *http.Client, method, url, token, body string) (int, string) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

// TestAPIKeyEndToEnd drives the real server, local authenticator and store:
// an admin issues a key, the key authenticates bearer requests, survives a
// restart, is stored only as its SHA-256, and stops working once revoked.
func TestAPIKeyEndToEnd(t *testing.T) {
	const (
		adminEmail    = "admin@example.com"
		adminPassword = "TestPass1234!"
	)
	dataDir := filepath.Join(t.TempDir(), "data")
	port := freeLoopbackPort(t)
	configPath := writeServerConfig(t, dataDir, port, adminEmail, adminPassword)

	first := bootAuthServer(t, configPath, port)
	admin, _ := loginAsAdmin(t, first.baseURL, adminEmail, adminPassword)

	code, body := apiKeyRequest(t, admin, http.MethodPost, first.baseURL+"/api/v1/api-keys", "",
		`{"name":"deploy-bot","role":"editor","expires_in_days":30}`)
	if code != http.StatusCreated {
		t.Fatalf("create key: %d %s", code, body)
	}
	var created struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil || !strings.HasPrefix(created.Key, "anb_"+created.ID+"_") {
		t.Fatalf("create response %s: %v", body, err)
	}

	if code, body := apiKeyRequest(t, nil, http.MethodGet, first.baseURL+"/api/v1/souls", created.Key, ""); code != http.StatusOK {
		t.Fatalf("GET /souls with key: %d %s", code, body)
	}
	if code, _ := apiKeyRequest(t, nil, http.MethodGet, first.baseURL+"/api/v1/api-keys", created.Key, ""); code != http.StatusForbidden {
		t.Fatalf("key listing keys: %d, want 403", code)
	}
	first.stop(t)

	// Hash-only persistence: no file under the data directory (WAL and
	// snapshots included) holds the secret; the stored record is checked for
	// the hash after the restart below.
	secret := created.Key[strings.LastIndex(created.Key, "_")+1:]
	hash := core.HashAPIKey(created.Key)
	_ = filepath.WalkDir(dataDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(string(raw), secret) {
			t.Errorf("%s contains the API key secret", path)
		}
		return nil
	})

	second := bootAuthServer(t, configPath, port)
	defer second.stop(t)
	records, err := second.deps.Store.PrefixScan("default/apikeys/")
	if err != nil || len(records) != 1 {
		t.Fatalf("stored API key records = %d, %v; want 1", len(records), err)
	}
	for k, v := range records {
		if strings.Contains(string(v), secret) || !strings.Contains(string(v), hash) {
			t.Fatalf("record %s is not hash-only: %s", k, v)
		}
	}
	if code, body := apiKeyRequest(t, nil, http.MethodGet, second.baseURL+"/api/v1/souls", created.Key, ""); code != http.StatusOK {
		t.Fatalf("key after restart: %d %s", code, body)
	}

	admin2, _ := loginAsAdmin(t, second.baseURL, adminEmail, adminPassword)
	if code, body := apiKeyRequest(t, admin2, http.MethodDelete, second.baseURL+"/api/v1/api-keys/"+created.ID, "", ""); code != http.StatusOK {
		t.Fatalf("revoke: %d %s", code, body)
	}
	if code, _ := apiKeyRequest(t, nil, http.MethodGet, second.baseURL+"/api/v1/souls", created.Key, ""); code != http.StatusUnauthorized {
		t.Fatalf("revoked key: %d, want 401", code)
	}
	code, body = apiKeyRequest(t, admin2, http.MethodGet, second.baseURL+"/api/v1/api-keys", "", "")
	if code != http.StatusOK || !strings.Contains(body, `"status":"revoked"`) || strings.Contains(body, hash) {
		t.Fatalf("list after revoke: %d %s", code, body)
	}
}
