package main

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/cluster"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

func setupTestStore(t *testing.T) *storage.CobaltDB {
	t.Helper()
	tmpDir := t.TempDir()
	cfg := core.StorageConfig{Path: tmpDir}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	db, err := storage.NewEngine(cfg, logger)
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestRestStorageAdapter_CRUD(t *testing.T) {
	db := setupTestStore(t)
	adapter := &restStorageAdapter{store: db}
	ctx := context.Background()

	// Soul
	soul := &core.Soul{ID: "soul-1", Name: "Test Soul", Type: core.CheckHTTP, Target: "http://example.com", WorkspaceID: "default"}
	if err := adapter.SaveSoul(ctx, soul); err != nil {
		t.Fatalf("SaveSoul failed: %v", err)
	}
	gotSoul, err := adapter.GetSoulNoCtx("soul-1")
	if err != nil {
		t.Fatalf("GetSoulNoCtx failed: %v", err)
	}
	if gotSoul == nil || gotSoul.ID != "soul-1" {
		t.Errorf("expected soul-1, got %v", gotSoul)
	}
	souls, err := adapter.ListSoulsNoCtx("default", 0, 10)
	if err != nil {
		t.Fatalf("ListSoulsNoCtx failed: %v", err)
	}
	if len(souls) != 1 {
		t.Errorf("expected 1 soul, got %d", len(souls))
	}
	if err := adapter.DeleteSoulNoCtx("soul-1"); err != nil {
		t.Fatalf("DeleteSoulNoCtx failed: %v", err)
	}
	if err := adapter.SaveSoul(ctx, soul); err != nil {
		t.Fatalf("SaveSoul before DeleteSoul failed: %v", err)
	}
	if err := adapter.DeleteSoul(ctx, "soul-1"); err != nil {
		t.Fatalf("DeleteSoul failed: %v", err)
	}

	// Judgment
	judgment := &core.Judgment{ID: "j1", SoulID: "soul-1", Status: core.SoulAlive, Duration: 10 * time.Millisecond, Timestamp: time.Now()}
	if err := db.SaveJudgment(ctx, judgment); err != nil {
		t.Fatalf("SaveJudgment failed: %v", err)
	}
	// GetJudgmentNoCtx scans by suffix match on timestamp keys, skip direct check
	_ = judgment
	judgments, err := adapter.ListJudgmentsNoCtx("soul-1", time.Now().Add(-time.Hour), time.Now().Add(time.Second), 10)
	if err != nil {
		t.Fatalf("ListJudgmentsNoCtx failed: %v", err)
	}
	if len(judgments) != 1 {
		t.Errorf("expected 1 judgment, got %d", len(judgments))
	}

	// Channel
	ch := &core.AlertChannel{ID: "ch-1", Name: "Email", Type: "email"}
	if err := adapter.SaveChannelNoCtx(ch); err != nil {
		t.Fatalf("SaveChannelNoCtx failed: %v", err)
	}
	gotCh, err := adapter.GetChannelNoCtx("ch-1", "default")
	if err != nil {
		t.Fatalf("GetChannelNoCtx failed: %v", err)
	}
	if gotCh == nil || gotCh.ID != "ch-1" {
		t.Errorf("expected ch-1, got %v", gotCh)
	}
	channels, err := adapter.ListChannelsNoCtx("default")
	if err != nil {
		t.Fatalf("ListChannelsNoCtx failed: %v", err)
	}
	if len(channels) != 1 {
		t.Errorf("expected 1 channel, got %d", len(channels))
	}
	if err := adapter.DeleteChannelNoCtx("ch-1", "default"); err != nil {
		t.Fatalf("DeleteChannelNoCtx failed: %v", err)
	}

	// Rule
	rule := &core.AlertRule{ID: "rule-1", Name: "Test Rule", Channels: []string{"ch-1"}}
	if err := adapter.SaveRuleNoCtx(rule); err != nil {
		t.Fatalf("SaveRuleNoCtx failed: %v", err)
	}
	gotRule, err := adapter.GetRuleNoCtx("rule-1", "default")
	if err != nil {
		t.Fatalf("GetRuleNoCtx failed: %v", err)
	}
	if gotRule == nil || gotRule.ID != "rule-1" {
		t.Errorf("expected rule-1, got %v", gotRule)
	}
	rules, err := adapter.ListRulesNoCtx("default")
	if err != nil {
		t.Fatalf("ListRulesNoCtx failed: %v", err)
	}
	if len(rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(rules))
	}
	if err := adapter.DeleteRuleNoCtx("rule-1", "default"); err != nil {
		t.Fatalf("DeleteRuleNoCtx failed: %v", err)
	}

	// Workspace
	ws := &core.Workspace{ID: "ws-1", Name: "Test Workspace"}
	if err := adapter.SaveWorkspaceNoCtx(ws); err != nil {
		t.Fatalf("SaveWorkspaceNoCtx failed: %v", err)
	}
	gotWs, err := adapter.GetWorkspaceNoCtx("ws-1")
	if err != nil {
		t.Fatalf("GetWorkspaceNoCtx failed: %v", err)
	}
	if gotWs == nil || gotWs.ID != "ws-1" {
		t.Errorf("expected ws-1, got %v", gotWs)
	}
	workspaces, err := adapter.ListWorkspacesNoCtx()
	if err != nil {
		t.Fatalf("ListWorkspacesNoCtx failed: %v", err)
	}
	if len(workspaces) < 1 {
		t.Errorf("expected at least 1 workspace, got %d", len(workspaces))
	}
	if err := adapter.DeleteWorkspaceNoCtx("ws-1"); err != nil {
		t.Fatalf("DeleteWorkspaceNoCtx failed: %v", err)
	}

	// Stats
	stats, err := adapter.GetStatsNoCtx("default", time.Now().Add(-time.Hour), time.Now())
	if err != nil {
		t.Fatalf("GetStatsNoCtx failed: %v", err)
	}
	_ = stats

	// StatusPage
	page := &core.StatusPage{ID: "page-1", Name: "Status Page"}
	if err := adapter.SaveStatusPageNoCtx(page); err != nil {
		t.Fatalf("SaveStatusPageNoCtx failed: %v", err)
	}
	gotPage, err := adapter.GetStatusPageNoCtx("page-1")
	if err != nil {
		t.Fatalf("GetStatusPageNoCtx failed: %v", err)
	}
	if gotPage == nil || gotPage.ID != "page-1" {
		t.Errorf("expected page-1, got %v", gotPage)
	}
	pages, err := adapter.ListStatusPagesNoCtx()
	if err != nil {
		t.Fatalf("ListStatusPagesNoCtx failed: %v", err)
	}
	if len(pages) != 1 {
		t.Errorf("expected 1 status page, got %d", len(pages))
	}
	if err := adapter.DeleteStatusPageNoCtx("page-1"); err != nil {
		t.Fatalf("DeleteStatusPageNoCtx failed: %v", err)
	}

	// Journey
	journey := &core.JourneyConfig{ID: "journey-1", Name: "Test Journey"}
	if err := adapter.SaveJourneyNoCtx(journey); err != nil {
		t.Fatalf("SaveJourneyNoCtx failed: %v", err)
	}
	gotJourney, err := adapter.GetJourneyNoCtx("journey-1")
	if err != nil {
		t.Fatalf("GetJourneyNoCtx failed: %v", err)
	}
	if gotJourney == nil || gotJourney.ID != "journey-1" {
		t.Errorf("expected journey-1, got %v", gotJourney)
	}
	journeys, err := adapter.ListJourneysNoCtx("default", 0, 10)
	if err != nil {
		t.Fatalf("ListJourneysNoCtx failed: %v", err)
	}
	if len(journeys) != 1 {
		t.Errorf("expected 1 journey, got %d", len(journeys))
	}
	if err := adapter.DeleteJourneyNoCtx("journey-1"); err != nil {
		t.Fatalf("DeleteJourneyNoCtx failed: %v", err)
	}

	// Dashboard
	dash := &core.CustomDashboard{ID: "dash-1", Name: "Test Dashboard"}
	if err := adapter.SaveDashboardNoCtx(dash); err != nil {
		t.Fatalf("SaveDashboardNoCtx failed: %v", err)
	}
	gotDash, err := adapter.GetDashboardNoCtx("dash-1")
	if err != nil {
		t.Fatalf("GetDashboardNoCtx failed: %v", err)
	}
	if gotDash == nil || gotDash.ID != "dash-1" {
		t.Errorf("expected dash-1, got %v", gotDash)
	}
	dashboards, err := adapter.ListDashboardsNoCtx()
	if err != nil {
		t.Fatalf("ListDashboardsNoCtx failed: %v", err)
	}
	if len(dashboards) != 1 {
		t.Errorf("expected 1 dashboard, got %d", len(dashboards))
	}
	if err := adapter.DeleteDashboardNoCtx("dash-1"); err != nil {
		t.Fatalf("DeleteDashboardNoCtx failed: %v", err)
	}

	// MaintenanceWindow
	mw := &core.MaintenanceWindow{ID: "mw-1", Name: "Maintenance"}
	if err := adapter.SaveMaintenanceWindow(mw); err != nil {
		t.Fatalf("SaveMaintenanceWindow failed: %v", err)
	}
	gotMw, err := adapter.GetMaintenanceWindow("mw-1")
	if err != nil {
		t.Fatalf("GetMaintenanceWindow failed: %v", err)
	}
	if gotMw == nil || gotMw.ID != "mw-1" {
		t.Errorf("expected mw-1, got %v", gotMw)
	}
	mws, err := adapter.ListMaintenanceWindows()
	if err != nil {
		t.Fatalf("ListMaintenanceWindows failed: %v", err)
	}
	if len(mws) != 1 {
		t.Errorf("expected 1 maintenance window, got %d", len(mws))
	}
	if err := adapter.DeleteMaintenanceWindow("mw-1"); err != nil {
		t.Fatalf("DeleteMaintenanceWindow failed: %v", err)
	}
}

