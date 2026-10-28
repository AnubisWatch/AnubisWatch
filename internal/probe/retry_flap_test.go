package probe

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// scriptedChecker returns the scripted statuses in order (the last one
// repeats) and records the semaphore occupancy seen by each attempt.
type scriptedChecker struct {
	mu       sync.Mutex
	script   []core.SoulStatus
	calls    int
	semSeen  []int
	engine   *Engine
	failWith error
}

func (c *scriptedChecker) Type() core.CheckType           { return core.CheckTCP }
func (c *scriptedChecker) Validate(soul *core.Soul) error { return nil }
func (c *scriptedChecker) Judge(ctx context.Context, soul *core.Soul) (*core.Judgment, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.calls
	if i >= len(c.script) {
		i = len(c.script) - 1
	}
	c.calls++
	if c.engine != nil {
		c.semSeen = append(c.semSeen, len(c.engine.semaphore))
	}
	if c.failWith != nil && c.script[i] == core.SoulDead {
		return nil, c.failWith
	}
	return &core.Judgment{SoulID: soul.ID, Status: c.script[i], Message: string(c.script[i])}, nil
}

func (c *scriptedChecker) setScript(s ...core.SoulStatus) {
	c.mu.Lock()
	c.script = s
	c.calls = 0
	c.mu.Unlock()
}

type retryClock struct {
	mu    sync.Mutex
	now   time.Time
	waits []time.Duration
}

func (f *retryClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *retryClock) Advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
}

// Wait records the pause and advances the clock instead of sleeping.
func (f *retryClock) Wait(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	f.waits = append(f.waits, d)
	f.now = f.now.Add(d)
	f.mu.Unlock()
	return nil
}

type recordingAlerter struct {
	mu    sync.Mutex
	calls []*core.Judgment
	prev  []core.SoulStatus
}

func (a *recordingAlerter) ProcessJudgment(soul *core.Soul, prev core.SoulStatus, j *core.Judgment) {
	a.mu.Lock()
	a.calls = append(a.calls, j)
	a.prev = append(a.prev, prev)
	a.mu.Unlock()
}

type retryHarness struct {
	engine  *Engine
	checker *scriptedChecker
	store   *recordingProbeStorage
	alerter *recordingAlerter
	clock   *retryClock
	runner  *soulRunner
}

func newRetryHarness(t *testing.T, soul *core.Soul, cfg EngineConfig, script ...core.SoulStatus) *retryHarness {
	t.Helper()
	registry := NewCheckerRegistry()
	checker := &scriptedChecker{script: script}
	registry.Register(checker)
	store := &recordingProbeStorage{souls: map[string]*core.Soul{}}
	alerter := &recordingAlerter{}
	if cfg.MaxConcurrentChecks == 0 {
		cfg.MaxConcurrentChecks = 4
	}
	e := newTestEngine(t, EngineOptions{Registry: registry, Store: store, Alerter: alerter, Config: cfg})
	checker.engine = e
	clock := &retryClock{now: time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)}
	e.now = clock.Now
	e.retryWait = clock.Wait
	runner := &soulRunner{soul: soul, interval: soulInterval(soul)}
	return &retryHarness{engine: e, checker: checker, store: store, alerter: alerter, clock: clock, runner: runner}
}

func retrySoul(retries int, retryInterval time.Duration) *core.Soul {
	return &core.Soul{
		ID: "retry-soul", WorkspaceID: "default", Name: "retry", Type: core.CheckTCP,
		Target: "example.com:443", Weight: core.Duration{Duration: time.Minute},
		Timeout: core.Duration{Duration: time.Second}, Enabled: true,
		Retries: retries, RetryInterval: core.Duration{Duration: retryInterval},
	}
}

