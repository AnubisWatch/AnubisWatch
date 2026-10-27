package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestJudgmentIdentitySurvivesTimestampCollisionAndRestart(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		for _, rollup := range []bool{false, true} {
			t.Run(fmt.Sprintf("encrypted=%v/rollup=%v", encrypted, rollup), func(t *testing.T) {
				cfg := core.StorageConfig{Path: t.TempDir(), Encryption: core.EncryptionConfig{Enabled: encrypted, Key: "identity-fixture"}}
				db, err := NewEngine(cfg, newTestLogger())
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = db.Close() }()
				ctx := core.ContextWithWorkspaceID(context.Background(), "tenant")
				at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
				save := db.SaveJudgment
				if rollup {
					save = NewTimeSeriesStore(db, core.TimeSeriesConfig{}, newTestLogger()).SaveJudgment
				}
				for _, id := range []string{"first", "second"} {
					if err := save(ctx, &core.Judgment{ID: id, WorkspaceID: "tenant", SoulID: "soul", Timestamp: at, Status: core.SoulAlive}); err != nil {
						t.Fatal(err)
					}
				}
				check := func() {
					t.Helper()
					items, err := db.QueryJudgments(ctx, "tenant", "soul", at, at.Add(time.Nanosecond), 0)
					if err != nil || len(items) != 2 {
						t.Fatalf("distinct IDs lost: count=%d err=%v", len(items), err)
					}
					if items[0].ID != "second" || items[1].ID != "first" {
						t.Fatalf("unstable tie ordering: %v %v", items[0].ID, items[1].ID)
					}
					for _, id := range []string{"first", "second"} {
						j, err := db.GetJudgmentByID(ctx, "tenant", id)
						if err != nil || j.ID != id {
							t.Fatalf("ID lookup %s: %v %v", id, j, err)
						}
						j, err = db.GetJudgmentNoCtx(id)
						if err != nil || j.ID != id {
							t.Fatalf("API ID lookup %s: %v %v", id, j, err)
						}
						if _, err = db.GetJudgmentByID(ctx, "other", id); err == nil {
							t.Fatal("cross-workspace ID lookup succeeded")
						}
					}
					if _, err := db.GetJudgment(ctx, "tenant", "soul", at); err != nil {
						t.Fatalf("timestamp compatibility: %v", err)
					}
					items, err = db.QueryJudgments(ctx, "tenant", "soul", at.Add(-time.Second), at, 0)
					if err != nil || len(items) != 0 {
						t.Fatalf("exclusive end: count=%d err=%v", len(items), err)
					}
				}
				check()
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
				db, err = NewEngine(cfg, newTestLogger())
				if err != nil {
					t.Fatal(err)
				}
				check()
				rm := NewRetentionManager(db, core.RetentionConfig{}, cfg.Path, newTestLogger())
				if err := rm.purgeRawData(at.Add(time.Second)); err != nil {
					t.Fatal(err)
				}
				for _, id := range []string{"first", "second"} {
					if _, err := db.GetJudgmentNoCtx(id); err == nil {
						t.Fatalf("retention retained index %s", id)
					}
				}
			})
		}
	}
}

func TestJudgmentLegacyRecordCoexistsWithoutDuplicateOnResave(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	ctx := context.Background()
	at := time.Unix(123, 0).UTC()
	legacy := &core.Judgment{ID: "legacy", WorkspaceID: "default", SoulID: "soul", Timestamp: at}
	value, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("default/judgments/soul/%d", at.UnixNano())
	if err := db.Put(key, value); err != nil {
		t.Fatal(err)
	}
	if err := db.Put("default/judgment-idx/legacy", []byte(key)); err != nil {
		t.Fatal(err)
	}
	db.RebuildIndexes()
	if err := db.SaveJudgment(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	other := *legacy
	other.ID = "other"
	if err := db.SaveJudgment(ctx, &other); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveJudgment(ctx, &other); err != nil {
		t.Fatal(err)
	}
	items, err := db.QueryJudgments(ctx, "default", "soul", at, at.Add(time.Second), 0)
	if err != nil || len(items) != 2 {
		t.Fatalf("legacy coexistence/resave: count=%d err=%v", len(items), err)
	}
	for _, id := range []string{"legacy", "other"} {
		if j, err := db.GetJudgmentByID(ctx, "default", id); err != nil || j.ID != id {
			t.Fatalf("lookup %s: %v %v", id, j, err)
		}
	}
}

func TestConcurrentJudgmentsKeepDistinctIdentities(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	const count = 24
	at := time.Unix(123, 0).UTC()
	start := make(chan struct{})
	results := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start
			results <- db.SaveJudgment(context.Background(), &core.Judgment{ID: fmt.Sprint(id), WorkspaceID: "default", SoulID: "soul", Timestamp: at})
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	items, err := db.QueryJudgments(context.Background(), "default", "soul", at, at.Add(time.Second), 0)
	if err != nil || len(items) != count {
		t.Fatalf("concurrent count=%d want=%d err=%v", len(items), count, err)
	}
}
