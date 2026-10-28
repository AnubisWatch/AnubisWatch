package probe

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// fakeClock is a manually advanced clock.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

// hbStore is a replicated-storage stand-in: ping state plus judgments.
type hbStore struct {
	mu        sync.Mutex
	states    map[string]*core.HeartbeatState
	judgments []*core.Judgment
}

func newHBStore() *hbStore { return &hbStore{states: map[string]*core.HeartbeatState{}} }

func (s *hbStore) ping(soulID string, ev core.HeartbeatEvent, at time.Time, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.states[soulID]
	if st == nil {
		st = &core.HeartbeatState{SoulID: soulID}
		s.states[soulID] = st
	}
	st.Apply(ev, at, msg, "", "")
}

func (s *hbStore) GetHeartbeatState(_ context.Context, _, soulID string) (*core.HeartbeatState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st := s.states[soulID]; st != nil {
		cp := *st
		return &cp, nil
	}
	return nil, nil
}

func (s *hbStore) LatestJudgment(_ context.Context, _, soulID string) (*core.Judgment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var latest *core.Judgment
	for _, j := range s.judgments {
		if j.SoulID == soulID && (latest == nil || j.Timestamp.After(latest.Timestamp)) {
			latest = j
		}
	}
	return latest, nil
}

func (s *hbStore) SaveJudgment(_ context.Context, j *core.Judgment) error {
	s.mu.Lock()
	s.judgments = append(s.judgments, j)
	s.mu.Unlock()
	return nil
}

func (s *hbStore) GetSoul(context.Context, string, string) (*core.Soul, error) { return nil, nil }
func (s *hbStore) ListSouls(context.Context, string) ([]*core.Soul, error)     { return nil, nil }

func (s *hbStore) statuses() []core.SoulStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]core.SoulStatus, len(s.judgments))
	for i, j := range s.judgments {
		out[i] = j.Status
	}
	return out
}

func heartbeatSoul(created time.Time) *core.Soul {
	s := &core.Soul{
		ID: "hb-1", WorkspaceID: "default", Name: "cron", Type: core.CheckHeartbeat,
		Weight: core.Duration{Duration: time.Minute}, Enabled: true, CreatedAt: created,
		Heartbeat: &core.HeartbeatConfig{Grace: core.Duration{Duration: 30 * time.Second}},
	}
	_ = s.EnsureHeartbeatToken()
	return s
}

// judge runs one evaluation and stores the result like the engine does.
func judge(t *testing.T, c *HeartbeatChecker, store *hbStore, soul *core.Soul) *core.Judgment {
	t.Helper()
	j, err := c.Judge(context.Background(), soul)
	if errors.Is(err, ErrNoJudgment) {
		return nil
	}
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	_ = store.SaveJudgment(context.Background(), j)
	return j
}