func TestRetry_SucceedsOnSecondAttempt(t *testing.T) {
	h := newRetryHarness(t, retrySoul(3, 0), EngineConfig{}, core.SoulDead, core.SoulAlive)
	h.engine.judgeSoul(context.Background(), h.runner)

	if got := h.store.judgmentCount(); got != 1 {
		t.Fatalf("judgments recorded = %d, want exactly 1", got)
	}
	j := h.store.latestJudgment()
	if j.Status != core.SoulAlive {
		t.Fatalf("status = %s, want alive", j.Status)
	}
	if j.Details == nil || j.Details.Attempts != 2 {
		t.Fatalf("attempts = %+v, want 2", j.Details)
	}
	if h.checker.calls != 2 {
		t.Fatalf("checker calls = %d, want 2", h.checker.calls)
	}
	if len(h.clock.waits) != 1 || h.clock.waits[0] != core.DefaultRetryInterval {
		t.Fatalf("waits = %v, want one default retry interval", h.clock.waits)
	}
	if len(h.alerter.calls) != 1 {
		t.Fatalf("alerter saw %d judgments, want 1", len(h.alerter.calls))
	}
}

func TestRetry_AllAttemptsFailRecordsSingleDeadJudgment(t *testing.T) {
	h := newRetryHarness(t, retrySoul(2, 2*time.Second), EngineConfig{}, core.SoulDead)
	h.checker.failWith = errors.New("connection refused")
	h.engine.judgeSoul(context.Background(), h.runner)

	if got := h.store.judgmentCount(); got != 1 {
		t.Fatalf("judgments recorded = %d, want exactly 1", got)
	}
	j := h.store.latestJudgment()
	if j.Status != core.SoulDead || j.Details == nil || j.Details.Attempts != 3 {
		t.Fatalf("judgment = %s attempts=%+v, want dead after 3 attempts", j.Status, j.Details)
	}
	if h.checker.calls != 3 {
		t.Fatalf("checker calls = %d, want 3", h.checker.calls)
	}
	if len(h.clock.waits) != 2 || h.clock.waits[0] != 2*time.Second {
		t.Fatalf("waits = %v, want two 2s pauses", h.clock.waits)
	}
}

func TestRetry_ZeroRetriesKeepsSingleAttempt(t *testing.T) {
	h := newRetryHarness(t, retrySoul(0, 0), EngineConfig{}, core.SoulDead, core.SoulAlive)
	h.engine.judgeSoul(context.Background(), h.runner)

	j := h.store.latestJudgment()
	if h.store.judgmentCount() != 1 || j.Status != core.SoulDead {
		t.Fatalf("want one dead judgment, got %d (%v)", h.store.judgmentCount(), j)
	}
	if j.Details != nil && (j.Details.Attempts != 0 || j.Details.Flapping || j.Details.FlapChange != "") {
		t.Fatalf("retries=0 must not annotate the judgment: %+v", j.Details)
	}
	if h.checker.calls != 1 || len(h.clock.waits) != 0 {
		t.Fatalf("calls=%d waits=%v, want 1 call and no waits", h.checker.calls, h.clock.waits)
	}
}

func TestRetry_StopsBeforeNextScheduledCheck(t *testing.T) {
	soul := retrySoul(5, 4*time.Second)
	soul.Weight = core.Duration{Duration: 10 * time.Second}
	h := newRetryHarness(t, soul, EngineConfig{}, core.SoulDead)
	h.engine.judgeSoul(context.Background(), h.runner)

	// Attempts at t=0, 4s, 8s; a retry at 12s would overrun the 10s interval.
	j := h.store.latestJudgment()
	if j == nil || j.Details == nil || j.Details.Attempts != 3 {
		t.Fatalf("judgment = %+v, want dead after 3 attempts", j)
	}
}

func TestRetry_HeartbeatIgnoresRetries(t *testing.T) {
	soul := &core.Soul{Type: core.CheckHeartbeat, Retries: 3}
	if soul.EffectiveRetries() != 0 {
		t.Fatalf("heartbeat EffectiveRetries = %d, want 0", soul.EffectiveRetries())
	}
}

