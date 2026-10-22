package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Password-protected status pages, end to end against the real server: create a
// page with a password over the REST API, confirm the public route refuses it,
// unlock it with that password, then clear the password and confirm the page is
// public again. The dashboard editor drives exactly these calls.

const (
	passwordE2ESoulName = "password-e2e-component"
)

// passwordE2ESlug returns a slug unique to this run. The package's tests share
// one data directory, so a fixed slug would collide with the page a previous
// run left behind, and a slug names exactly one public URL.
func passwordE2ESlug() string {
	return "password-e2e-" + core.GenerateID()
}

func passwordE2EValue() string {
	return "pw-" + core.GenerateID() + "-" + core.GenerateID()
}

func passwordE2EJSON(t *testing.T, method, url string, client *http.Client, body any) (int, []byte) {
	t.Helper()

	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal %s %s: %v", method, url, err)
		}
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, url, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// TestStatusPagePasswordEndToEnd is the operator's flow: set a password in the
// editor, unlock the page with it, then clear it.
func TestStatusPagePasswordEndToEnd(t *testing.T) {
	dir := t.TempDir()
	port := freeLoopbackPort(t)
	const email = "admin@example.com"
	const adminPassword = "AdminPassword123!"

	configPath := writeServerConfig(t, dir, port, email, adminPassword)
	ts := bootAuthServer(t, configPath, port)
	defer ts.stop(t)

	client, _ := loginAsAdmin(t, ts.baseURL, email, adminPassword)

	// A component the page will publish, so the rendered page is distinguishable
	// from the password form (which never contains component names).
	status, raw := passwordE2EJSON(t, "POST", ts.baseURL+"/api/v1/souls", client, map[string]any{
		"name": passwordE2ESoulName, "type": "http", "target": "http://127.0.0.1:9/",
		"weight": "60s", "timeout": "5s", "enabled": true,
		"http": map[string]any{"method": "GET", "valid_status": []int{200}},
	})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("create soul: status %d: %s", status, strings.TrimSpace(string(raw)))
	}
	var soul core.Soul
	if err := json.Unmarshal(raw, &soul); err != nil {
		t.Fatalf("decode soul: %v", err)
	}

	password := passwordE2EValue()

	slug := passwordE2ESlug()

	// 1. Create the page as password-protected through the API.
	status, raw = passwordE2EJSON(t, "POST", ts.baseURL+"/api/v1/status-pages", client, map[string]any{
		"name": "Password E2E", "slug": slug,
		"visibility": core.VisibilityProtected, "password": password,
		"souls": []string{soul.ID},
	})
	if status != http.StatusCreated {
		t.Fatalf("create protected page: status %d: %s", status, strings.TrimSpace(string(raw)))
	}
	if strings.Contains(string(raw), "password_hash") {
		t.Errorf("the create response leaked the hash: %s", strings.TrimSpace(string(raw)))
	}
	var page core.StatusPage
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if !page.PasswordSet {
		t.Errorf("the create response does not report password_set: %s", strings.TrimSpace(string(raw)))
	}

	publicURL := ts.baseURL + "/status/" + slug

	// 2. The public route refuses it: no password, then a wrong one.
	status, raw = passwordE2EJSON(t, "GET", publicURL, client, nil)
	if status != http.StatusOK || strings.Contains(string(raw), passwordE2ESoulName) {
		t.Errorf("a protected page served its data without a password: status %d: %.200s", status, string(raw))
	}
	status, raw = passwordE2EJSON(t, "GET", publicURL+"?password="+passwordE2EValue(), client, nil)
	if status != http.StatusOK || strings.Contains(string(raw), passwordE2ESoulName) {
		t.Errorf("a protected page served its data for a wrong password: status %d: %.200s", status, string(raw))
	}

	// 3. Unlock it with the password the editor set.
	status, raw = passwordE2EJSON(t, "GET", publicURL+"?password="+password, client, nil)
	if status != http.StatusOK {
		t.Fatalf("unlocking the page: status %d: %.200s", status, string(raw))
	}
	if !strings.Contains(string(raw), passwordE2ESoulName) {
		t.Errorf("the unlocked page did not serve its component: %.300s", string(raw))
	}

	// 4. Clearing the password makes the page public again.
	status, raw = passwordE2EJSON(t, "PUT", ts.baseURL+"/api/v1/status-pages/"+page.ID, client, map[string]any{
		"name": "Password E2E", "slug": slug,
		"visibility": core.VisibilityPublic, "password": "",
		// "enabled" is deliberately omitted: an update must preserve it instead of
		// disabling the page. The public fetch below is the assertion.
		"souls": []string{soul.ID},
	})
	if status != http.StatusOK {
		t.Fatalf("clearing the password: status %d: %s", status, strings.TrimSpace(string(raw)))
	}
	if strings.Contains(string(raw), `"password_set":true`) {
		t.Errorf("the update response still reports a password: %s", strings.TrimSpace(string(raw)))
	}

	status, raw = passwordE2EJSON(t, "GET", publicURL, client, nil)
	if status != http.StatusOK {
		t.Fatalf("public page after clearing: status %d: %.200s", status, string(raw))
	}
	if !strings.Contains(string(raw), passwordE2ESoulName) {
		t.Errorf("the page still refuses without a password after clearing it: %.300s", string(raw))
	}

	// The stored page must no longer hold a hash.
	stored, err := ts.deps.Store.GetStatusPageNoCtx(page.ID)
	if err != nil {
		t.Fatalf("read stored page: %v", err)
	}
	if stored.PasswordHash != "" {
		t.Errorf("clearing the password left a hash in the store: %q", stored.PasswordHash)
	}
	if stored.Visibility != core.VisibilityPublic {
		t.Errorf("stored visibility = %q, want public", stored.Visibility)
	}
}
