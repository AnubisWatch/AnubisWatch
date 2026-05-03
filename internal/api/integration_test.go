//go:build integration
// +build integration

package api_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/alert"
	restapi "github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

// integrationTestServer provides a test server with all dependencies
type integrationTestServer struct {
	storage *storage.CobaltDB
	alert   *alert.Manager
	logger  *slog.Logger
	tmpDir  string
}

// setupIntegrationTest creates a test environment with real storage
func setupIntegrationTest(t *testing.T) *integrationTestServer {
	t.Helper()

	// Create temp directory for storage
	tmpDir, err := os.MkdirTemp("", "anubis-integration-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Create storage
	storageCfg := core.StorageConfig{
		Path: filepath.Join(tmpDir, "data"),
	}
	db, err := storage.NewEngine(storageCfg, logger)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create storage: %v", err)
	}

	// Create alert manager
	alertStorage := &alertStorageAdapter{db}
	alertMgr := alert.NewManager(alertStorage, logger)
	if err := alertMgr.Start(); err != nil {
		db.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to start alert manager: %v", err)
	}

	ts := &integrationTestServer{
		storage: db,
		alert:   alertMgr,
		logger:  logger,
		tmpDir:  tmpDir,
	}

	t.Cleanup(func() {
		alertMgr.Stop()
		db.Close()
		os.RemoveAll(tmpDir)
	})

	return ts
}

