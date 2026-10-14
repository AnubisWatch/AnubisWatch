package journey

import (
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"testing"
)

func auditBodyRegex(t *testing.T, target, body, pattern string, details bool, want bool) {
	t.Helper()
	e := NewExecutor(nil, newTestLogger())
	j := &core.Judgment{}
	if details {
		j.Details = &core.JudgmentDetails{ResponseBody: body}
	}
	got := e.runAssertion(j, core.Assertion{Type: "regex", Target: target, Expected: pattern}).Passed
	t.Logf("target=%q body=%q EXPECTED: passed=%v ACTUAL: passed=%v", target, body, want, got)
	if got != want {
		t.Fatal("regression mismatch")
	}
}
func TestAuditRegexOptionalTarget(t *testing.T) {
	t.Run("target-control", func(t *testing.T) { auditBodyRegex(t, "body", "ready 200", `ready \d+`, true, true) })
	t.Run("omitted-target", func(t *testing.T) { auditBodyRegex(t, "", "ready 200", `ready \d+`, true, true) })
	t.Run("mismatch", func(t *testing.T) { auditBodyRegex(t, "", "pending", `^ready$`, true, false) })
	t.Run("invalid-regex", func(t *testing.T) { auditBodyRegex(t, "", "ready", "[", true, false) })
	t.Run("no-details", func(t *testing.T) { auditBodyRegex(t, "", "", ".*", false, false) })
	t.Run("empty-body", func(t *testing.T) { auditBodyRegex(t, "", "", `^$`, true, true) })
}