func TestRetry_CancellationDuringWaitRecordsNothing(t *testing.T) {
	h := newRetryHarness(t, retrySoul(3, 0), EngineConfig{}, core.SoulDead)
	ctx, cancel := context.WithCancel(context.Background())
	h.engine.retryWait = func(ctx context.Context, d time.Duration) error {
		cancel()
		return ctx.Err()
	}
	h.engine.judgeSoul(ctx, h.runner)

	if h.store.judgmentCount() != 0 || len(h.alerter.calls) != 0 {
		t.Fatalf("a canceled assignment recorded %d judgments", h.store.judgmentCount())
	}
	if h.checker.calls != 1 {
		t.Fatalf("checker calls = %d, want 1", h.checker.calls)
	}
	if n := len(h.engine.semaphore); n != 0 {
		t.Fatalf("semaphore slots leaked: %d", n)
	}
	if n := h.engine.checksRunning.Load(); n != 0 {
		t.Fatalf("checksRunning = %d, want 0", n)
	}
}

func TestRetry_ReleasesSemaphoreWhileWaitingAndHonoursIt(t *testing.T) {
	h := newRetryHarness(t, retrySoul(2, 0), EngineConfig{MaxConcurrentChecks: 1}, core.SoulDead, core.SoulAlive)
	var seenWhileWaiting []int
	h.engine.retryWait = func(ctx context.Context, d time.Duration) error {
		seenWhileWaiting = append(seenWhileWaiting, len(h.engine.semaphore))
		return nil
	}
	h.engine.judgeSoul(context.Background(), h.runner)

	if len(seenWhileWaiting) != 1 || seenWhileWaiting[0] != 0 {
		t.Fatalf("semaphore during retry wait = %v, want released (0)", seenWhileWaiting)
	}
	for i, n := range h.checker.semSeen {
		if n != 1 {
			t.Fatalf("attempt %d ran with %d semaphore slots held, want 1", i+1, n)
		}
	}
	if len(h.engine.semaphore) != 0 {
		t.Fatal("semaphore slot leaked after the check")
	}

	// A saturated engine: the retry cannot reacquire a slot, and canceling
	// the assignment must end the wait without recording anything.
	h2 := newRetryHarness(t, retrySoul(2, 0), EngineConfig{MaxConcurrentChecks: 1}, core.SoulDead)
	ctx, cancel := context.WithCancel(context.Background())
	h2.engine.retryWait = func(ctx context.Context, d time.Duration) error {
		h2.engine.semaphore <- struct{}{} // another check takes the only slot
		cancel()
		return nil
	}
	done := make(chan struct{})
	go func() {
		h2.engine.judgeSoul(ctx, h2.runner)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("judgeSoul blocked on a saturated semaphore after cancellation")
	}
	<-h2.engine.semaphore
	if h2.store.judgmentCount() != 0 || h2.checker.calls != 1 {
		t.Fatalf("judgments=%d calls=%d, want 0 and 1", h2.store.judgmentCount(), h2.checker.calls)
	}
}

