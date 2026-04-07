package storage

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// setupBenchmarkDB creates a temporary database for benchmarking
func setupBenchmarkDB(b *testing.B) (*CobaltDB, func()) {
	tempDir := b.TempDir()
	cfg := core.StorageConfig{
		Path: tempDir,
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	store, err := NewEngine(cfg, logger)
	if err != nil {
		b.Fatal(err)
	}

	cleanup := func() {
		store.Close()
	}

	return store, cleanup
}

// BenchmarkSaveJudgment benchmarks judgment storage
func BenchmarkSaveJudgment(b *testing.B) {
	store, cleanup := setupBenchmarkDB(b)
	defer cleanup()

	ctx := context.Background()
	judgment := &core.Judgment{
		ID:        "bench-judgment",
		SoulID:    "bench-soul",
		Status:    core.SoulAlive,
		Duration:  100 * time.Millisecond,
		Timestamp: time.Now(),
		Region:    "bench-region",
		JackalID:  "bench-jackal",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		judgment.ID = fmt.Sprintf("bench-judgment-%d", i)
		_ = store.SaveJudgment(ctx, judgment)
	}
}

// BenchmarkGetSoul benchmarks soul retrieval
func BenchmarkGetSoul(b *testing.B) {
	store, cleanup := setupBenchmarkDB(b)
	defer cleanup()

	ctx := context.Background()

	// Create test soul
	soul := &core.Soul{
		ID:          "bench-soul",
		Name:        "Benchmark Soul",
		Type:        core.CheckHTTP,
		Target:      "http://localhost:8080",
		WorkspaceID: "default",
		Enabled:     true,
	}
	_ = store.SaveSoul(ctx, soul)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = store.GetSoul(ctx, "default", "bench-soul")
	}
}

// BenchmarkSaveSoul benchmarks soul storage
func BenchmarkSaveSoul(b *testing.B) {
	store, cleanup := setupBenchmarkDB(b)
	defer cleanup()

	ctx := context.Background()
	soul := &core.Soul{
		Name:        "Benchmark Soul",
		Type:        core.CheckHTTP,
		Target:      "http://localhost:8080",
		WorkspaceID: "default",
		Enabled:     true,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		soul.ID = fmt.Sprintf("bench-soul-%d", i)
		_ = store.SaveSoul(ctx, soul)
	}
}

// BenchmarkListSouls benchmarks soul listing
func BenchmarkListSouls(b *testing.B) {
	store, cleanup := setupBenchmarkDB(b)
	defer cleanup()

	ctx := context.Background()

	// Create test souls
	for i := 0; i < 100; i++ {
		soul := &core.Soul{
			ID:          fmt.Sprintf("bench-soul-%d", i),
			Name:        fmt.Sprintf("Soul %d", i),
			Type:        core.CheckHTTP,
			Target:      "http://localhost:8080",
			WorkspaceID: "default",
			Enabled:     true,
		}
		_ = store.SaveSoul(ctx, soul)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = store.ListSouls(ctx, "default", 0, 50)
	}
}

// BenchmarkSaveAlertEvent benchmarks alert event storage
func BenchmarkSaveAlertEvent(b *testing.B) {
	store, cleanup := setupBenchmarkDB(b)
	defer cleanup()

	event := &core.AlertEvent{
		ID:        "bench-event",
		SoulID:    "bench-soul",
		SoulName:  "Bench Soul",
		Status:    core.SoulDead,
		Severity:  core.SeverityCritical,
		Timestamp: time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		event.ID = fmt.Sprintf("bench-event-%d", i)
		_ = store.SaveAlertEvent(event)
	}
}
