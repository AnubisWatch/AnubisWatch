package alert

// Regression coverage for the status_for alert condition's dwell time.
//
// core.AlertCondition documents its Duration field as "for status_for"
// (internal/core/verdict.go) and AlertRule.validate accepts the "status_for"
// type, so an operator can configure "page after the target has been dead for
// 5 minutes" through the REST API. Both status_for implementations used to
// ignore Duration and reduce the condition to an instantaneous equality test on
// the current status, so the configured dwell time was silently discarded and
// the rule paged on the very first failing judgment.
//
// These tests drive the real Manager.ProcessJudgment path with a real rule and
// assert on whether an alert event is actually queued.

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func statusForTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newStatusForManager returns a Manager with no storage and no worker. Start()
// is never called, so nothing drains m.queue and its length records exactly
// which rules decided to alert.
func newStatusForManager(t *testing.T) *Manager {
	t.Helper()
	return NewManager(nil, statusForTestLogger())
}

func statusForTestRule(id string, status core.SoulStatus, dwell time.Duration) *core.AlertRule {
	return &core.AlertRule{
		ID:          id,
		Name:        "Sustained outage",
		Enabled:     true,
		WorkspaceID: "default",
		Scope:       core.RuleScope{Type: "all"},
		Conditions: []core.AlertCondition{{
			Type:     "status_for",
			Status:   string(status),
			Duration: core.Duration{Duration: dwell},
		}},
		Channels: []string{"chan-1"},
		Severity: core.SeverityCritical,
	}
}

func statusForTestJudgment(status core.SoulStatus) (*core.Soul, *core.Judgment) {
	soul := &core.Soul{
		ID: "soul-1", Name: "API", Type: core.CheckHTTP,
		Target: "http://example.com", WorkspaceID: "default", Enabled: true,
	}
	judgment := &core.Judgment{
		ID: core.GenerateID(), SoulID: soul.ID, WorkspaceID: "default",
		Timestamp: time.Now().UTC(), Status: status,
	}
	return soul, judgment
}

func drainStatusForQueue(m *Manager) {
	for len(m.queue) > 0 {
		<-m.queue
	}
}

// TestStatusForHonoursConfiguredDuration pins that a status_for rule with a
// dwell time does not alert on the first matching judgment.
func TestStatusForHonoursConfiguredDuration(t *testing.T) {
	m := newStatusForManager(t)

	rule := statusForTestRule("rule-1", core.SoulDead, time.Hour)
	if err := rule.Validate(); err != nil {
		t.Fatalf("rule must be accepted as configured, got: %v", err)
	}
	if err := m.RegisterRule(rule); err != nil {
		t.Fatalf("RegisterRule: %v", err)
	}

	soul, judgment := statusForTestJudgment(core.SoulDead)
	m.ProcessJudgment(soul, core.SoulUnknown, judgment)

	if got := len(m.queue); got != 0 {
		t.Fatalf("status_for with duration=1h alerted on the first dead judgment (%d events); "+
			"the configured dwell time was ignored", got)
	}
}

// TestStatusForFiresAfterDwellElapsed covers the positive side: the rule must
// alert once the dwell has elapsed, and not before. A 1ms dwell against a 50ms
// gap keeps this independent of machine speed.
func TestStatusForFiresAfterDwellElapsed(t *testing.T) {
	m := newStatusForManager(t)

	if err := m.RegisterRule(statusForTestRule("rule-1", core.SoulDead, time.Millisecond)); err != nil {
		t.Fatalf("RegisterRule: %v", err)
	}

	soul, first := statusForTestJudgment(core.SoulDead)
	m.ProcessJudgment(soul, core.SoulUnknown, first)
	if got := len(m.queue); got != 0 {
		t.Fatalf("status_for alerted before its dwell elapsed (%d events)", got)
	}
	drainStatusForQueue(m)

	time.Sleep(50 * time.Millisecond)

	_, second := statusForTestJudgment(core.SoulDead)
	m.ProcessJudgment(soul, core.SoulUnknown, second)

	if got := len(m.queue); got != 1 {
		t.Fatalf("status_for should alert once its dwell elapsed, queued %d events (want 1)", got)
	}
}