func TestFlap_StartAndStopMarkJudgments(t *testing.T) {
	soul := retrySoul(0, 0)
	soul.FlapDetection = &core.FlapDetectionConfig{Enabled: true, Threshold: 3, Window: core.Duration{Duration: 10 * time.Minute}}
	h := newRetryHarness(t, soul, EngineConfig{}, core.SoulAlive)

	statuses := []core.SoulStatus{core.SoulAlive, core.SoulDead, core.SoulAlive, core.SoulDead}
	var last *core.Judgment
	for i, st := range statuses {
		h.checker.setScript(st)
		h.engine.judgeSoul(context.Background(), h.runner)
		h.clock.Advance(time.Minute)
		last = h.store.latestJudgment()
		wantFlapping := i == 3 // 3 transitions after the 4th judgment
		gotFlapping := last.Details != nil && last.Details.Flapping
		if gotFlapping != wantFlapping {
			t.Fatalf("judgment %d flapping = %v, want %v", i, gotFlapping, wantFlapping)
		}
	}
	if last.Details.FlapChange != core.FlapStarted {
		t.Fatalf("flap change = %q, want started", last.Details.FlapChange)
	}

	// Still flapping: the next judgment is flagged without a new change.
	h.checker.setScript(core.SoulDead)
	h.engine.judgeSoul(context.Background(), h.runner)
	if d := h.store.latestJudgment().Details; d == nil || !d.Flapping || d.FlapChange != "" {
		t.Fatalf("steady flapping judgment details = %+v", d)
	}

	// Stable for longer than the window: transitions age out and flapping ends once.
	h.clock.Advance(11 * time.Minute)
	h.engine.judgeSoul(context.Background(), h.runner)
	d := h.store.latestJudgment().Details
	if d == nil || d.Flapping || d.FlapChange != core.FlapStopped {
		t.Fatalf("stabilizing judgment details = %+v, want stopped", d)
	}
	h.engine.judgeSoul(context.Background(), h.runner)
	if d := h.store.latestJudgment().Details; d != nil && (d.Flapping || d.FlapChange != "") {
		t.Fatalf("stable judgment still carries flap state: %+v", d)
	}
}

func TestFlap_DisabledByDefault(t *testing.T) {
	h := newRetryHarness(t, retrySoul(0, 0), EngineConfig{}, core.SoulAlive)
	for i := 0; i < 10; i++ {
		if i%2 == 0 {
			h.checker.setScript(core.SoulAlive)
		} else {
			h.checker.setScript(core.SoulDead)
		}
		h.engine.judgeSoul(context.Background(), h.runner)
		if d := h.store.latestJudgment().Details; d != nil && d.Flapping {
			t.Fatalf("judgment %d flagged flapping with detection disabled", i)
		}
	}
}

func TestFlap_GlobalPolicyAndSoulOverride(t *testing.T) {
	global := core.FlapDetectionConfig{Enabled: true, Threshold: 2}
	soul := retrySoul(0, 0)
	h := newRetryHarness(t, soul, EngineConfig{FlapDetection: global}, core.SoulAlive)
	for _, st := range []core.SoulStatus{core.SoulAlive, core.SoulDead, core.SoulAlive} {
		h.checker.setScript(st)
		h.engine.judgeSoul(context.Background(), h.runner)
	}
	if d := h.store.latestJudgment().Details; d == nil || d.FlapChange != core.FlapStarted {
		t.Fatalf("global policy did not start flapping: %+v", d)
	}

	// Disabling it on the soul ends flapping with one stopped change.
	h.runner.setSoul(&core.Soul{ID: soul.ID, Name: soul.Name, Type: soul.Type, Target: soul.Target,
		Weight: soul.Weight, Timeout: soul.Timeout, FlapDetection: &core.FlapDetectionConfig{Enabled: false}})
	h.checker.setScript(core.SoulDead)
	h.engine.judgeSoul(context.Background(), h.runner)
	if d := h.store.latestJudgment().Details; d == nil || d.Flapping || d.FlapChange != core.FlapStopped {
		t.Fatalf("soul override did not stop flapping: %+v", d)
	}
}

func TestObserveFlap_BoundsHistory(t *testing.T) {
	r := &soulRunner{lastStatus: core.SoulAlive}
	cfg := core.FlapSettings{Enabled: true, Threshold: 4, Window: time.Hour}
	now := time.Unix(0, 0)
	statuses := []core.SoulStatus{core.SoulDead, core.SoulAlive}
	for i := 0; i < 100; i++ {
		st := statuses[i%2]
		_, _, _ = r.observeFlap(st, now.Add(time.Duration(i)*time.Second), cfg)
		r.lastStatus = st
	}
	if len(r.flapTransitions) > cfg.Threshold {
		t.Fatalf("kept %d transitions, want at most %d", len(r.flapTransitions), cfg.Threshold)
	}
	if !r.flapping {
		t.Fatal("expected flapping")
	}
}

