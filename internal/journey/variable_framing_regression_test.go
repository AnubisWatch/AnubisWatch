package journey

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestJourneyDedupDistinguishesVariableBoundaries(t *testing.T) {
	cases := []struct {
		name          string
		before, after map[string]string
		changed       bool
	}{
		{"plain_change_control", map[string]string{"a": "x", "b": "y"}, map[string]string{"a": "changed", "b": "y"}, true},
		{"identical_multiline_control", map[string]string{"a": "x\nb=y", "b": "z"}, map[string]string{"a": "x\nb=y", "b": "z"}, false},
		{"ambiguous_newline_boundaries", map[string]string{"a": "x\nb=y", "b": "z"}, map[string]string{"a": "x", "b": "y\nb=z"}, true},
		{"unicode_boundaries", map[string]string{"a": "α\nb=β", "b": "γ"}, map[string]string{"a": "α", "b": "β\nb=γ"}, true},
		{"trailing_newline_change_control", map[string]string{"a": "x", "b": "y"}, map[string]string{"a": "x", "b": "y\n"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var response atomic.Value
			setBody := func(values map[string]string) {
				b, err := json.Marshal(values)
				if err != nil {
					t.Fatal(err)
				}
				response.Store(b)
			}
			setBody(tc.before)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(response.Load().([]byte))
			}))
			defer srv.Close()
			db := newTestDB(t)
			defer db.Close()
			e := NewExecutor(db, newTestLogger())
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			j := &core.JourneyConfig{ID: "framed-variables", WorkspaceID: "default", Steps: []core.JourneyStep{{
				Name: "extract", Type: core.CheckHTTP, Target: srv.URL, Timeout: core.Duration{Duration: time.Second},
				HTTP:    &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}},
				Extract: map[string]core.ExtractionRule{"a": {From: "body", Path: "$.a"}, "b": {From: "body", Path: "$.b"}},
			}}}
			first := e.executeJourneyRun(ctx, j, true)
			if first == nil || first.Status != core.SoulAlive || !reflect.DeepEqual(first.Variables, tc.before) {
				t.Fatalf("fixture extraction failed: %+v, want %v", first, tc.before)
			}
			setBody(tc.after)
			second := e.executeJourneyRun(ctx, j, true)
			if tc.changed && second == nil {
				t.Errorf("FAIL: distinct captured values %q -> %q were discarded as duplicate", tc.before, tc.after)
			}
			if !tc.changed && second != nil {
				t.Error("unchanged captured values were not deduplicated")
			}
			if second != nil && !reflect.DeepEqual(second.Variables, tc.after) {
				t.Errorf("extracted values=%q, want %q", second.Variables, tc.after)
			}
			records, err := e.ListRuns(ctx, "default", j.ID, 10)
			if err != nil {
				t.Fatal(err)
			}
			wantCount := 1
			if tc.changed {
				wantCount = 2
			}
			if len(records) != wantCount {
				t.Errorf("FAIL: stored runs=%d, want %d", len(records), wantCount)
			}
			if second != nil {
				stored, err := e.GetRun(ctx, "default", j.ID, second.ID)
				if err != nil || stored == nil || !reflect.DeepEqual(stored.Variables, tc.after) {
					t.Errorf("changed values not persisted: stored=%+v err=%v", stored, err)
				}
			}
		})
	}
}
