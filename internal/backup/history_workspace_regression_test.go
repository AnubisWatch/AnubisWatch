package backup

// Regression coverage for workspace scoping in `anubis backup --include-history`.
//
// Manager.Create collected judgment history with
//
//	m.storage.ListJudgments(ctx, soul.ID, start, end, 0)
//
// while CobaltDB.ListJudgments resolves its key prefix from the context
// (core.WorkspaceIDFromContext, which falls back to "default"). Manager.Create
// never called core.ContextWithWorkspaceID, so the scan prefix was always
// "default/judgments/<soul>/" and every non-default workspace's history was
// silently dropped from the archive.
//
// These tests drive a REAL storage.CobaltDB rather than a mock on purpose. The
// pre-existing judgment_history_regression_test.go uses a mock whose
// ListJudgments filters on SoulID alone and ignores the workspace, so it agrees
// with the broken code and cannot observe this defect.

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

func historyWSLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// historyWSDB opens a real CobaltDB so prefix resolution matches production.
func historyWSDB(t *testing.T) *storage.CobaltDB {
	t.Helper()
	db, err := storage.NewEngine(core.StorageConfig{Path: t.TempDir()}, historyWSLogger(t))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// historyWSSeed writes a workspace, one soul in it, and one judgment for that soul.
func historyWSSeed(t *testing.T, db *storage.CobaltDB, wsID, soulID string) {
	t.Helper()
	ctx := context.Background()
	if err := db.SaveWorkspace(ctx, &core.Workspace{ID: wsID, Name: wsID, Slug: wsID}); err != nil {
		t.Fatalf("SaveWorkspace(%s): %v", wsID, err)
	}
	soul := &core.Soul{
		ID: soulID, WorkspaceID: wsID, Name: soulID, Type: core.CheckHTTP,
		Target: "http://example.com", Enabled: true,
	}
	if err := db.SaveSoul(ctx, soul); err != nil {
		t.Fatalf("SaveSoul(%s): %v", soulID, err)
	}
	judgment := &core.Judgment{
		ID:          "judgment-" + soulID,
		SoulID:      soulID,
		WorkspaceID: wsID,
		Timestamp:   time.Now().UTC().Add(-time.Hour),
		Status:      core.SoulAlive,
		Message:     "ok",
	}
	if err := db.SaveJudgment(ctx, judgment); err != nil {
		t.Fatalf("SaveJudgment(%s): %v", soulID, err)
	}
}

// historyWSBackup builds a Manager and runs Create with the given options.
func historyWSBackup(t *testing.T, db *storage.CobaltDB, opts Options) *Backup {
	t.Helper()
	m := NewManager(db, t.TempDir(), historyWSLogger(t))
	if err := m.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	backup, _, err := m.Create(context.Background(), opts)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return backup
}

// TestBackupHistoryIncludesNonDefaultWorkspace is the regression's core: a soul
// in a tenant workspace must contribute its history to the archive.
func TestBackupHistoryIncludesNonDefaultWorkspace(t *testing.T) {
	db := historyWSDB(t)
	historyWSSeed(t, db, "acme", "soul-acme")

	backup := historyWSBackup(t, db, Options{IncludeJudgments: true, JudgmentDays: 30})
	if len(backup.Data.Judgments) != 1 {
		t.Fatalf("backup contains %d judgments for workspace \"acme\", want 1; Create must "+
			"scope the context to each soul's workspace or ListJudgments scans the "+
			"\"default\" prefix and drops the history", len(backup.Data.Judgments))
	}
	if got := backup.Data.Judgments[0].WorkspaceID; got != "acme" {
		t.Fatalf("archived judgment belongs to workspace %q, want \"acme\"", got)
	}
}

// TestBackupHistoryIncludesEveryWorkspace covers a realistic mixed deployment:
// a default workspace plus a tenant workspace, each contributing history.
func TestBackupHistoryIncludesEveryWorkspace(t *testing.T) {
	db := historyWSDB(t)
	historyWSSeed(t, db, "default", "soul-default")
	historyWSSeed(t, db, "acme", "soul-acme")

	backup := historyWSBackup(t, db, Options{IncludeJudgments: true, JudgmentDays: 30})
	if len(backup.Data.Judgments) != 2 {
		t.Fatalf("backup contains %d judgments, want 2 (one per workspace)", len(backup.Data.Judgments))
	}
	seen := map[string]bool{}
	for _, j := range backup.Data.Judgments {
		seen[j.WorkspaceID] = true
	}
	if !seen["default"] || !seen["acme"] {
		t.Fatalf("archived workspaces = %v, want both \"default\" and \"acme\"", seen)
	}
}

// TestBackupHistoryRespectsDayWindow guards the day bound the fix must not
// disturb: a judgment older than the requested window is still excluded.
func TestBackupHistoryRespectsDayWindow(t *testing.T) {
	db := historyWSDB(t)
	historyWSSeed(t, db, "acme", "soul-acme")

	old := &core.Judgment{
		ID:          "judgment-ancient",
		SoulID:      "soul-acme",
		WorkspaceID: "acme",
		Timestamp:   time.Now().UTC().AddDate(0, 0, -90),
		Status:      core.SoulAlive,
		Message:     "ok",
	}
	if err := db.SaveJudgment(context.Background(), old); err != nil {
		t.Fatalf("SaveJudgment(old): %v", err)
	}

	backup := historyWSBackup(t, db, Options{IncludeJudgments: true, JudgmentDays: 7})
	if len(backup.Data.Judgments) != 1 {
		t.Fatalf("backup contains %d judgments, want 1 (the in-window one only)",
			len(backup.Data.Judgments))
	}
}

// TestBackupDefaultWorkspaceHistoryIsUnchanged is the control: the default
// workspace already worked before the fix and must keep working.
func TestBackupDefaultWorkspaceHistoryIsUnchanged(t *testing.T) {
	db := historyWSDB(t)
	historyWSSeed(t, db, "default", "soul-default")

	backup := historyWSBackup(t, db, Options{IncludeJudgments: true, JudgmentDays: 30})
	if len(backup.Data.Judgments) != 1 {
		t.Fatalf("default workspace produced %d judgments, want 1", len(backup.Data.Judgments))
	}
}

// TestBackupWithoutHistoryOptionStaysEmpty is a second control: without
// IncludeJudgments the archive must contain no history at all.
func TestBackupWithoutHistoryOptionStaysEmpty(t *testing.T) {
	db := historyWSDB(t)
	historyWSSeed(t, db, "acme", "soul-acme")

	backup := historyWSBackup(t, db, Options{IncludeJudgments: false})
	if len(backup.Data.Judgments) != 0 {
		t.Fatalf("history present without IncludeJudgments (%d entries)",
			len(backup.Data.Judgments))
	}
}