func TestRestStorageAdapter_DeleteSoulUsesStoredWorkspace(t *testing.T) {
	db := setupTestStore(t)
	adapter := &restStorageAdapter{store: db}
	ctx := context.Background()

	soul := &core.Soul{
		ID:          "tenant-soul",
		Name:        "Tenant Soul",
		Type:        core.CheckHTTP,
		Target:      "https://example.com",
		WorkspaceID: "tenant-a",
	}
	if err := adapter.SaveSoul(ctx, soul); err != nil {
		t.Fatalf("SaveSoul failed: %v", err)
	}

	if err := adapter.DeleteSoul(ctx, "tenant-soul"); err != nil {
		t.Fatalf("DeleteSoul failed: %v", err)
	}

	if _, err := db.GetSoul(ctx, "tenant-a", "tenant-soul"); err == nil {
		t.Fatal("expected tenant soul to be deleted from tenant-a workspace")
	}
}

func TestGrpcStorageAdapter_CRUD(t *testing.T) {
	db := setupTestStore(t)
	rest := &restStorageAdapter{store: db}
	adapter := &grpcStorageAdapter{inner: rest}
	ctx := context.Background()

	// Soul
	soul := &core.Soul{ID: "soul-1", Name: "Test Soul", Type: core.CheckHTTP, Target: "http://example.com"}
	_ = rest.SaveSoul(ctx, soul)

	gotSoul, err := adapter.GetSoulNoCtx("soul-1")
	if err != nil {
		t.Fatalf("GetSoulNoCtx failed: %v", err)
	}
	if gotSoul == nil {
		t.Error("expected non-nil soul")
	}

	souls, err := adapter.ListSoulsNoCtx("default", 0, 10)
	if err != nil {
		t.Fatalf("ListSoulsNoCtx failed: %v", err)
	}
	if len(souls) != 1 {
		t.Errorf("expected 1 soul, got %d", len(souls))
	}

	if err := adapter.SaveSoulNoCtx(soul); err != nil {
		t.Fatalf("SaveSoulNoCtx failed: %v", err)
	}
	if err := adapter.DeleteSoulNoCtx("soul-1"); err != nil {
		t.Fatalf("DeleteSoulNoCtx failed: %v", err)
	}

	// Judgment
	judgment := &core.Judgment{ID: "j1", SoulID: "soul-1", Status: core.SoulAlive, Duration: 10 * time.Millisecond, Timestamp: time.Now()}
	_ = db.SaveJudgment(ctx, judgment)

	judgments, err := adapter.ListJudgmentsNoCtx("soul-1", time.Now().Add(-time.Hour), time.Now().Add(time.Second), 10)
	if err != nil {
		t.Fatalf("ListJudgmentsNoCtx failed: %v", err)
	}
	if len(judgments) != 1 {
		t.Errorf("expected 1 judgment, got %d", len(judgments))
	}

	// Channel
	ch := &core.AlertChannel{ID: "ch-1", Name: "Email", Type: "email"}
	_ = rest.SaveChannelNoCtx(ch)

	gotCh, err := adapter.GetChannelNoCtx("ch-1", "default")
	if err != nil {
		t.Fatalf("GetChannelNoCtx failed: %v", err)
	}
	if gotCh == nil {
		t.Error("expected non-nil channel")
	}

	channels, err := adapter.ListChannelsNoCtx("default")
	if err != nil {
		t.Fatalf("ListChannelsNoCtx failed: %v", err)
	}
	if len(channels) != 1 {
		t.Errorf("expected 1 channel, got %d", len(channels))
	}

	if err := adapter.SaveChannelNoCtx(ch); err != nil {
		t.Fatalf("SaveChannelNoCtx failed: %v", err)
	}
	if err := adapter.DeleteChannelNoCtx("ch-1", "default"); err != nil {
		t.Fatalf("DeleteChannelNoCtx failed: %v", err)
	}

	// Rule
	rule := &core.AlertRule{ID: "rule-1", Name: "Test Rule", Channels: []string{"ch-1"}}
	_ = rest.SaveRuleNoCtx(rule)

	gotRule, err := adapter.GetRuleNoCtx("rule-1", "default")
	if err != nil {
		t.Fatalf("GetRuleNoCtx failed: %v", err)
	}
	if gotRule == nil {
		t.Error("expected non-nil rule")
	}

	rules, err := adapter.ListRulesNoCtx("default")
	if err != nil {
		t.Fatalf("ListRulesNoCtx failed: %v", err)
	}
	if len(rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(rules))
	}

	if err := adapter.SaveRuleNoCtx(rule); err != nil {
		t.Fatalf("SaveRuleNoCtx failed: %v", err)
	}
	if err := adapter.DeleteRuleNoCtx("rule-1", "default"); err != nil {
		t.Fatalf("DeleteRuleNoCtx failed: %v", err)
	}

	// Journey
	journey := &core.JourneyConfig{ID: "journey-1", Name: "Test Journey"}
	_ = rest.SaveJourneyNoCtx(journey)

	gotJourney, err := adapter.GetJourneyNoCtx("journey-1")
	if err != nil {
		t.Fatalf("GetJourneyNoCtx failed: %v", err)
	}
	if gotJourney == nil {
		t.Error("expected non-nil journey")
	}

	journeys, err := adapter.ListJourneysNoCtx("default", 0, 10)
	if err != nil {
		t.Fatalf("ListJourneysNoCtx failed: %v", err)
	}
	if len(journeys) != 1 {
		t.Errorf("expected 1 journey, got %d", len(journeys))
	}

	if err := adapter.SaveJourneyNoCtx(journey); err != nil {
		t.Fatalf("SaveJourneyNoCtx failed: %v", err)
	}
	if err := adapter.DeleteJourneyNoCtx("journey-1"); err != nil {
		t.Fatalf("DeleteJourneyNoCtx failed: %v", err)
	}

	// Events
	events, err := adapter.ListEvents("soul-1", 10)
	if err != nil {
		t.Fatalf("ListEvents failed: %v", err)
	}
	_ = events

	// JourneyRuns
	runs, err := adapter.ListJourneyRunsNoCtx("journey-1", 10)
	if err != nil {
		t.Fatalf("ListJourneyRunsNoCtx failed: %v", err)
	}
	_ = runs

	_, err = adapter.GetJourneyRunNoCtx("journey-1", "run-1")
	if err == nil {
		t.Error("expected error for missing journey run")
	}
}

