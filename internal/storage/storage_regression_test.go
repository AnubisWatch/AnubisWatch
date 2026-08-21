package storage

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestEncryptedWALRestartPreservesCiphertextInTree(t *testing.T) {
	dir := t.TempDir()
	cfg := core.StorageConfig{
		Path: dir,
		Encryption: core.EncryptionConfig{
			Enabled: true,
			Key:     "restart-regression-encryption-key",
		},
	}
	key := "test/encrypted-restart"
	want := []byte(`{"secret":"survives restart"}`)

	db, err := NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if err := db.Put(key, want); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close before restart: %v", err)
	}

	for restart := 1; restart <= 2; restart++ {
		db, err = NewEngine(cfg, newTestLogger())
		if err != nil {
			t.Fatalf("NewEngine after restart %d: %v", restart, err)
		}

		got, getErr := db.Get(key)
		if getErr != nil {
			db.Close()
			t.Fatalf("Get after restart %d: %v", restart, getErr)
		}
		if !bytes.Equal(got, want) {
			db.Close()
			t.Fatalf("Get after restart %d = %q, want %q", restart, got, want)
		}
		if err := db.Close(); err != nil {
			t.Fatalf("Close after restart %d: %v", restart, err)
		}
	}
}

func TestListVerdictsSortsBeforeLimit(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	ctx := context.Background()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	const total = 64
	for i := 0; i < total; i++ {
		verdict := &core.Verdict{
			ID:          fmt.Sprintf("verdict-%03d", i),
			WorkspaceID: "default",
			Status:      core.VerdictActive,
			FiredAt:     base.Add(time.Duration(i) * time.Minute),
		}
		if err := db.SaveVerdict(ctx, verdict); err != nil {
			t.Fatalf("SaveVerdict(%d): %v", i, err)
		}
	}

	for attempt := 0; attempt < 10; attempt++ {
		got, err := db.ListVerdicts(ctx, "default", core.VerdictActive, 3)
		if err != nil {
			t.Fatalf("ListVerdicts: %v", err)
		}
		wantIDs := []string{"verdict-063", "verdict-062", "verdict-061"}
		if len(got) != len(wantIDs) {
			t.Fatalf("len(ListVerdicts) = %d, want %d", len(got), len(wantIDs))
		}
		for i, wantID := range wantIDs {
			if got[i].ID != wantID {
				t.Fatalf("attempt %d result[%d].ID = %q, want %q", attempt, i, got[i].ID, wantID)
			}
		}
	}
}

func TestCobaltDBLogStorePreservesEntryType(t *testing.T) {
	cfg := core.StorageConfig{Path: t.TempDir()}
	db, err := NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	store := NewCobaltDBLogStore(db)
	want := &core.RaftLogEntry{
		Index: 9,
		Term:  4,
		Type:  core.LogConfiguration,
		Data:  []byte("configuration"),
	}
	if err := store.StoreLog(want); err != nil {
		t.Fatalf("StoreLog: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close before restart: %v", err)
	}

	db, err = NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("NewEngine after restart: %v", err)
	}
	defer db.Close()
	store = NewCobaltDBLogStore(db)

	var got core.RaftLogEntry
	if err := store.GetLog(want.Index, &got); err != nil {
		t.Fatalf("GetLog after restart: %v", err)
	}
	if got.Type != want.Type {
		t.Fatalf("GetLog Type = %v, want %v", got.Type, want.Type)
	}
}

func TestTimeSeriesStoreSerializesSameBucketUpdates(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	ts := NewTimeSeriesStore(db, core.TimeSeriesConfig{}, newTestLogger())
	ctx := core.ContextWithWorkspaceID(context.Background(), "default")
	bucket := time.Date(2026, 2, 3, 4, 5, 0, 0, time.UTC)
	const count = 100

	start := make(chan struct{})
	errCh := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errCh <- ts.updateSummary(ctx, &core.Judgment{
				SoulID:    "same-bucket-soul",
				Status:    core.SoulAlive,
				Duration:  time.Duration(i+1) * time.Millisecond,
				Timestamp: bucket.Add(time.Duration(i) * time.Millisecond),
			}, Resolution1Min)
		}(i)
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("updateSummary: %v", err)
		}
	}

	summaries, err := ts.QuerySummaries(ctx, "default", "same-bucket-soul", Resolution1Min, bucket, bucket.Add(time.Minute))
	if err != nil {
		t.Fatalf("QuerySummaries: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("len(QuerySummaries) = %d, want 1", len(summaries))
	}
	if summaries[0].Count != count || summaries[0].SuccessCount != count {
		t.Fatalf("summary counts = (%d, %d), want (%d, %d)", summaries[0].Count, summaries[0].SuccessCount, count, count)
	}
}

func TestTimeSeriesStoreCompactionLifecycleIsIdempotent(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	ts := NewTimeSeriesStore(db, core.TimeSeriesConfig{}, newTestLogger())
	ts.StopCompaction()
	ts.StopCompaction()

	ts.StartCompaction()
	firstStopCh := ts.stopCh
	ts.StartCompaction()
	if ts.stopCh != firstStopCh {
		t.Fatal("second StartCompaction replaced the running lifecycle")
	}

	ts.StopCompaction()
	ts.StopCompaction()

	ts.StartCompaction()
	if ts.stopCh == nil || ts.stopCh == firstStopCh {
		t.Fatal("StartCompaction after Stop did not create a fresh lifecycle")
	}
	ts.StopCompaction()
}

func TestRetentionPurgeRemovesJudgmentIndexes(t *testing.T) {
	db, err := NewEngine(core.StorageConfig{
		Path: t.TempDir(),
		Encryption: core.EncryptionConfig{
			Enabled: true,
			Key:     "retention-regression-encryption-key",
		},
	}, newTestLogger())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer db.Close()

	ctx := core.ContextWithWorkspaceID(context.Background(), "default")
	judgment := &core.Judgment{
		ID:          "expired-judgment",
		WorkspaceID: "default",
		SoulID:      "retention-soul",
		Status:      core.SoulAlive,
		Timestamp:   time.Now().Add(-48 * time.Hour),
	}
	if err := db.SaveJudgment(ctx, judgment); err != nil {
		t.Fatalf("SaveJudgment: %v", err)
	}

	rm := NewRetentionManager(db, core.RetentionConfig{}, t.TempDir(), newTestLogger())
	if err := rm.purgeRawData(time.Now().Add(-24 * time.Hour)); err != nil {
		t.Fatalf("purgeRawData: %v", err)
	}

	if _, err := db.GetJudgmentNoCtx(judgment.ID); err == nil {
		t.Fatal("GetJudgmentNoCtx succeeded after retention purge")
	}
	idxKey := "default/judgment-idx/" + judgment.ID
	if _, err := db.Get(idxKey); err == nil {
		t.Fatal("durable judgment index key still exists after retention purge")
	}
	db.mu.RLock()
	_, indexed := db.judgmentIndex[judgment.ID]
	db.mu.RUnlock()
	if indexed {
		t.Fatal("in-memory judgment index still exists after retention purge")
	}
}
