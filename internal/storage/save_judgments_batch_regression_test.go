package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func batchTestJudgments(base time.Time) []*core.Judgment {
	var out []*core.Judgment
	for i := 0; i < 12; i++ {
		status := core.SoulAlive
		if i%4 == 3 {
			status = core.SoulDead
		}
		out = append(out, &core.Judgment{
			ID: fmt.Sprintf("j%02d", i), WorkspaceID: "default", SoulID: fmt.Sprintf("soul-%d", i%2),
			Status: status, Timestamp: base.Add(time.Duration(i) * 5 * time.Second),
			Duration: time.Duration(10+i) * time.Millisecond,
		})
	}
	return out
}

func summariesOf(t *testing.T, db *CobaltDB) map[string]JudgmentSummary {
	t.Helper()
	raw, err := db.PrefixScan("default/ts/")
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]JudgmentSummary)
	for key, value := range raw {
		var s JudgmentSummary
		if json.Unmarshal(value, &s) == nil && s.Count > 0 {
			out[key] = s
		}
	}
	return out
}

// Replicated judgments are applied in batches. SaveJudgments must leave the
// same judgments and summaries as saving them one by one, in one fsync, and
// skip judgments that are already stored so a replayed batch counts nothing
// twice.
func TestSaveJudgmentsMatchesSequentialSaves(t *testing.T) {
	base := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()

	seqDB := openSnapshotTestDB(t, false)
	seq := NewTimeSeriesStore(seqDB, core.TimeSeriesConfig{}, newTestLogger())
	for _, j := range batchTestJudgments(base) {
		if err := seq.SaveJudgment(ctx, j); err != nil {
			t.Fatal(err)
		}
	}

	batchDB := openSnapshotTestDB(t, false)
	batch := NewTimeSeriesStore(batchDB, core.TimeSeriesConfig{}, newTestLogger())
	judgments := batchTestJudgments(base)
	judgments = append(judgments, judgments[3]) // repeated in the batch
	var syncs atomic.Int32
	old := walSyncSeam
	walSyncSeam = func(f *os.File) error { syncs.Add(1); return old(f) }
	err := batch.SaveJudgments(ctx, judgments)
	walSyncSeam = old
	if err != nil {
		t.Fatal(err)
	}
	if got := syncs.Load(); got != 1 {
		t.Fatalf("WAL fsyncs for one batch = %d, want 1", got)
	}

	want, got := summariesOf(t, seqDB), summariesOf(t, batchDB)
	if len(want) == 0 || len(got) != len(want) {
		t.Fatalf("summaries: %d batched, %d sequential", len(got), len(want))
	}
	for key, w := range want {
		g := got[key]
		if g.Count != w.Count || g.SuccessCount != w.SuccessCount || g.FailureCount != w.FailureCount ||
			g.MinLatency != w.MinLatency || g.MaxLatency != w.MaxLatency || g.AvgLatency != w.AvgLatency {
			t.Fatalf("summary %s: batched %+v, sequential %+v", key, g, w)
		}
	}
	for _, j := range batchTestJudgments(base) {
		if _, err := batchDB.GetJudgmentByID(ctx, "default", j.ID); err != nil {
			t.Fatalf("judgment %s not stored: %v", j.ID, err)
		}
	}

	// Replaying the batch changes nothing.
	if err := batch.SaveJudgments(ctx, batchTestJudgments(base)); err != nil {
		t.Fatal(err)
	}
	for key, s := range summariesOf(t, batchDB) {
		if s.Count != want[key].Count {
			t.Fatalf("replayed batch recounted %s: %d, want %d", key, s.Count, want[key].Count)
		}
	}
	listed, err := batchDB.ListJudgmentsNoCtx("soul-0", base.Add(-time.Hour), base.Add(time.Hour), 100)
	if err != nil || len(listed) != 6 {
		t.Fatalf("index lookup after batch = %d judgments, %v; want 6", len(listed), err)
	}
}
