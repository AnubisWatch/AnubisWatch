package alert

import (
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"testing"
	"time"
)

func auditRuleSeverity(t *testing.T, severity core.Severity, status core.SoulStatus, want core.Severity) {
	t.Helper()
	m := NewManager(nil, newTestLogger())
	rule := &core.AlertRule{ID: "severity", Enabled: true, Severity: severity, Scope: core.RuleScope{Type: "all"}, Channels: []string{"local-fake"}, Conditions: []core.AlertCondition{{Type: "threshold", Metric: "latency", Operator: ">", Threshold: 100}}}
	if err := m.RegisterRule(rule); err != nil {
		t.Fatal(err)
	}
	m.ProcessJudgment(&core.Soul{ID: "severity-soul", WorkspaceID: "default"}, core.SoulAlive, &core.Judgment{SoulID: "severity-soul", Status: status, Duration: 200 * time.Millisecond})
	if len(m.queue) != 1 {
		t.Fatalf("expected one alert, got %d", len(m.queue))
	}
	event := <-m.queue
	t.Logf("rule=%q status=%s EXPECTED: severity=%s ACTUAL: severity=%s", severity, status, want, event.Severity)
	if event.Severity != want {
		t.Fatal("regression mismatch")
	}
	incidents := m.ListActiveIncidents()
	if len(incidents) != 1 || incidents[0].Severity != want {
		t.Fatal("incident severity mismatch")
	}
}
func TestAuditRuleSeverity(t *testing.T) {
	t.Run("default-control", func(t *testing.T) { auditRuleSeverity(t, "", core.SoulDead, core.SeverityCritical) })
	t.Run("explicit-warning", func(t *testing.T) { auditRuleSeverity(t, core.SeverityWarning, core.SoulDead, core.SeverityWarning) })
	t.Run("critical-latency", func(t *testing.T) { auditRuleSeverity(t, core.SeverityCritical, core.SoulAlive, core.SeverityCritical) })
	t.Run("explicit-info", func(t *testing.T) { auditRuleSeverity(t, core.SeverityInfo, core.SoulDegraded, core.SeverityInfo) })
	t.Run("default-info", func(t *testing.T) { auditRuleSeverity(t, "", core.SoulAlive, core.SeverityInfo) })
}