// Regression (review P1): a soul that settles into one status while flagged
// stops flapping after Threshold steady judgments instead of a full window,
// and the stop judgment carries the pre-flap baseline.
func TestFlap_SteadyStatusEndsFlappingWithBaseline(t *testing.T) {
	soul := retrySoul(0, 0)
	soul.FlapDetection = &core.FlapDetectionConfig{Enabled: true, Threshold: 3, Window: core.Duration{Duration: time.Hour}}
	h := newRetryHarness(t, soul, EngineConfig{}, core.SoulAlive)
	for _, st := range []core.SoulStatus{core.SoulAlive, core.SoulDead, core.SoulAlive, core.SoulDead} {
		h.checker.setScript(st)
		h.engine.judgeSoul(context.Background(), h.runner)
	}
	d := h.store.latestJudgment().Details
	if d == nil || d.FlapChange != core.FlapStarted || d.FlapBaseline != core.SoulAlive {
		t.Fatalf("start details = %+v, want started with alive baseline", d)
	}
	// Hard down: dead for Threshold judgments, well inside the 1h window.
	for i := 0; i < 3; i++ {
		h.engine.judgeSoul(context.Background(), h.runner)
		h.clock.Advance(time.Minute)
	}
	d = h.store.latestJudgment().Details
	if d == nil || d.Flapping || d.FlapChange != core.FlapStopped || d.FlapBaseline != core.SoulAlive {
		t.Fatalf("stop details = %+v, want stopped with alive baseline", d)
	}
	if h.store.latestJudgment().Status != core.SoulDead {
		t.Fatal("expected the stop judgment to be dead")
	}
}

type latestStore struct {
	recordingProbeStorage
	latest *core.Judgment
	since  time.Time
}

func (s *latestStore) LatestJudgment(ctx context.Context, ws, soulID string, since time.Time) (*core.Judgment, error) {
	s.since = since
	return s.latest, nil
}

// Regression (review P2): a new runner (interval change, owner move,
// restart) restores flap state from the latest stored judgment, keeps the
// soul flagged and later sends the single owed "stopped".
func TestFlap_RestoredWhenRunnerRestarts(t *testing.T) {
	soul := retrySoul(0, 0)
	soul.FlapDetection = &core.FlapDetectionConfig{Enabled: true, Threshold: 4, Window: core.Duration{Duration: 10 * time.Minute}}
	h := newRetryHarness(t, soul, EngineConfig{}, core.SoulDead)
	store := &latestStore{recordingProbeStorage: recordingProbeStorage{souls: map[string]*core.Soul{}}}
	h.engine.store = store
	h.store = &store.recordingProbeStorage
	store.latest = &core.Judgment{SoulID: soul.ID, Status: core.SoulDead, Timestamp: h.clock.Now().Add(-time.Minute),
		Details: &core.JudgmentDetails{Flapping: true, FlapBaseline: core.SoulAlive}}

	h.engine.restoreRunnerFlap(context.Background(), h.runner)
	if want := h.clock.Now().Add(-2*h.runner.interval - time.Minute); !store.since.Equal(want) {
		t.Fatalf("lookup since = %v, want %v", store.since, want)
	}
	h.engine.judgeSoul(context.Background(), h.runner)
	d := h.store.latestJudgment().Details
	if d == nil || !d.Flapping || d.FlapChange != "" || d.FlapBaseline != core.SoulAlive {
		t.Fatalf("first judgment after restart = %+v, want still flapping without a new start", d)
	}
	h.clock.Advance(11 * time.Minute)
	h.engine.judgeSoul(context.Background(), h.runner)
	d = h.store.latestJudgment().Details
	if d == nil || d.Flapping || d.FlapChange != core.FlapStopped || d.FlapBaseline != core.SoulAlive {
		t.Fatalf("after the window = %+v, want one stopped with the restored baseline", d)
	}

	// A stale flag (older than the window) is closed by the next judgment.
	r2 := &soulRunner{soul: soul, interval: h.runner.interval}
	store.latest.Timestamp = h.clock.Now().Add(-time.Hour)
	h.engine.restoreRunnerFlap(context.Background(), r2)
	h.engine.judgeSoul(context.Background(), r2)
	if d := h.store.latestJudgment().Details; d == nil || d.FlapChange != core.FlapStopped {
		t.Fatalf("stale restored flag = %+v, want stopped", d)
	}

	// Non-flapping history restores nothing.
	r3 := &soulRunner{soul: soul, interval: h.runner.interval}
	store.latest = &core.Judgment{SoulID: soul.ID, Status: core.SoulAlive, Timestamp: h.clock.Now()}
	h.engine.restoreRunnerFlap(context.Background(), r3)
	if r3.flapping || r3.lastStatus != "" {
		t.Fatalf("restored from a non-flapping judgment: %+v", r3)
	}
}