func TestHeartbeatCheckerLifecycle(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: t0}
	store := newHBStore()
	c := NewHeartbeatChecker(store)
	c.SetClock(clock.Now)
	soul := heartbeatSoul(t0)

	if err := c.Validate(soul); err != nil {
		t.Fatal(err)
	}
	// Fresh soul inside its first deadline: nothing recorded.
	if j := judge(t, c, store, soul); j != nil {
		t.Fatalf("fresh soul judged: %+v", j)
	}

	// /start then success ping: alive, duration measured, stamped at ping time.
	clock.Advance(10 * time.Second)
	store.ping(soul.ID, core.HeartbeatStart, clock.Now(), "")
	if j := judge(t, c, store, soul); j != nil {
		t.Fatalf("start ping must not judge: %+v", j)
	}
	clock.Advance(5 * time.Second)
	pingAt := clock.Now()
	store.ping(soul.ID, core.HeartbeatSuccess, pingAt, "")
	j := judge(t, c, store, soul)
	if j == nil || j.Status != core.SoulAlive || j.Duration != 5*time.Second || !j.Timestamp.Equal(pingAt) {
		t.Fatalf("ping judgment = %+v", j)
	}
	// The same ping is judged once.
	if j := judge(t, c, store, soul); j != nil {
		t.Fatalf("ping judged twice: %+v", j)
	}

	// Inside interval+grace (90s) after the ping: nothing.
	clock.Advance(89 * time.Second)
	if j := judge(t, c, store, soul); j != nil {
		t.Fatalf("judged before deadline: %+v", j)
	}
	// Deadline passed: dead.
	clock.Advance(2 * time.Second)
	j = judge(t, c, store, soul)
	if j == nil || j.Status != core.SoulDead {
		t.Fatalf("missed deadline = %+v, want dead", j)
	}
	// Not again within the interval ...
	clock.Advance(30 * time.Second)
	if j := judge(t, c, store, soul); j != nil {
		t.Fatalf("dead repeated within interval: %+v", j)
	}
	// ... but again once a full interval passed.
	clock.Advance(31 * time.Second)
	if j := judge(t, c, store, soul); j == nil || j.Status != core.SoulDead {
		t.Fatalf("repeated dead = %+v", j)
	}

	// Recovery.
	clock.Advance(5 * time.Second)
	store.ping(soul.ID, core.HeartbeatSuccess, clock.Now(), "")
	if j := judge(t, c, store, soul); j == nil || j.Status != core.SoulAlive || j.Duration != 0 {
		t.Fatalf("recovery = %+v", j)
	}

	// Explicit failure.
	clock.Advance(5 * time.Second)
	store.ping(soul.ID, core.HeartbeatFail, clock.Now(), "disk full")
	j = judge(t, c, store, soul)
	if j == nil || j.Status != core.SoulDead || j.Message != "job reported failure: disk full" {
		t.Fatalf("fail ping = %+v", j)
	}

	want := []core.SoulStatus{core.SoulAlive, core.SoulDead, core.SoulDead, core.SoulAlive, core.SoulDead}
	got := store.statuses()
	if len(got) != len(want) {
		t.Fatalf("judgments = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("judgments = %v, want %v", got, want)
		}
	}
}

func TestHeartbeatCheckerNeverPinged(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: t0.Add(2 * time.Minute)}
	store := newHBStore()
	c := NewHeartbeatChecker(store)
	c.SetClock(clock.Now)
	soul := heartbeatSoul(t0) // created 2m ago, deadline was 90s after creation
	if j := judge(t, c, store, soul); j == nil || j.Status != core.SoulDead {
		t.Fatalf("never-pinged soul past its deadline = %+v, want dead", j)
	}
}

// A new owner (restart or cluster takeover) seeds its progress from the
// replicated judgments: it neither re-judges a ping the previous owner judged
// nor repeats a dead judgment before the interval elapsed.
func TestHeartbeatCheckerTakeoverSeedsFromStorage(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: t0}
	store := newHBStore()
	soul := heartbeatSoul(t0)

	first := NewHeartbeatChecker(store)
	first.SetClock(clock.Now)
	clock.Advance(10 * time.Second)
	store.ping(soul.ID, core.HeartbeatSuccess, clock.Now(), "")
	if j := judge(t, first, store, soul); j == nil || j.Status != core.SoulAlive {
		t.Fatalf("first owner = %+v", j)
	}

	second := NewHeartbeatChecker(store)
	second.SetClock(clock.Now)
	if j := judge(t, second, store, soul); j != nil {
		t.Fatalf("new owner re-judged an old ping: %+v", j)
	}
	clock.Advance(91 * time.Second)
	if j := judge(t, first, store, soul); j == nil || j.Status != core.SoulDead {
		t.Fatalf("first owner dead = %+v", j)
	}
	third := NewHeartbeatChecker(store)
	third.SetClock(clock.Now)
	clock.Advance(10 * time.Second)
	if j := judge(t, third, store, soul); j != nil {
		t.Fatalf("new owner repeated dead within interval: %+v", j)
	}
}

