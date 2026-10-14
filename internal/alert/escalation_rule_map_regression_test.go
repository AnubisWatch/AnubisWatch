package alert

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// checkEscalations used to copy the rule map reference under RLock and then index that
// map once per incident after releasing the lock. Assigning a map copies only the
// header, so those reads happened on the live map — the same map RegisterRule and
// UnregisterRule write under m.mu. A rule edit concurrent with an escalation check is
// therefore a data race on a Go map, which the runtime aborts with
// "fatal error: concurrent map read and map write": the monitoring server dies because
// an operator edited a rule while an incident was being escalated.
//
// This test widens the unlocked read window with many incidents while a writer churns
// rules, so the race is exercised rather than hoped for. It is meaningful with and
// without -race: Go's runtime detects a concurrent map read and write itself.

func ruleMapRaceManager(t *testing.T, incidents int) *Manager {
	t.Helper()

	manager := NewManager(&mockAlertStorage{}, newTestLogger())
	// Start() is not called: the test drives checkEscalations directly.

	now := time.Now()
	manager.mu.Lock()
	for i := 0; i < incidents; i++ {
		id := fmt.Sprintf("inc-%06d", i)
		manager.incidents[id] = &core.Incident{
			ID:              id,
			RuleID:          "rule-1",
			Status:          core.IncidentOpen,
			StartedAt:       now.Add(-time.Hour),
			EscalationLevel: 0,
		}
	}
	manager.channels["ch-1"] = &core.AlertChannel{ID: "ch-1", Name: "regression-channel", Type: core.ChannelWebHook, Enabled: true}
	manager.rules["rule-1"] = &core.AlertRule{
		ID:      "rule-1",
		Name:    "regression-rule",
		Enabled: true,
		// Wait longer than the test runs, so shouldEscalate stays false and this test
		// exercises the map access rather than the escalation itself.
		Escalation: &core.EscalationPolicy{Stages: []core.EscalationStage{{
			Wait:     core.Duration{Duration: time.Hour},
			Channels: []string{"ch-1"},
		}}},
	}
	manager.mu.Unlock()

	return manager
}

// TestEscalationCheckReadsRulesUnderTheLock pins that the escalation checker does not
// touch the rule map after releasing the mutex.
func TestEscalationCheckReadsRulesUnderTheLock(t *testing.T) {
	manager := ruleMapRaceManager(t, 20000)

	// A rule edit is ordinary operation: the API registers and removes rules at any time.
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			manager.mu.Lock()
			manager.rules[fmt.Sprintf("churn-%d", i%16)] = &core.AlertRule{ID: fmt.Sprintf("churn-%d", i%16)}
			delete(manager.rules, fmt.Sprintf("churn-%d", (i+8)%16))
			manager.mu.Unlock()
		}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		manager.checkEscalations()
	}

	close(stop)
	wg.Wait()
}

// TestEscalationStillEscalatesTheRightIncidents is the neighbouring control: the rule
// resolution moved under the lock, so escalation must still reach the configured stage.
func TestEscalationStillEscalatesTheRightIncidents(t *testing.T) {
	manager := ruleMapRaceManager(t, 1)
	manager.mu.Lock()
	manager.rules["rule-1"].Escalation.Stages[0].Wait = core.Duration{Duration: 0}
	manager.incidents["inc-000000"].StartedAt = time.Now().Add(-time.Hour)
	manager.mu.Unlock()

	manager.checkEscalations()

	manager.mu.RLock()
	level := manager.incidents["inc-000000"].EscalationLevel
	manager.mu.RUnlock()
	if level != 1 {
		t.Fatalf("the incident was not escalated: level = %d, want 1", level)
	}
}