// windowedLatestStore honours since, like the real store.
type windowedLatestStore struct {
	recordingProbeStorage
	latest *core.Judgment
	calls  []time.Time
}

func (s *windowedLatestStore) LatestJudgment(ctx context.Context, ws, soulID string, since time.Time) (*core.Judgment, error) {
	s.calls = append(s.calls, since)
	if s.latest == nil || s.latest.Timestamp.Before(since) {
		return nil, nil
	}
	return s.latest, nil
}

// A process that was down longer than two intervals still restores a flag
// set within the flap window, so the owed "stopped" notice is sent. The
// wider scan runs only when the short one is empty and flap detection is on.
func TestFlap_RestoredAfterLongOutage(t *testing.T) {
	soul := retrySoul(0, 0)
	soul.FlapDetection = &core.FlapDetectionConfig{Enabled: true, Threshold: 4, Window: core.Duration{Duration: 10 * time.Minute}}
	h := newRetryHarness(t, soul, EngineConfig{}, core.SoulDead)
	store := &windowedLatestStore{recordingProbeStorage: recordingProbeStorage{souls: map[string]*core.Soul{}}}
	h.engine.store = store
	short := 2*h.runner.interval + time.Minute
	if short >= 8*time.Minute {
		t.Fatalf("control: interval %v leaves no gap between the scans", h.runner.interval)
	}
	flagged := func(age time.Duration) *core.Judgment {
		return &core.Judgment{SoulID: soul.ID, Status: core.SoulDead, Timestamp: h.clock.Now().Add(-age),
			Details: &core.JudgmentDetails{Flapping: true, FlapBaseline: core.SoulAlive}}
	}

	store.latest = flagged(8 * time.Minute)
	r := &soulRunner{soul: soul, interval: h.runner.interval}
	h.engine.restoreRunnerFlap(context.Background(), r)
	if !r.flapping || r.flapBaseline != core.SoulAlive {
		t.Fatalf("flag 8m old (window 10m) not restored: %+v", r)
	}
	if len(store.calls) != 2 || !store.calls[1].Equal(h.clock.Now().Add(-10*time.Minute)) {
		t.Fatalf("lookups = %v, want short then window", store.calls)
	}

	// Older than the window is beyond the bounded scan: the soul starts
	// unflagged (documented limit).
	store.calls, store.latest = nil, flagged(20*time.Minute)
	r = &soulRunner{soul: soul, interval: h.runner.interval}
	h.engine.restoreRunnerFlap(context.Background(), r)
	if r.flapping {
		t.Fatal("flag older than the window was restored")
	}

	// Flap detection off: only the short scan runs.
	soul.FlapDetection = &core.FlapDetectionConfig{Enabled: false}
	store.calls, store.latest = nil, flagged(8*time.Minute)
	r = &soulRunner{soul: soul, interval: h.runner.interval}
	h.engine.restoreRunnerFlap(context.Background(), r)
	if len(store.calls) != 1 {
		t.Fatalf("lookups with flap detection off = %d, want 1", len(store.calls))
	}
}
