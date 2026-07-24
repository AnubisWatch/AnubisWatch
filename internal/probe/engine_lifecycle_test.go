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

func TestPeriodicJudgmentCarriesSoulWorkspaceBeforePersistenceAndCallback(t *testing.T) {
	checker := &countingChecker{}
	registry := NewCheckerRegistry()
	registry.Register(checker)
	store := &recordingProbeStorage{souls: map[string]*core.Soul{}}
	callback := make(chan *core.Judgment, 1)
	engine := newTestEngine(t, EngineOptions{
		Registry: registry,
		Store:    store,
		OnJudgment: func(j *core.Judgment) {
			callback <- j
		},
	})

	soul := &core.Soul{
		ID:          "tenant-periodic",
		WorkspaceID: "tenant-a",
		Name:        "tenant-periodic",
		Type:        checker.Type(),
		Weight:      core.Duration{Duration: time.Hour},
	}
	engine.AssignSouls([]*core.Soul{soul})

	select {
	case judgment := <-callback:
		if judgment.WorkspaceID != soul.WorkspaceID {
			t.Fatalf("callback workspace = %q, want %q", judgment.WorkspaceID, soul.WorkspaceID)
		}
	case <-time.After(time.Second):
		t.Fatal("periodic judgment callback was not invoked")
	}

	stored := store.latestJudgment()
	if stored == nil {
		t.Fatal("periodic judgment was not persisted")
	}
	if stored.WorkspaceID != soul.WorkspaceID {
		t.Fatalf("stored workspace = %q, want %q", stored.WorkspaceID, soul.WorkspaceID)
	}
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
