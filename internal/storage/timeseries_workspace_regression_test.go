package storage

import (
	"context"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"testing"
	"time"
)

func auditSummaryWorkspace(t *testing.T, explicit, contextWS, expected string, repeats int) {
	t.Helper()
	db := newTestDB(t)
	defer db.Close()
	ts := NewTimeSeriesStore(db, core.TimeSeriesConfig{}, newTestLogger())
	ctx := core.ContextWithWorkspaceID(context.Background(), contextWS)
	at := time.Date(2026, 2, 3, 4, 5, 10, 0, time.UTC)
	for i := 0; i < repeats; i++ {
		j := &core.Judgment{WorkspaceID: explicit, SoulID: "summary-soul", Timestamp: at.Add(time.Duration(i) * time.Second), Status: core.SoulAlive}
		if err := ts.SaveJudgment(ctx, j); err != nil {
			t.Fatal(err)
		}
		if _, err := db.GetJudgment(ctx, expected, j.SoulID, j.Timestamp); err != nil {
			t.Fatal("raw result missing:", err)
		}
	}
	summaries, err := ts.QuerySummaries(ctx, expected, "summary-soul", Resolution1Min, at.Add(-time.Minute), at.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("explicit=%q context=%q EXPECTED: summaries=1 ACTUAL: summaries=%d", explicit, contextWS, len(summaries))
	if len(summaries) != 1 {
		t.Fatal("PROBLEM CONFIRMED")
	}
	if summaries[0].WorkspaceID != expected || summaries[0].Count != repeats {
		t.Fatalf("summary=%+v want workspace=%s count=%d", summaries[0], expected, repeats)
	}
}
func TestAuditSummaryWorkspace(t *testing.T) {
	t.Run("control", func(t *testing.T) { auditSummaryWorkspace(t, "default", "default", "default", 1) })
	t.Run("background-workspace", func(t *testing.T) { auditSummaryWorkspace(t, "tenant-a", "default", "tenant-a", 1) })
	t.Run("context-fallback", func(t *testing.T) { auditSummaryWorkspace(t, "", "tenant-b", "tenant-b", 1) })
	t.Run("explicit-precedence", func(t *testing.T) { auditSummaryWorkspace(t, "tenant-a", "tenant-b", "tenant-a", 1) })
	t.Run("repeated", func(t *testing.T) { auditSummaryWorkspace(t, "tenant-a", "default", "tenant-a", 2) })
	if !t.Failed() {
		t.Log("FIX VERIFIED")
	}
}