func TestHeartbeatEvalInterval(t *testing.T) {
	s := heartbeatSoul(time.Now())
	if got := heartbeatEvalInterval(s); got != 7500*time.Millisecond {
		t.Fatalf("eval interval = %s", got)
	}
	s.Weight = core.Duration{Duration: 24 * time.Hour}
	s.Heartbeat.Grace = core.Duration{}
	if got := heartbeatEvalInterval(s); got != 30*time.Second {
		t.Fatalf("eval interval (daily) = %s", got)
	}
	s.Weight = core.Duration{Duration: time.Second}
	if got := heartbeatEvalInterval(s); got != time.Second {
		t.Fatalf("eval interval floor = %s", got)
	}
}

// End to end through the engine: a ping kicks the runner, which records the
// alive judgment through the normal path (storage, callback, alerter); no
// judgment is recorded while the deadline holds.
func TestEngineHeartbeatPingIsJudgedThroughNormalPath(t *testing.T) {
	store := newHBStore()
	registry := &CheckerRegistry{checkers: map[core.CheckType]Checker{}}
	checker := NewHeartbeatChecker(store)
	registry.Register(checker)

	var mu sync.Mutex
	var alerted []core.SoulStatus
	alerter := alerterFunc(func(_ *core.Soul, _ core.SoulStatus, j *core.Judgment) {
		mu.Lock()
		alerted = append(alerted, j.Status)
		mu.Unlock()
	})
	published := make(chan *core.Judgment, 4)
	e := NewEngine(EngineOptions{
		Registry: registry, Store: store, Alerter: alerter, NodeID: "n1",
		OnJudgment: func(j *core.Judgment) { published <- j },
	})
	defer e.Stop()

	soul := heartbeatSoul(time.Now())
	e.AssignSouls([]*core.Soul{soul})
	waitFor(t, func() bool { return e.Liveness().LastJudgmentAt.After(time.Time{}) })
	if n := len(store.statuses()); n != 0 {
		t.Fatalf("judgments before any ping: %d", n)
	}

	store.ping(soul.ID, core.HeartbeatSuccess, time.Now(), "")
	if !e.EvaluateSoul(soul.ID) {
		t.Fatal("EvaluateSoul: soul not assigned")
	}
	select {
	case j := <-published:
		if j.Status != core.SoulAlive || j.JackalID != "n1" || j.WorkspaceID != "default" {
			t.Fatalf("published = %+v", j)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ping was not judged")
	}
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(alerted) == 1 && alerted[0] == core.SoulAlive
	})
	if e.EvaluateSoul("missing") {
		t.Fatal("EvaluateSoul on unknown soul reported true")
	}
	if _, err := e.TriggerImmediate(context.Background(), soul.ID); !errors.Is(err, ErrNoJudgment) {
		t.Fatalf("TriggerImmediate on a judged heartbeat = %v, want ErrNoJudgment", err)
	}
	e.RemoveSoul(soul.ID)
	checker.mu.Lock()
	_, kept := checker.progress[soul.ID]
	checker.mu.Unlock()
	if kept {
		t.Fatal("RemoveSoul did not forget heartbeat progress")
	}
}

type alerterFunc func(*core.Soul, core.SoulStatus, *core.Judgment)

func (f alerterFunc) ProcessJudgment(s *core.Soul, prev core.SoulStatus, j *core.Judgment) {
	f(s, prev, j)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met")
}

func TestHeartbeatCheckerMemorySource(t *testing.T) {
	c := NewHeartbeatChecker(nil)
	soul := heartbeatSoul(time.Now())
	c.RecordPing(soul.ID, core.HeartbeatSuccess, time.Now(), "")
	j, err := c.Judge(context.Background(), soul)
	if err != nil || j.Status != core.SoulAlive {
		t.Fatalf("memory ping = %+v, %v", j, err)
	}
	if err := c.Validate(&core.Soul{Type: core.CheckHeartbeat}); err == nil {
		t.Fatal("Validate without config must fail")
	}
}
