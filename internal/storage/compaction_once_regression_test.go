package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Compaction rebuilt every eligible target bucket on each 5-minute run from
// whatever source summaries still existed. Once retention had deleted part of
// a bucket's sources, the next run overwrote the rollup from the survivors,
// losing counts and skewing uptime. A rollup is now built once its whole span
// is past the threshold, records a snapshot of each source, and is rebuilt
// only when a source changes (late data), with deleted sources taken from
// their snapshots.

func saveMinuteSummaries(t *testing.T, ts *TimeSeriesStore, start time.Time, n int) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < n; i++ {
		j := &core.Judgment{
			ID: fmt.Sprintf("j-%d", i), SoulID: "soul", WorkspaceID: "default",
			Status: core.SoulAlive, Duration: 20 * time.Millisecond,
			Timestamp: start.Add(time.Duration(i) * time.Minute),
		}
		if err := ts.SaveJudgment(ctx, j); err != nil {
			t.Fatalf("SaveJudgment: %v", err)
		}
	}
}

func fiveMinuteCount(t *testing.T, ts *TimeSeriesStore, bucket time.Time) int {
	t.Helper()
	got, err := ts.QuerySummaries(context.Background(), "default", "soul", Resolution5Min, bucket, bucket.Add(time.Second))
	if err != nil {
		t.Fatalf("QuerySummaries: %v", err)
	}
	if len(got) == 0 {
		return -1
	}
	return got[0].Count
}

func TestCompactionRollupSurvivesPartialSourcePurge(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	ts := NewTimeSeriesStore(db, core.TimeSeriesConfig{}, newTestLogger())

	bucket := time.Now().Add(-2 * time.Hour).Truncate(5 * time.Minute)
	saveMinuteSummaries(t, ts, bucket, 5)

	if err := ts.compactToResolution(Resolution1Min, Resolution5Min, time.Hour); err != nil {
		t.Fatalf("compactToResolution: %v", err)
	}
	if got := fiveMinuteCount(t, ts, bucket); got != 5 {
		t.Fatalf("5-minute rollup count = %d, want 5", got)
	}

	// Retention deletes the two oldest 1-minute summaries of the bucket.
	for i := 0; i < 2; i++ {
		key := fmt.Sprintf("default/ts/soul/1min/%d", bucket.Add(time.Duration(i)*time.Minute).Unix())
		if err := db.Delete(key); err != nil {
			t.Fatalf("Delete %s: %v", key, err)
		}
	}

	if err := ts.compactToResolution(Resolution1Min, Resolution5Min, time.Hour); err != nil {
		t.Fatalf("compactToResolution: %v", err)
	}
	if got := fiveMinuteCount(t, ts, bucket); got != 5 {
		t.Fatalf("rollup rebuilt from partly purged sources: count = %d, want 5", got)
	}
}

func TestCompactionWaitsForTheWholeTargetBucket(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	ts := NewTimeSeriesStore(db, core.TimeSeriesConfig{}, newTestLogger())

	// Threshold 1h. A bucket that starts 62 minutes ago has its first source
	// past the cutoff but ends only 57 minutes ago, so it is still open.
	open := time.Now().Add(-62 * time.Minute).Truncate(5 * time.Minute)
	if open.Add(5 * time.Minute).Before(time.Now().Add(-time.Hour)) {
		open = open.Add(5 * time.Minute) // keep the bucket straddling the cutoff
	}
	saveMinuteSummaries(t, ts, open, 5)

	if err := ts.compactToResolution(Resolution1Min, Resolution5Min, time.Hour); err != nil {
		t.Fatalf("compactToResolution: %v", err)
	}
	if got := fiveMinuteCount(t, ts, open); got != -1 {
		t.Fatalf("rollup built for a bucket still inside the threshold (count %d); it would be frozen incomplete", got)
	}
}

func lateJudgment(t *testing.T, ts *TimeSeriesStore, at time.Time) {
	t.Helper()
	j := &core.Judgment{
		ID: fmt.Sprintf("late-%d", at.UnixNano()), SoulID: "soul", WorkspaceID: "default",
		Status: core.SoulDead, Duration: 900 * time.Millisecond, Timestamp: at,
	}
	if err := ts.SaveJudgment(context.Background(), j); err != nil {
		t.Fatalf("SaveJudgment: %v", err)
	}
}

