package journey

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestJourneyNumericComparisonsPreserveOperandPrecision(t *testing.T) {
	cases := []struct {
		name, actual, operator, expected string
		want                             bool
	}{
		{"large_gt", "9007199254740993", "greater_than", "9007199254740992", true},
		{"large_lt", "9007199254740992", "less_than", "9007199254740993", true},
		{"large_ge_false", "9007199254740992", ">=", "9007199254740993", false},
		{"large_le_false", "9007199254740993", "<=", "9007199254740992", false},
		{"negative_gt", "-9007199254740992", "gt", "-9007199254740993", true},
		{"decimal_gt", "0.10000000000000001", ">", "0.1", true},
		{"decimal_lt", "0.1", "<", ".10000000000000001", true},
		{"exponent_gt", "9007199254740993", ">", "9.007199254740992e15", true},
		{"underflow_gt", "1e-400", ">", "0", true},
		{"ge_equal_control", "1.25", "ge", "1.25", true},
		{"le_equal_control", "2", "le", "2", true},
		{"ordinary_control", "4", "greater_than", "3", true},
		{"false_control", "2", ">", "3", false},
		{"invalid_control", "unavailable", "<", "100", false},
		{"actual_overflow_control", "1e999", ">", "1", false},
		{"threshold_overflow_control", "1", "<", "1e999", false},
		{"nan_control", "NaN", ">=", "0", false},
		{"infinity_control", "Infinity", ">", "0", true},
		{"same_infinity_control", "-Inf", "<=", "-Infinity", true},
		{"string_equality_control", "1.0", "equals", "1", false},
	}
	for _, source := range []string{"json_path", "header"} {
		for _, tc := range cases {
			t.Run(source+"/"+tc.name, func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("X-Count", tc.actual)
					// The operand is a string, so extraction cannot round it as a JSON number.
					_ = json.NewEncoder(w).Encode(map[string]string{"count": tc.actual})
				}))
				defer srv.Close()
				target := "$.count"
				if source == "header" {
					target = "X-Count"
				}
				j := &core.JourneyConfig{ID: "precise-comparison", WorkspaceID: "default", Steps: []core.JourneyStep{{
					Name: "compare", Type: core.CheckHTTP, Target: srv.URL, Timeout: core.Duration{Duration: time.Second},
					Assertions: []core.Assertion{{Type: source, Target: target, Operator: tc.operator, Expected: tc.expected}},
				}}}
				db := newTestDB(t)
				defer db.Close()
				e := NewExecutor(db, newTestLogger())
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				run, err := e.RunOnce(ctx, j)
				if err != nil {
					t.Fatal(err)
				}
				want := core.SoulDead
				if tc.want {
					want = core.SoulAlive
				}
				if run.Status != want {
					t.Errorf("FAIL: %q %s %q produced status=%s, want %s", tc.actual, tc.operator, tc.expected, run.Status, want)
				}
				records, err := e.ListRuns(ctx, "default", j.ID, 10)
				if err != nil || len(records) != 1 {
					t.Fatalf("persistence: count=%d err=%v", len(records), err)
				}
				if records[0].Status != want {
					t.Errorf("FAIL: stored status=%s, want %s", records[0].Status, want)
				}
			})
		}
	}
}
