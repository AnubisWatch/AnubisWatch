package alert

import (
	"errors"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"testing"
	"time"
)

type rateHistoryStorage struct {
	*mockAlertStorage
	judgments []*core.Judgment
	err       error
}

func (s *rateHistoryStorage) ListAlertJudgments(_, _ string, _, _ time.Time) ([]*core.Judgment, error) {
	return s.judgments, s.err
}

func TestFailureRateUsesWindowAndThreshold(t *testing.T) {
	end := time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)
	sample := func(id string, status core.SoulStatus, ago time.Duration) *core.Judgment {
		return &core.Judgment{ID: id, SoulID: "s", WorkspaceID: "default", Status: status, Timestamp: end.Add(-ago)}
	}
	for _, tc := range []struct {
		name      string
		history   []*core.Judgment
		threshold int
		want      bool
	}{
		{"below threshold", []*core.Judgment{sample("a", core.SoulAlive, time.Second), sample("b", core.SoulAlive, 2*time.Second)}, 50, false},
		{"at threshold", []*core.Judgment{sample("a", core.SoulAlive, time.Second)}, 50, true},
		{"above threshold", []*core.Judgment{sample("a", core.SoulDead, time.Second)}, 50, true},
		{"expired failures excluded", []*core.Judgment{sample("a", core.SoulDead, 2*time.Hour), sample("b", core.SoulAlive, time.Second), sample("c", core.SoulAlive, 2*time.Second)}, 50, false},
		{"future failures excluded", []*core.Judgment{sample("a", core.SoulDead, -time.Hour), sample("b", core.SoulAlive, time.Second), sample("c", core.SoulAlive, 2*time.Second)}, 50, false},
		{"boundary included", []*core.Judgment{sample("a", core.SoulAlive, time.Hour)}, 50, true},
		{"unknown excluded", []*core.Judgment{sample("a", core.SoulUnknown, time.Second), sample("b", core.SoulEmbalmed, 2*time.Second)}, 100, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &rateHistoryStorage{judgments: tc.history}
			manager := NewManager(store, newTestLogger())
			cond := core.AlertCondition{Type: "failure_rate", Threshold: tc.threshold, Window: core.Duration{Duration: time.Hour}}
			current := sample("now", core.SoulDead, 0)
			if got := manager.failureRateCondition(cond, current); got != tc.want {
				t.Fatalf("trigger=%v want %v", got, tc.want)
			}
			// Including the current result in persistence must not double count it.
			store.judgments = append(store.judgments, current)
			if got := manager.failureRateCondition(cond, current); got != tc.want {
				t.Fatalf("persisted current double counted: trigger=%v", got)
			}
			if got := manager.evaluateCondition(cond, core.SoulAlive, current); got != tc.want {
				t.Fatalf("compound condition differs: %v", got)
			}
		})
	}
}
func TestFailureRateHistoryFailureAndTenantIsolation(t *testing.T) {
	now := time.Now()
	current := &core.Judgment{ID: "now", SoulID: "s", WorkspaceID: "default", Status: core.SoulAlive, Timestamp: now}
	other := &core.Judgment{ID: "other", SoulID: "s", WorkspaceID: "tenant-b", Status: core.SoulDead, Timestamp: now.Add(-time.Second)}
	store := &rateHistoryStorage{judgments: []*core.Judgment{other}}
	manager := NewManager(store, newTestLogger())
	cond := core.AlertCondition{Type: "failure_rate", Threshold: 1}
	if manager.failureRateCondition(cond, current) {
		t.Fatal("other tenant entered the denominator")
	}
	store.err = errors.New("disk read failed")
	current.Status = core.SoulDead
	if manager.failureRateCondition(cond, current) {
		t.Fatal("storage failure fabricated an error rate")
	}
}
func TestFailureRatePrunesUnneededHistory(t *testing.T) {
	manager := NewManager(nil, newTestLogger())
	now := time.Now()
	rule := &core.AlertRule{Enabled: true, Conditions: []core.AlertCondition{{Type: "failure_rate", Threshold: 50, Window: core.Duration{Duration: time.Minute}}}}
	old := &core.Judgment{SoulID: "s", Status: core.SoulDead, Timestamp: now.Add(-2 * time.Minute)}
	manager.noteRateJudgment(old, []*core.AlertRule{rule})
	latest := *old
	latest.Status = core.SoulAlive
	latest.Timestamp = now
	manager.noteRateJudgment(&latest, []*core.AlertRule{rule})
	if len(manager.rateSamples[rateKey(&latest)]) != 1 {
		t.Fatal("expired observations retained")
	}
	if manager.failureRateCondition(rule.Conditions[0], &latest) {
		t.Fatal("expired failure triggered rule")
	}
	manager.noteRateJudgment(&latest, nil)
	if len(manager.rateSamples) != 0 {
		t.Fatal("removed rule retained observations")
	}
}
