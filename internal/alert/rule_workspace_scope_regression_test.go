package alert

import (
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// A rule belongs to one workspace. A rule with the default "all souls" scope
// must not fire for, or open incidents on, souls of another workspace.
func TestRuleDoesNotApplyToSoulsOfOtherWorkspaces(t *testing.T) {
	run := func(ruleWS, soulWS string) (queued bool, incidents int) {
		m := NewManager(nil, newTestLogger())
		if err := m.RegisterRule(&core.AlertRule{
			ID: "rule", Name: "rule", Enabled: true, WorkspaceID: ruleWS,
			Conditions: []core.AlertCondition{{Type: "consecutive_failures", Threshold: 1}},
			Channels:   []string{"c"},
		}); err != nil {
			t.Fatal(err)
		}
		m.ProcessJudgment(&core.Soul{ID: "soul", Name: "soul", WorkspaceID: soulWS, Type: core.CheckHTTP},
			core.SoulAlive, &core.Judgment{SoulID: "soul", Status: core.SoulDead, Message: "down", Duration: time.Second})
		select {
		case <-m.queue:
			queued = true
		default:
		}
		return queued, len(m.ListIncidents())
	}

	for _, tc := range []struct{ name, ruleWS, soulWS string }{
		{"same workspace control", "team-a", "team-a"},
		{"empty means default control", "", "default"},
		{"default both empty control", "", ""},
	} {
		if q, n := run(tc.ruleWS, tc.soulWS); !q || n != 1 {
			t.Fatalf("%s: queued=%v incidents=%d, want queued with 1 incident", tc.name, q, n)
		}
	}
	for _, tc := range []struct{ name, ruleWS, soulWS string }{
		{"other workspace", "team-a", "team-b"},
		{"rule in team, soul in default", "team-a", ""},
		{"rule in default, soul in team", "", "team-b"},
	} {
		if q, n := run(tc.ruleWS, tc.soulWS); q || n != 0 {
			t.Errorf("%s: rule fired across workspaces: queued=%v incidents=%d", tc.name, q, n)
		}
	}
}
