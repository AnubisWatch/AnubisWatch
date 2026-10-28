package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestPrivateStatusUsesDashboardLoginCookie(t *testing.T) {
	dir := t.TempDir()
	port := freeLoopbackPort(t)
	email, password := "audit@example.com", "Fixture-"+core.GenerateID()+"a1!"
	config := writeServerConfig(t, dir, port, email, password)
	s := bootAuthServer(t, config, port)
	defer s.stop(t)
	client, _ := loginAsAdmin(t, s.baseURL, email, password)
	status, raw := passwordE2EJSON(t, "GET", s.baseURL+"/api/v1/auth/me", client, nil)
	if status != http.StatusOK {
		t.Fatalf("invalid login control: %d %s", status, raw)
	}
	fmt.Println("CONTROL EXPECTED: dashboard session authenticated; ACTUAL: 200")
	slug := "audit-private-" + strings.ToLower(core.GenerateID())
	status, raw = passwordE2EJSON(t, "POST", s.baseURL+"/api/v1/status-pages", client, map[string]any{"name": "Audit private inventory", "slug": slug, "visibility": "private", "enabled": true})
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("invalid page fixture: %d %s", status, raw)
	}
	var page core.StatusPage
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	publicURL := s.baseURL + "/status/" + slug
	u, err := url.Parse(publicURL)
	if err != nil {
		t.Fatal(err)
	}
	var token string
	for _, cookie := range client.Jar.Cookies(u) {
		if cookie.Name == "auth_token" {
			token = cookie.Value
		}
	}
	if token == "" {
		t.Fatal("real login did not issue expected cookie")
	}
	// Neighbouring control: the same valid session works under the handler's
	// legacy name, proving this is cookie wiring rather than invalid identity.
	req, _ := http.NewRequest(http.MethodGet, publicURL, nil)
	req.AddCookie(&http.Cookie{Name: "anubis_session", Value: token})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	controlBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(controlBody), page.Name) {
		t.Fatalf("invalid legacy cookie control: %d", resp.StatusCode)
	}
	fmt.Println("CONTROL EXPECTED: same session under legacy cookie accepted; ACTUAL: 200")
	status, raw = passwordE2EJSON(t, "GET", publicURL, client, nil)
	fmt.Printf("EXPECTED: logged-in browser can open its private status page (200); ACTUAL: %d %s\n", status, strings.TrimSpace(string(raw)))
	if status != http.StatusOK {
		t.Fatal("PROBLEM CONFIRMED")
	}

	for _, suffix := range []string{"?format=json", "/feed.xml"} {
		status, raw = passwordE2EJSON(t, "GET", publicURL+suffix, client, nil)
		if status != http.StatusOK {
			t.Fatalf("authenticated %s status=%d %s", suffix, status, raw)
		}
	}
	status, _ = passwordE2EJSON(t, "GET", publicURL, http.DefaultClient, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("anonymous private page status=%d", status)
	}
	fmt.Println("FIX VERIFIED")
}
