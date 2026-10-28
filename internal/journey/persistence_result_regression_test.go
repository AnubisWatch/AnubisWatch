package journey

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// An unsupported checker produces a completed failure without any network I/O.
// Persistence acknowledgement must be independent of the check's health.
func persistenceJourneyFixture(version string) *core.JourneyConfig {
	return &core.JourneyConfig{ID: "persistence", WorkspaceID: "tenant", Enabled: true,
		Weight: core.Duration{Duration: time.Hour}, Variables: map[string]string{"version": version},
		Steps: []core.JourneyStep{{Type: core.CheckType("fixture-unsupported"), Extract: map[string]core.ExtractionRule{"version": {From: "body", Path: "$.version"}}}}}
}

func TestRunOnceReturnsPersistenceOutcome(t *testing.T) {
	failure := errors.New("save unavailable")
	for _, tc := range []struct {
		name                 string
		succeedAt, wantCalls int
		cancel               bool
		wantErr              error
	}{
		{"success", 1, 1, false, nil}, {"retry_success", 3, 3, false, nil},
		{"exhausted", 0, 3, false, failure}, {"cancel_during_save", 0, 1, true, context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				e := NewExecutor(nil, newTestLogger())
				j := persistenceJourneyFixture("initial")
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				calls := 0
				e.SetRunSaver(func(context.Context, *core.JourneyRun) error {
					calls++
					if tc.cancel {
						cancel()
					}
					if calls == tc.succeedAt {
						return nil
					}
					return failure
				})
				run, err := e.RunOnce(ctx, j)
				if run == nil || len(run.Steps) != 1 || run.Status != core.SoulDead {
					t.Fatalf("completed check lost: %+v", run)
				}
				if !errors.Is(err, tc.wantErr) || calls != tc.wantCalls {
					t.Fatalf("err=%v calls=%d; want %v/%d", err, calls, tc.wantErr, tc.wantCalls)
				}
				e.mu.RLock()
				hash := e.lastHash[j.ID]
				e.mu.RUnlock()
				if (hash != "") != (err == nil) {
					t.Fatalf("dedup published without successful save: hash=%q err=%v", hash, err)
				}
			})
		})
	}
}

func TestRunOnceClosedStorageDoesNotAcknowledge(t *testing.T) {
	db := newTestDB(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	synctest.Test(t, func(t *testing.T) {
		e := NewExecutor(db, newTestLogger())
		run, err := e.RunOnce(context.Background(), persistenceJourneyFixture("closed"))
		if run == nil || err == nil {
			t.Fatalf("closed storage acknowledged: run=%v err=%v", run, err)
		}
	})
}

func TestScheduledPersistenceFailureRemainsRetryable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := NewExecutor(nil, newTestLogger())
		j := persistenceJourneyFixture("stable")
		calls := 0
		fail := true
		e.SetRunSaver(func(context.Context, *core.JourneyRun) error {
			calls++
			if fail {
				return errors.New("unavailable")
			}
			return nil
		})
		e.executeJourney(context.Background(), j)
		if calls != 3 || e.lastHash[j.ID] != "" {
			t.Fatal("failed scheduled save consumed dedup")
		}
		fail = false
		e.executeJourney(context.Background(), j)
		if calls != 4 || e.lastHash[j.ID] == "" {
			t.Fatal("subsequent scheduled run did not persist")
		}
		e.executeJourney(context.Background(), j)
		if calls != 4 {
			t.Fatal("successful scheduled run stopped deduplicating")
		}
	})
}