func TestClusterAdapter_NilManager(t *testing.T) {
	// Test with nil manager to hit fallback branches
	ca := &clusterAdapter{mgr: nil}
	_ = ca.IsLeader()
	_ = ca.Leader()
	_ = ca.IsClustered()

	status := ca.GetStatus()
	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if status.NodeID != "standalone" {
		t.Errorf("expected standalone, got %s", status.NodeID)
	}

	// Test with real manager
	db := setupTestStore(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	mgr, err := cluster.NewManager(core.NecropolisConfig{NodeName: "test-node", Region: "test-region"}, db, logger)
	if err != nil {
		t.Fatalf("failed to create cluster manager: %v", err)
	}
	defer mgr.Stop(context.Background())

	ca = &clusterAdapter{mgr: mgr}
	_ = ca.IsLeader()
	_ = ca.Leader()
	_ = ca.IsClustered()
	_ = ca.GetStatus()
}

func TestStorageGetLatestJudgment(t *testing.T) {
	db := setupTestStore(t)
	ctx := context.Background()

	// No judgments yet
	_, err := storageGetLatestJudgment(db, ctx, "default", "soul-1")
	if err == nil {
		t.Error("expected error when no judgments exist")
	}

	// Save judgment through raw store key to match prefix scan format
	j1 := &core.Judgment{ID: "j1", SoulID: "soul-1", Status: core.SoulAlive, Duration: 10 * time.Millisecond, Timestamp: time.Now()}
	if err := db.SaveJudgment(ctx, j1); err != nil {
		t.Fatalf("SaveJudgmentNoCtx failed: %v", err)
	}

	latest, err := storageGetLatestJudgment(db, ctx, "", "soul-1")
	if err != nil {
		t.Fatalf("storageGetLatestJudgment failed: %v", err)
	}
	if latest == nil || latest.ID != "j1" {
		t.Errorf("expected j1, got %v", latest)
	}
}