func fiveMinuteSummary(t *testing.T, ts *TimeSeriesStore, bucket time.Time) *JudgmentSummary {
	t.Helper()
	got, err := ts.QuerySummaries(context.Background(), "default", "soul", Resolution5Min, bucket, bucket.Add(time.Second))
	if err != nil || len(got) != 1 {
		t.Fatalf("QuerySummaries = %d summaries, err %v; want 1", len(got), err)
	}
	return got[0]
}

func TestCompactionFoldsLateDataIntoBuiltRollup(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	ts := NewTimeSeriesStore(db, core.TimeSeriesConfig{}, newTestLogger())

	bucket := time.Now().Add(-2 * time.Hour).Truncate(5 * time.Minute)
	saveMinuteSummaries(t, ts, bucket, 5)
	if err := ts.compactToResolution(Resolution1Min, Resolution5Min, time.Hour); err != nil {
		t.Fatalf("compactToResolution: %v", err)
	}

	// A failed check for minute 2 arrives after the rollup was built.
	lateJudgment(t, ts, bucket.Add(2*time.Minute+10*time.Second))
	if err := ts.compactToResolution(Resolution1Min, Resolution5Min, time.Hour); err != nil {
		t.Fatalf("compactToResolution: %v", err)
	}

	got := fiveMinuteSummary(t, ts, bucket)
	if got.Count != 6 || got.FailureCount != 1 || got.MaxLatency != 900 {
		t.Fatalf("rollup after late data = count %d, failures %d, max %.0fms; want 6, 1, 900ms",
			got.Count, got.FailureCount, got.MaxLatency)
	}
}

func TestCompactionLateDataKeepsSourcesRetentionDeleted(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	ts := NewTimeSeriesStore(db, core.TimeSeriesConfig{}, newTestLogger())

	bucket := time.Now().Add(-2 * time.Hour).Truncate(5 * time.Minute)
	saveMinuteSummaries(t, ts, bucket, 5)
	if err := ts.compactToResolution(Resolution1Min, Resolution5Min, time.Hour); err != nil {
		t.Fatalf("compactToResolution: %v", err)
	}

	// Retention deletes the first two minutes, then late data lands in minute 4:
	// the rebuild must count the deleted minutes from the rollup's snapshots.
	for i := 0; i < 2; i++ {
		if err := db.Delete(fmt.Sprintf("default/ts/soul/1min/%d", bucket.Add(time.Duration(i)*time.Minute).Unix())); err != nil {
			t.Fatalf("Delete: %v", err)
		}
	}
	lateJudgment(t, ts, bucket.Add(4*time.Minute+10*time.Second))
	if err := ts.compactToResolution(Resolution1Min, Resolution5Min, time.Hour); err != nil {
		t.Fatalf("compactToResolution: %v", err)
	}

	if got := fiveMinuteSummary(t, ts, bucket); got.Count != 6 || got.SuccessCount != 5 {
		t.Fatalf("rollup = count %d, successes %d; want 6 and 5 (2 deleted + 3 kept minutes + 1 late failure)",
			got.Count, got.SuccessCount)
	}
}

func TestCompactionLeavesUnchangedRollupAlone(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	ts := NewTimeSeriesStore(db, core.TimeSeriesConfig{}, newTestLogger())

	bucket := time.Now().Add(-2 * time.Hour).Truncate(5 * time.Minute)
	saveMinuteSummaries(t, ts, bucket, 5)
	if err := ts.compactToResolution(Resolution1Min, Resolution5Min, time.Hour); err != nil {
		t.Fatalf("compactToResolution: %v", err)
	}

	// Mark the stored rollup; a run with no source changes must not rewrite it.
	key := fmt.Sprintf("default/ts/soul/5min/%d", bucket.Unix())
	stored := fiveMinuteSummary(t, ts, bucket)
	stored.UptimePercent = 12.5
	data, _ := json.Marshal(stored)
	if err := db.Put(key, data); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := ts.compactToResolution(Resolution1Min, Resolution5Min, time.Hour); err != nil {
		t.Fatalf("compactToResolution: %v", err)
	}
	if got := fiveMinuteSummary(t, ts, bucket); got.UptimePercent != 12.5 {
		t.Fatal("a rollup with unchanged sources was rewritten; every run would rewrite every rollup")
	}
}
