package probe

import (
	"io"
	"log/slog"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func newCovTestProbeLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestSetOnJudgment verifies the callback setter works correctly
// including the nil-clearing path.
func TestSetOnJudgment(t *testing.T) {
	engine := NewEngine(EngineOptions{
		Registry: NewCheckerRegistry(),
		Logger:   newCovTestProbeLogger(),
	})

	// Initially nil
	cb := engine.onJudgment.Load()
	if cb != nil {
		t.Error("Expected nil callback initially")
	}

	// Set a callback
	called := false
	engine.SetOnJudgment(func(j *core.Judgment) {
		called = true
	})

	cb = engine.onJudgment.Load()
	if cb == nil {
		t.Fatal("Expected non-nil callback after SetOnJudgment")
	}
	(*cb)(&core.Judgment{})
	if !called {
		t.Error("Callback was not invoked")
	}

	// Clear with nil
	engine.SetOnJudgment(nil)
	cb = engine.onJudgment.Load()
	if cb != nil {
		t.Error("Expected nil callback after clearing")
	}
}

// TestEngineStats verifies engine statistics are correctly tracked.
func TestEngineStats(t *testing.T) {
	engine := NewEngine(EngineOptions{
		Registry: NewCheckerRegistry(),
		Logger:   newCovTestProbeLogger(),
	})

	stats := engine.Stats()
	if stats == nil {
		t.Fatal("Expected non-nil stats")
	}
	if _, ok := stats["active_souls"]; !ok {
		t.Error("Expected active_souls in stats")
	}
	if _, ok := stats["total_checks"]; !ok {
		t.Error("Expected total_checks in stats")
	}
}

// TestEngineConfig verifies the config is returned correctly.
func TestEngineConfig(t *testing.T) {
	engine := NewEngine(EngineOptions{
		Registry: NewCheckerRegistry(),
		Logger:   newCovTestProbeLogger(),
		Config: EngineConfig{
			MaxConcurrentChecks: 50,
			CircuitBreaker: CircuitBreakerConfig{
				Enabled:          true,
				FailureThreshold: 3,
			},
		},
	})

	cfg := engine.Config()
	if cfg.MaxConcurrentChecks != 50 {
		t.Errorf("Expected 50, got %d", cfg.MaxConcurrentChecks)
	}
	if !cfg.CircuitBreaker.Enabled {
		t.Error("Expected circuit breaker enabled")
	}
}