// TestIntegration_SoulLifecycle tests creating, reading, updating, and deleting souls
func TestIntegration_SoulLifecycle(t *testing.T) {
	ts := setupIntegrationTest(t)
	ctx := context.Background()

	ws := &core.Workspace{ID: "default", Name: "Default"}
	if err := ts.storage.SaveWorkspace(ctx, ws); err != nil {
		t.Fatalf("Failed to save workspace: %v", err)
	}

	soul := &core.Soul{
		ID:          "integration-soul-1",
		Name:        "Integration HTTP Soul",
		Type:        core.CheckHTTP,
		Target:      "https://example.com/health",
		WorkspaceID: "default",
		Enabled:     true,
		Weight:      core.Duration{Duration: time.Minute},
		Timeout:     core.Duration{Duration: 5 * time.Second},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := ts.storage.SaveSoul(ctx, soul); err != nil {
		t.Fatalf("Failed to save soul: %v", err)
	}

	retrieved, err := ts.storage.GetSoul(ctx, "default", soul.ID)
	if err != nil {
		t.Fatalf("Failed to get soul: %v", err)
	}
	if retrieved.Name != soul.Name || retrieved.Target != soul.Target {
		t.Fatalf("Unexpected soul after create: %#v", retrieved)
	}

	retrieved.Enabled = false
	retrieved.Name = "Integration HTTP Soul Disabled"
	retrieved.UpdatedAt = time.Now()
	if err := ts.storage.SaveSoul(ctx, retrieved); err != nil {
		t.Fatalf("Failed to update soul: %v", err)
	}

	listed, err := ts.storage.ListSouls(ctx, "default", 0, 10)
	if err != nil {
		t.Fatalf("Failed to list souls: %v", err)
	}
	if len(listed) != 1 || listed[0].Enabled || listed[0].Name != "Integration HTTP Soul Disabled" {
		t.Fatalf("Unexpected souls after update: %#v", listed)
	}

	if err := ts.storage.DeleteSoul(ctx, "default", soul.ID); err != nil {
		t.Fatalf("Failed to delete soul: %v", err)
	}
	if _, err := ts.storage.GetSoul(ctx, "default", soul.ID); err == nil {
		t.Fatal("Expected error after deleting soul")
	}
}

// TestIntegration_AlertFlow tests the full alert flow
func TestIntegration_AlertFlow(t *testing.T) {
	ts := setupIntegrationTest(t)

	channel := &core.AlertChannel{
		ID:          "integration-channel-1",
		Name:        "Integration Webhook",
		Type:        core.ChannelWebHook,
		Enabled:     true,
		WorkspaceID: "default",
		Config: map[string]interface{}{
			"url": "https://example.com/webhook",
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := ts.alert.RegisterChannel(channel); err != nil {
		t.Fatalf("Failed to register channel: %v", err)
	}

	rule := &core.AlertRule{
		ID:          "integration-rule-1",
		Name:        "Integration Status Rule",
		Enabled:     true,
		WorkspaceID: "default",
		Scope:       core.RuleScope{Type: "all"},
		Conditions: []core.AlertCondition{{
			Type:   "status_for",
			Status: string(core.SoulDead),
		}},
		Channels:    []string{channel.ID},
		Severity:    core.SeverityCritical,
		Cooldown:    core.Duration{Duration: 5 * time.Minute},
		AutoResolve: true,
		CreatedAt:   time.Now(),
	}
	if err := ts.alert.RegisterRule(rule); err != nil {
		t.Fatalf("Failed to register rule: %v", err)
	}

	if _, err := ts.storage.GetAlertChannel(channel.ID, "default"); err != nil {
		t.Fatalf("Failed to retrieve persisted channel: %v", err)
	}
	if _, err := ts.storage.GetAlertRule(rule.ID, "default"); err != nil {
		t.Fatalf("Failed to retrieve persisted rule: %v", err)
	}

	if got, err := ts.alert.GetChannel(channel.ID); err != nil || got.Name != channel.Name {
		t.Fatalf("Failed to retrieve channel from alert manager: channel=%#v err=%v", got, err)
	}

	if got, err := ts.alert.GetRule(rule.ID); err != nil || got.Name != rule.Name {
		t.Fatalf("Failed to retrieve rule from alert manager: rule=%#v err=%v", got, err)
	}

	if err := ts.alert.DeleteRuleWithWorkspace(rule.ID, "default"); err != nil {
		t.Fatalf("Failed to delete alert rule: %v", err)
	}
	if err := ts.alert.DeleteChannelWithWorkspace(channel.ID, "default"); err != nil {
		t.Fatalf("Failed to delete alert channel: %v", err)
	}
	if _, err := ts.storage.GetAlertRule(rule.ID, "default"); err == nil {
		t.Fatal("Expected error after deleting alert rule")
	}
	if _, err := ts.storage.GetAlertChannel(channel.ID, "default"); err == nil {
		t.Fatal("Expected error after deleting alert channel")
	}
}

// TestIntegration_JudgmentStorage tests judgment storage and retrieval
func TestIntegration_JudgmentStorage(t *testing.T) {
	ts := setupIntegrationTest(t)
	ctx := context.Background()

	// Create a workspace and soul
	ws := &core.Workspace{
		ID:   "test-ws",
		Name: "Test Workspace",
	}
	if err := ts.storage.SaveWorkspace(ctx, ws); err != nil {
		t.Fatalf("Failed to save workspace: %v", err)
	}

	soul := &core.Soul{
		ID:          "test-soul-1",
		Name:        "Test Soul",
		Type:        core.CheckHTTP,
		Target:      "https://example.com",
		WorkspaceID: "test-ws",
		Enabled:     true,
		CreatedAt:   time.Now(),
	}
	if err := ts.storage.SaveSoul(ctx, soul); err != nil {
		t.Fatalf("Failed to save soul: %v", err)
	}

	// Create judgments
	for i := 0; i < 5; i++ {
		judgment := &core.Judgment{
			ID:          core.GenerateID(),
			SoulID:      soul.ID,
			Status:      core.SoulAlive,
			Duration:    time.Duration(100+i*10) * time.Millisecond,
			Timestamp:   time.Now().Add(-time.Duration(i) * time.Hour),
			WorkspaceID: "test-ws",
		}
		if err := ts.storage.SaveJudgment(ctx, judgment); err != nil {
			t.Fatalf("Failed to save judgment: %v", err)
		}
	}

	// Retrieve judgments
	judgments, err := ts.storage.ListJudgments(ctx, soul.ID, time.Now().Add(-24*time.Hour), time.Now(), 10)
	if err != nil {
		t.Fatalf("Failed to list judgments: %v", err)
	}

	if len(judgments) != 5 {
		t.Errorf("Expected 5 judgments, got %d", len(judgments))
	}

	// Verify judgments are in correct order (newest first)
	for i := 0; i < len(judgments)-1; i++ {
		if judgments[i].Timestamp.Before(judgments[i+1].Timestamp) {
			t.Error("Judgments not sorted by timestamp (newest first)")
		}
	}
}

// TestIntegration_ChannelOperations tests alert channel CRUD operations
func TestIntegration_ChannelOperations(t *testing.T) {
	ts := setupIntegrationTest(t)

	channel := &core.AlertChannel{
		ID:      "test-channel-1",
		Name:    "Test Slack Channel",
		Type:    core.ChannelSlack,
		Enabled: true,
		Config: map[string]interface{}{
			"webhook_url": "https://hooks.slack.com/test",
		},
	}

	// Save channel
	if err := ts.storage.SaveAlertChannel(channel); err != nil {
		t.Fatalf("Failed to save channel: %v", err)
	}

	// Retrieve channel
	retrieved, err := ts.storage.GetAlertChannel(channel.ID, "default")
	if err != nil {
		t.Fatalf("Failed to get channel: %v", err)
	}

	if retrieved.Name != channel.Name {
		t.Errorf("Channel name mismatch: %s != %s", retrieved.Name, channel.Name)
	}

	// List channels
	channels, err := ts.storage.ListAlertChannels("default")
	if err != nil {
		t.Fatalf("Failed to list channels: %v", err)
	}

	if len(channels) != 1 {
		t.Errorf("Expected 1 channel, got %d", len(channels))
	}

	// Delete channel
	if err := ts.storage.DeleteAlertChannel(channel.ID, "default"); err != nil {
		t.Fatalf("Failed to delete channel: %v", err)
	}

	// Verify deletion
	_, err = ts.storage.GetAlertChannel(channel.ID, "default")
	if err == nil {
		t.Error("Expected error when getting deleted channel")
	}
}

// TestIntegration_StatusPageOperations tests status page CRUD
func TestIntegration_StatusPageOperations(t *testing.T) {
	ts := setupIntegrationTest(t)

	page := &core.StatusPage{
		ID:          "test-page-1",
		Name:        "Test Status Page",
		Slug:        "test-status",
		Description: "Test status page description",
		Souls:       []string{"soul1", "soul2"},
		Enabled:     true,
		CreatedAt:   time.Now(),
	}

	// Save page
	if err := ts.storage.SaveStatusPage(page); err != nil {
		t.Fatalf("Failed to save status page: %v", err)
	}

	// Retrieve by ID
	retrieved, err := ts.storage.GetStatusPage(page.ID)
	if err != nil {
		t.Fatalf("Failed to get status page: %v", err)
	}

	if retrieved.Name != page.Name {
		t.Errorf("Status page name mismatch: %s != %s", retrieved.Name, page.Name)
	}

	// Retrieve by slug
	bySlug, err := ts.storage.GetStatusPageBySlug(page.Slug)
	if err != nil {
		t.Fatalf("Failed to get status page by slug: %v", err)
	}

	if bySlug.ID != page.ID {
		t.Errorf("Status page ID mismatch: %s != %s", bySlug.ID, page.ID)
	}

	// List pages
	pages, err := ts.storage.ListStatusPages()
	if err != nil {
		t.Fatalf("Failed to list status pages: %v", err)
	}

	if len(pages) != 1 {
		t.Errorf("Expected 1 status page, got %d", len(pages))
	}
}

// TestIntegration_WorkspaceIsolation tests that workspaces are isolated
func TestIntegration_WorkspaceIsolation(t *testing.T) {
	ts := setupIntegrationTest(t)
	ctx := context.Background()

	// Create two workspaces
	ws1 := &core.Workspace{ID: "ws1", Name: "Workspace 1"}
	ws2 := &core.Workspace{ID: "ws2", Name: "Workspace 2"}

	if err := ts.storage.SaveWorkspace(ctx, ws1); err != nil {
		t.Fatalf("Failed to save workspace 1: %v", err)
	}
	if err := ts.storage.SaveWorkspace(ctx, ws2); err != nil {
		t.Fatalf("Failed to save workspace 2: %v", err)
	}

	// Create souls in each workspace
	soul1 := &core.Soul{
		ID:          "soul-ws1",
		Name:        "Soul in WS1",
		WorkspaceID: "ws1",
		Type:        core.CheckHTTP,
		Target:      "https://example1.com",
	}
	soul2 := &core.Soul{
		ID:          "soul-ws2",
		Name:        "Soul in WS2",
		WorkspaceID: "ws2",
		Type:        core.CheckHTTP,
		Target:      "https://example2.com",
	}

	if err := ts.storage.SaveSoul(ctx, soul1); err != nil {
		t.Fatalf("Failed to save soul 1: %v", err)
	}
	if err := ts.storage.SaveSoul(ctx, soul2); err != nil {
		t.Fatalf("Failed to save soul 2: %v", err)
	}

	// List souls in workspace 1
	souls1, err := ts.storage.ListSouls(ctx, "ws1", 0, 100)
	if err != nil {
		t.Fatalf("Failed to list souls in ws1: %v", err)
	}

	if len(souls1) != 1 || souls1[0].ID != "soul-ws1" {
		t.Errorf("Expected 1 soul (soul-ws1) in ws1, got %v", souls1)
	}

	// List souls in workspace 2
	souls2, err := ts.storage.ListSouls(ctx, "ws2", 0, 100)
	if err != nil {
		t.Fatalf("Failed to list souls in ws2: %v", err)
	}

	if len(souls2) != 1 || souls2[0].ID != "soul-ws2" {
		t.Errorf("Expected 1 soul (soul-ws2) in ws2, got %v", souls2)
	}
}

// HTTP integration tests

func TestIntegration_HTTPRoutes(t *testing.T) {
	ts := setupIntegrationTest(t)
	port := reserveIntegrationPort(t)

	server := restapi.NewRESTServer(
		core.ServerConfig{Host: "127.0.0.1", Port: port},
		core.AuthConfig{Enabled: core.BoolPtr(false)},
		&restStorageAdapter{store: ts.storage},
		nil,
		ts.alert,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		ts.logger,
	)

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start()
	}()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Stop(ctx)
	})

	baseURL := "http://127.0.0.1:" + fmt.Sprint(port)
	waitForIntegrationHTTP(t, baseURL+"/health", errCh)

	for _, tc := range []struct {
		path     string
		contains string
	}{
		{path: "/health", contains: "healthy"},
		{path: "/ready", contains: "ready"},
		{path: "/metrics", contains: "anubis_build_info"},
		{path: "/api/openapi.json", contains: `"/metrics"`},
	} {
		resp, err := http.Get(baseURL + tc.path)
		if err != nil {
			t.Fatalf("GET %s failed: %v", tc.path, err)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			t.Fatalf("Failed to read %s response: %v", tc.path, readErr)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s returned %d: %s", tc.path, resp.StatusCode, string(body))
		}
		if !strings.Contains(string(body), tc.contains) {
			t.Fatalf("GET %s response missing %q: %s", tc.path, tc.contains, string(body))
		}
	}
}

func reserveIntegrationPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to reserve port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func waitForIntegrationHTTP(t *testing.T, url string, errCh <-chan error) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case err := <-errCh:
			t.Fatalf("HTTP server exited before readiness: %v", err)
		case <-deadline:
			t.Fatalf("Timed out waiting for %s", url)
		default:
		}

		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Helper types for integration tests

type alertStorageAdapter struct {
	store *storage.CobaltDB
}

func (a *alertStorageAdapter) SaveChannel(ch *core.AlertChannel) error {
	return a.store.SaveAlertChannel(ch)
}

func (a *alertStorageAdapter) GetChannel(id string, workspace string) (*core.AlertChannel, error) {
	return a.store.GetAlertChannel(id, workspace)
}

func (a *alertStorageAdapter) ListChannels(workspace string) ([]*core.AlertChannel, error) {
	return a.store.ListAlertChannels(workspace)
}

func (a *alertStorageAdapter) DeleteChannel(id string, workspace string) error {
	return a.store.DeleteAlertChannel(id, workspace)
}

func (a *alertStorageAdapter) SaveRule(rule *core.AlertRule) error {
	return a.store.SaveAlertRule(rule)
}

func (a *alertStorageAdapter) GetRule(id string, workspace string) (*core.AlertRule, error) {
	return a.store.GetAlertRule(id, workspace)
}

