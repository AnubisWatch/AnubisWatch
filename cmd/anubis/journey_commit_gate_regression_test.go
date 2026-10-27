package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type journeyAckMutation func(core.FSMCommand, time.Duration) (uint64, uint64, error)

func (f journeyAckMutation) ApplyMutation(c core.FSMCommand, d time.Duration) (uint64, uint64, error) {
	return f(c, d)
}

func TestJourneyCommitWaitsForAppliedRecord(t *testing.T) {
	db := newAlertKeyDB(t)
	synctest.Test(t, func(t *testing.T) {
		run := &core.JourneyRun{JourneyID: "j", WorkspaceID: "tenant", StartedAt: 1}
		entered := make(chan core.FSMCommand, 1)
		release := make(chan struct{})
		done := make(chan error, 1)
		applier := journeyAckMutation(func(c core.FSMCommand, d time.Duration) (uint64, uint64, error) {
			if d != 30*time.Second {
				t.Errorf("timeout=%v", d)
			}
			entered <- c
			<-release
			return 1, 1, db.ApplyFSMCommand(&c)
		})
		go func() { done <- saveJourneyRunSync(context.Background(), run, "owner", applier) }()
		cmd := <-entered
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("acknowledged before apply")
		default:
		}
		if _, err := db.Get(cmd.Key); err == nil {
			t.Fatal("fixture pre-apply record exists")
		}
		var body core.JourneyRun
		if err := json.Unmarshal(cmd.Value, &body); err != nil {
			t.Fatal(err)
		}
		if body.ID == "" || cmd.Op != core.FSMSet || cmd.Origin != "owner" || cmd.Key != "tenant/journey-runs/j/1/"+body.ID {
			t.Fatalf("bad canonical command: %+v", cmd)
		}
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		stored, err := db.GetJourneyRun(context.Background(), "tenant", "j", run.ID)
		if err != nil || stored.ID != run.ID {
			t.Fatalf("acknowledged but unreadable: %v", err)
		}
	})
}

func TestJourneyCommitHonorsFailureAndRequestBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		failure := errors.New("no quorum")
		run := &core.JourneyRun{JourneyID: "j", StartedAt: 7}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		calls := 0
		applier := journeyAckMutation(func(c core.FSMCommand, d time.Duration) (uint64, uint64, error) {
			calls++
			if d <= 0 || d > 2*time.Second {
				t.Fatalf("request budget lost: %v", d)
			}
			if c.Key != "default/journey-runs/j/7/"+run.ID {
				t.Fatal("default workspace key changed")
			}
			return 0, 0, failure
		})
		if err := saveJourneyRunSync(ctx, run, "owner", applier); !errors.Is(err, failure) || calls != 1 {
			t.Fatalf("consensus error swallowed: %v", err)
		}
		cancel()
		if err := saveJourneyRunSync(ctx, run, "owner", applier); !errors.Is(err, context.Canceled) || calls != 1 {
			t.Fatalf("canceled submission: calls=%d err=%v", calls, err)
		}
		expired, finish := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer finish()
		if err := saveJourneyRunSync(expired, run, "owner", applier); !errors.Is(err, context.DeadlineExceeded) || calls != 1 {
			t.Fatalf("expired submission: %v", err)
		}
	})
}
