package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// quotaStore enforces max_souls/max_journeys the way the server's storage
// adapter does: creates past the limit fail, updates of existing records pass.
type quotaStore struct {
	*mockStorage
	maxSouls, maxJourneys int
	floor                 time.Duration
}

func (q *quotaStore) SaveSoul(ctx context.Context, soul *core.Soul) error {
	existing, ok := q.souls[soul.ID]
	if !ok || existing.Weight != soul.Weight {
		if err := core.CheckQuotaInterval(soul.WorkspaceID, q.floor, soul.Weight.Duration); err != nil {
			return err
		}
	}
	if !ok {
		if err := core.CheckQuotaCount(soul.WorkspaceID, core.QuotaMaxSouls, q.maxSouls, len(q.souls)); err != nil {
			return err
		}
	}
	return q.mockStorage.SaveSoul(ctx, soul)
}

func (q *quotaStore) SaveJourneyNoCtx(j *core.JourneyConfig) error {
	if _, ok := q.journeys[j.ID]; !ok {
		if err := core.CheckQuotaCount(j.WorkspaceID, core.QuotaMaxJourneys, q.maxJourneys, len(q.journeys)); err != nil {
			return err
		}
	}
	return q.mockStorage.SaveJourneyNoCtx(j)
}

func (q *quotaStore) QuotaUsage(workspace string) (*core.WorkspaceQuotaUsage, error) {
	return &core.WorkspaceQuotaUsage{Workspace: workspace, Enforced: true,
		Souls:    core.QuotaCounter{Used: len(q.souls), Limit: q.maxSouls},
		Journeys: core.QuotaCounter{Used: len(q.journeys), Limit: q.maxJourneys}}, nil
}

// quotaAlerts refuses new channels like the alert manager does when its
// storage refuses them (the error arrives wrapped).
type quotaAlerts struct {
	*mockAlertManager
	max int
}

func (q *quotaAlerts) RegisterChannel(ch *core.AlertChannel) error {
	if _, ok := q.channels[ch.ID]; !ok {
		if err := core.CheckQuotaCount(ch.WorkspaceID, core.QuotaMaxAlertChannels, q.max, len(q.channels)); err != nil {
			return fmt.Errorf("save channel: %w", err)
		}
	}
	return q.mockAlertManager.RegisterChannel(ch)
}

func quotaCtx(method, path, body string) (*Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return &Context{Request: req, Response: rec, Workspace: "default", Params: map[string]string{}}, rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not one JSON object: %q", rec.Body.String())
	}
	return out
}

func TestRESTQuotaExceededIs409(t *testing.T) {
	store := &quotaStore{mockStorage: newMockStorage(), maxSouls: 1, maxJourneys: 1, floor: 30 * time.Second}
	store.souls["old"] = &core.Soul{ID: "old", WorkspaceID: "default", Name: "old", Type: core.CheckHTTP, Target: "https://example.com", Weight: core.Duration{Duration: 10 * time.Second}}
	store.journeys["j0"] = &core.JourneyConfig{ID: "j0", WorkspaceID: "default", Name: "j0"}
	server := newTestServerWithStorage(store)
	alerts := &quotaAlerts{mockAlertManager: &mockAlertManager{channels: map[string]*core.AlertChannel{"c0": {ID: "c0"}}}, max: 1}
	server.alert = alerts

	soul := `{"name":"new","type":"http","target":"https://example.com","weight":"60s","enabled":false}`
	journey := `{"name":"j1","steps":[{"name":"s","type":"http","target":"https://example.com"}]}`
	channel := `{"name":"c1","type":"webhook","config":{"url":"https://hooks.example.com"}}`

	cases := []struct {
		name      string
		method    string
		path      string
		params    map[string]string
		body      string
		handler   func(*Context) error
		wantCode  int
		wantQuota string
		wantLimit interface{}
	}{
		{"create soul past max_souls", "POST", "/api/v1/souls", nil, soul, server.handleCreateSoul, http.StatusConflict, core.QuotaMaxSouls, float64(1)},
		{"create journey past max_journeys", "POST", "/api/v1/journeys", nil, journey, server.handleCreateJourney, http.StatusConflict, core.QuotaMaxJourneys, float64(1)},
		{"create channel past max_alert_channels", "POST", "/api/v1/channels", nil, channel, server.handleCreateChannel, http.StatusConflict, core.QuotaMaxAlertChannels, float64(1)},
		{"edit over-quota soul keeping its interval", "PUT", "/api/v1/souls/old", map[string]string{"id": "old"},
			`{"name":"renamed","type":"http","target":"https://example.com","weight":"10s"}`, server.handleUpdateSoul, http.StatusOK, "", nil},
		{"edit soul to a new interval below the floor", "PUT", "/api/v1/souls/old", map[string]string{"id": "old"},
			`{"name":"renamed","type":"http","target":"https://example.com","weight":"20s"}`, server.handleUpdateSoul, http.StatusConflict, core.QuotaCheckIntervalMin, "30s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, rec := quotaCtx(tc.method, tc.path, tc.body)
			if tc.params != nil {
				ctx.Params = tc.params
			}
			if err := tc.handler(ctx); err != nil {
				t.Fatalf("handler: %v", err)
			}
			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.wantCode, rec.Body.String())
			}
			out := decodeBody(t, rec)
			if tc.wantQuota == "" {
				return
			}
			if out["quota"] != tc.wantQuota || out["limit"] != tc.wantLimit || out["code"] != "quota_exceeded" {
				t.Fatalf("body = %v, want quota %s limit %v", out, tc.wantQuota, tc.wantLimit)
			}
			if msg, _ := out["error"].(string); !strings.Contains(msg, tc.wantQuota) {
				t.Fatalf("error message %q does not name the quota", msg)
			}
		})
	}
}

