package alert

import (
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"testing"
)

func TestAuditNestedCompound(t *testing.T) {
	dead := core.AlertCondition{Type: "status_for", Status: "dead"}
	alive := core.AlertCondition{Type: "status_for", Status: "alive"}
	group := func(logic string, children ...core.AlertCondition) core.AlertCondition {
		return core.AlertCondition{Type: "compound", Logic: logic, SubConditions: children}
	}
	cases := []struct {
		name string
		cond core.AlertCondition
		want int
	}{
		{"control-flat", group("and", dead), 1},
		{"nested-or-within-and", group("and", group("or", alive, dead)), 1},
		{"nested-false", group("and", dead, group("or", alive)), 0}, {"nested-empty", group("or", group("and")), 0}, {"nested-and-within-or", group("or", alive, group("and", dead, dead)), 1}, {"deep-group", group("and", group("or", group("and", dead))), 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewManager(nil, newTestLogger())
			rule := &core.AlertRule{ID: "nested-rule", Name: "Nested", Enabled: true, Scope: core.RuleScope{Type: "all"}, Channels: []string{"fake"}, Conditions: []core.AlertCondition{tc.cond}}
			if err := m.RegisterRule(rule); err != nil {
				t.Fatal(err)
			}
			m.ProcessJudgment(&core.Soul{ID: "nested-soul", WorkspaceID: "default"}, core.SoulAlive, &core.Judgment{SoulID: "nested-soul", Status: core.SoulDead})
			t.Logf("EXPECTED: queued alerts=%d ACTUAL: queued alerts=%d", tc.want, len(m.queue))
			if len(m.queue) != tc.want {
				t.Error("nested condition changed alert semantics")
			}
		})
	}

}
