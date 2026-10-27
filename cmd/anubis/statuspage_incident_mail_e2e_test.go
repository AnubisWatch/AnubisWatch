package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/mail/mailtest"
	"github.com/AnubisWatch/anubiswatch/internal/probe"
)

// Status page incidents end to end against the real server and a fake SMTP
// relay: each change posted through the dedicated incident endpoints reaches
// the save hook (restStorageAdapter.onStatusPageSave) once, so a confirmed
// subscriber gets exactly one email per incident, update and resolution, and
// none for a metadata edit.
func TestStatusPageIncidentMailEndToEnd(t *testing.T) {
	previous := probe.DefaultValidator.Load()
	t.Cleanup(func() { probe.DefaultValidator.Store(previous) })

	relay := mailtest.NewServer(t)
	dir := t.TempDir()
	port := freeLoopbackPort(t)
	const email = "admin@example.com"
	const adminPassword = "AdminPassword123!"
	config := fmt.Sprintf(`{
  "storage": {"path": %q},
  "server": {"host": "127.0.0.1", "port": %d, "grpc_port": 0, "public_url": "https://status.example.com"},
  "necropolis": {"node_name": "incident-mail-node"},
  "security": {"allow_private_targets": true},
  "mail": {"host": %q, "port": %d, "from": "status@example.com", "tls": "none"},
  "auth": {"type": "local", "local": {"admin_email": %q, "admin_password": %q}}
}`, filepath.ToSlash(dir), port, relay.Host(), relay.Port(), email, adminPassword)
	configPath := filepath.Join(filepath.Dir(dir), "anubis-incident-mail.json")
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	ts := bootAuthServer(t, configPath, port)
	defer ts.stop(t)
	client, _ := loginAsAdmin(t, ts.baseURL, email, adminPassword)

	status, raw := passwordE2EJSON(t, "POST", ts.baseURL+"/api/v1/status-pages", client, map[string]any{
		"name": "Incident Mail", "slug": "incident-mail-" + core.GenerateID(),
	})
	if status != http.StatusCreated {
		t.Fatalf("create page = %d %s", status, raw)
	}
	var page core.StatusPage
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []*core.StatusPageSubscription{
		{ID: core.GenerateID(), PageID: page.ID, Email: "reader@example.com", Type: "email", Confirmed: true, UnsubscribeToken: core.GenerateID()},
		{ID: core.GenerateID(), PageID: page.ID, Email: "pending@example.com", Type: "email", Confirmed: false, UnsubscribeToken: core.GenerateID()},
	} {
		if err := ts.deps.Store.SaveStatusPageSubscription(sub); err != nil {
			t.Fatal(err)
		}
	}

	expect := func(step string, n int, subject string) {
		t.Helper()
		msgs := relay.WaitFor(n, 5*time.Second)
		time.Sleep(200 * time.Millisecond) // a duplicate would arrive by now
		msgs = relay.Messages()
		if len(msgs) != n {
			t.Fatalf("%s: relay has %d messages, want %d", step, len(msgs), n)
		}
		if subject == "" {
			return
		}
		last := msgs[n-1]
		if len(last.To) != 1 || last.To[0] != "reader@example.com" || !strings.Contains(last.Header("Subject"), subject) {
			t.Fatalf("%s: got %q to %v", step, last.Header("Subject"), last.To)
		}
	}

	base := ts.baseURL + "/api/v1/status-pages/" + page.ID + "/incidents"
	status, raw = passwordE2EJSON(t, "POST", base, client, map[string]any{"title": "Checkout errors", "impact": "major", "message": "Investigating."})
	if status != http.StatusCreated {
		t.Fatalf("create incident = %d %s", status, raw)
	}
	var inc core.StatusIncident
	if err := json.Unmarshal(raw, &inc); err != nil {
		t.Fatal(err)
	}
	expect("create", 1, "Incident: Checkout errors")

	if status, raw = passwordE2EJSON(t, "POST", base+"/"+inc.ID+"/updates", client, map[string]any{"message": "Fix deployed", "status": "monitoring"}); status != http.StatusCreated {
		t.Fatalf("update = %d %s", status, raw)
	}
	expect("update", 2, "Update: Checkout errors")

	if status, raw = passwordE2EJSON(t, "PATCH", base+"/"+inc.ID, client, map[string]any{"impact": "minor"}); status != http.StatusOK {
		t.Fatalf("patch = %d %s", status, raw)
	}
	expect("patch", 2, "")

	if status, raw = passwordE2EJSON(t, "POST", base+"/"+inc.ID+"/resolve", client, map[string]any{}); status != http.StatusCreated {
		t.Fatalf("resolve = %d %s", status, raw)
	}
	expect("resolve", 3, "Resolved: Checkout errors")
}