func (a *alertStorageAdapter) ListRules(workspace string) ([]*core.AlertRule, error) {
	return a.store.ListAlertRules(workspace)
}

func (a *alertStorageAdapter) DeleteRule(id string, workspace string) error {
	return a.store.DeleteAlertRule(id, workspace)
}

func (a *alertStorageAdapter) SaveEvent(event *core.AlertEvent) error {
	return a.store.SaveAlertEvent(event)
}

func (a *alertStorageAdapter) ListEvents(soulID string, limit int) ([]*core.AlertEvent, error) {
	return a.store.ListAlertEvents(soulID, limit)
}

func (a *alertStorageAdapter) SaveIncident(incident *core.Incident) error {
	return a.store.SaveIncident(incident)
}

func (a *alertStorageAdapter) GetIncident(id string) (*core.Incident, error) {
	return a.store.GetIncident(id)
}

func (a *alertStorageAdapter) ListActiveIncidents() ([]*core.Incident, error) {
	return a.store.ListActiveIncidents()
}

type restStorageAdapter struct {
	store *storage.CobaltDB
}

func (a *restStorageAdapter) GetSoulNoCtx(id string) (*core.Soul, error) {
	return a.store.GetSoulNoCtx(id)
}

func (a *restStorageAdapter) ListSoulsNoCtx(workspace string, offset, limit int) ([]*core.Soul, error) {
	return a.store.ListSoulsNoCtx(workspace, offset, limit)
}

