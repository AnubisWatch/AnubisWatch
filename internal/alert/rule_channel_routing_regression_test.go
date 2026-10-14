package alert

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// A rule's alert must reach only the channels the rule names. dispatch used to
// send every alert to every enabled channel in the workspace and never read
// rule.Channels, so a rule routed to an on-call pager also paged every Slack,
// email and webhook channel in the workspace, and vice versa.

type routingRecorder struct {
	mu       sync.Mutex
	channels []string
}

func (r *routingRecorder) Send(_ context.Context, _ *core.AlertEvent, ch *core.AlertChannel) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.channels = append(r.channels, ch.ID)
	return nil
}

func (r *routingRecorder) Validate(map[string]any) error { return nil }

func (r *routingRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := append([]string(nil), r.channels...)
	sort.Strings(out)
	return out
}

// waitDelivered waits for want deliveries, then a little longer so any extra,
// misrouted delivery would also be observed.
func (r *routingRecorder) waitDelivered(want int) []string {
	deadline := time.Now().Add(5 * time.Second)
	for len(r.snapshot()) < want && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond)
	return r.snapshot()
}

func routingRule(id, soulID string, channels ...string) *core.AlertRule {
	return &core.AlertRule{
		ID:         id,
		Name:       id,
		Enabled:    true,
		Scope:      core.RuleScope{Type: "specific", SoulIDs: []string{soulID}},
		Conditions: []core.AlertCondition{{Type: "consecutive_failures", Threshold: 1}},
		Channels:   channels,
	}
}

func routingManager(t *testing.T, recorder *routingRecorder) *Manager {
	t.Helper()

	channel := func(id string) *core.AlertChannel {
		return &core.AlertChannel{ID: id, Name: id, Type: core.ChannelWebHook, Enabled: true}
	}
	storage := &mockAlertStorage{
		channels: map[string]*core.AlertChannel{
			"pager": channel("pager"),
			"slack": channel("slack"),
			"email": channel("email"),
		},
		rules: map[string]*core.AlertRule{
			"to-pager": routingRule("to-pager", "soul-a", "pager"),
			// A repeated channel ID must not deliver twice; an unknown one is skipped.
			"to-slack": routingRule("to-slack", "soul-b", "slack", "slack", "deleted-channel"),
		},
	}

	manager := NewManager(storage, newTestLogger())
	manager.dispatchers[core.ChannelWebHook] = recorder
	if err := manager.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = manager.Stop() })
	return manager
}

func failJudgment(manager *Manager, soulID string) {
	soul := &core.Soul{ID: soulID, WorkspaceID: "default", Name: soulID, Type: core.CheckHTTP}
	manager.ProcessJudgment(soul, core.SoulAlive, &core.Judgment{
		SoulID: soulID, Status: core.SoulDead, Message: "down", Duration: time.Second,
	})
}

func TestAlertIsDeliveredOnlyToTheRulesChannels(t *testing.T) {
	recorder := &routingRecorder{}
	manager := routingManager(t, recorder)

	failJudgment(manager, "soul-a")
	if got := recorder.waitDelivered(1); len(got) != 1 || got[0] != "pager" {
		t.Fatalf("rule routed to [pager] delivered to %v", got)
	}
}

func TestRuleChannelListIsDeduplicatedAndSkipsMissingChannels(t *testing.T) {
	recorder := &routingRecorder{}
	manager := routingManager(t, recorder)

	failJudgment(manager, "soul-b")
	if got := recorder.waitDelivered(1); len(got) != 1 || got[0] != "slack" {
		t.Fatalf("rule routed to [slack slack deleted-channel] delivered to %v", got)
	}
}

func TestAlertForDeletedRuleIsNotBroadcast(t *testing.T) {
	recorder := &routingRecorder{}
	manager := routingManager(t, recorder)

	manager.dispatch(&core.AlertEvent{
		RuleID: "no-such-rule", SoulID: "soul-a", WorkspaceID: "default", Status: core.SoulDead,
	})
	if got := recorder.waitDelivered(0); len(got) != 0 {
		t.Fatalf("event from a deleted rule was broadcast to %v", got)
	}
}
