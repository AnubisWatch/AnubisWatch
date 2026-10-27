package alert

// Regression test: checkEscalations must evaluate shouldEscalate while
// holding m.mu.RLock. It reads incident fields (AckedAt, LastEscalatedAt,
// EscalationLevel) that updateIncident (AcknowledgeIncident/ResolveIncident)
// and escalateIncident write under the write lock; deciding after RUnlock
// raced with an acknowledge and was caught by the race detector.
// Run with -race.

import (
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestEscalationCheckDoesNotRaceWithAcknowledge(t *testing.T) {
	m := NewManager(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	rule := &core.AlertRule{
		ID:   "rule-esc",
		Name: "esc",
		Escalation: &core.EscalationPolicy{Stages: []core.EscalationStage{
			{Wait: core.Duration{Duration: time.Nanosecond}, Condition: "not_acknowledged"},
		}},
	}
	incident := &core.Incident{
		ID:          "inc-1",
		RuleID:      rule.ID,
		SoulID:      "soul-1",
		WorkspaceID: "default",
		Status:      core.IncidentOpen,
		StartedAt:   time.Now().Add(-time.Hour),
	}

	m.mu.Lock()
	m.rules[rule.ID] = rule
	m.incidents[incident.ID] = incident
	m.mu.Unlock()

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(2)

	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			m.checkEscalations()
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			_ = m.AcknowledgeIncident("inc-1", "user", "default")
			if i%50 == 0 { // reopen so shouldEscalate keeps reading AckedAt
				m.mu.Lock()
				incident.Status = core.IncidentOpen
				m.mu.Unlock()
			}
		}
	}()

	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()
}
