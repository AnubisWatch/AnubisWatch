package probe

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// scheduleFakeChecker returns a scripted status without touching the network
// and counts how often it ran.
type scheduleFakeChecker struct {
	status atomic.Value // core.SoulStatus
	fail   atomic.Bool  // return an error instead of a judgment
	calls  atomic.Int64
	ran    chan string // optional: receives the soul ID after each Judge
}

const scheduleFakeType core.CheckType = "schedule-fake"

func newScheduleFakeChecker(status core.SoulStatus) *scheduleFakeChecker {
	c := &scheduleFakeChecker{}
	c.status.Store(status)
	return c
}

func (c *scheduleFakeChecker) Type() core.CheckType      { return scheduleFakeType }
func (c *scheduleFakeChecker) Validate(*core.Soul) error { return nil }
func (c *scheduleFakeChecker) Judge(_ context.Context, s *core.Soul) (*core.Judgment, error) {
	c.calls.Add(1)
	if c.ran != nil {
		defer func() { c.ran <- s.ID }()
	}
	if c.fail.Load() {
		return nil, errors.New("connection refused")
	}
	return &core.Judgment{Status: c.status.Load().(core.SoulStatus), Message: "scripted"}, nil
}

type countingProbeAlerter struct {
	mu    sync.Mutex
	calls []core.SoulStatus
}

func (a *countingProbeAlerter) ProcessJudgment(_ *core.Soul, _ core.SoulStatus, j *core.Judgment) {
	a.mu.Lock()
	a.calls = append(a.calls, j.Status)
	a.mu.Unlock()
}

func (a *countingProbeAlerter) snapshot() []core.SoulStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]core.SoulStatus(nil), a.calls...)
}

// An open circuit used to skip scheduled checks entirely, so an outage left no
// judgments behind and recovery was noticed only after the breaker timeout.
// Every tick must now produce exactly one real judgment, one publication and
// one alert evaluation — no gaps and no extra (synthetic) alert input.
func TestEngineOpenCircuitStillJudges(t *testing.T) {
	cases := []struct {
		name       string
		failByErr  bool
		ticks      int
		thenStatus core.SoulStatus
	}{
		{name: "dead-judgments", ticks: 6, thenStatus: core.SoulAlive},
		{name: "checker-errors", failByErr: true, ticks: 6, thenStatus: core.SoulAlive},
		{name: "still-down-after-open", ticks: 4, thenStatus: core.SoulDead},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checker := newScheduleFakeChecker(core.SoulDead)
			checker.fail.Store(tc.failByErr)
			registry := NewCheckerRegistry()
			registry.Register(checker)
			store := &recordingProbeStorage{}
			alerter := &countingProbeAlerter{}
			var published atomic.Int64
			e := newTestEngine(t, EngineOptions{
				Registry:   registry,
				Store:      store,
				Alerter:    alerter,
				OnJudgment: func(*core.Judgment) { published.Add(1) },
				Config: EngineConfig{CircuitBreaker: CircuitBreakerConfig{
					Enabled: true, FailureThreshold: 2, SuccessThreshold: 3, Timeout: time.Hour,
				}},
			})
			soul := &core.Soul{ID: "cb-" + tc.name, Type: scheduleFakeType, Weight: core.Duration{Duration: time.Minute}}
			runner := &soulRunner{soul: soul, interval: time.Minute, lastStatus: core.SoulUnknown}

			for i := 0; i < tc.ticks; i++ {
				e.judgeSoul(context.Background(), runner)
			}
			cb := e.getCircuitBreaker(soul.ID)
			cb.mu.RLock()
			state := cb.state
			cb.mu.RUnlock()
			if state != "open" {
				t.Fatalf("breaker state = %q after %d failures, want open (precondition)", state, tc.ticks)
			}

			// Recovery (or continued outage) is observed on the very next tick.
			checker.fail.Store(false)
			checker.status.Store(tc.thenStatus)
			e.judgeSoul(context.Background(), runner)

			want := int64(tc.ticks + 1)
			if got := checker.calls.Load(); got != want {
				t.Errorf("checker ran %d times, want %d (no skipped ticks)", got, want)
			}
			if got := int64(store.judgmentCount()); got != want {
				t.Errorf("saved %d judgments, want %d (no history gaps)", got, want)
			}
			if got := published.Load(); got != want {
				t.Errorf("published %d judgments, want %d", got, want)
			}
			calls := alerter.snapshot()
			if int64(len(calls)) != want {
				t.Fatalf("alerter evaluated %d judgments, want %d", len(calls), want)
			}
			if last := calls[len(calls)-1]; last != tc.thenStatus {
				t.Errorf("last evaluated status = %s, want %s", last, tc.thenStatus)
			}
			if got := runner.getStatus(); got != tc.thenStatus {
				t.Errorf("runner status = %s, want %s", got, tc.thenStatus)
			}
		})
	}
}