func TestRESTGetQuota(t *testing.T) {
	cases := []struct {
		name         string
		store        Storage
		wantEnforced bool
		wantLimit    float64
	}{
		{"enforcing store", &quotaStore{mockStorage: newMockStorage(), maxSouls: 7}, true, 7},
		{"store without quotas", newMockStorage(), false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := newTestServerWithStorage(tc.store)
			ctx, rec := quotaCtx("GET", "/api/v1/quota", "")
			if err := server.handleGetQuota(ctx); err != nil {
				t.Fatal(err)
			}
			out := decodeBody(t, rec)
			souls, _ := out["souls"].(map[string]interface{})
			if rec.Code != http.StatusOK || out["enforced"] != tc.wantEnforced || souls["limit"] != tc.wantLimit || out["workspace"] != "default" {
				t.Fatalf("GET /api/v1/quota = %d %v", rec.Code, out)
			}
		})
	}
}

func TestMCPQuotaExceededError(t *testing.T) {
	store := &quotaStore{mockStorage: newMockStorage(), maxSouls: 1}
	store.souls["old"] = &core.Soul{ID: "old", WorkspaceID: "default"}
	server := NewMCPServer(store, &mockProbeEngine{}, &mockAlertManager{}, newTestLogger())
	ctx := core.ContextWithUserRole(core.ContextWithWorkspaceID(context.Background(), "default"), string(core.RoleAdmin))
	params, _ := json.Marshal(map[string]interface{}{
		"name":      "create_soul",
		"arguments": map[string]string{"name": "x", "type": "http", "target": "https://example.com", "interval": "60s"},
	})
	resp := server.handleCallTool(ctx, &MCPRequest{ID: 1, Params: params})
	if resp.Error == nil || resp.Error.Code != mcpQuotaExceededCode {
		t.Fatalf("MCP create_soul past the quota = %+v, want code %d", resp.Error, mcpQuotaExceededCode)
	}
	data, _ := resp.Error.Data.(map[string]interface{})
	if data["quota"] != core.QuotaMaxSouls || !strings.Contains(resp.Error.Message, "1 of 1") {
		t.Fatalf("MCP quota error = %+v", resp.Error)
	}

	// Under the limit the same call succeeds.
	store.maxSouls = 0
	if resp := server.handleCallTool(ctx, &MCPRequest{ID: 2, Params: params}); resp.Error != nil {
		t.Fatalf("unlimited create_soul failed: %+v", resp.Error)
	}
}

func TestCreateWorkspaceStampsEnforcedQuotas(t *testing.T) {
	store := &quotaStore{mockStorage: newMockStorage(), maxSouls: 7, maxJourneys: 3}
	server := newTestServerWithStorage(store)
	ctx, rec := quotaCtx("POST", "/api/v1/workspaces", `{"name":"Team","quotas":{"max_souls":9999}}`)
	if err := server.handleCreateWorkspace(ctx); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var ws core.Workspace
	if err := json.Unmarshal(rec.Body.Bytes(), &ws); err != nil {
		t.Fatal(err)
	}
	if ws.Quotas.MaxSouls != 7 || ws.Quotas.MaxJourneys != 3 {
		t.Fatalf("workspace quotas = %+v, want the enforced limits", ws.Quotas)
	}
}
