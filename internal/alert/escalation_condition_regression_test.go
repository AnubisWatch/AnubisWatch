package alert

import (
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// EscalationStage.Condition was parsed but never evaluated: an acknowledged
// incident stopped escalating even under "not_resolved".
func TestShouldEscalateHonoursStageCondition(t *testing.T) {
	m := &Manager{}
	acked := time.Now()
	started := time.Now().Add(-time.Hour)
	incident := &core.Incident{StartedAt: started, AckedAt: &acked}

	rule := func(cond string) *core.AlertRule {
		return &core.AlertRule{Escalation: &core.EscalationPolicy{Stages: []core.EscalationStage{
			{Wait: core.Duration{Duration: time.Minute}, Condition: cond},
		}}}
	}
	if m.shouldEscalate(incident, rule("")) {
		t.Error("default condition escalated an acknowledged incident")
	}
	if m.shouldEscalate(incident, rule("not_acknowledged")) {
		t.Error("not_acknowledged escalated an acknowledged incident")
	}
	if !m.shouldEscalate(incident, rule("not_resolved")) {
		t.Error("not_resolved did not escalate an acknowledged, unresolved incident")
	}
}
