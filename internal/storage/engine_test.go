package storage

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"log/slog"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))
}

func newTestDB(t *testing.T) *CobaltDB {
	dir := t.TempDir()
	cfg := core.StorageConfig{Path: dir}
	db, err := NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("failed to create test DB: %v", err)
	}
	return db
}

func TestCobaltDB_SaveAndGetSoul(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	ctx := context.Background()
	soul := &core.Soul{
		ID:          "test-soul-1",
		WorkspaceID: "default",
		Name:        "Test Soul",
		Type:        core.CheckHTTP,
		Target:      "https://example.com",
		Weight:      core.Duration{Duration: 60 * time.Second},
		Timeout:     core.Duration{Duration: 10 * time.Second},
		Enabled:     true,
		Tags:        []string{"test", "production"},
		HTTP: &core.HTTPConfig{
			Method:      "GET",
			ValidStatus: []int{200, 204},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Save soul
	if err := db.SaveSoul(ctx, soul); err != nil {
		t.Fatalf("SaveSoul failed: %v", err)
	}

	// Get soul
	retrieved, err := db.GetSoul(ctx, "default", "test-soul-1")
	if err != nil {
		t.Fatalf("GetSoul failed: %v", err)
	}

	if retrieved.ID != soul.ID {
		t.Errorf("expected ID %s, got %s", soul.ID, retrieved.ID)
	}
	if retrieved.Name != soul.Name {
		t.Errorf("expected name %s, got %s", soul.Name, retrieved.Name)
	}
	if retrieved.Type != soul.Type {
		t.Errorf("expected type %s, got %s", soul.Type, retrieved.Type)
	}
}

func TestCobaltDB_ListSouls(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// Create multiple souls
	souls := []*core.Soul{
		{ID: "soul-1", Name: "Soul 1", Type: core.CheckHTTP, Target: "https://api1.com", WorkspaceID: "default"},
		{ID: "soul-2", Name: "Soul 2", Type: core.CheckTCP, Target: "tcp://api2.com:443", WorkspaceID: "default"},
		{ID: "soul-3", Name: "Soul 3", Type: core.CheckDNS, Target: "8.8.8.8", WorkspaceID: "default"},
	}

	for _, soul := range souls {
		if err := db.SaveSoul(ctx, soul); err != nil {
			t.Fatalf("SaveSoul failed: %v", err)
		}
	}

	// List souls
	retrieved, err := db.ListSouls(ctx, "default", 0, 100)
	if err != nil {
		t.Fatalf("ListSouls failed: %v", err)
	}

	if len(retrieved) != 3 {
		t.Errorf("expected 3 souls, got %d", len(retrieved))
	}
}

func TestCobaltDB_ListSouls_Pagination(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// Create 10 souls
	for i := 0; i < 10; i++ {
		soul := &core.Soul{
			ID:          string(rune('a' + i)),
			Name:        string(rune('A' + i)),
			Type:        core.CheckHTTP,
			Target:      "https://example.com",
			WorkspaceID: "default",
		}
		if err := db.SaveSoul(ctx, soul); err != nil {
			t.Fatalf("SaveSoul failed: %v", err)
		}
	}

	// Test pagination
	firstPage, err := db.ListSouls(ctx, "default", 0, 5)
	if err != nil {
		t.Fatalf("ListSouls failed: %v", err)
	}
	if len(firstPage) != 5 {
		t.Errorf("expected 5 souls on first page, got %d", len(firstPage))
	}

	secondPage, err := db.ListSouls(ctx, "default", 5, 5)
	if err != nil {
		t.Fatalf("ListSouls failed: %v", err)
	}
	if len(secondPage) != 5 {
		t.Errorf("expected 5 souls on second page, got %d", len(secondPage))
	}
}

func TestCobaltDB_DeleteSoul(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// Create and save a soul
	soul := &core.Soul{
		ID:          "to-delete",
		Name:        "To Delete",
		Type:        core.CheckHTTP,
		Target:      "https://example.com",
		WorkspaceID: "default",
	}

	if err := db.SaveSoul(ctx, soul); err != nil {
		t.Fatalf("SaveSoul failed: %v", err)
	}

	// Delete soul
	if err := db.DeleteSoul(ctx, "default", "to-delete"); err != nil {
		t.Fatalf("DeleteSoul failed: %v", err)
	}

	// Verify soul is deleted
	_, err := db.GetSoul(ctx, "default", "to-delete")
	if err == nil {
		t.Error("expected error getting deleted soul")
	}
}

func TestCobaltDB_SaveJudgment(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	ctx := context.Background()

	judgment := &core.Judgment{
		ID:        "judgment-1",
		SoulID:    "test-soul",
		JackalID:  "jackal-1",
		Region:    "default",
		Timestamp: time.Now().UTC(),
		Duration:  150 * time.Millisecond,
		Status:    core.SoulAlive,
		StatusCode: 200,
		Message:   "OK",
	}

	if err := db.SaveJudgment(ctx, judgment); err != nil {
		t.Fatalf("SaveJudgment failed: %v", err)
	}
}

func TestCobaltDB_GetStats(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// Create souls
	souls := []*core.Soul{
		{ID: "soul-1", Name: "Soul 1", Type: core.CheckHTTP, Target: "https://api1.com", WorkspaceID: "default"},
		{ID: "soul-2", Name: "Soul 2", Type: core.CheckTCP, Target: "tcp://api2.com:443", WorkspaceID: "default"},
	}

	for _, soul := range souls {
		if err := db.SaveSoul(ctx, soul); err != nil {
			t.Fatalf("SaveSoul failed: %v", err)
		}
	}

	// Create judgments
	now := time.Now()
	judgments := []*core.Judgment{
		{SoulID: "soul-1", Timestamp: now.Add(-1 * time.Hour), Status: core.SoulAlive},
		{SoulID: "soul-1", Timestamp: now.Add(-30 * time.Minute), Status: core.SoulAlive},
		{SoulID: "soul-2", Timestamp: now.Add(-1 * time.Hour), Status: core.SoulDead},
	}

	for _, j := range judgments {
		j.ID = core.GenerateID()
		if err := db.SaveJudgment(ctx, j); err != nil {
			t.Fatalf("SaveJudgment failed: %v", err)
		}
	}

	// Get stats
	stats, err := db.GetStats(ctx, "default", now.Add(-2*time.Hour), now)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if stats.TotalSouls != 2 {
		t.Errorf("expected 2 total souls, got %d", stats.TotalSouls)
	}
}

func TestCobaltDB_Channel(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	ctx := context.Background()

	channel := &core.ChannelConfig{
		Name: "test-channel",
		Type: "webhook",
		Webhook: &core.WebhookConfig{
			URL:    "https://hooks.example.com/alert",
			Method: "POST",
		},
	}

	// Save channel
	if err := db.SaveChannel(ctx, channel); err != nil {
		t.Fatalf("SaveChannel failed: %v", err)
	}

	// Get channel
	retrieved, err := db.GetChannel(ctx, "test-channel")
	if err != nil {
		t.Fatalf("GetChannel failed: %v", err)
	}

	if retrieved.Name != channel.Name {
		t.Errorf("expected name %s, got %s", channel.Name, retrieved.Name)
	}
	if retrieved.Type != channel.Type {
		t.Errorf("expected type %s, got %s", channel.Type, retrieved.Type)
	}
}

func TestCobaltDB_Rule(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	ctx := context.Background()

	rule := &core.AlertRule{
		ID:      "rule-1",
		Name:    "Test Rule",
		Enabled: true,
		Scope: core.RuleScope{
			Type: "all",
		},
		Conditions: []core.AlertCondition{
			{
				Type: "status_change",
				From: "alive",
				To:   "dead",
			},
		},
		Channels: []string{"test-channel"},
	}

	// Save rule
	if err := db.SaveRule(ctx, rule); err != nil {
		t.Fatalf("SaveRule failed: %v", err)
	}

	// Get rule
	retrieved, err := db.GetRule(ctx, "rule-1")
	if err != nil {
		t.Fatalf("GetRule failed: %v", err)
	}

	if retrieved.Name != rule.Name {
		t.Errorf("expected name %s, got %s", rule.Name, retrieved.Name)
	}
	if !retrieved.Enabled {
		t.Error("expected rule to be enabled")
	}
}

func TestCobaltDB_Workspace(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	ctx := context.Background()

	ws := &core.Workspace{
		ID:          "ws-1",
		Name:        "Test Workspace",
		Slug:        "test-ws",
		Description: "A test workspace",
		OwnerID:     "user-1",
		Status:      core.WorkspaceActive,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// Save workspace
	if err := db.SaveWorkspace(ctx, ws); err != nil {
		t.Fatalf("SaveWorkspace failed: %v", err)
	}

	// Get workspace
	retrieved, err := db.GetWorkspace(ctx, "ws-1")
	if err != nil {
		t.Fatalf("GetWorkspace failed: %v", err)
	}

	if retrieved.Name != ws.Name {
		t.Errorf("expected name %s, got %s", ws.Name, retrieved.Name)
	}
	if retrieved.Slug != ws.Slug {
		t.Errorf("expected slug %s, got %s", ws.Slug, retrieved.Slug)
	}

	// List workspaces
	workspaces, err := db.ListWorkspaces(ctx)
	if err != nil {
		t.Fatalf("ListWorkspaces failed: %v", err)
	}

	if len(workspaces) != 1 {
		t.Errorf("expected 1 workspace, got %d", len(workspaces))
	}
}
