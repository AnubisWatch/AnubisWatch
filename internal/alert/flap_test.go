package alert

import (
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func drainQueue(m *Manager) []*core.AlertEvent {
	var out []*core.AlertEvent
	for {
		select {
		case e := <-m.queue:
			out = append(out, e)
		default:
			return out
		}
	}
}

func flapJudgment(status core.SoulStatus, flapping bool, change string) *core.Judgment {
	j := &core.Judgment{SoulID: "flap-soul", Status: status, Timestamp: time.Now()}
	if flapping || change != "" {
		j.Details = &core.JudgmentDetails{Flapping: flapping, FlapChange: change}
	}
	return j
}

func newFlapManager(t *testing.T) (*Manager, *core.Soul) {
	t.Helper()
	m := NewManager(nil, newTestLogger())
	rule := &core.AlertRule{
		ID: "flap-rule", Name: "down", Enabled: true, AutoResolve: true,
		Scope:      core.RuleScope{Type: "all"},
		Channels:   []string{"ch"},
		Cooldown:   core.Duration{Duration: time.Hour},
		Conditions: []core.AlertCondition{{Type: "status_change", From: "alive", To: "dead"}},
	}
	if err := m.RegisterRule(rule); err != nil {
		t.Fatal(err)
	}
	return m, &core.Soul{ID: "flap-soul", Name: "api", WorkspaceID: "default"}
}

func TestFlap_SuppressesStatusAlertsAndSendsStartStopOnce(t *testing.T) {
	m, soul := newFlapManager(t)

	// Flapping starts on a dead judgment: one "started" notice, no status alert.
	m.ProcessJudgment(soul, core.SoulAlive, flapJudgment(core.SoulDead, true, core.FlapStarted))
	ev := drainQueue(m)
	if len(ev) != 1 || ev[0].Kind != core.AlertKindFlappingStarted || ev[0].Severity != core.SeverityWarning {
		t.Fatalf("flap start events = %+v, want one flapping_started", ev)
	}
	if ev[0].Details["flapping"] != core.FlapStarted {
		t.Fatalf("flap start details = %v", ev[0].Details)
	}

	// While flapping, transitions that would alert are suppressed.
	for i := 0; i < 6; i++ {
		prev, next := core.SoulDead, core.SoulAlive
		if i%2 == 1 {
			prev, next = core.SoulAlive, core.SoulDead
		}
		m.ProcessJudgment(soul, prev, flapJudgment(next, true, ""))
	}
	if ev := drainQueue(m); len(ev) != 0 {
		t.Fatalf("flapping soul produced %d alerts, want 0", len(ev))
	}
	if n := len(m.ListActiveIncidents()); n != 0 {
		t.Fatalf("flapping opened %d incidents, want 0", n)
	}

	// A repeated "started" inside the rule cooldown is deduplicated.
	m.ProcessJudgment(soul, core.SoulDead, flapJudgment(core.SoulAlive, true, core.FlapStarted))
	if ev := drainQueue(m); len(ev) != 0 {
		t.Fatalf("duplicate flap start produced %d events", len(ev))
	}

	// Stabilizing sends exactly one "stopped" notice.
	m.ProcessJudgment(soul, core.SoulAlive, flapJudgment(core.SoulAlive, false, core.FlapStopped))
	ev = drainQueue(m)
	if len(ev) != 1 || ev[0].Kind != core.AlertKindFlappingStopped || ev[0].Severity != core.SeverityInfo {
		t.Fatalf("flap stop events = %+v, want one flapping_stopped", ev)
	}

	// After stabilizing, ordinary transitions alert again: the status alert's
	// dedup entry is separate from the flap notices sent above.
	m.ProcessJudgment(soul, core.SoulAlive, flapJudgment(core.SoulDead, false, ""))
	if ev := drainQueue(m); len(ev) != 1 || ev[0].Kind != "" {
		t.Fatalf("post-flap transition: got %+v", ev)
	}
}

func TestFlap_NoticeDedupKeyIsSeparateFromStatus(t *testing.T) {
	ev := &core.AlertEvent{RuleID: "r", SoulID: "s", Status: core.SoulDead}
	if dedupStatus(ev) != core.SoulDead {
		t.Fatalf("status alert dedup status = %s", dedupStatus(ev))
	}
	ev.Kind = core.AlertKindFlappingStarted
	if dedupStatus(ev) != core.SoulStatus(core.AlertKindFlappingStarted) {
		t.Fatalf("flap notice dedup status = %s", dedupStatus(ev))
	}
}

// Regression (review P1): a soul that goes hard down while flagged flapping
// stabilizes dead->dead. Rules must compare against the pre-flap status so
// the outage still opens an incident and alerts.
func TestFlap_HardDownWhileFlappingAlertsWhenStabilized(t *testing.T) {
	m, soul := newFlapManager(t)
	m.ProcessJudgment(soul, core.SoulAlive, flapJudgment(core.SoulDead, true, core.FlapStarted))
	m.ProcessJudgment(soul, core.SoulDead, flapJudgment(core.SoulAlive, true, ""))
	m.ProcessJudgment(soul, core.SoulAlive, flapJudgment(core.SoulDead, true, "")) // real outage begins
	for i := 0; i < 5; i++ {
		m.ProcessJudgment(soul, core.SoulDead, flapJudgment(core.SoulDead, true, ""))
	}
	drainQueue(m)
	m.ProcessJudgment(soul, core.SoulDead, flapJudgment(core.SoulDead, false, core.FlapStopped))
	for i := 0; i < 20; i++ {
		m.ProcessJudgment(soul, core.SoulDead, flapJudgment(core.SoulDead, false, ""))
	}
	statusAlerts := 0
	for _, e := range drainQueue(m) {
		if e.Kind == "" && e.Status == core.SoulDead {
			statusAlerts++
			if e.PrevStatus != core.SoulAlive {
				t.Fatalf("status alert prev = %s, want the pre-flap alive", e.PrevStatus)
			}
		}
	}
	if statusAlerts != 1 || len(m.ListActiveIncidents()) != 1 {
		t.Fatalf("hard outage after flapping: %d status alerts, %d incidents; want 1 and 1", statusAlerts, len(m.ListActiveIncidents()))
	}
}

// The engine's recorded baseline wins over the manager's memory, so a
// manager that never saw the start (ownership moved) still alerts.
func TestFlap_StabilizedUsesRecordedBaseline(t *testing.T) {
	m, soul := newFlapManager(t)
	j := flapJudgment(core.SoulDead, false, core.FlapStopped)
	j.Details.FlapBaseline = core.SoulAlive
	m.ProcessJudgment(soul, core.SoulDead, j)
	if n := len(m.ListActiveIncidents()); n != 1 {
		t.Fatalf("incidents = %d, want 1", n)
	}
}

// Reverse direction: down before flapping, up when it stabilizes. The open
// incident is resolved and a recovery rule fires against the dead baseline.
func TestFlap_RecoveredWhenStabilizedResolvesIncident(t *testing.T) {
	m, soul := newFlapManager(t)
	recovery := &core.AlertRule{ID: "recovery-rule", Name: "up", Enabled: true,
		Scope: core.RuleScope{Type: "all"}, Channels: []string{"ch"},
		Conditions: []core.AlertCondition{{Type: "recovery"}}}
	if err := m.RegisterRule(recovery); err != nil {
		t.Fatal(err)
	}
	m.ProcessJudgment(soul, core.SoulAlive, flapJudgment(core.SoulDead, false, ""))
	if n := len(m.ListActiveIncidents()); n != 1 {
		t.Fatalf("setup: incidents = %d, want 1", n)
	}
	// Flapping starts on the first recovery, so the down incident stays open.
	m.ProcessJudgment(soul, core.SoulDead, flapJudgment(core.SoulAlive, true, core.FlapStarted))
	m.ProcessJudgment(soul, core.SoulAlive, flapJudgment(core.SoulDead, true, ""))
	drainQueue(m)
	m.ProcessJudgment(soul, core.SoulAlive, flapJudgment(core.SoulAlive, false, core.FlapStopped))
	recovered := 0
	for _, e := range drainQueue(m) {
		if e.Kind == "" && e.RuleID == "recovery-rule" && e.PrevStatus == core.SoulDead {
			recovered++
		}
	}
	if recovered != 1 {
		t.Fatalf("recovery alerts = %d, want 1", recovered)
	}
	for _, inc := range m.ListActiveIncidents() {
		if inc.RuleID == "flap-rule" {
			t.Fatalf("down incident still open after stabilizing alive: %+v", inc)
		}
	}
}
