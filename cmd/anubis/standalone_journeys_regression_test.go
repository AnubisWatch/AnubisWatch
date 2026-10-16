package main

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/journey"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

// Standalone used to schedule only config-file journeys: journeys created,
// edited or deleted through the API never (re)scheduled, and stored journeys
// were not loaded after a restart.
func TestStandaloneJourneysFollowStorage(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	dir := t.TempDir()
	open := func() *storage.CobaltDB {
		db, err := storage.NewEngine(core.StorageConfig{Path: dir}, logger)
		if err != nil {
			t.Fatal(err)
		}
		return db
	}
	newJourney := func(id string) *core.JourneyConfig {
		return &core.JourneyConfig{
			ID: id, Name: id, WorkspaceID: "default", Enabled: true,
			Weight:  core.Duration{Duration: time.Hour},
			Timeout: core.Duration{Duration: time.Second},
			Steps: []core.JourneyStep{{Name: "s", Type: core.CheckTCP, Target: "127.0.0.1:9",
				Timeout: core.Duration{Duration: 100 * time.Millisecond}, TCP: &core.TCPConfig{}}},
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db := open()
	exec := journey.NewExecutor(db, logger)
	sync := &standaloneJourneys{exec: exec, cfg: &core.Config{}, store: db, logger: logger}
	adapter := &restStorageAdapter{store: db, onJourneyChange: sync.sync}

	// A write before start is picked up by start.
	if err := adapter.SaveJourneyNoCtx(newJourney("j1")); err != nil {
		t.Fatal(err)
	}
	if got := exec.Running(); len(got) != 0 {
		t.Fatalf("scheduled before start: %v", got)
	}
	sync.start(ctx)
	if got := exec.Running(); !slices.Equal(got, []string{"j1"}) {
		t.Fatalf("after start running = %v, want [j1]", got)
	}

	if err := adapter.SaveJourneyNoCtx(newJourney("j2")); err != nil {
		t.Fatal(err)
	}
	if got := exec.Running(); !slices.Equal(got, []string{"j1", "j2"}) {
		t.Fatalf("after API create running = %v, want [j1 j2]", got)
	}

	disabled := newJourney("j2")
	disabled.Enabled = false
	if err := adapter.SaveJourneyNoCtx(disabled); err != nil {
		t.Fatal(err)
	}
	if got := exec.Running(); !slices.Equal(got, []string{"j1"}) {
		t.Fatalf("after disable running = %v, want [j1]", got)
	}

	if err := adapter.DeleteJourneyNoCtx("j1"); err != nil {
		t.Fatal(err)
	}
	if got := exec.Running(); len(got) != 0 {
		t.Fatalf("after delete running = %v, want none", got)
	}

	// Restart: stored journeys are scheduled again.
	if err := adapter.SaveJourneyNoCtx(newJourney("j3")); err != nil {
		t.Fatal(err)
	}
	exec.StopAll()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = open()
	defer db.Close()
	exec = journey.NewExecutor(db, logger)
	defer exec.StopAll()
	(&standaloneJourneys{exec: exec, cfg: &core.Config{}, store: db, logger: logger}).start(ctx)
	if got := exec.Running(); !slices.Equal(got, []string{"j3"}) {
		t.Fatalf("after restart running = %v, want [j3]", got)
	}
}
