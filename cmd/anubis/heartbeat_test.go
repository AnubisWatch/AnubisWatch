package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

func TestHeartbeatRecorderStandalone(t *testing.T) {
	db, err := storage.NewEngine(core.StorageConfig{Path: t.TempDir()}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	var woken []string
	rec := &heartbeatRecorder{store: db, nodeID: "solo", now: func() time.Time { return now },
		wake: func(id string) { woken = append(woken, id) }}
	soul := &core.Soul{ID: "hb-1", WorkspaceID: "default", Type: core.CheckHeartbeat}
	ctx := context.Background()

	if st, err := rec.GetHeartbeatState(ctx, "default", soul.ID); err != nil || st != nil {
		t.Fatalf("empty state = %+v, %v", st, err)
	}
	if err := rec.RecordHeartbeat(ctx, soul, core.HeartbeatStart, "", "203.0.113.9"); err != nil {
		t.Fatal(err)
	}
	if len(woken) != 0 {
		t.Fatal("a start ping must not wake the evaluator")
	}
	now = now.Add(42 * time.Second)
	if err := rec.RecordHeartbeat(ctx, soul, core.HeartbeatSuccess, "", "203.0.113.9"); err != nil {
		t.Fatal(err)
	}
	st, err := heartbeatSource{store: db}.GetHeartbeatState(ctx, "default", soul.ID)
	if err != nil || st == nil {
		t.Fatalf("state = %+v, %v", st, err)
	}
	if !st.LastPingAt.Equal(now) || st.LastDuration != 42*time.Second || st.LastNode != "solo" || st.LastSource != "203.0.113.9" || st.Pings != 1 {
		t.Fatalf("state = %+v", st)
	}
	if len(woken) != 1 || woken[0] != soul.ID {
		t.Fatalf("woken = %v", woken)
	}
	if j, err := (heartbeatSource{store: db}).LatestJudgment(ctx, "default", soul.ID); err != nil || j != nil {
		t.Fatalf("latest judgment of a fresh soul = %+v, %v", j, err)
	}

	cfg := &core.Config{Souls: []core.Soul{{Name: "cfg-hb", Type: core.CheckHeartbeat}, {Name: "web", Type: core.CheckHTTP}}}
	assignConfigHeartbeatIDs(cfg)
	if cfg.Souls[0].ID != configEntityID("config-soul-", "cfg-hb") || cfg.Souls[1].ID != "" {
		t.Fatalf("config IDs = %q, %q", cfg.Souls[0].ID, cfg.Souls[1].ID)
	}
}
