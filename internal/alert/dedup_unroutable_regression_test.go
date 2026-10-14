package alert

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// An alert that no channel could even be attempted for must keep its dedup
// entry. Releasing it is right after a failed send (the channel may recover),
// but when the rule names no existing channel, or every channel is disabled,
// retrying cannot help: releasing then reran the whole alert path, event and
// incident writes included, on every check instead of once per cooldown.

type failingDispatcher struct{ calls int }

func (d *failingDispatcher) Send(context.Context, *core.AlertEvent, *core.AlertChannel) error {
	d.calls++
	return errors.New("smtp: connection refused")
}

func (d *failingDispatcher) Validate(map[string]any) error { return nil }

// unroutableManager returns a manager with no workers running, so each test
// drives the alert path synchronously through ProcessJudgment and dispatch.
func unroutableManager(t *testing.T, ruleChannels []string, channels ...*core.AlertChannel) (*Manager, *failingDispatcher) {
	t.Helper()
	m := NewManager(nil, newTestLogger())
	d := &failingDispatcher{}
	m.dispatchers[core.ChannelWebHook] = d
	for _, ch := range channels {
		if err := m.RegisterChannel(ch); err != nil {
			t.Fatalf("RegisterChannel: %v", err)
		}
	}
	if err := m.RegisterRule(&core.AlertRule{
		ID: "rule", Name: "rule", Enabled: true, WorkspaceID: "default",
		Conditions: []core.AlertCondition{{Type: "consecutive_failures", Threshold: 1}},
		Channels:   ruleChannels,
	}); err != nil {
		t.Fatalf("RegisterRule: %v", err)
	}
	return m, d
}

// failAndDispatch runs one failing judgment and dispatches what it queued. It
// reports whether an alert was queued, i.e. whether dedup let it through.
func failAndDispatch(m *Manager) bool {
	soul := &core.Soul{ID: "soul", WorkspaceID: "default", Name: "soul", Type: core.CheckHTTP}
	m.ProcessJudgment(soul, core.SoulAlive, &core.Judgment{
		SoulID: "soul", Status: core.SoulDead, Message: "down", Duration: time.Second,
	})
	select {
	case event := <-m.queue:
		m.dispatch(event)
		return true
	default:
		return false
	}
}

func webhookChannel(id string, enabled bool) *core.AlertChannel {
	return &core.AlertChannel{ID: id, Name: id, Type: core.ChannelWebHook, Enabled: enabled, WorkspaceID: "default"}
}

func TestUnroutableAlertKeepsDedupEntry(t *testing.T) {
	cases := map[string]struct {
		ruleChannels []string
		channels     []*core.AlertChannel
	}{
		"rule names a deleted channel": {ruleChannels: []string{"deleted"}},
		"rule's only channel disabled": {ruleChannels: []string{"off"}, channels: []*core.AlertChannel{webhookChannel("off", false)}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			m, d := unroutableManager(t, tc.ruleChannels, tc.channels...)

			if !failAndDispatch(m) {
				t.Fatal("setup: the first failing judgment did not raise an alert")
			}
			for i := 0; i < 3; i++ {
				if failAndDispatch(m) {
					t.Fatalf("check %d re-raised an alert that has nowhere to go: the dedup entry was "+
						"released although no send was attempted", i+2)
				}
			}
			if d.calls != 0 {
				t.Fatalf("dispatcher called %d times for an unroutable alert", d.calls)
			}
		})
	}
}

// TestFailedSendStillReleasesDedupEntry is the neighbouring control: when a
// send was attempted and failed, the next check must retry.
func TestFailedSendStillReleasesDedupEntry(t *testing.T) {
	m, d := unroutableManager(t, []string{"on"}, webhookChannel("on", true))

	if !failAndDispatch(m) {
		t.Fatal("setup: the first failing judgment did not raise an alert")
	}
	if !failAndDispatch(m) {
		t.Fatal("after a failed send the next check was deduplicated, so the outage is never reported")
	}
	if d.calls != 2 {
		t.Fatalf("dispatcher calls = %d, want 2 (one per check)", d.calls)
	}
}
