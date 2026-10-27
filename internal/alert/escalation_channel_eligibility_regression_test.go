package alert

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type recordingDispatcher struct {
	mu   sync.Mutex
	sent []string
}

func (d *recordingDispatcher) Send(_ context.Context, _ *core.AlertEvent, ch *core.AlertChannel) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sent = append(d.sent, ch.ID)
	return nil
}

func (d *recordingDispatcher) Validate(map[string]any) error { return nil }

// Escalation must apply the same channel eligibility as normal dispatch: a
// disabled channel is never notified, and a channel belonging to another
// workspace never receives this workspace's incident.
func TestEscalationOnlyNotifiesEligibleChannels(t *testing.T) {
	m := NewManager(nil, newTestLogger())
	d := &recordingDispatcher{}
	m.dispatchers[core.ChannelWebHook] = d

	for _, ch := range []*core.AlertChannel{
		{ID: "on", Name: "on", Type: core.ChannelWebHook, Enabled: true, WorkspaceID: "default"},
		{ID: "off", Name: "off", Type: core.ChannelWebHook, Enabled: false, WorkspaceID: "default"},
		{ID: "foreign", Name: "foreign", Type: core.ChannelWebHook, Enabled: true, WorkspaceID: "other"},
	} {
		m.channels[ch.ID] = ch
	}
	rule := &core.AlertRule{ID: "rule", Name: "rule", Enabled: true, WorkspaceID: "default",
		Escalation: &core.EscalationPolicy{Stages: []core.EscalationStage{
			{Wait: core.Duration{Duration: time.Nanosecond}, Channels: []string{"on", "off", "foreign"}},
		}}}
	incident := &core.Incident{ID: "inc", RuleID: "rule", SoulID: "soul", SoulName: "soul",
		WorkspaceID: "default", Status: core.IncidentOpen, StartedAt: time.Now().Add(-time.Hour)}
	m.rules[rule.ID] = rule
	m.incidents[incident.ID] = incident

	m.escalateIncident(incident, rule)

	if len(d.sent) != 1 || d.sent[0] != "on" {
		t.Fatalf("escalation notified %v, want only [on] (disabled and foreign-workspace channels must be skipped)", d.sent)
	}
}
