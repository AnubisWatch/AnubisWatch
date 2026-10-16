package alert

import (
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Regression: the manager had no way to tell anyone that an incident opened,
// was acknowledged or resolved, or that an alert went out, so the dashboard
// never received live incident/alert events.

type hookRecorder struct {
	mu        sync.Mutex
	incidents []core.Incident
	alerts    []core.AlertEvent
}

func (r *hookRecorder) incidentStatuses() []core.IncidentStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]core.IncidentStatus, 0, len(r.incidents))
	for _, inc := range r.incidents {
		out = append(out, inc.Status)
	}
	return out
}

func newHookedManager(t *testing.T, rec *hookRecorder) *Manager {
	t.Helper()
	storage := &mockAlertStorage{
		channels:  map[string]*core.AlertChannel{},
		rules:     map[string]*core.AlertRule{},
		incidents: map[string]*core.Incident{},
	}
	manager := NewManager(storage, newTestLogger())
	rule := routingRule("r1", "soul-1")
	rule.AutoResolve = true
	if err := manager.RegisterRule(rule); err != nil {
		t.Fatal(err)
	}
	manager.SetIncidentHook(func(inc *core.Incident) {
		// The hook runs outside the manager's locks, so it may read back.
		_ = manager.ListIncidents()
		rec.mu.Lock()
		rec.incidents = append(rec.incidents, *inc)
		rec.mu.Unlock()
	})
	manager.SetAlertHook(func(event *core.AlertEvent) {
		rec.mu.Lock()
		rec.alerts = append(rec.alerts, *event)
		rec.mu.Unlock()
	})
	return manager
}

func equalStatuses(got, want []core.IncidentStatus) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestIncidentHook_FiresOnEveryLifecycleChange(t *testing.T) {
	cases := []struct {
		name  string
		close func(m *Manager, id string) error
		want  []core.IncidentStatus
	}{
		{"acknowledge then resolve", func(m *Manager, id string) error {
			if err := m.AcknowledgeIncident(id, "u1", "default"); err != nil {
				return err
			}
			return m.ResolveIncident(id, "u1", "default")
		}, []core.IncidentStatus{core.IncidentOpen, core.IncidentAcked, core.IncidentResolved}},
		{"auto-resolve on recovery", func(m *Manager, _ string) error {
			soul := &core.Soul{ID: "soul-1", WorkspaceID: "default", Name: "soul-1", Type: core.CheckHTTP}
			m.ProcessJudgment(soul, core.SoulDead, &core.Judgment{SoulID: "soul-1", Status: core.SoulAlive})
			return nil
		}, []core.IncidentStatus{core.IncidentOpen, core.IncidentResolved}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &hookRecorder{}
			manager := newHookedManager(t, rec)

			failJudgment(manager, "soul-1")
			// A second failure appends to the open incident; it is not a
			// lifecycle change and must not be reported again.
			failJudgment(manager, "soul-1")
			incidents := manager.ListIncidents()
			if len(incidents) != 1 {
				t.Fatalf("expected one incident, got %d", len(incidents))
			}
			if err := tc.close(manager, incidents[0].ID); err != nil {
				t.Fatal(err)
			}

			if got := rec.incidentStatuses(); !equalStatuses(got, tc.want) {
				t.Fatalf("hook saw %v, want %v", got, tc.want)
			}
			rec.mu.Lock()
			first := rec.incidents[0]
			rec.mu.Unlock()
			if first.SoulName != "soul-1" || first.WorkspaceID != "default" || first.Severity == "" {
				t.Fatalf("hook snapshot lacks routing/display fields: %+v", first)
			}
		})
	}
}

func TestAlertHook_FiresWhenAnAlertIsDispatched(t *testing.T) {
	rec := &hookRecorder{}
	manager := newHookedManager(t, rec)
	if err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Stop() })

	failJudgment(manager, "soul-1")

	deadline := time.Now().Add(5 * time.Second)
	for {
		rec.mu.Lock()
		n := len(rec.alerts)
		rec.mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.alerts) != 1 {
		t.Fatalf("expected one dispatched alert, got %d", len(rec.alerts))
	}
	if got := rec.alerts[0]; got.SoulID != "soul-1" || got.WorkspaceID != "default" || got.Status != core.SoulDead {
		t.Fatalf("unexpected alert event: %+v", got)
	}
}
