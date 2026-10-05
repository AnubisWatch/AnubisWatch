package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"sync"
	"testing"
)

func TestJourneyRunSameTimestampPersistence(t *testing.T) {
	ctx := context.Background()
	cfg := core.StorageConfig{Path: t.TempDir()}
	db, e := NewEngine(cfg, newTestLogger())
	if e != nil {
		t.Fatal(e)
	}
	defer func() { db.Close() }()
	for _, ms := range []int64{0, 1, 1760000000123} {
		journey := fmt.Sprintf("j-%d", ms)
		for _, id := range []string{"first", "second"} {
			run := &core.JourneyRun{ID: id, JourneyID: journey, StartedAt: ms, Duration: 1}
			if e = db.SaveJourneyRun(ctx, run); e != nil {
				t.Fatal(e)
			}
			run.Duration = 2
			if e = db.SaveJourneyRun(ctx, run); e != nil {
				t.Fatal(e)
			}
		}
		runs, e := db.QueryJourneyRuns(ctx, "", journey, 0)
		if e != nil || len(runs) != 2 {
			t.Fatal("equal timestamp/update collision")
		}
		for _, id := range []string{"first", "second"} {
			r, e := db.GetJourneyRun(ctx, "", journey, id)
			if e != nil || r.Duration != 2 {
				t.Fatal("update/read identity")
			}
		}
	}
	legacy := &core.JourneyRun{ID: "legacy", JourneyID: "legacy-journey", StartedAt: 7, Duration: 1}
	raw, e := json.Marshal(legacy)
	if e != nil {
		t.Fatal(e)
	}
	if e = db.Put("default/journey-runs/legacy-journey/7", raw); e != nil {
		t.Fatal(e)
	}
	legacy.Duration = 9
	if e = db.SaveJourneyRun(ctx, legacy); e != nil {
		t.Fatal(e)
	}
	runs, e := db.QueryJourneyRuns(ctx, "", "legacy-journey", 0)
	if e != nil || len(runs) != 1 || runs[0].Duration != 9 {
		t.Fatal("legacy update duplicated")
	}
	if e = db.SaveJourneyRun(ctx, &core.JourneyRun{ID: "modern", JourneyID: "legacy-journey", StartedAt: 7}); e != nil {
		t.Fatal(e)
	}
	start := make(chan struct{})
	errs := make(chan error, 4)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Go(func() {
			<-start
			errs <- db.SaveJourneyRun(ctx, &core.JourneyRun{ID: fmt.Sprintf("parallel-%d", i), JourneyID: "parallel", StartedAt: 8})
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	runs, e = db.QueryJourneyRuns(ctx, "", "parallel", 0)
	if e != nil || len(runs) != 4 {
		t.Fatal("parallel same timestamp")
	}
	if e = db.Close(); e != nil {
		t.Fatal(e)
	}
	db, e = NewEngine(cfg, newTestLogger())
	if e != nil {
		t.Fatal(e)
	}
	runs, e = db.QueryJourneyRuns(ctx, "", "legacy-journey", 0)
	if e != nil || len(runs) != 2 {
		t.Fatal("reopen legacy/modern")
	}
	for _, id := range []string{"legacy", "modern"} {
		if _, e = db.GetJourneyRun(ctx, "", "legacy-journey", id); e != nil {
			t.Fatal(e)
		}
	}
}
