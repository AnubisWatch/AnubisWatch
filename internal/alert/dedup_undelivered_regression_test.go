package alert

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// A failed delivery must not consume the dedup window. isDuplicate's contract is
// to suppress alerts the operator has already been told about; an alert that no
// channel accepted was never sent, so recording it as sent silently swallowed the
// notification for the whole window (rule cooldown, default 5 minutes) after a
// transient channel outage.

type dedupRegressionDispatcher struct {
	mu       sync.Mutex
	failOnce bool
	calls    int
	success  int
}

func (d *dedupRegressionDispatcher) Send(context.Context, *core.AlertEvent, *core.AlertChannel) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.calls++
	if d.failOnce && d.calls == 1 {
		return errors.New("transient channel outage")
	}
	d.success++
	return nil
}

func (d *dedupRegressionDispatcher) Validate(map[string]any) error { return nil }

func (d *dedupRegressionDispatcher) counts() (calls, success int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls, d.success
}

const (
	dedupRegressionRuleID = "dedup-regression-rule"
	dedupRegressionSoulID = "dedup-regression-soul"
)

func dedupRegressionManager(t *testing.T, dispatcher *dedupRegressionDispatcher) *Manager {
	t.Helper()

	storage := &mockAlertStorage{
		channels: map[string]*core.AlertChannel{
			"dedup-regression-channel": {
				ID:      "dedup-regression-channel",
				Name:    "Dedup Regression Channel",
				Type:    core.ChannelWebHook,
				Enabled: true,
			},
		},
		rules: map[string]*core.AlertRule{
			dedupRegressionRuleID: {
				ID:      dedupRegressionRuleID,
				Name:    "Dedup Regression Rule",
				Enabled: true,
				Scope:   core.RuleScope{Type: "all"},
				Conditions: []core.AlertCondition{
					{Type: "consecutive_failures", Threshold: 1},
				},
				Channels: []string{"dedup-regression-channel"},
			},
		},
	}

	manager := NewManager(storage, newTestLogger())
	manager.dispatchers[core.ChannelWebHook] = dispatcher
	if err := manager.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = manager.Stop() })
	return manager
}

func dedupRegressionJudgment(t *testing.T, manager *Manager, dispatcher *dedupRegressionDispatcher, wantCalls int) (calls, success int) {
	t.Helper()

	soul := &core.Soul{ID: dedupRegressionSoulID, WorkspaceID: "default", Name: "Dedup Soul", Type: core.CheckHTTP}
	manager.ProcessJudgment(soul, core.SoulAlive, &core.Judgment{
		SoulID: dedupRegressionSoulID, Status: core.SoulDead, Message: "down", Duration: time.Second,
	})

	deadline := time.Now().Add(5 * time.Second)
	for {
		calls, success = dispatcher.counts()
		if calls >= wantCalls || time.Now().After(deadline) {
			return calls, success
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestUndeliveredAlertDoesNotConsumeDedupWindow pins the contract: after a failed
// delivery the same alert must still reach the channel.
func TestUndeliveredAlertDoesNotConsumeDedupWindow(t *testing.T) {
	dispatcher := &dedupRegressionDispatcher{failOnce: true}
	manager := dedupRegressionManager(t, dispatcher)

	calls, success := dedupRegressionJudgment(t, manager, dispatcher, 1)
	if calls != 1 || success != 0 {
		t.Fatalf("setup failed: expected one failed attempt, got calls=%d success=%d", calls, success)
	}

	calls, success = dedupRegressionJudgment(t, manager, dispatcher, 2)
	if success == 0 {
		t.Errorf("the channel was never notified although the service is down: the failed delivery consumed "+
			"the dedup window (calls=%d, successes=%d)", calls, success)
	}
}

// TestDeliveredAlertIsStillDeduplicated is the neighbouring control: a repeat the
// operator has already been told about must stay suppressed.
func TestDeliveredAlertIsStillDeduplicated(t *testing.T) {
	dispatcher := &dedupRegressionDispatcher{}
	manager := dedupRegressionManager(t, dispatcher)

	if _, success := dedupRegressionJudgment(t, manager, dispatcher, 1); success != 1 {
		t.Fatalf("the first alert was not delivered (successes=%d)", success)
	}

	dedupRegressionJudgment(t, manager, dispatcher, 2)
	time.Sleep(150 * time.Millisecond)

	if calls, _ := dispatcher.counts(); calls != 1 {
		t.Errorf("the delivered alert was not deduplicated (calls=%d)", calls)
	}
}
