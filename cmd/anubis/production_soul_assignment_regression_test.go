package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/probe"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

func TestProductionSoulAssignmentLoadsAllStoredSouls(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := storage.NewEngine(core.StorageConfig{Path: t.TempDir()}, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	cfg := &core.Config{}
	if got := collectSouls(ctx, cfg, db, logger); len(got) != 0 {
		t.Fatalf("empty control: %d", len(got))
	}
	for i := 0; i < 10001; i++ {
		soul := &core.Soul{ID: fmt.Sprintf("soul%05d", i), WorkspaceID: "default", Name: "Stored", Type: core.CheckTCP, Target: "127.0.0.1:1", Enabled: i == 10000, Weight: core.Duration{Duration: time.Hour}, Timeout: core.Duration{Duration: time.Millisecond}}
		if i < 10000 {
			soul.Regions = []string{"other-region"}
		}
		if err := db.SaveSoul(ctx, soul); err != nil {
			t.Fatal(err)
		}
		if i == 9999 {
			if got := collectSouls(ctx, cfg, db, logger); len(got) != 10000 {
				t.Fatalf("boundary control: %d", len(got))
			}
		}
	}
	t.Run("cluster collection", func(t *testing.T) {
		got := collectSouls(ctx, cfg, db, logger)
		if len(got) != 10001 {
			t.Errorf("EXPECTED: all 10001 stored souls; ACTUAL: %d; PROBLEM CONFIRMED", len(got))
		}
	})
	t.Run("standalone assignment", func(t *testing.T) {
		engine := probe.NewEngine(probe.EngineOptions{Registry: probe.NewCheckerRegistry(), Store: &probeStorageAdapter{store: db}, Logger: logger})
		defer engine.Stop()
		server := NewServer(&ServerDependencies{Config: cfg, Store: db, ProbeEngine: engine, Logger: logger})
		if err := server.Start(ctx); err != nil {
			t.Fatal(err)
		}
		got := engine.ListActiveSouls()
		if len(got) != 1 || got[0].ID != "soul10000" {
			t.Errorf("EXPECTED: enabled soul beyond old limit assigned; ACTUAL: %d active; PROBLEM CONFIRMED", len(got))
		}
	})
	t.Run("workspace and config control", func(t *testing.T) {
		if err := db.SaveSoul(ctx, &core.Soul{ID: "other", WorkspaceID: "tenant-b", Name: "Other"}); err != nil {
			t.Fatal(err)
		}
		if err := db.SaveWorkspace(ctx, &core.Workspace{ID: "tenant-b", Name: "Tenant B"}); err != nil {
			t.Fatal(err)
		}
		cfg.Souls = []core.Soul{{ID: "soul00000", Name: "Configured override"}}
		got := collectSouls(ctx, cfg, db, logger)
		if len(got) != 10002 {
			t.Errorf("EXPECTED: deduplicated 10002 souls across workspaces; ACTUAL: %d", len(got))
		}
		if got[0].Name != "Configured override" {
			t.Error("configured override lost")
		}
	})
}
