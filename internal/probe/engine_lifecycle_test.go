package probe

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type countingChecker struct {
	count atomic.Int64
}

func (c *countingChecker) Type() core.CheckType      { return core.CheckType("counting") }
func (c *countingChecker) Validate(*core.Soul) error { return nil }
func (c *countingChecker) Judge(_ context.Context, soul *core.Soul) (*core.Judgment, error) {
	c.count.Add(1)
	return &core.Judgment{SoulID: soul.ID, Status: core.SoulAlive}, nil
}

func TestAssignSoulsRestartsRunnerWhenIntervalChanges(t *testing.T) {
	checker := &countingChecker{}
	registry := NewCheckerRegistry()
	registry.Register(checker)
	engine := newTestEngine(t, EngineOptions{Registry: registry})

	soul := &core.Soul{
		ID:     "changing-interval",
		Name:   "changing-interval",
		Type:   checker.Type(),
		Weight: core.Duration{Duration: time.Hour},
	}
	engine.AssignSouls([]*core.Soul{soul})
	deadline := time.Now().Add(time.Second)
	for checker.count.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if checker.count.Load() != 1 {
		t.Fatalf("initial runner did not execute: %d", checker.count.Load())
	}

	updated := *soul
	updated.Weight = core.Duration{Duration: 10 * time.Millisecond}
	engine.AssignSouls([]*core.Soul{&updated})
	deadline = time.Now().Add(time.Second)
	for checker.count.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if checker.count.Load() < 3 {
		t.Fatalf("updated interval was not applied; checks=%d", checker.count.Load())
	}

	engine.mu.RLock()
	interval := engine.souls[soul.ID].interval
	engine.mu.RUnlock()
	if interval != updated.Weight.Duration {
		t.Fatalf("runner interval = %v, want %v", interval, updated.Weight.Duration)
	}
}
