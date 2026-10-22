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

func TestScheduledJourneyCancellationDoesNotPublish(t *testing.T) {
	for _, mode := range []string{"stop", "stop_all", "assignment_removed", "parent_cancel", "completed_control", "budget_timeout_control"} {
		t.Run(mode, func(t *testing.T) {
			entered := make(chan struct{})
			release := make(chan struct{})
			if mode == "completed_control" {
				close(release)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				select {
				case <-r.Context().Done():
					return
				case <-release:
				}
				_, _ = io.WriteString(w, `{"value":"stable"}`)
			}))
			defer srv.Close()
			db := newTestDB(t)
			defer db.Close()
			e := NewExecutor(db, newTestLogger())
			ctx, cancel := context.WithCancel(context.Background())
			saved := make(chan *core.JourneyRun, 1)
			e.SetRunSaver(func(saveCtx context.Context, run *core.JourneyRun) error {
				if err := db.SaveJourneyRun(saveCtx, run); err != nil {
					return err
				}
				saved <- run
				return nil
			})
			j := &core.JourneyConfig{ID: "cancel-publication", WorkspaceID: "default", Weight: core.Duration{Duration: time.Hour}, Steps: []core.JourneyStep{{
				Name: "wait", Type: core.CheckHTTP, Target: srv.URL, HTTP: &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}},
				Extract: map[string]core.ExtractionRule{"value": {From: "body", Path: "$.value"}},
			}}}
			if mode == "budget_timeout_control" {
				j.Timeout.Duration = 50 * time.Millisecond
			}
			token := &journeyRun{cancel: cancel}
			e.running[j.ID] = token
			done := make(chan struct{})
			// Bootstrap the same loop and ownership token as Start, adding only a join
			// barrier so assertions cannot race the interrupted execution's completion.
			go func() { defer close(done); e.runJourneyLoop(ctx, j, token) }()
			defer func() {
				cancel()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("fixture loop cleanup timed out")
				}
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("fixture HTTP request never entered")
			}
			wantCount := 0
			switch mode {
			case "stop":
				e.Stop(j.ID)
			case "stop_all":
				e.StopAll()
			case "assignment_removed":
				e.Assign(context.Background(), nil)
			case "parent_cancel":
				cancel()
			default:
				wantCount = 1
				select {
				case run := <-saved:
					want := core.SoulAlive
					if mode == "budget_timeout_control" {
						want = core.SoulDead
					}
					if run.Status != want {
						t.Errorf("control status=%s want %s", run.Status, want)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("control result was not saved")
				}
				e.Stop(j.ID)
			}
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("fixture loop did not finish")
			}
			records, err := e.ListRuns(context.Background(), "default", j.ID, 10)
			if err != nil {
				t.Fatal(err)
			}
			if len(records) != wantCount {
				t.Errorf("FAIL: %s persisted %d runs, want %d; cancellation is not target downtime", mode, len(records), wantCount)
			}
			if wantCount == 0 && e.lastHash[j.ID] != "" {
				t.Error("FAIL: cancelled scheduled run advanced dedup state")
			}
		})
	}
}
