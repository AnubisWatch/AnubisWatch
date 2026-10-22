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

func TestJourneyRegexComparisonOperator(t *testing.T) {
	cases := []struct {
		name, kind, actual, operator, expected string
		want                                   bool
	}{
		{"json_match", "json_path", "resource-42", "regex", `^resource-[0-9]+$`, true},
		{"header_match", "header", "resource-42", "regex", `^resource-[0-9]+$`, true},
		{"status_match", "status_code", "unused", "regex", `^2[0-9]{2}$`, true},
		{"duration_match", "response_time", "unused", "regex", `^[0-9]+$`, true},
		{"empty_value_match", "json_path", "", "regex", `^$`, true},
		{"invalid_pattern_equal_to_actual", "json_path", "[", "regex", `[`, false},
		{"nonmatch_control", "json_path", "pending", "regex", `^resource-[0-9]+$`, false},
		{"literal_pattern_control", "json_path", "resource-42", "regex", `resource-42`, true},
		{"equality_control", "json_path", "resource-42", "equals", `resource-42`, true},
		{"whole_body_type_control", "regex", "resource-42", "regex", `resource-[0-9]+`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]string{"value": tc.actual})
			if err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Probe-Value", tc.actual)
				_, _ = w.Write(body)
			}))
			defer srv.Close()
			assertion := core.Assertion{Type: tc.kind, Operator: tc.operator, Expected: tc.expected}
			if tc.kind == "json_path" {
				assertion.Target = "$.value"
			}
			if tc.kind == "header" {
				assertion.Target = "X-Probe-Value"
			}
			j := &core.JourneyConfig{ID: "regex-comparison", WorkspaceID: "default", Steps: []core.JourneyStep{{
				Name: "assert", Type: core.CheckHTTP, Target: srv.URL, Timeout: core.Duration{Duration: time.Second}, Assertions: []core.Assertion{assertion},
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
			wantStatus := core.SoulDead
			if tc.want {
				wantStatus = core.SoulAlive
			}
			if run.Status != wantStatus {
				t.Errorf("FAIL: %s operator=%s pattern=%q status=%s, want %s; steps=%+v", tc.kind, tc.operator, tc.expected, run.Status, wantStatus, run.Steps)
			}
			records, err := e.ListRuns(ctx, "default", j.ID, 10)
			if err != nil || len(records) != 1 {
				t.Fatalf("persistence: count=%d err=%v", len(records), err)
			}
			if records[0].Status != wantStatus {
				t.Errorf("FAIL: stored status=%s, want %s", records[0].Status, wantStatus)
			}
		})
	}
}
