package alert

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestOwnershipSeedReplacesEvaluationOnly(t *testing.T) {
	at := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		history []*core.Judgment
		want    int
		status  core.SoulStatus
	}{
		{"new_failure", []*core.Judgment{{Status: core.SoulDead, Timestamp: at}, {Status: core.SoulAlive, Timestamp: at.Add(-time.Second)}}, 1, core.SoulDead},
		{"recovered", []*core.Judgment{{Status: core.SoulAlive, Timestamp: at}}, 0, core.SoulAlive},
		{"empty", nil, 0, ""}, {"nil_entries", []*core.Judgment{nil}, 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewManager(nil, newTestLogger())
			m.LoadRule(&core.AlertRule{ID: "r"})
			m.LoadRule(&core.AlertRule{ID: "r2"})
			m.failureStreak["r:s"] = 9
			m.failureStreak["r2:s"] = 7
			m.failureStreak["r:other"] = 6
			m.statusSince["s"] = statusHold{Status: core.SoulDead, Since: at.Add(-time.Hour)}
			entries := []*core.AlertHistoryEntry{{Key: "dedup:r:s:dead", Count: 1, LastSent: at}, {Key: "ratelimit:ch:s:", Count: 2, LastSent: at}}
			for _, e := range entries {
				m.LoadDedupState(e)
			}
			m.SeedOwnedSoul("s", tc.history)
			for _, rule := range []string{"r", "r2"} {
				if m.failureStreakCount(rule, "s") != tc.want {
					t.Fatalf("stale %s streak: %d", rule, m.failureStreakCount(rule, "s"))
				}
			}
			if m.failureStreak["r:other"] != 6 {
				t.Fatal("another soul reset")
			}
			hold, ok := m.statusSince["s"]
			if tc.status == "" {
				if ok {
					t.Fatal("empty history retained clock")
				}
			} else if !ok || hold.Status != tc.status || !hold.Since.Equal(at) {
				t.Fatalf("stale hold: %+v", hold)
			}
			for _, e := range entries {
				if !reflect.DeepEqual(m.history.Entries[e.Key], e) {
					t.Fatal("notification history changed")
				}
			}
		})
	}
}

type ownershipNotificationSink struct{ events []core.AlertEvent }

func (s *ownershipNotificationSink) Send(_ context.Context, e *core.AlertEvent, _ *core.AlertChannel) error {
	s.events = append(s.events, *e)
	return nil
}
func (s *ownershipNotificationSink) Validate(map[string]any) error { return nil }

func TestOwnershipReturnNotifiesAtActualThreshold(t *testing.T) {
	m := NewManager(nil, newTestLogger())
	sink := &ownershipNotificationSink{}
	m.dispatchers[core.ChannelWebHook] = sink
	m.LoadChannel(&core.AlertChannel{ID: "sink", WorkspaceID: "default", Type: core.ChannelWebHook, Enabled: true})
	m.LoadRule(&core.AlertRule{ID: "r", WorkspaceID: "default", Enabled: true, Scope: core.RuleScope{Type: "all"}, Channels: []string{"sink"}, Conditions: []core.AlertCondition{{Type: "consecutive_failures", Threshold: 3}}})
	at := time.Now()
	m.failureStreak["r:s"] = 20
	m.SeedOwnedSoul("s", []*core.Judgment{{Status: core.SoulAlive, Timestamp: at.Add(-time.Minute)}, {Status: core.SoulDead, Timestamp: at.Add(-time.Second)}})
	soul := &core.Soul{ID: "s", WorkspaceID: "default"}
	j := &core.Judgment{ID: "second", Status: core.SoulDead, Timestamp: at}
	m.ProcessJudgment(soul, core.SoulDead, j)
	if len(m.queue) != 0 || len(m.ListActiveIncidents()) != 0 {
		t.Fatal("paged before third failure")
	}
	j.ID = "third"
	m.ProcessJudgment(soul, core.SoulDead, j)
	if len(m.queue) != 1 {
		t.Fatal("third failure not queued")
	}
	m.dispatch(<-m.queue)
	if len(sink.events) != 1 || sink.events[0].SoulID != "s" {
		t.Fatal("local notification missing")
	}
}

func TestOwnershipSeedDoesNotChangeGenericLiveSeed(t *testing.T) {
	m := NewManager(nil, newTestLogger())
	m.LoadRule(&core.AlertRule{ID: "r"})
	m.failureStreak["r:s"] = 7
	m.SeedFromJudgments("s", []*core.Judgment{{Status: core.SoulDead, Timestamp: time.Now()}})
	if m.failureStreakCount("r", "s") != 7 {
		t.Fatal("generic seeding overwrote live state")
	}
	m.SeedOwnedSoul("", nil)
	if m.failureStreakCount("r", "s") != 7 {
		t.Fatal("empty ID touched another soul")
	}
}
