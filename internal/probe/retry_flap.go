package probe

import (
	"context"
	"errors"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// runAttempt runs one check attempt under the soul's timeout.
func runAttempt(ctx context.Context, checker Checker, soul *core.Soul) (*core.Judgment, error) {
	checkCtx, cancel := context.WithTimeout(ctx, soulTimeout(soul))
	defer cancel()
	return checker.Judge(checkCtx, soul)
}

// attemptFailed reports whether an attempt's outcome is a failure that a
// confirmation retry should re-check. ErrNoJudgment (nothing due) is not.
func attemptFailed(j *core.Judgment, err error) bool {
	if err != nil {
		return !errors.Is(err, ErrNoJudgment)
	}
	return j == nil || j.Status == core.SoulDead
}

// judgmentDetails returns j.Details, allocating it when absent.
func judgmentDetails(j *core.Judgment) *core.JudgmentDetails {
	if j.Details == nil {
		j.Details = &core.JudgmentDetails{}
	}
	return j.Details
}

// sleepContext waits d or until ctx ends.
func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// applyFlapState feeds the judgment's status to the soul's flap detector and
// marks the judgment: Details.Flapping while the soul flaps, and
// Details.FlapChange on the judgment that started or ended flapping. The
// alert manager reads both, so every node that sees the judgment (REST, WS,
// replicated history) sees the same flag; only the owner evaluates it.
func (e *Engine) applyFlapState(runner *soulRunner, soul *core.Soul, j *core.Judgment) {
	if runner == nil || j == nil {
		return
	}
	settings := core.ResolveFlapSettings(soul.FlapDetection, e.config.FlapDetection)
	flapping, change, baseline := runner.observeFlap(j.Status, e.now(), settings)
	if !flapping && change == "" {
		return
	}
	d := judgmentDetails(j)
	d.Flapping = flapping
	d.FlapChange = change
	d.FlapBaseline = baseline
	switch change {
	case core.FlapStarted:
		e.logger.Warn("soul is flapping", "soul", soul.Name, "id", soul.ID,
			"threshold", settings.Threshold, "window", settings.Window)
	case core.FlapStopped:
		e.logger.Info("soul stopped flapping", "soul", soul.Name, "id", soul.ID, "status", j.Status)
	}
}

// observeFlap records a status against the runner's last status and returns
// whether the soul is flapping, whether that just changed, and the pre-flap
// baseline status (while flapping and on the stop). It does not update
// lastStatus (swapStatus does, after persistence).
//
// Flapping ends when fewer than StopBelow transitions remain in the window,
// or once the status has held for Threshold consecutive judgments, so a soul
// that goes hard down while flagged is not muted for a whole window.
func (r *soulRunner) observeFlap(status core.SoulStatus, now time.Time, cfg core.FlapSettings) (bool, string, core.SoulStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	stop := func() (bool, string, core.SoulStatus) {
		baseline := r.flapBaseline
		r.flapping = false
		r.flapBaseline = ""
		r.flapTransitions = nil
		r.flapSteady = 0
		return false, core.FlapStopped, baseline
	}
	if !cfg.Enabled {
		r.flapTransitions = nil
		if r.flapping {
			return stop()
		}
		return false, "", ""
	}
	prev := r.lastStatus
	if prev != "" && prev != core.SoulUnknown && prev != status {
		r.flapTransitions = append(r.flapTransitions, now)
		r.flapSteady = 0
	} else {
		r.flapSteady++
	}
	cutoff := now.Add(-cfg.Window)
	drop := 0
	for drop < len(r.flapTransitions) && !r.flapTransitions[drop].After(cutoff) {
		drop++
	}
	// Only the newest Threshold transitions matter for either decision.
	if keep := len(r.flapTransitions) - drop; keep > cfg.Threshold {
		drop = len(r.flapTransitions) - cfg.Threshold
	}
	if drop > 0 {
		r.flapTransitions = append(r.flapTransitions[:0], r.flapTransitions[drop:]...)
	}
	n := len(r.flapTransitions)
	switch {
	case !r.flapping && n >= cfg.Threshold:
		r.flapping = true
		r.flapBaseline = prev
		return true, core.FlapStarted, prev
	case r.flapping && (n < cfg.StopBelow() || r.flapSteady >= cfg.Threshold):
		return stop()
	}
	if r.flapping {
		return true, "", r.flapBaseline
	}
	return false, "", ""
}

// restoreFlap seeds a new runner from the soul's latest stored judgment, so
// a runner restart (interval change, ownership move, process restart) keeps
// a flapping soul flagged and still sends its single "stopped" notice. The
// seeded transitions age out one window after that judgment.
//
// A stale flag is restored too: its seeded transitions are already outside
// the window, so the next judgment sends the owed "stopped" notice.
func (r *soulRunner) restoreFlap(latest *core.Judgment, cfg core.FlapSettings) {
	if latest == nil || latest.Details == nil || !latest.Details.Flapping {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lastStatus != "" || r.flapping {
		return // the runner already judged; live state wins
	}
	r.flapping = true
	r.flapBaseline = latest.Details.FlapBaseline
	r.lastStatus = latest.Status
	seed := cfg.StopBelow()
	if seed < 1 {
		seed = 1
	}
	r.flapTransitions = make([]time.Time, seed)
	for i := range r.flapTransitions {
		r.flapTransitions[i] = latest.Timestamp
	}
}

// latestJudgmentStore is optionally implemented by Storage; the engine uses
// it to restore flap state when a runner starts.
type latestJudgmentStore interface {
	LatestJudgment(ctx context.Context, workspaceID, soulID string, since time.Time) (*core.Judgment, error)
}

// restoreRunnerFlap loads the latest judgment and seeds the runner's flap
// state from it. Errors are ignored: the soul then starts unflagged.
func (e *Engine) restoreRunnerFlap(ctx context.Context, runner *soulRunner) {
	store, ok := e.store.(latestJudgmentStore)
	if !ok {
		return
	}
	soul := runner.getSoul()
	now := e.now()
	settings := core.ResolveFlapSettings(soul.FlapDetection, e.config.FlapDetection)
	// The previous runner judged about one interval ago; looking further
	// back would make every runner start scan a day of history.
	since := now.Add(-2*runner.interval - time.Minute)
	latest, err := store.LatestJudgment(ctx, soul.WorkspaceID, soul.ID, since)
	if err != nil || latest == nil {
		return
	}
	runner.restoreFlap(latest, settings)
}
