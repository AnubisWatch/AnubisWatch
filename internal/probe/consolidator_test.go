package probe

import (
	"context"
	"sync"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// stubConsolidator answers every judgment the same way.
type stubConsolidator struct {
	mu      sync.Mutex
	publish bool
	status  core.SoulStatus
	calls   int
}

func (c *stubConsolidator) Consolidate(_ *core.Soul, j *core.Judgment) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.publish && c.status != "" {
		j.Status = c.status
	}
	return c.publish
}

func runConsolidated(t *testing.T, c Consolidator, rawStatus core.SoulStatus) (*recordingProbeStorage, *auditStatusAlerter, *soulRunner) {
	t.Helper()
	checker := &auditStatusChecker{status: rawStatus}
	registry := NewCheckerRegistry()
	registry.Register(checker)
	store := &recordingProbeStorage{souls: map[string]*core.Soul{}}
	alerter := &auditStatusAlerter{}
	e := NewEngine(EngineOptions{Registry: registry, Store: store, Alerter: alerter, Logger: newTestProbeLogger()})
	defer e.Stop()
	e.SetConsolidator(c)
	soul := &core.Soul{ID: "redundant", Type: checker.Type()}
	runner := &soulRunner{soul: soul, lastStatus: core.SoulUnknown}
	e.souls[soul.ID] = runner
	e.judgeSoul(context.Background(), runner)
	return store, alerter, runner
}

func TestConsolidatorVoteIsNotStoredOrAlertedOn(t *testing.T) {
	c := &stubConsolidator{publish: false}
	store, alerter, runner := runConsolidated(t, c, core.SoulDead)

	if c.calls != 1 {
		t.Fatalf("consolidator called %d times, want 1", c.calls)
	}
	if n := store.judgmentCount(); n != 0 {
		t.Fatalf("%d judgments stored for a vote, want 0", n)
	}
	if len(alerter.previous) != 0 {
		t.Fatalf("alert rules evaluated %d times for a vote, want 0", len(alerter.previous))
	}
	if got := runner.getStatus(); got != core.SoulUnknown {
		t.Fatalf("a vote moved the soul's status to %q", got)
	}
}

func TestConsolidatorQuorumStatusReplacesTheRawStatus(t *testing.T) {
	store, alerter, runner := runConsolidated(t, &stubConsolidator{publish: true, status: core.SoulDegraded}, core.SoulDead)

	j := store.latestJudgment()
	if j == nil || j.Status != core.SoulDegraded {
		t.Fatalf("stored judgment = %+v, want the quorum's degraded", j)
	}
	if len(alerter.previous) != 1 {
		t.Fatalf("alert rules evaluated %d times, want 1", len(alerter.previous))
	}
	if got := runner.getStatus(); got != core.SoulDegraded {
		t.Fatalf("runner status = %q, want degraded", got)
	}
}

func TestNoConsolidatorLeavesJudgmentsAlone(t *testing.T) {
	store, _, _ := runConsolidated(t, nil, core.SoulDead)
	if j := store.latestJudgment(); j == nil || j.Status != core.SoulDead {
		t.Fatalf("stored judgment = %+v, want the raw dead status", j)
	}
}
