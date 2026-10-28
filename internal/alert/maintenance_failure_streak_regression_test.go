package alert

import (
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// A consecutive_failures rule counts an unbroken run of dead checks. Checks
// made during a maintenance window are suppressed, but they still happen, and a
// healthy one still breaks the run. ProcessJudgment returned before the streak
// was updated, so a soul that failed before maintenance, recovered during it
// and failed once after it was reported as having failed N+1 times in a row.
func TestMaintenanceDoesNotHideARecoveryFromTheFailureStreak(t *testing.T) {
	storage := &mockAlertStorage{
		channels:  map[string]*core.AlertChannel{},
		rules:     map[string]*core.AlertRule{},
		incidents: map[string]*core.Incident{},
	}
	m := NewManager(storage, newTestLogger())
	if err := m.RegisterRule(&core.AlertRule{
		ID: "rule", Name: "rule", Enabled: true, WorkspaceID: "default",
		Conditions: []core.AlertCondition{{Type: "consecutive_failures", Threshold: 3}},
		Channels:   []string{"c"},
	}); err != nil {
		t.Fatal(err)
	}
	soul := &core.Soul{ID: "soul", Name: "soul", WorkspaceID: "default", Type: core.CheckHTTP}
	judge := func(status core.SoulStatus) {
		m.ProcessJudgment(soul, core.SoulAlive, &core.Judgment{SoulID: "soul", Status: status, Message: "x", Duration: time.Second})
	}
	queued := func() bool {
		select {
		case <-m.queue:
			return true
		default:
			return false
		}
	}
	setMaintenance := func(active bool) {
		storage.maintenance = nil
		if active {
			now := time.Now().UTC()
			storage.maintenance = []*core.MaintenanceWindow{{
				ID: "w", Name: "w", WorkspaceID: "default", Enabled: true,
				StartTime: now.Add(-time.Hour), EndTime: now.Add(time.Hour),
			}}
		}
	}

	// Control: three dead checks in a row, no maintenance, alert on the third.
	judge(core.SoulDead)
	judge(core.SoulDead)
	if queued() {
		t.Fatal("control: alert fired before the third consecutive failure")
	}
	judge(core.SoulDead)
	if !queued() {
		t.Fatal("control: no alert after three consecutive failures")
	}

	// Fresh manager state for the real scenario: two failures, then a healthy
	// check during maintenance, then one failure after it.
	m2 := NewManager(storage, newTestLogger())
	if err := m2.RegisterRule(&core.AlertRule{
		ID: "rule", Name: "rule", Enabled: true, WorkspaceID: "default",
		Conditions: []core.AlertCondition{{Type: "consecutive_failures", Threshold: 3}},
		Channels:   []string{"c"},
	}); err != nil {
		t.Fatal(err)
	}
	m = m2
	setMaintenance(false)
	judge(core.SoulDead)
	judge(core.SoulDead)
	setMaintenance(true)
	judge(core.SoulAlive) // recovered, but only seen inside the window
	setMaintenance(false)
	judge(core.SoulDead) // first failure of a NEW run
	if queued() {
		t.Fatalf("alert fired on the first failure after a recovery: the healthy check during maintenance did not reset the failure streak (streak=%d)",
			m.failureStreakCount("rule", "soul"))
	}
}
