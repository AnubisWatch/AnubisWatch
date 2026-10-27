package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

func newQuotaTestDB(t *testing.T) *storage.CobaltDB {
	t.Helper()
	db, err := storage.NewEngine(core.StorageConfig{Path: t.TempDir()}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func quotaTestSoul(id, workspace string, interval time.Duration) *core.Soul {
	return &core.Soul{ID: id, WorkspaceID: workspace, Name: id, Type: core.CheckHTTP, Target: "https://example.com",
		Weight: core.Duration{Duration: interval}, Timeout: core.Duration{Duration: 5 * time.Second}}
}

// quotaSurfaces are the write paths every API surface goes through: REST and
// MCP call restStorageAdapter, gRPC calls grpcStorageAdapter (which forwards
// to it), and channels from every surface and the config file reach the alert
// manager's alertStorageAdapter.
type quotaSurface struct {
	name  string
	quota string
	save  func(rest *restStorageAdapter, alerts *alertStorageAdapter, ws, id string) error
}

var quotaSurfaces = []quotaSurface{
	{"rest/mcp souls", core.QuotaMaxSouls, func(r *restStorageAdapter, _ *alertStorageAdapter, ws, id string) error {
		return r.SaveSoul(context.Background(), quotaTestSoul(id, ws, time.Minute))
	}},
	{"grpc souls", core.QuotaMaxSouls, func(r *restStorageAdapter, _ *alertStorageAdapter, ws, id string) error {
		return (&grpcStorageAdapter{inner: r}).SaveSoulNoCtx(quotaTestSoul(id, ws, time.Minute))
	}},
	{"journeys", core.QuotaMaxJourneys, func(r *restStorageAdapter, _ *alertStorageAdapter, ws, id string) error {
		return r.SaveJourneyNoCtx(&core.JourneyConfig{ID: id, WorkspaceID: ws, Name: id, Weight: core.Duration{Duration: time.Minute}})
	}},
	{"alert manager channels", core.QuotaMaxAlertChannels, func(_ *restStorageAdapter, a *alertStorageAdapter, ws, id string) error {
		return a.SaveChannel(&core.AlertChannel{ID: id, WorkspaceID: ws, Name: id, Type: core.ChannelWebHook})
	}},
	{"grpc store channels", core.QuotaMaxAlertChannels, func(r *restStorageAdapter, _ *alertStorageAdapter, ws, id string) error {
		return (&grpcStorageAdapter{inner: r}).SaveChannelNoCtx(&core.AlertChannel{ID: id, WorkspaceID: ws, Name: id, Type: core.ChannelWebHook})
	}},
}

func quotaAdapters(db *storage.CobaltDB, tenants core.TenantsConfig) (*restStorageAdapter, *alertStorageAdapter) {
	guard := newQuotaGuard(db, tenants)
	return &restStorageAdapter{store: db, quota: guard}, &alertStorageAdapter{store: db, quota: guard}
}

func TestQuotaGuardRefusesCreatesPastTheLimit(t *testing.T) {
	limits := core.QuotaConfig{MaxSouls: 2, MaxJourneys: 2, MaxAlertChannels: 2}
	for _, surface := range quotaSurfaces {
		t.Run(surface.name, func(t *testing.T) {
			db := newQuotaTestDB(t)
			rest, alerts := quotaAdapters(db, core.TenantsConfig{Enabled: true, DefaultQuotas: limits})

			for _, id := range []string{"a", "b"} {
				if err := surface.save(rest, alerts, "default", id); err != nil {
					t.Fatalf("create %s within the limit: %v", id, err)
				}
			}
			err := surface.save(rest, alerts, "default", "c")
			qe, ok := core.AsQuotaExceeded(err)
			if !ok {
				t.Fatalf("third create: got %v, want a QuotaExceededError", err)
			}
			if qe.Quota != surface.quota || qe.Limit != 2 || qe.Current != 2 || qe.Workspace != "default" {
				t.Fatalf("quota error = %+v, want %s limit 2 current 2 in default", qe, surface.quota)
			}
			if !strings.Contains(err.Error(), surface.quota) || !strings.Contains(err.Error(), "2") {
				t.Fatalf("error %q does not name the quota and limit", err)
			}
			// Updating an existing resource adds nothing and is never refused.
			if err := surface.save(rest, alerts, "default", "a"); err != nil {
				t.Fatalf("update at the limit refused: %v", err)
			}
			// Quotas are per workspace.
			if err := surface.save(rest, alerts, "tenant-b", "c"); err != nil {
				t.Fatalf("create in another workspace refused: %v", err)
			}
		})
	}
}

func TestQuotaGuardKeepsOverQuotaDataEditable(t *testing.T) {
	db := newQuotaTestDB(t)
	// Three souls written before the limit was lowered to 1.
	open, _ := quotaAdapters(db, core.TenantsConfig{})
	for _, id := range []string{"a", "b", "c"} {
		if err := open.SaveSoul(context.Background(), quotaTestSoul(id, "default", time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	rest, _ := quotaAdapters(db, core.TenantsConfig{Enabled: true, DefaultQuotas: core.QuotaConfig{MaxSouls: 1}})

	edited := quotaTestSoul("b", "default", time.Minute)
	edited.Name = "renamed"
	if err := rest.SaveSoul(context.Background(), edited); err != nil {
		t.Fatalf("editing an existing soul over the quota was refused: %v", err)
	}
	if got, err := rest.GetSoulNoCtx("b"); err != nil || got.Name != "renamed" {
		t.Fatalf("edited soul = %+v, %v", got, err)
	}
	if souls, err := rest.ListSoulsNoCtx("default", 0, 0); err != nil || len(souls) != 3 {
		t.Fatalf("over-quota souls must stay readable: %d, %v", len(souls), err)
	}
	if err := rest.SaveSoul(context.Background(), quotaTestSoul("d", "default", time.Minute)); err == nil {
		t.Fatal("a new soul was accepted while the workspace is over its quota")
	}
	// Deleting one still leaves the workspace at 2 > 1, so creates stay blocked.
	if err := rest.DeleteSoul(context.Background(), "c"); err != nil {
		t.Fatal(err)
	}
	if _, ok := core.AsQuotaExceeded(rest.SaveSoul(context.Background(), quotaTestSoul("d", "default", time.Minute))); !ok {
		t.Fatal("create should stay blocked until the workspace is under its limit")
	}
}

func TestQuotaGuardUnlimited(t *testing.T) {
	cases := []struct {
		name    string
		tenants core.TenantsConfig
	}{
		{"tenants enabled, quotas 0", core.TenantsConfig{Enabled: true}},
		{"tenants disabled, quotas set", core.TenantsConfig{Enabled: false, DefaultQuotas: core.QuotaConfig{MaxSouls: 1, MaxJourneys: 1, MaxAlertChannels: 1, CheckIntervalMin: core.Duration{Duration: time.Hour}}}},
		{"no guard", core.TenantsConfig{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, surface := range quotaSurfaces {
				db := newQuotaTestDB(t)
				rest, alerts := quotaAdapters(db, tc.tenants)
				if tc.name == "no guard" {
					rest.quota, alerts.quota = nil, nil
				}
				for i := 0; i < 5; i++ {
					if err := surface.save(rest, alerts, "default", fmt.Sprintf("r%d", i)); err != nil {
						t.Fatalf("%s create %d refused: %v", surface.name, i, err)
					}
				}
			}
		})
	}
}

func TestQuotaGuardCheckIntervalFloor(t *testing.T) {
	db := newQuotaTestDB(t)
	// A fast soul and journey that predate the floor.
	if err := db.SaveSoul(context.Background(), quotaTestSoul("fast", "default", 10*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveJourney(context.Background(), &core.JourneyConfig{ID: "fast-j", WorkspaceID: "default", Name: "fast-j", Weight: core.Duration{Duration: 10 * time.Second}}); err != nil {
		t.Fatal(err)
	}
	rest, _ := quotaAdapters(db, core.TenantsConfig{Enabled: true, DefaultQuotas: core.QuotaConfig{CheckIntervalMin: core.Duration{Duration: 30 * time.Second}}})

	cases := []struct {
		name    string
		save    func() error
		refused bool
	}{
		{"create soul below floor", func() error {
			return rest.SaveSoul(context.Background(), quotaTestSoul("new", "default", 10*time.Second))
		}, true},
		{"create soul at floor", func() error {
			return rest.SaveSoul(context.Background(), quotaTestSoul("new", "default", 30*time.Second))
		}, false},
		{"edit old soul keeping its interval", func() error {
			s := quotaTestSoul("fast", "default", 10*time.Second)
			s.Name = "renamed"
			return rest.SaveSoul(context.Background(), s)
		}, false},
		{"edit old soul to another fast interval", func() error {
			return rest.SaveSoul(context.Background(), quotaTestSoul("fast", "default", 15*time.Second))
		}, true},
		{"edit old soul above the floor", func() error {
			return rest.SaveSoul(context.Background(), quotaTestSoul("fast", "default", time.Minute))
		}, false},
		{"create journey below floor", func() error {
			return rest.SaveJourneyNoCtx(&core.JourneyConfig{ID: "j", WorkspaceID: "default", Name: "j", Weight: core.Duration{Duration: 5 * time.Second}})
		}, true},
		{"edit old journey keeping its interval", func() error {
			return rest.SaveJourneyNoCtx(&core.JourneyConfig{ID: "fast-j", WorkspaceID: "default", Name: "renamed", Weight: core.Duration{Duration: 10 * time.Second}})
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.save()
			qe, refused := core.AsQuotaExceeded(err)
			if refused != tc.refused {
				t.Fatalf("refused = %v (err %v), want %v", refused, err, tc.refused)
			}
			if !refused && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if refused && (qe.Quota != core.QuotaCheckIntervalMin || qe.Floor != 30*time.Second) {
				t.Fatalf("quota error = %+v", qe)
			}
		})
	}
}

func TestQuotaUsageReport(t *testing.T) {
	db := newQuotaTestDB(t)
	limits := core.QuotaConfig{MaxSouls: 5, MaxJourneys: 3, MaxAlertChannels: 2, RetentionDays: 30, CheckIntervalMin: core.Duration{Duration: 30 * time.Second}, MaxTeamMembers: 9}
	rest, alerts := quotaAdapters(db, core.TenantsConfig{Enabled: true, DefaultQuotas: limits})
	for _, s := range quotaSurfaces {
		if s.name == "grpc souls" || s.name == "grpc store channels" {
			continue
		}
		if err := s.save(rest, alerts, "default", "x"); err != nil {
			t.Fatal(err)
		}
	}
	u, err := rest.QuotaUsage("")
	if err != nil {
		t.Fatal(err)
	}
	want := core.WorkspaceQuotaUsage{Workspace: "default", Enforced: true,
		Souls: core.QuotaCounter{Used: 1, Limit: 5}, Journeys: core.QuotaCounter{Used: 1, Limit: 3}, AlertChannels: core.QuotaCounter{Used: 1, Limit: 2},
		TeamMembers:      core.QuotaCounter{Limit: 9},
		CheckIntervalMin: core.Duration{Duration: 30 * time.Second}, RetentionDays: 30}
	if *u != want {
		t.Fatalf("usage = %+v, want %+v", *u, want)
	}

	off, _ := quotaAdapters(db, core.TenantsConfig{DefaultQuotas: limits})
	u, err = off.QuotaUsage("default")
	if err != nil {
		t.Fatal(err)
	}
	if u.Enforced || u.Souls.Limit != 0 || u.Souls.Used != 1 || u.RetentionDays != 0 {
		t.Fatalf("tenants disabled must report usage with no limits: %+v", *u)
	}
}

func TestApplyQuotaRetentionOnlyWithTenants(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		cfg := &core.Config{Tenants: core.TenantsConfig{Enabled: enabled, DefaultQuotas: core.QuotaConfig{RetentionDays: 7}}}
		cfg.Storage.TimeSeries.Retention = core.RetentionConfig{Raw: core.Duration{Duration: 30 * 24 * time.Hour}, Day: "unlimited"}
		applyQuotaRetention(cfg, nil)
		got := cfg.Storage.TimeSeries.Retention
		if enabled && (got.Raw.Duration != 7*24*time.Hour || got.Day != (7*24*time.Hour).String()) {
			t.Fatalf("enabled: retention not capped: %+v", got)
		}
		if !enabled && (got.Raw.Duration != 30*24*time.Hour || got.Day != "unlimited") {
			t.Fatalf("disabled: retention changed: %+v", got)
		}
	}
}

// TestQuotaEnforcedOverHTTPAndMCP drives the real server: REST and the MCP
// endpoint both hit max_souls through the shared storage adapter, an existing
// soul stays editable at the limit, and GET /api/v1/quota reports usage.
func TestQuotaEnforcedOverHTTPAndMCP(t *testing.T) {
	const email, password = "admin@example.com", "TestPass1234!"
	dataDir := filepath.Join(t.TempDir(), "data")
	// ANUBIS_DATA_DIR (set for the whole package by TestMain) overrides
	// storage.path; without this the souls of an earlier run (-count>1) stay in
	// the shared directory and the "within quota" creates fail.
	t.Setenv("ANUBIS_DATA_DIR", dataDir)
	port := freeLoopbackPort(t)
	config := fmt.Sprintf(`{
  "storage": {"path": %q},
  "server": {"host": "127.0.0.1", "port": %d, "grpc_port": 0},
  "necropolis": {"node_name": "quota-test-node"},
  "tenants": {"enabled": true, "default_quotas": {"max_souls": 2, "max_journeys": 1, "max_alert_channels": 1}},
  "auth": {"type": "local", "local": {"admin_email": %q, "admin_password": %q}}
}`, filepath.ToSlash(dataDir), port, email, password)
	configPath := filepath.Join(filepath.Dir(dataDir), "anubis.json")
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := bootAuthServer(t, configPath, port)
	defer srv.stop(t)
	client, _ := loginAsAdmin(t, srv.baseURL, email, password)

	do := func(method, path, body string) (int, map[string]interface{}) {
		t.Helper()
		req, err := http.NewRequest(method, srv.baseURL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		var out map[string]interface{}
		if err := json.Unmarshal(raw, &out); err != nil && len(raw) > 0 {
			t.Logf("non-object body %d: %s", resp.StatusCode, raw)
		}
		return resp.StatusCode, out
	}
	soulBody := func(name string) string {
		return fmt.Sprintf(`{"name":%q,"type":"http","target":"https://example.com","enabled":false,"weight":"60s","timeout":"5s"}`, name)
	}

	var firstID string
	for i := 0; i < 2; i++ {
		code, out := do(http.MethodPost, "/api/v1/souls", soulBody(fmt.Sprintf("q%d", i)))
		if code != http.StatusCreated {
			t.Fatalf("create %d within the quota: %d %v", i, code, out)
		}
		if i == 0 {
			firstID, _ = out["id"].(string)
		}
	}
	code, out := do(http.MethodPost, "/api/v1/souls", soulBody("q2"))
	if code != http.StatusConflict || out["quota"] != core.QuotaMaxSouls || out["limit"] != float64(2) || out["code"] != "quota_exceeded" {
		t.Fatalf("REST create past the quota = %d %v, want 409 naming max_souls limit 2", code, out)
	}
	if code, out := do(http.MethodPut, "/api/v1/souls/"+firstID, soulBody("renamed")); code != http.StatusOK {
		t.Fatalf("editing an existing soul at the quota = %d %v", code, out)
	}

	mcp := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create_soul","arguments":{"name":"m","type":"http","target":"https://example.com","interval":"60s"}}}`
	code, out = do(http.MethodPost, "/api/v1/mcp", mcp)
	mcpErr, _ := out["error"].(map[string]interface{})
	if code != http.StatusOK || mcpErr == nil || mcpErr["code"] != float64(mcpQuotaCodeForTest) || !strings.Contains(fmt.Sprint(mcpErr["message"]), core.QuotaMaxSouls) {
		t.Fatalf("MCP create_soul past the quota = %d %v, want error %d naming max_souls", code, out, mcpQuotaCodeForTest)
	}

	code, out = do(http.MethodGet, "/api/v1/quota", "")
	souls, _ := out["souls"].(map[string]interface{})
	if code != http.StatusOK || out["enforced"] != true || souls["used"] != float64(2) || souls["limit"] != float64(2) {
		t.Fatalf("GET /api/v1/quota = %d %v", code, out)
	}
}

// mcpQuotaCodeForTest mirrors api.mcpQuotaExceededCode (unexported).
const mcpQuotaCodeForTest = -32002

func TestQuotaGuardAdmitMember(t *testing.T) {
	db := newQuotaTestDB(t)
	cases := []struct {
		name    string
		tenants core.TenantsConfig
		current int
		refused bool
	}{
		{"tenants off", core.TenantsConfig{DefaultQuotas: core.QuotaConfig{MaxTeamMembers: 1}}, 5, false},
		{"unlimited", core.TenantsConfig{Enabled: true}, 500, false},
		{"below limit", core.TenantsConfig{Enabled: true, DefaultQuotas: core.QuotaConfig{MaxTeamMembers: 3}}, 2, false},
		{"at limit", core.TenantsConfig{Enabled: true, DefaultQuotas: core.QuotaConfig{MaxTeamMembers: 3}}, 3, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := newQuotaGuard(db, tc.tenants).admitMember("", tc.current)
			qe, ok := core.AsQuotaExceeded(err)
			if ok != tc.refused {
				t.Fatalf("admitMember = %v, refused want %v", err, tc.refused)
			}
			if ok && (qe.Quota != core.QuotaMaxTeamMembers || qe.Workspace != "default" || qe.Limit != 3) {
				t.Fatalf("quota error = %+v", qe)
			}
		})
	}
}
