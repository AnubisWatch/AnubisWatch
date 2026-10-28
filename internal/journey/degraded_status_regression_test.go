package journey

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// A degraded step is a partial failure, not a failure. The platform treats
// "degraded" as a distinct, non-failed state everywhere else: souls report it
// as "Heart is heavy", the uptime ratio counts it as healthy, the status page
// reports degraded performance rather than an outage, alerting has its own
// "degraded" condition, and the dashboard renders a journey run's "degraded"
// status in its own colour.
//
// The journey executor had no degraded handling at all, so a step that merely
// missed a performance budget (HTTP feather, packet loss, latency) was
// recorded as a dead journey and aborted the remaining steps.

type degradedJourneyFixture struct {
	first  *httptest.Server
	second *httptest.Server
	hits   *atomic.Int64
}

func newDegradedJourneyFixture(t *testing.T) *degradedJourneyFixture {
	t.Helper()
	f := &degradedJourneyFixture{hits: &atomic.Int64{}}
	f.first = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	f.second = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f.hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(f.first.Close)
	t.Cleanup(f.second.Close)
	return f
}

// degradedJourneyStep builds a journey step against url. A feather of one
// nanosecond is always exceeded by a real request, so the HTTP checker
// reports the step as degraded rather than alive.
func degradedJourneyStep(name, url string, feather time.Duration) core.JourneyStep {
	return core.JourneyStep{
		Name:    name,
		Type:    core.CheckHTTP,
		Target:  url,
		Timeout: core.Duration{Duration: 5 * time.Second},
		HTTP: &core.HTTPConfig{
			Method:      http.MethodGet,
			ValidStatus: []int{http.StatusOK},
			Feather:     core.Duration{Duration: feather},
		},
	}
}

func TestJourneyDegradedStepStatus(t *testing.T) {
	cases := []struct {
		name          string
		firstFeather  time.Duration
		firstStatus   int
		secondStatus  int
		wantRun       core.SoulStatus
		wantSteps     int
		wantSecondHit bool
	}{
		{
			// A step that only missed its budget degrades the journey.
			name: "degraded_step", firstFeather: time.Nanosecond, firstStatus: http.StatusOK,
			wantRun: core.SoulDegraded, wantSteps: 2, wantSecondHit: true,
		},
		{
			// A failed step still fails the journey, and wins over a degraded one.
			name: "degraded_then_dead", firstFeather: time.Nanosecond, firstStatus: http.StatusOK,
			secondStatus: http.StatusInternalServerError,
			wantRun:      core.SoulDead, wantSteps: 2, wantSecondHit: true,
		},
		{
			name: "all_alive_control", firstFeather: 0, firstStatus: http.StatusOK,
			wantRun: core.SoulAlive, wantSteps: 2, wantSecondHit: true,
		},
		{
			// A failed step still aborts the journey when continue_on_failure is off.
			name: "dead_step_control", firstFeather: 0, firstStatus: http.StatusInternalServerError,
			wantRun: core.SoulDead, wantSteps: 1, wantSecondHit: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newDegradedJourneyFixture(t)
			first := degradedJourneyStep("first", f.first.URL, tc.firstFeather)
			first.HTTP.ValidStatus = []int{tc.firstStatus}
			second := degradedJourneyStep("second", f.second.URL, 0)
			secondStatus := tc.secondStatus
			if secondStatus == 0 {
				secondStatus = http.StatusOK
			}
			second.HTTP.ValidStatus = []int{secondStatus}
			j := &core.JourneyConfig{
				ID: "degraded-status", WorkspaceID: "default",
				Steps: []core.JourneyStep{first, second},
			}
			db := newTestDB(t)
			defer db.Close()
			e := NewExecutor(db, newTestLogger())
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			run, err := e.RunOnce(ctx, j)
			if err != nil {
				t.Fatal(err)
			}
			if tc.firstFeather > 0 && run.Steps[0].Status != core.SoulDegraded {
				t.Errorf("fixture: first step status=%s, want degraded", run.Steps[0].Status)
			}
			if run.Status != tc.wantRun {
				t.Errorf("FAIL: journey run status=%s, want %s (steps=%+v)", run.Status, tc.wantRun, run.Steps)
			}
			if len(run.Steps) != tc.wantSteps {
				t.Errorf("FAIL: journey executed %d steps, want %d (steps=%+v)", len(run.Steps), tc.wantSteps, run.Steps)
			}
			if got := f.hits.Load() > 0; got != tc.wantSecondHit {
				t.Errorf("FAIL: second step reached=%v, want %v", got, tc.wantSecondHit)
			}
			records, err := e.ListRuns(ctx, "default", j.ID, 10)
			if err != nil || len(records) != 1 {
				t.Fatalf("persistence: count=%d err=%v", len(records), err)
			}
			if records[0].Status != tc.wantRun {
				t.Errorf("FAIL: stored run status=%s, want %s", records[0].Status, tc.wantRun)
			}
		})
	}
}
