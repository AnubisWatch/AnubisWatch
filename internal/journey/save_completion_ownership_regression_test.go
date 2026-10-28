package journey

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestRetiredJourneySaverCannotOverwriteDedup(t *testing.T) {
	for _, mode := range []string{"replacement", "stop", "stop_all", "removed", "parent_cancel", "current_control"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				e := NewExecutor(nil, newTestLogger())
				old := persistenceJourneyFixture("old")
				entered := make(chan struct{})
				release := make(chan struct{})
				saved := make(chan *core.JourneyRun, 4)
				e.SetRunSaver(func(_ context.Context, r *core.JourneyRun) error {
					if r.Variables["version"] == "old" {
						close(entered)
						<-release
					}
					saved <- r
					return nil
				})
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if err := e.Start(ctx, old); err != nil {
					t.Fatal(err)
				}
				<-entered
				expected := ""
				switch mode {
				case "replacement":
					current := persistenceJourneyFixture("new")
					e.Assign(ctx, []*core.JourneyConfig{current})
					r := <-saved
					synctest.Wait()
					expected = e.computeDedupHashFromRun(current, r)
					e.mu.RLock()
					got := e.lastHash[current.ID]
					e.mu.RUnlock()
					if got != expected {
						t.Fatal("replacement control was not saved")
					}
				case "stop":
					e.Stop(old.ID)
				case "stop_all":
					e.StopAll()
				case "removed":
					e.Assign(ctx, nil)
				case "parent_cancel":
					cancel()
				}
				close(release)
				late := <-saved
				synctest.Wait()
				if mode == "current_control" {
					expected = e.computeDedupHashFromRun(old, late)
				}
				e.mu.RLock()
				actual := e.lastHash[old.ID]
				e.mu.RUnlock()
				if actual != expected {
					t.Errorf("%s stale saver changed dedup: got=%q want=%q", mode, actual, expected)
				}
				if mode == "replacement" {
					if r := e.executeJourneyRun(ctx, persistenceJourneyFixture("new"), true); r != nil {
						t.Error("replacement was duplicated after stale completion")
					}
				}
				e.StopAll()
				synctest.Wait()
			})
		})
	}
}

func TestManualCommittedRunStillPublishesDedupAfterCallerCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := NewExecutor(nil, newTestLogger())
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		e.SetRunSaver(func(context.Context, *core.JourneyRun) error { cancel(); return nil })
		j := persistenceJourneyFixture("manual")
		run, err := e.RunOnce(ctx, j)
		if err != nil || run == nil || e.lastHash[j.ID] == "" {
			t.Fatalf("committed manual run discarded: run=%v err=%v", run, err)
		}
	})
}
