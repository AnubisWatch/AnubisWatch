package journey

import (
	"encoding/json"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"testing"
)

func TestAuditNumericAssertions(t *testing.T) {
	e := NewExecutor(nil, newTestLogger())
	cases := []struct {
		name, actual, op, expected string
		want                       bool
	}{
		{"control-numeric", "4", "less_than", "100", true},
		{"nonnumeric-response", "unavailable", "less_than", "100", false},
		{"empty-response", "", "<=", "0", false},
		{"invalid-threshold", "1", ">", "unavailable", false},
		{"actual-overflow", "1e999", ">", "1", false},
		{"expected-overflow", "1", "<", "1e999", false},
		{"gt-invalid", "unavailable", "gt", "-1", false},
		{"ge-invalid", "unavailable", "ge", "0", false},
		{"le-invalid", "unavailable", "le", "100", false},
		{"lt-invalid-threshold", "-1", "lt", "bad", false},
		{"le-invalid-threshold", "-1", "le", "bad", false},
		{"ge-invalid-threshold", "1", "ge", "bad", false},
		{"zero-boundary", "0", ">=", "0", true},
		{"valid-negative", "-1", "<", "0", true},
		{"valid-gt", "2", ">", "1", true},
		{"valid-le", "0", "<=", "0", true},
		{"valid-false", "2", "<", "1", false},
		{"string-equality", "unavailable", "equals", "unavailable", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]string{"count": tc.actual})
			if err != nil {
				t.Fatal(err)
			}
			j := &core.Judgment{Status: core.SoulAlive, Details: &core.JudgmentDetails{ResponseBody: string(body)}}
			ar := e.runAssertion(j, core.Assertion{Type: "json_path", Target: "$.count", Operator: tc.op, Expected: tc.expected})
			t.Logf("EXPECTED: assertion passed=%v ACTUAL: assertion passed=%v", tc.want, ar.Passed)
			if ar.Passed != tc.want {
				t.Error("numeric assertion accepted invalid operand")
			}
		})
	}

}
