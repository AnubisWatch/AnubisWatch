package journey

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestManualSaverDoesNotChangeScheduledBatchPath(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := NewExecutor(nil, newTestLogger())
		scheduled, manual := 0, 0
		failure := errors.New("no quorum")
		e.SetRunSaver(func(context.Context, *core.JourneyRun) error { scheduled++; return nil })
		e.SetRunOnceSaver(func(context.Context, *core.JourneyRun) error { manual++; return failure })
		run, err := e.RunOnce(context.Background(), persistenceJourneyFixture("manual"))
		if run == nil || !errors.Is(err, failure) || manual != 3 || scheduled != 0 {
			t.Fatalf("manual fell back to queue: manual=%d scheduled=%d err=%v", manual, scheduled, err)
		}
		if e.lastHash[run.JourneyID] != "" {
			t.Fatal("uncommitted manual run deduplicated")
		}
		e.executeJourney(context.Background(), persistenceJourneyFixture("scheduled"))
		if scheduled != 1 || manual != 3 {
			t.Fatal("scheduled path waited on manual saver")
		}
		e.SetRunOnceSaver(nil)
		_, err = e.RunOnce(context.Background(), persistenceJourneyFixture("fallback"))
		if err != nil || scheduled != 2 {
			t.Fatal("nil override did not preserve default saver")
		}
	})
}
