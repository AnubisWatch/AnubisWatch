package storage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// QueryJudgments collects judgments from a map (RangeScan/PrefixScan return
// one), so stopping at `limit` while iterating it kept whichever judgments
// the map happened to yield first. Every sibling list method (ListJudgments,
// ListVerdicts, QueryJourneyRuns) sorts newest-first and then applies the
// limit; a limited query must return the newest judgments, deterministically.
func TestQueryJudgmentsLimitReturnsTheNewestJudgments(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	const total = 12
	for i := 0; i < total; i++ {
		j := &core.Judgment{
			ID: fmt.Sprintf("j-%02d", i), SoulID: "limit-order-soul", WorkspaceID: "default",
			Status: core.SoulAlive, Timestamp: now.Add(-time.Duration(i) * time.Minute),
		}
		if err := db.SaveJudgment(ctx, j); err != nil {
			t.Fatal(err)
		}
	}
	start, end := now.Add(-time.Hour), now.Add(time.Minute)

	// Control: without a limit every judgment in the window comes back.
	all, err := db.QueryJudgments(ctx, "default", "limit-order-soul", start, end, 0)
	if err != nil || len(all) != total {
		t.Fatalf("control unlimited: %d judgments err=%v, want %d", len(all), err, total)
	}

	// Map iteration order is randomised per iteration, so repeat: a correct
	// implementation returns the same newest three every time.
	for run := 0; run < 25; run++ {
		got, err := db.QueryJudgments(ctx, "default", "limit-order-soul", start, end, 3)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 3 {
			t.Fatalf("run %d: %d judgments, want 3", run, len(got))
		}
		for i, want := range []string{"j-00", "j-01", "j-02"} {
			if got[i].ID != want {
				ids := []string{got[0].ID, got[1].ID, got[2].ID}
				t.Fatalf("run %d: limit 3 returned %v, want the newest [j-00 j-01 j-02] in that order", run, ids)
			}
		}
	}
}
