package journey

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestJourneyJSONPathAssertionRequiresResolvedValue(t *testing.T) {
	cases := []struct {
		name, body, path, operator, expected string
		want                                 bool
	}{
		{"malformed_not_equal", "not-json", "$.status", "not_equals", "error", false},
		{"missing_empty_equal", `{"message":"ok"}`, "$.status", "equals", "", false},
		{"missing_nested", `{"status":{}}`, "$.status.code", "not_equals", "error", false},
		{"invalid_path_prefix", `{"status":""}`, "status", "equals", "", false},
		{"wrong_container", `{"status":"ok"}`, "$.status.code", "regex", ".*", false},
		{"trailing_json", `{"status":""} {}`, "$.status", "equals", "", false},
		{"empty_body", "", "$.status", "regex", "^$", false},
		{"missing_regex", `{}`, "$.status", "regex", ".*", false},
		{"empty_string_control", `{"status":""}`, "$.status", "equals", "", true},
		{"empty_not_equal_control", `{"status":""}`, "$.status", "not_equals", "error", true},
		{"null_control", `{"status":null}`, "$.status", "equals", "", true},
		{"present_control", `{"status":"ok"}`, "$.status", "not_equals", "error", true},
		{"mismatch_control", `{"status":"error"}`, "$.status", "not_equals", "error", false},
		{"zero_control", `{"status":0}`, "$.status", "equals", "0", true},
		{"false_control", `{"status":false}`, "$.status", "equals", "false", true},
		{"object_control", `{"status":{}}`, "$.status", "equals", "{}", true},
		{"array_control", `{"status":[]}`, "$.status", "equals", "[]", true},
		{"large_integer_control", `{"status":9007199254740993}`, "$.status", "equals", "9007199254740993", true},
		{"fraction_control", `{"status":1.25}`, "$.status", "equals", "1.25", true},
		{"exponent_control", `{"status":1e3}`, "$.status", "equals", "1000", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			j := &core.JourneyConfig{ID: "json-resolution", WorkspaceID: "default", Steps: []core.JourneyStep{{
				Name: "assert", Type: core.CheckHTTP, Target: srv.URL, Timeout: core.Duration{Duration: time.Second},
				Assertions: []core.Assertion{{Type: "json_path", Target: tc.path, Operator: tc.operator, Expected: tc.expected}},
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
				t.Errorf("FAIL: JSON-path assertion for %q %s %q returned %s, want %s (body=%q)", tc.path, tc.operator, tc.expected, run.Status, want, tc.body)
			}
			records, err := e.ListRuns(ctx, "default", j.ID, 10)
			if err != nil || len(records) != 1 {
				t.Fatalf("persistence: count=%d err=%v", len(records), err)
			}
			if records[0].Status != want {
				t.Errorf("FAIL: stored health=%s, want %s", records[0].Status, want)
			}
		})
	}
}
