package storage

import (
	"context"
	"fmt"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"testing"
	"time"
)

func auditJudgmentRange(t *testing.T, startSeconds, endSeconds int64, limit, want int) {
	t.Helper()
	db := newTestDB(t)
	defer db.Close()
	ctx := context.Background()
	start := time.Unix(startSeconds, 0).UTC()
	end := time.Unix(endSeconds, 0).UTC()
	for i, at := range []time.Time{start.Add(-time.Second), start, start.Add(time.Second), end, end.Add(time.Second)} {
		if err := db.SaveJudgment(ctx, &core.Judgment{ID: fmt.Sprintf("range-%d", i), SoulID: "range-soul", WorkspaceID: "default", Timestamp: at, Status: core.SoulAlive}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.QueryJudgments(ctx, "default", "range-soul", start, end, limit)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("range=[%d,%d) limit=%d EXPECTED: count=%d ACTUAL: count=%d", startSeconds, endSeconds, limit, want, len(got))
	if len(got) != want {
		t.Fatal("regression mismatch")
	}
	for _, j := range got {
		if j.Timestamp.Before(start) || !j.Timestamp.Before(end) {
			t.Fatal("out of range judgment")
		}
	}
}
func TestAuditJudgmentChronologicalBounds(t *testing.T) {
	t.Run("same-width-control", func(t *testing.T) { auditJudgmentRange(t, 20, 22, 0, 2) })
	t.Run("decimal-width-crossing", func(t *testing.T) { auditJudgmentRange(t, 9, 11, 0, 2) })
	t.Run("negative-epoch", func(t *testing.T) { auditJudgmentRange(t, -4, -2, 0, 2) })
	t.Run("cross-zero", func(t *testing.T) { auditJudgmentRange(t, -1, 1, 0, 2) })
	t.Run("empty-window", func(t *testing.T) { auditJudgmentRange(t, 9, 9, 0, 0) })
	t.Run("reversed-window", func(t *testing.T) { auditJudgmentRange(t, 11, 9, 0, 0) })
	t.Run("limited-crossing", func(t *testing.T) { auditJudgmentRange(t, 9, 11, 1, 1) })
}
