package probe

import (
	"context"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"testing"
)

type auditStatusChecker struct{ status core.SoulStatus }

func (*auditStatusChecker) Type() core.CheckType      { return core.CheckType("audit-status") }
func (*auditStatusChecker) Validate(*core.Soul) error { return nil }
func (c *auditStatusChecker) Judge(context.Context, *core.Soul) (*core.Judgment, error) {
	return &core.Judgment{Status: c.status}, nil
}

type auditStatusAlerter struct{ previous []core.SoulStatus }

func (a *auditStatusAlerter) ProcessJudgment(_ *core.Soul, previous core.SoulStatus, _ *core.Judgment) {
	a.previous = append(a.previous, previous)
}
func auditOptionalAlerter(t *testing.T, immediate, withAlerter bool, statuses []core.SoulStatus) {
	t.Helper()
	checker := &auditStatusChecker{}
	registry := NewCheckerRegistry()
	registry.Register(checker)
	alerter := &auditStatusAlerter{}
	opts := EngineOptions{Registry: registry, Logger: newTestProbeLogger()}
	if withAlerter {
		opts.Alerter = alerter
	}
	e := NewEngine(opts)
	defer e.Stop()
	soul := &core.Soul{ID: "status-soul", Type: checker.Type()}
	runner := &soulRunner{soul: soul, lastStatus: core.SoulUnknown}
	e.souls[soul.ID] = runner
	previous := core.SoulUnknown
	for _, want := range statuses {
		checker.status = want
		if immediate {
			if _, err := e.TriggerImmediate(context.Background(), soul.ID); err != nil {
				t.Fatal(err)
			}
		} else {
			e.judgeSoul(context.Background(), runner)
		}
		got, err := e.GetSoulStatus(soul.ID)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("immediate=%v alerter=%v EXPECTED: status=%s ACTUAL: status=%s", immediate, withAlerter, want, *got)
		if *got != want {
			t.Fatal("regression mismatch")
		}
		if withAlerter && alerter.previous[len(alerter.previous)-1] != previous {
			t.Fatal("previous status mismatch")
		}
		previous = want
	}
}
func TestAuditOptionalAlerterStatus(t *testing.T) {
	t.Run("alerter-control", func(t *testing.T) { auditOptionalAlerter(t, false, true, []core.SoulStatus{core.SoulAlive}) })
	t.Run("no-alerter", func(t *testing.T) { auditOptionalAlerter(t, false, false, []core.SoulStatus{core.SoulAlive}) })
	t.Run("immediate-no-alerter", func(t *testing.T) {
		auditOptionalAlerter(t, true, false, []core.SoulStatus{core.SoulAlive, core.SoulDead, core.SoulDegraded})
	})
	t.Run("scheduled-transitions", func(t *testing.T) {
		auditOptionalAlerter(t, false, false, []core.SoulStatus{core.SoulDead, core.SoulAlive, core.SoulDegraded})
	})
	t.Run("immediate-previous-status", func(t *testing.T) {
		auditOptionalAlerter(t, true, true, []core.SoulStatus{core.SoulDead, core.SoulAlive})
	})
}