// TestStatusForResetsClockOnStatusChange pins that a flapping target does not
// accumulate dwell across separate incidents. The dwell is deliberately shorter
// than the recovery gap, so this only passes if the clock restarts when the
// status changes: dead, then a recovery long enough to satisfy the dwell, then
// dead again must still wait.
func TestStatusForResetsClockOnStatusChange(t *testing.T) {
	m := newStatusForManager(t)

	if err := m.RegisterRule(statusForTestRule("rule-1", core.SoulDead, 50*time.Millisecond)); err != nil {
		t.Fatalf("RegisterRule: %v", err)
	}

	soul, dead := statusForTestJudgment(core.SoulDead)
	m.ProcessJudgment(soul, core.SoulUnknown, dead)
	drainStatusForQueue(m)

	// Recover, wait past the dwell, then fail again: a brand new outage whose
	// clock must start over rather than inherit the earlier spell.
	_, alive := statusForTestJudgment(core.SoulAlive)
	m.ProcessJudgment(soul, core.SoulDead, alive)
	drainStatusForQueue(m)

	time.Sleep(80 * time.Millisecond)

	_, deadAgain := statusForTestJudgment(core.SoulDead)
	m.ProcessJudgment(soul, core.SoulAlive, deadAgain)

	if got := len(m.queue); got != 0 {
		t.Fatalf("status_for carried dwell across a recovery and fired immediately on the next dead spell "+
			"(%d events); the clock must restart when the status changes", got)
	}
}

// TestStatusForWithoutDurationFiresImmediately is the control: a rule that
// configures no dwell keeps the original behaviour. This is the shape used by
// the existing compound and integration tests, so it must not regress.
func TestStatusForWithoutDurationFiresImmediately(t *testing.T) {
	m := newStatusForManager(t)

	if err := m.RegisterRule(statusForTestRule("rule-1", core.SoulDead, 0)); err != nil {
		t.Fatalf("RegisterRule: %v", err)
	}

	soul, judgment := statusForTestJudgment(core.SoulDead)
	m.ProcessJudgment(soul, core.SoulUnknown, judgment)

	if got := len(m.queue); got != 1 {
		t.Fatalf("status_for without a duration queued %d events (want 1)", got)
	}
}

// TestStatusForIgnoresNonMatchingStatus is a second control: a dwell time must
// not make an unrelated status fire.
func TestStatusForIgnoresNonMatchingStatus(t *testing.T) {
	m := newStatusForManager(t)

	if err := m.RegisterRule(statusForTestRule("rule-1", core.SoulDead, time.Hour)); err != nil {
		t.Fatalf("RegisterRule: %v", err)
	}

	soul, judgment := statusForTestJudgment(core.SoulAlive)
	m.ProcessJudgment(soul, core.SoulUnknown, judgment)

	if got := len(m.queue); got != 0 {
		t.Fatalf("status_for=dead fired for an alive judgment (%d events)", got)
	}
}

// TestStatusForHonoursDurationInsideCompound pins the second implementation
// site: a status_for sub-condition reached through a compound condition must
// honour its dwell time too.
func TestStatusForHonoursDurationInsideCompound(t *testing.T) {
	m := newStatusForManager(t)

	rule := &core.AlertRule{
		ID:          "rule-compound",
		Name:        "Sustained outage (compound)",
		Enabled:     true,
		WorkspaceID: "default",
		Scope:       core.RuleScope{Type: "all"},
		Conditions: []core.AlertCondition{{
			Type:  "compound",
			Logic: "and",
			SubConditions: []core.AlertCondition{
				{Type: "status_for", Status: string(core.SoulDead), Duration: core.Duration{Duration: time.Hour}},
				{Type: "status_for", Status: string(core.SoulDead), Duration: core.Duration{Duration: 0}},
			},
		}},
		Channels: []string{"chan-1"},
		Severity: core.SeverityCritical,
	}
	if err := m.RegisterRule(rule); err != nil {
		t.Fatalf("RegisterRule: %v", err)
	}

	soul, judgment := statusForTestJudgment(core.SoulDead)
	m.ProcessJudgment(soul, core.SoulUnknown, judgment)

	if got := len(m.queue); got != 0 {
		t.Fatalf("compound containing a dwell-gated status_for alerted immediately (%d events)", got)
	}
}
