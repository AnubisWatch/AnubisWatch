package journey

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestJourneyDedupPreservesHealthChanges(t *testing.T) {
	cases := []struct {
		name          string
		before, after []int
		changed       bool
	}{
		{"outage", []int{200}, []int{500}, true},
		{"recovery", []int{500}, []int{200}, true},
		{"same_healthy_control", []int{200}, []int{200}, false},
		{"same_unhealthy_control", []int{500}, []int{500}, false},
		{"different_failed_step", []int{500, 200}, []int{200, 500}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			codes := make([]atomic.Int64, len(tc.before))
			for i, code := range tc.before {
				codes[i].Store(int64(code))
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				i, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/"))
				if err != nil || i < 0 || i >= len(codes) {
					http.Error(w, "invalid fixture path", 400)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(int(codes[i].Load()))
				_, _ = io.WriteString(w, `{"value":"stable"}`)
			}))
			defer srv.Close()
			db := newTestDB(t)
			defer db.Close()
			e := NewExecutor(db, newTestLogger())
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			j := &core.JourneyConfig{ID: "health-dedup", WorkspaceID: "default", ContinueOnFailure: true}
			for i := range tc.before {
				j.Steps = append(j.Steps, core.JourneyStep{
					Name: fmt.Sprintf("step-%d", i), Type: core.CheckHTTP, Target: fmt.Sprintf("%s/%d", srv.URL, i),
					Timeout: core.Duration{Duration: time.Second}, HTTP: &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}},
					Extract: map[string]core.ExtractionRule{"value": {From: "body", Path: "$.value"}},
				})
			}
			first := e.executeJourneyRun(ctx, j, true)
			if first == nil || first.Variables["value"] != "stable" {
				t.Fatalf("fixture initial run/extraction invalid: %+v", first)
			}
			initial, err := e.ListRuns(ctx, "default", j.ID, 10)
			if err != nil || len(initial) != 1 {
				t.Fatalf("fixture initial persistence: len=%d err=%v", len(initial), err)
			}
			for i, code := range tc.after {
				codes[i].Store(int64(code))
			}
			second := e.executeJourneyRun(ctx, j, true)
			if tc.changed && second == nil {
				t.Errorf("FAIL: changed health %v -> %v was discarded as duplicate", tc.before, tc.after)
			}
			if !tc.changed && second != nil {
				t.Error("identical health and extraction was not deduplicated")
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
				t.Errorf("FAIL: stored runs=%d, want %d after health %v -> %v", len(records), wantCount, tc.before, tc.after)
			}
			if second != nil {
				if second.Variables["value"] != "stable" {
					t.Fatal("fixture extraction changed")
				}
				for i, code := range tc.after {
					want := core.SoulAlive
					if code != 200 {
						want = core.SoulDead
					}
					if second.Steps[i].Status != want {
						t.Errorf("step %d status=%s want %s", i, second.Steps[i].Status, want)
					}
				}
				stored, err := e.GetRun(ctx, "default", j.ID, second.ID)
				if err != nil || stored == nil || stored.Status != second.Status {
					t.Fatalf("changed run not readable: run=%+v err=%v", stored, err)
				}
			}
		})
	}
}