func TestSoulStartOffset(t *testing.T) {
	cases := []struct {
		name      string
		id        string
		interval  time.Duration
		maxJitter time.Duration
		wantMax   time.Duration // exclusive upper bound; 0 means must be exactly 0
	}{
		{name: "interval-smaller-than-cap", id: "a", interval: 5 * time.Second, maxJitter: 30 * time.Second, wantMax: 5 * time.Second},
		{name: "cap-smaller-than-interval", id: "b", interval: time.Hour, maxJitter: 30 * time.Second, wantMax: 30 * time.Second},
		{name: "jitter-disabled", id: "c", interval: time.Minute, maxJitter: 0},
		{name: "negative-cap", id: "d", interval: time.Minute, maxJitter: -time.Second},
		{name: "empty-id", id: "", interval: time.Minute, maxJitter: 10 * time.Second, wantMax: 10 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := soulStartOffset(tc.id, tc.interval, tc.maxJitter)
			if again := soulStartOffset(tc.id, tc.interval, tc.maxJitter); again != got {
				t.Fatalf("offset not deterministic: %v then %v", got, again)
			}
			if tc.wantMax == 0 {
				if got != 0 {
					t.Fatalf("offset = %v, want 0", got)
				}
				return
			}
			if got < 0 || got >= tc.wantMax {
				t.Fatalf("offset = %v, want in [0, %v)", got, tc.wantMax)
			}
		})
	}

	// Different souls must actually spread out across the window.
	seen := map[time.Duration]bool{}
	for _, id := range []string{"soul-1", "soul-2", "soul-3", "soul-4", "soul-5", "soul-6", "soul-7", "soul-8"} {
		seen[soulStartOffset(id, time.Minute, 30*time.Second)] = true
	}
	if len(seen) < 6 {
		t.Fatalf("8 souls mapped to only %d distinct offsets", len(seen))
	}
}

func runnerDelays(e *Engine) map[string]time.Duration {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make(map[string]time.Duration, len(e.souls))
	for id, r := range e.souls {
		out[id] = r.startDelay
	}
	return out
}

func TestAssignSoulsStartJitter(t *testing.T) {
	const jitter = time.Hour // large enough that delayed souls never run during the test
	mk := func(id string) *core.Soul {
		return &core.Soul{ID: id, Type: scheduleFakeType, Weight: core.Duration{Duration: 2 * time.Hour}}
	}
	cases := []struct {
		name      string
		jitter    time.Duration
		prior     []*core.Soul // assigned first (one at a time, via UpsertSoul)
		assign    []*core.Soul
		wantDelay map[string]bool // soul ID -> expects a non-zero (hash) delay
	}{
		{
			name:      "bulk-start-is-spread",
			jitter:    jitter,
			assign:    []*core.Soul{mk("bulk-a"), mk("bulk-b"), mk("bulk-c")},
			wantDelay: map[string]bool{"bulk-a": true, "bulk-b": true, "bulk-c": true},
		},
		{
			name:      "single-new-soul-runs-now",
			jitter:    jitter,
			prior:     []*core.Soul{mk("old-a"), mk("old-b")},
			assign:    []*core.Soul{mk("old-a"), mk("old-b"), mk("new-c")},
			wantDelay: map[string]bool{"old-a": false, "old-b": false, "new-c": false},
		},
		{
			name:      "jitter-disabled",
			jitter:    0,
			assign:    []*core.Soul{mk("off-a"), mk("off-b")},
			wantDelay: map[string]bool{"off-a": false, "off-b": false},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checker := newScheduleFakeChecker(core.SoulAlive)
			registry := NewCheckerRegistry()
			registry.Register(checker)
			e := newTestEngine(t, EngineOptions{Registry: registry, Config: EngineConfig{StartJitter: tc.jitter}})
			for _, s := range tc.prior {
				e.UpsertSoul(s)
			}
			e.AssignSouls(tc.assign)

			delays := runnerDelays(e)
			for id, wantNonZero := range tc.wantDelay {
				got, ok := delays[id]
				if !ok {
					t.Fatalf("soul %s not scheduled", id)
				}
				want := time.Duration(0)
				if wantNonZero {
					want = soulStartOffset(id, 2*time.Hour, tc.jitter)
				}
				if got != want {
					t.Errorf("soul %s start delay = %v, want %v", id, got, want)
				}
			}
		})
	}
}

