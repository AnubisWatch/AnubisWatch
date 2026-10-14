package probe

import (
	"context"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// MaxConcurrentChecks bounds concurrent checks, and the engine reports ChecksRunning
// from the counter judgeSoul maintains. TriggerImmediate — reached from the REST
// force-check endpoint, the MCP force_check tool and the gRPC ForceCheck RPC — used to
// run its check without acquiring that semaphore, so manual checks overran the
// configured limit and were invisible in the reported running count.

type concurrencyGatedChecker struct {
	started chan struct{}
	release chan struct{}
}

func (c *concurrencyGatedChecker) Type() core.CheckType      { return core.CheckHTTP }
func (c *concurrencyGatedChecker) Validate(*core.Soul) error { return nil }

func (c *concurrencyGatedChecker) Judge(ctx context.Context, _ *core.Soul) (*core.Judgment, error) {
	select {
	case c.started <- struct{}{}:
	default:
	}
	select {
	case <-c.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &core.Judgment{ID: core.GenerateID(), Status: core.SoulAlive, Timestamp: time.Now().UTC()}, nil
}

func concurrencySoul(id string) *core.Soul {
	return &core.Soul{ID: id, Name: id, Type: core.CheckHTTP, Target: "http://127.0.0.1:9/", Enabled: true}
}

func concurrencyEngine(t *testing.T, gate *concurrencyGatedChecker) *Engine {
	t.Helper()
	registry := NewCheckerRegistry()
	registry.Register(gate)
	return NewEngine(EngineOptions{
		Registry: registry,
		Config:   EngineConfig{MaxConcurrentChecks: 1},
	})
}

// TestManualCheckWaitsForAConcurrencySlot pins that a manually triggered check obeys
// MaxConcurrentChecks instead of overrunning it.
func TestManualCheckWaitsForAConcurrencySlot(t *testing.T) {
	gate := &concurrencyGatedChecker{started: make(chan struct{}, 8), release: make(chan struct{})}
	engine := concurrencyEngine(t, gate)

	engine.AssignSouls([]*core.Soul{concurrencySoul("soul-a")})
	select {
	case <-gate.started:
	case <-time.After(2 * time.Second):
		t.Fatal("harness: the scheduled check never started")
	}

	engine.UpsertSoul(concurrencySoul("soul-b"))
	result := make(chan error, 1)
	go func() {
		_, err := engine.ForceCheck("soul-b")
		result <- err
	}()

	select {
	case <-gate.started:
		t.Fatal("a manually triggered check ran while the only concurrency slot was held")
	case <-time.After(500 * time.Millisecond):
	}

	close(gate.release)

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("the manual check failed after the slot was released: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the manual check never completed after the slot was released")
	}
}

// TestManualCheckRunsWhenSlotsAreFree is the neighbouring control: the limit must not
// stop manual checks from working when the engine is idle.
func TestManualCheckRunsWhenSlotsAreFree(t *testing.T) {
	gate := &concurrencyGatedChecker{started: make(chan struct{}, 8), release: make(chan struct{})}
	close(gate.release) // never block: the slot is free throughout
	engine := concurrencyEngine(t, gate)
	engine.AssignSouls([]*core.Soul{concurrencySoul("soul-a")})

	judgment, err := engine.ForceCheck("soul-a")
	if err != nil {
		t.Fatalf("a manual check with a free slot must run: %v", err)
	}
	if judgment == nil || judgment.Status != core.SoulAlive {
		t.Fatalf("unexpected judgment from a manual check: %+v", judgment)
	}
}
