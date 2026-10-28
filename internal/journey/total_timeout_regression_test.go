package journey

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/probe"
)

// This checker models a context-aware remote operation. The executor, registry,
// step dispatch, aggregation, and persistence dispatch are real production code.
type journeyBudgetChecker struct{}

func (*journeyBudgetChecker) Type() core.CheckType      { return "journey-budget-test" }
func (*journeyBudgetChecker) Validate(*core.Soul) error { return nil }
func (*journeyBudgetChecker) Judge(ctx context.Context, s *core.Soul) (*core.Judgment, error) {
	delay, err := time.ParseDuration(s.Target)
	if err != nil {
		return nil, err
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return &core.Judgment{Status: core.SoulAlive, Details: &core.JudgmentDetails{}}, nil
	}
}
func init() { probe.RegisterChecker(&journeyBudgetChecker{}) }

func TestJourneyTotalTimeout(t *testing.T) {
	cases := []struct {
		name                           string
		budget, parent, delay, elapsed time.Duration
		steps                          int
		want                           core.SoulStatus
		scheduled                      bool
	}{
		{"within_budget_control", 3 * time.Second, 10 * time.Second, 2 * time.Second, 2 * time.Second, 1, core.SoulAlive, false},
		{"timeout", time.Second, 10 * time.Second, 2 * time.Second, time.Second, 1, core.SoulDead, false},
		{"shared_across_steps", 1500 * time.Millisecond, 10 * time.Second, time.Second, 1500 * time.Millisecond, 2, core.SoulDead, false},
		{"parent_deadline_control", 3 * time.Second, 500 * time.Millisecond, 2 * time.Second, 500 * time.Millisecond, 1, core.SoulDead, false},
		{"unset_timeout_control", 0, 10 * time.Second, 2 * time.Second, 2 * time.Second, 1, core.SoulAlive, false},
		{"negative_timeout_control", -time.Second, 10 * time.Second, 2 * time.Second, 2 * time.Second, 1, core.SoulAlive, false},
		{"scheduled_timeout", time.Second, 10 * time.Second, 2 * time.Second, time.Second, 1, core.SoulDead, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				e := NewExecutor(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
				ctx, cancel := context.WithTimeout(context.Background(), tc.parent)
				defer cancel()
				saves := 0
				var saved *core.JourneyRun
				e.SetRunSaver(func(saveCtx context.Context, run *core.JourneyRun) error {
					saves++
					saved = run
					if saveCtx.Err() != ctx.Err() {
						t.Errorf("save context incorrectly inherits expired execution budget: %v", saveCtx.Err())
					}
					return nil
				})
				j := &core.JourneyConfig{ID: "budget", Timeout: core.Duration{Duration: tc.budget}}
				for i := 0; i < tc.steps; i++ {
					j.Steps = append(j.Steps, core.JourneyStep{Name: "wait", Type: "journey-budget-test", Target: tc.delay.String()})
				}
				start := time.Now()
				var run *core.JourneyRun
				if tc.scheduled {
					run = e.executeJourneyRun(ctx, j, true)
				} else {
					var err error
					run, err = e.RunOnce(ctx, j)
					if err != nil {
						t.Fatal(err)
					}
				}
				if run == nil {
					t.Fatal("run missing")
				}
				if got := time.Since(start); got != tc.elapsed {
					t.Errorf("FAIL: execution took %s, want %s (budget %s)", got, tc.elapsed, tc.budget)
				}
				if run.Status != tc.want {
					t.Errorf("FAIL: run status = %s, want %s", run.Status, tc.want)
				}
				if saves != 1 || saved != run {
					t.Errorf("run not saved exactly once: saves=%d", saves)
				}
			})
		})
	}
}