// A delayed runner must not check before its offset, while an undelayed one
// (UpsertSoul: API create / manual paths) runs at once; TriggerImmediate
// ("check now") ignores the offset entirely.
func TestStartDelayDefersOnlyScheduledFirstCheck(t *testing.T) {
	checker := newScheduleFakeChecker(core.SoulAlive)
	checker.ran = make(chan string, 8)
	registry := NewCheckerRegistry()
	registry.Register(checker)
	e := newTestEngine(t, EngineOptions{Registry: registry, Config: EngineConfig{StartJitter: time.Hour}})

	delayed := &core.Soul{ID: "delayed", Type: scheduleFakeType, Weight: core.Duration{Duration: 2 * time.Hour}}
	e.mu.Lock()
	e.startSoul(delayed, time.Hour)
	e.mu.Unlock()
	e.UpsertSoul(&core.Soul{ID: "prompt", Type: scheduleFakeType, Weight: core.Duration{Duration: 2 * time.Hour}})

	select {
	case id := <-checker.ran:
		if id != "prompt" {
			t.Fatalf("first check was for %q, want the undelayed soul", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("undelayed soul was not checked promptly")
	}

	if _, err := e.TriggerImmediate(context.Background(), "delayed"); err != nil {
		t.Fatalf("TriggerImmediate: %v", err)
	}
	if id := <-checker.ran; id != "delayed" {
		t.Fatalf("manual check ran for %q, want delayed", id)
	}
	select {
	case id := <-checker.ran:
		t.Fatalf("unexpected scheduled check for %q before its offset", id)
	case <-time.After(100 * time.Millisecond):
	}
	if got := checker.calls.Load(); got != 2 {
		t.Fatalf("checker ran %d times, want 2", got)
	}
}

func TestEngineLiveness(t *testing.T) {
	checker := newScheduleFakeChecker(core.SoulAlive)
	checker.ran = make(chan string, 8)
	registry := NewCheckerRegistry()
	registry.Register(checker)
	e := newTestEngine(t, EngineOptions{Registry: registry, Config: EngineConfig{StartJitter: time.Hour}})

	if l := e.Liveness(); !l.LastJudgmentAt.IsZero() || l.ScheduledSouls != 0 || l.ChecksRunning != 0 {
		t.Fatalf("fresh engine liveness = %+v, want zero", l)
	}

	// Two bulk-assigned souls get an hour-scale offset, so nothing runs on
	// its own: the counts are deterministic.
	e.AssignSouls([]*core.Soul{
		{ID: "live-a", Type: scheduleFakeType, Weight: core.Duration{Duration: 2 * time.Hour}},
		{ID: "live-b", Type: scheduleFakeType, Weight: core.Duration{Duration: 2 * time.Hour}},
	})
	if l := e.Liveness(); l.ScheduledSouls != 2 || !l.LastJudgmentAt.IsZero() {
		t.Fatalf("after assign liveness = %+v, want 2 souls and no judgment", l)
	}

	before := time.Now().UTC()
	if _, err := e.TriggerImmediate(context.Background(), "live-a"); err != nil {
		t.Fatalf("TriggerImmediate: %v", err)
	}
	after := time.Now().UTC()
	l := e.Liveness()
	if l.LastJudgmentAt.Before(before) || l.LastJudgmentAt.After(after) {
		t.Fatalf("LastJudgmentAt = %v, want within [%v, %v]", l.LastJudgmentAt, before, after)
	}

	// A judgment completing "earlier" never moves the clock backwards.
	future := time.Now().Add(time.Hour).UnixNano()
	e.lastJudgmentAt.Store(future)
	e.markJudgment()
	if got := e.lastJudgmentAt.Load(); got != future {
		t.Fatalf("markJudgment moved last judgment from %d to %d", future, got)
	}

	e.RemoveSoul("live-b")
	if l := e.Liveness(); l.ScheduledSouls != 1 {
		t.Fatalf("after remove ScheduledSouls = %d, want 1", l.ScheduledSouls)
	}
}

func TestNewEngineConcurrencyDefaults(t *testing.T) {
	cases := []struct {
		name       string
		max        int
		jitter     time.Duration
		wantMax    int
		wantJitter time.Duration
	}{
		{name: "unset-uses-default", max: 0, wantMax: core.DefaultMaxConcurrentChecks},
		{name: "negative-uses-default", max: -5, jitter: -time.Second, wantMax: core.DefaultMaxConcurrentChecks},
		{name: "explicit", max: 7, jitter: time.Second, wantMax: 7, wantJitter: time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEngine(t, EngineOptions{Config: EngineConfig{MaxConcurrentChecks: tc.max, StartJitter: tc.jitter}})
			if got := cap(e.semaphore); got != tc.wantMax {
				t.Errorf("semaphore cap = %d, want %d", got, tc.wantMax)
			}
			if got := e.Config().StartJitter; got != tc.wantJitter {
				t.Errorf("StartJitter = %v, want %v", got, tc.wantJitter)
			}
		})
	}
	if got := DefaultEngineConfig().StartJitter; got != DefaultStartJitter {
		t.Errorf("DefaultEngineConfig().StartJitter = %v, want %v", got, DefaultStartJitter)
	}
}