func (a *restStorageAdapter) SaveSoul(ctx context.Context, soul *core.Soul) error {
	return a.store.SaveSoul(ctx, soul)
}

func (a *restStorageAdapter) DeleteSoul(ctx context.Context, id string) error {
	return a.store.DeleteSoul(ctx, "default", id)
}

func (a *restStorageAdapter) GetJudgmentNoCtx(id string) (*core.Judgment, error) {
	return a.store.GetJudgmentNoCtx(id)
}

func (a *restStorageAdapter) ListJudgmentsNoCtx(soulID string, start, end time.Time, limit int) ([]*core.Judgment, error) {
	return a.store.ListJudgmentsNoCtx(soulID, start, end, limit)
}

func (a *restStorageAdapter) GetChannelNoCtx(id string, workspace string) (*core.AlertChannel, error) {
	return a.store.GetChannelNoCtx(id, workspace)
}

func (a *restStorageAdapter) ListChannelsNoCtx(workspace string) ([]*core.AlertChannel, error) {
	return a.store.ListChannelsNoCtx(workspace)
}

func (a *restStorageAdapter) SaveChannelNoCtx(ch *core.AlertChannel) error {
	return a.store.SaveChannelNoCtx(ch)
}

func (a *restStorageAdapter) DeleteChannelNoCtx(id string, workspace string) error {
	return a.store.DeleteChannelNoCtx(id, workspace)
}

func (a *restStorageAdapter) GetRuleNoCtx(id string, workspace string) (*core.AlertRule, error) {
	return a.store.GetRuleNoCtx(id, workspace)
}

