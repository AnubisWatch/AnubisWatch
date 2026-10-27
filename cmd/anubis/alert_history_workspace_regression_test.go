package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/alert"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

var _ alert.JudgmentHistoryStore = (*alertStorageAdapter)(nil)

func TestAlertHistoryAdapterPreservesWorkspaceAndWindow(t *testing.T) {
	db, err := storage.NewEngine(core.StorageConfig{Path: t.TempDir()}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	adapter := &alertStorageAdapter{store: db}
	end := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	start := end.Add(-time.Minute)
	for i, sample := range []struct {
		workspace, soul string
		at              time.Time
	}{
		{"default", "target", start}, {"default", "target", end},
		{"default", "target", start.Add(-time.Nanosecond)}, {"default", "target", end.Add(time.Nanosecond)},
		{"tenant-b", "target", end.Add(-time.Second)}, {"default", "another", end.Add(-time.Second)},
	} {
		if err := db.SaveJudgment(context.Background(), &core.Judgment{ID: fmt.Sprintf("history-%d", i), WorkspaceID: sample.workspace, SoulID: sample.soul, Timestamp: sample.at, Status: core.SoulAlive}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name, workspace, soul string
		start, end            time.Time
		want                  int
	}{
		{"inclusive boundaries", "default", "target", start, end, 2},
		{"tenant isolation", "tenant-b", "target", start, end, 1},
		{"empty", "default", "missing", start, end, 0},
		{"point window", "default", "target", end, end, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := adapter.ListAlertJudgments(tc.workspace, tc.soul, tc.start, tc.end)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tc.want {
				t.Fatalf("got %d judgments, want %d", len(got), tc.want)
			}
			for _, j := range got {
				if j.WorkspaceID != tc.workspace || j.SoulID != tc.soul {
					t.Fatalf("unexpected judgment: %+v", j)
				}
			}
		})
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.ListAlertJudgments("default", "target", start, end); err == nil {
		t.Fatal("closed storage error was swallowed")
	}
}
