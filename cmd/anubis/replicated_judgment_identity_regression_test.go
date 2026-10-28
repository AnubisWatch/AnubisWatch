package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

func TestReplicatedJudgmentsDeduplicateByIDNotTimestamp(t *testing.T) {
	db, err := storage.NewEngine(core.StorageConfig{Path: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ts := storage.NewTimeSeriesStore(db, core.TimeSeriesConfig{}, slog.Default())
	store := nodeJudgmentStore{store: db, ts: ts}
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, id := range []string{"first", "second", "first", "second"} {
		j := &core.Judgment{ID: id, WorkspaceID: "default", SoulID: "soul", Timestamp: at, Status: core.SoulAlive}
		raw, err := json.Marshal(j)
		if err != nil {
			t.Fatal(err)
		}
		if err := applyReplicatedJudgment(&core.FSMCommand{Op: core.FSMSet, Value: raw}, store); err != nil {
			t.Fatal(err)
		}
	}
	items, err := db.QueryJudgments(context.Background(), "default", "soul", at, at.Add(time.Second), 0)
	if err != nil || len(items) != 2 {
		t.Fatalf("replica history count=%d err=%v", len(items), err)
	}
	key := fmt.Sprintf("default/ts/soul/1min/%d", at.Unix())
	raw, err := db.Get(key)
	if err != nil {
		t.Fatal(err)
	}
	var summary storage.JudgmentSummary
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	if summary.Count != 2 {
		t.Fatalf("replay double-counted or dropped collision: %d", summary.Count)
	}
}