func (a *restStorageAdapter) ListRulesNoCtx(workspace string) ([]*core.AlertRule, error) {
	return a.store.ListRulesNoCtx(workspace)
}

func (a *restStorageAdapter) SaveRuleNoCtx(rule *core.AlertRule) error {
	return a.store.SaveRuleNoCtx(rule)
}

func (a *restStorageAdapter) DeleteRuleNoCtx(id string, workspace string) error {
	return a.store.DeleteRuleNoCtx(id, workspace)
}

func (a *restStorageAdapter) GetWorkspaceNoCtx(id string) (*core.Workspace, error) {
	return a.store.GetWorkspaceNoCtx(id)
}

func (a *restStorageAdapter) ListWorkspacesNoCtx() ([]*core.Workspace, error) {
	return a.store.ListWorkspacesNoCtx()
}

func (a *restStorageAdapter) SaveWorkspaceNoCtx(ws *core.Workspace) error {
	return a.store.SaveWorkspaceNoCtx(ws)
}

func (a *restStorageAdapter) DeleteWorkspaceNoCtx(id string) error {
	return a.store.DeleteWorkspaceNoCtx(id)
}

func (a *restStorageAdapter) GetStatsNoCtx(workspace string, start, end time.Time) (*core.Stats, error) {
	return a.store.GetStatsNoCtx(workspace, start, end)
}

func (a *restStorageAdapter) GetStatusPageNoCtx(id string) (*core.StatusPage, error) {
	return a.store.GetStatusPageNoCtx(id)
}

func (a *restStorageAdapter) ListStatusPagesNoCtx() ([]*core.StatusPage, error) {
	return a.store.ListStatusPagesNoCtx()
}

func (a *restStorageAdapter) SaveStatusPageNoCtx(page *core.StatusPage) error {
	return a.store.SaveStatusPageNoCtx(page)
}

func (a *restStorageAdapter) DeleteStatusPageNoCtx(id string) error {
	return a.store.DeleteStatusPageNoCtx(id)
}

func (a *restStorageAdapter) GetJourneyNoCtx(id string) (*core.JourneyConfig, error) {
	return a.store.GetJourneyNoCtx(id)
}

func (a *restStorageAdapter) ListJourneysNoCtx(workspace string, offset, limit int) ([]*core.JourneyConfig, error) {
	return a.store.ListJourneysNoCtx(workspace, offset, limit)
}

func (a *restStorageAdapter) SaveJourneyNoCtx(journey *core.JourneyConfig) error {
	return a.store.SaveJourneyNoCtx(journey)
}

func (a *restStorageAdapter) DeleteJourneyNoCtx(id string) error {
	return a.store.DeleteJourneyNoCtx(id)
}

func (a *restStorageAdapter) GetDashboardNoCtx(id string) (*core.CustomDashboard, error) {
	return a.store.GetDashboardNoCtx(id)
}

func (a *restStorageAdapter) ListDashboardsNoCtx() ([]*core.CustomDashboard, error) {
	return a.store.ListDashboardsNoCtx()
}

func (a *restStorageAdapter) SaveDashboardNoCtx(dashboard *core.CustomDashboard) error {
	return a.store.SaveDashboardNoCtx(dashboard)
}

func (a *restStorageAdapter) DeleteDashboardNoCtx(id string) error {
	return a.store.DeleteDashboardNoCtx(id)
}

func (a *restStorageAdapter) GetMaintenanceWindow(id string) (*core.MaintenanceWindow, error) {
	return a.store.GetMaintenanceWindow(id)
}

func (a *restStorageAdapter) ListMaintenanceWindows() ([]*core.MaintenanceWindow, error) {
	return a.store.ListMaintenanceWindows()
}

func (a *restStorageAdapter) SaveMaintenanceWindow(w *core.MaintenanceWindow) error {
	return a.store.SaveMaintenanceWindow(w)
}

func (a *restStorageAdapter) DeleteMaintenanceWindow(id string) error {
	return a.store.DeleteMaintenanceWindow(id)
}
