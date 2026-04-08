//go:build integration
// +build integration

package api_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/alert"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

// integrationTestServer provides a test server with all dependencies
type integrationTestServer struct {
	storage *storage.CobaltDB
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
	_ = ts

	// This is a placeholder for the full integration test
	// In a real implementation, we would:
	// 1. Create a soul via HTTP POST
	// 2. Verify the soul was created
	// 3. Get the soul via HTTP GET
	// 4. Update the soul via HTTP PUT
	// 5. Delete the soul via HTTP DELETE

	t.Skip("Integration tests require running server - skipped in short mode")
}

// TestIntegration_AlertFlow tests the full alert flow
func TestIntegration_AlertFlow(t *testing.T) {
	ts := setupIntegrationTest(t)
	_ = ts

	// This is a placeholder for the full integration test
	// In a real implementation, we would:
	// 1. Create an alert channel
	// 2. Create an alert rule
	// 3. Trigger a judgment that matches the rule
	// 4. Verify the alert was dispatched

	t.Skip("Integration tests require running server - skipped in short mode")
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
	retrieved, err := ts.storage.GetAlertChannel(channel.ID)
	if err != nil {
		t.Fatalf("Failed to get channel: %v", err)
	}

	if retrieved.Name != channel.Name {
		t.Errorf("Channel name mismatch: %s != %s", retrieved.Name, channel.Name)
	}

	// List channels
	channels, err := ts.storage.ListAlertChannels()
	if err != nil {
		t.Fatalf("Failed to list channels: %v", err)
	}

	if len(channels) != 1 {
		t.Errorf("Expected 1 channel, got %d", len(channels))
	}

	// Delete channel
	if err := ts.storage.DeleteAlertChannel(channel.ID); err != nil {
		t.Fatalf("Failed to delete channel: %v", err)
	}

	// Verify deletion
	_, err = ts.storage.GetAlertChannel(channel.ID)
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
	// This test would start a real HTTP server and test endpoints
	// Skipping for now as it requires more setup
	t.Skip("HTTP integration tests require full server setup")
}

// Helper types for integration tests

type alertStorageAdapter struct {
	store *storage.CobaltDB
}

func (a *alertStorageAdapter) SaveChannel(ch *core.AlertChannel) error {
	return a.store.SaveAlertChannel(ch)
}

func (a *alertStorageAdapter) GetChannel(id string) (*core.AlertChannel, error) {
	return a.store.GetAlertChannel(id)
}

func (a *alertStorageAdapter) ListChannels() ([]*core.AlertChannel, error) {
	return a.store.ListAlertChannels()
}

func (a *alertStorageAdapter) DeleteChannel(id string) error {
	return a.store.DeleteAlertChannel(id)
}

func (a *alertStorageAdapter) SaveRule(rule *core.AlertRule) error {
	return a.store.SaveAlertRule(rule)
}

func (a *alertStorageAdapter) GetRule(id string) (*core.AlertRule, error) {
	return a.store.GetAlertRule(id)
}

func (a *alertStorageAdapter) ListRules() ([]*core.AlertRule, error) {
	return a.store.ListAlertRules()
}

func (a *alertStorageAdapter) DeleteRule(id string) error {
	return a.store.DeleteAlertRule(id)
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

func (a *restStorageAdapter) GetChannelNoCtx(id string) (*core.AlertChannel, error) {
	return a.store.GetChannelNoCtx(id)
}

func (a *restStorageAdapter) ListChannelsNoCtx(workspace string) ([]*core.AlertChannel, error) {
	return a.store.ListChannelsNoCtx(workspace)
}

func (a *restStorageAdapter) SaveChannelNoCtx(ch *core.AlertChannel) error {
	return a.store.SaveChannelNoCtx(ch)
}

func (a *restStorageAdapter) DeleteChannelNoCtx(id string) error {
	return a.store.DeleteChannelNoCtx(id)
}

func (a *restStorageAdapter) GetRuleNoCtx(id string) (*core.AlertRule, error) {
	return a.store.GetRuleNoCtx(id)
}

func (a *restStorageAdapter) ListRulesNoCtx(workspace string) ([]*core.AlertRule, error) {
	return a.store.ListRulesNoCtx(workspace)
}

func (a *restStorageAdapter) SaveRuleNoCtx(rule *core.AlertRule) error {
	return a.store.SaveRuleNoCtx(rule)
}

func (a *restStorageAdapter) DeleteRuleNoCtx(id string) error {
	return a.store.DeleteRuleNoCtx(id)
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
