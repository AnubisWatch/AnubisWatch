package journey

import (
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"testing"
)

func auditHeaderAssertion(t *testing.T, target, operator, expected string, headers map[string]string, details, want bool) {
	t.Helper()
	e := NewExecutor(nil, newTestLogger())
	j := &core.Judgment{}
	if details {
		j.Details = &core.JudgmentDetails{ResponseHeaders: headers}
	}
	got := e.runAssertion(j, core.Assertion{Type: "header", Target: target, Operator: operator, Expected: expected}).Passed
	t.Logf("target=%q EXPECTED: passed=%v ACTUAL: passed=%v", target, want, got)
	if got != want {
		t.Fatal("regression mismatch")
	}
}
func TestAuditHeaderAssertionCasing(t *testing.T) {
	headers := map[string]string{"Content-Type": "application/json"}
	t.Run("exact-control", func(t *testing.T) {
		auditHeaderAssertion(t, "Content-Type", "equals", "application/json", headers, true, true)
	})
	t.Run("lowercase-target", func(t *testing.T) {
		auditHeaderAssertion(t, "content-type", "equals", "application/json", headers, true, true)
	})
	t.Run("uppercase-response", func(t *testing.T) {
		auditHeaderAssertion(t, "Content-Type", "contains", "json", map[string]string{"CONTENT-TYPE": "application/json"}, true, true)
	})
	t.Run("wrong-value", func(t *testing.T) {
		auditHeaderAssertion(t, "content-type", "equals", "text/plain", headers, true, false)
	})
	t.Run("missing-header", func(t *testing.T) {
		auditHeaderAssertion(t, "x-unavailable", "equals", "present", headers, true, false)
	})
	t.Run("missing-details", func(t *testing.T) {
		auditHeaderAssertion(t, "content-type", "equals", "application/json", nil, false, false)
	})
}
