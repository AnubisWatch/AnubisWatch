package alert

import (
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"testing"
	"time"
)

func auditThresholdCase(t *testing.T, wrapped bool, duration time.Duration, operator, metric string, code int, want int) {
	t.Helper()
	m := NewManager(nil, newTestLogger())
	cond := core.AlertCondition{Type: "threshold", Metric: metric, Operator: operator, Threshold: 100}
	if wrapped {
		cond = core.AlertCondition{Type: "compound", Logic: "and", SubConditions: []core.AlertCondition{cond}}
	}
	rule := &core.AlertRule{ID: "threshold", Name: "Threshold", Enabled: true, Scope: core.RuleScope{Type: "all"}, Channels: []string{"local-fake"}, Conditions: []core.AlertCondition{cond}}
	if err := m.RegisterRule(rule); err != nil {
		t.Fatal(err)
	}
	m.ProcessJudgment(&core.Soul{ID: "threshold-soul", WorkspaceID: "default"}, core.SoulAlive, &core.Judgment{SoulID: "threshold-soul", Status: core.SoulAlive, Duration: duration, StatusCode: code})
	t.Logf("compound=%v metric=%s operator=%s EXPECTED: alerts=%d ACTUAL: alerts=%d", wrapped, metric, operator, want, len(m.queue))
	if len(m.queue) != want {
		t.Fatal("PROBLEM CONFIRMED")
	}
	if len(m.ListActiveIncidents()) != want {
		t.Fatal("incident count does not match")
	}
}
func TestAuditAlertThreshold(t *testing.T) {
	t.Run("compound-control", func(t *testing.T) { auditThresholdCase(t, true, 200*time.Millisecond, ">", "latency", 200, 1) })
	t.Run("top-level", func(t *testing.T) { auditThresholdCase(t, false, 200*time.Millisecond, ">", "latency", 200, 1) })
	t.Run("boundary-strict", func(t *testing.T) { auditThresholdCase(t, false, 100*time.Millisecond, ">", "latency", 200, 0) })
	t.Run("below", func(t *testing.T) { auditThresholdCase(t, false, 99*time.Millisecond, ">", "latency", 200, 0) })
	t.Run("boundary-inclusive", func(t *testing.T) { auditThresholdCase(t, false, 100*time.Millisecond, ">=", "latency", 200, 1) })
	t.Run("status-code", func(t *testing.T) { auditThresholdCase(t, false, 0, ">", "status_code", 200, 1) })
	if !t.Failed() {
		t.Log("FIX VERIFIED")
	}
}
