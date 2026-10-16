package alert

import (
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// sharedDedupStorage stands in for the cluster's replicated keyspace: every
// manager built on it sees the dedup records the others shared.
type sharedDedupStorage struct {
	*mockAlertStorage
	mu      *sync.Mutex
	entries map[string]core.AlertHistoryEntry
	deletes *int
}

func newSharedDedupStorage() *sharedDedupStorage {
	deletes := 0
	return &sharedDedupStorage{mu: &sync.Mutex{}, entries: make(map[string]core.AlertHistoryEntry), deletes: &deletes}
}

// node returns a view of the shared records over its own channels and rules,
// as each cluster node has.
func (s *sharedDedupStorage) node(base *mockAlertStorage) *sharedDedupStorage {
	return &sharedDedupStorage{mockAlertStorage: base, mu: s.mu, entries: s.entries, deletes: s.deletes}
}

func (s *sharedDedupStorage) SaveDedupState(_ string, entry *core.AlertHistoryEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[entry.Key] = *entry
	return nil
}

func (s *sharedDedupStorage) DeleteDedupState(_ string, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, key)
	*s.deletes++
	return nil
}

func (s *sharedDedupStorage) ListDedupState() ([]*core.AlertHistoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*core.AlertHistoryEntry, 0, len(s.entries))
	for _, entry := range s.entries {
		copied := entry
		out = append(out, &copied)
	}
	return out, nil
}

func failoverAlertStorage() *mockAlertStorage {
	return &mockAlertStorage{
		channels: map[string]*core.AlertChannel{
			"failover-channel": {ID: "failover-channel", Name: "Failover", Type: core.ChannelWebHook, Enabled: true},
		},
		rules: map[string]*core.AlertRule{
			dedupRegressionRuleID: {
				ID:         dedupRegressionRuleID,
				Name:       "Failover Rule",
				Enabled:    true,
				Scope:      core.RuleScope{Type: "all"},
				Conditions: []core.AlertCondition{{Type: "consecutive_failures", Threshold: 1}},
				Channels:   []string{"failover-channel"},
				Cooldown:   core.Duration{Duration: time.Hour},
			},
		},
	}
}

func startFailoverManager(t *testing.T, storage AlertStorage, dispatcher *dedupRegressionDispatcher) *Manager {
	t.Helper()
	manager := NewManager(storage, newTestLogger())
	manager.dispatchers[core.ChannelWebHook] = dispatcher
	if err := manager.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = manager.Stop() })
	return manager
}

// Rules are evaluated only on a soul's owner, and the record of when an alert
// was last sent lived only in that node's memory. After the soul moved, the
// new owner had no record and notified again inside the cooldown. The record
// must be shared, and a node taking the soul over must honour it.
func TestDedupStateSurvivesOwnershipMove(t *testing.T) {
	cases := []struct {
		name      string
		shared    bool
		wantCalls int
	}{
		{"shared dedup state suppresses the repeat", true, 0},
		{"node-local dedup state (control) repeats", false, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			shared := newSharedDedupStorage()

			oldOwnerDispatch := &dedupRegressionDispatcher{}
			oldOwner := startFailoverManager(t, shared.node(failoverAlertStorage()), oldOwnerDispatch)
			if _, success := dedupRegressionJudgment(t, oldOwner, oldOwnerDispatch, 1); success != 1 {
				t.Fatalf("the old owner did not notify the first failure (successes=%d)", success)
			}
			waitForDedupEntries(t, shared, 1)

			var newOwnerStorage AlertStorage = failoverAlertStorage()
			if tc.shared {
				newOwnerStorage = shared.node(failoverAlertStorage())
			}
			newOwnerDispatch := &dedupRegressionDispatcher{}
			newOwner := startFailoverManager(t, newOwnerStorage, newOwnerDispatch)
			dedupRegressionJudgment(t, newOwner, newOwnerDispatch, tc.wantCalls)
			time.Sleep(150 * time.Millisecond)
			if calls, _ := newOwnerDispatch.counts(); calls != tc.wantCalls {
				t.Fatalf("the new owner sent %d notifications inside the cooldown, want %d", calls, tc.wantCalls)
			}
		})
	}
}

// A record shared while this node already runs is applied as it replicates,
// and an older replica never shortens a newer cooldown.
func TestLoadDedupStateKeepsNewestRecord(t *testing.T) {
	dispatcher := &dedupRegressionDispatcher{}
	manager := startFailoverManager(t, failoverAlertStorage(), dispatcher)
	key := dedupKey(dedupRegressionRuleID, dedupRegressionSoulID, core.SoulDead)

	manager.LoadDedupState(&core.AlertHistoryEntry{Key: key, Count: 1, FirstSent: time.Now(), LastSent: time.Now(), SoulStatus: core.SoulDead})
	manager.LoadDedupState(&core.AlertHistoryEntry{Key: key, Count: 1, LastSent: time.Now().Add(-2 * time.Hour), SoulStatus: core.SoulDead})
	manager.LoadDedupState(&core.AlertHistoryEntry{Key: "ratelimit:not-dedup", LastSent: time.Now()})

	dedupRegressionJudgment(t, manager, dispatcher, 0)
	time.Sleep(150 * time.Millisecond)
	if calls, _ := dispatcher.counts(); calls != 0 {
		t.Fatalf("a replicated dedup record did not suppress the alert (calls=%d)", calls)
	}
	if _, ok := manager.history.Entries["ratelimit:not-dedup"]; ok {
		t.Fatal("LoadDedupState installed a record that is not a dedup record")
	}

	manager.UnloadDedupState(key)
	dedupRegressionJudgment(t, manager, dispatcher, 1)
	if calls, _ := dispatcher.counts(); calls != 1 {
		t.Fatalf("after the record was released the alert was not sent (calls=%d)", calls)
	}
}

// An undelivered alert releases its dedup record cluster-wide, so the next
// owner retries it rather than inheriting a cooldown nobody was told about.
func TestUndeliveredAlertReleasesSharedDedupState(t *testing.T) {
	shared := newSharedDedupStorage()
	dispatcher := &dedupRegressionDispatcher{failOnce: true}
	manager := startFailoverManager(t, shared.node(failoverAlertStorage()), dispatcher)
	dedupRegressionJudgment(t, manager, dispatcher, 1)

	deadline := time.Now().Add(5 * time.Second)
	for {
		shared.mu.Lock()
		deletes, remaining := *shared.deletes, len(shared.entries)
		shared.mu.Unlock()
		if deletes == 1 && remaining == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the failed delivery did not release the shared record (deletes=%d, remaining=%d)", deletes, remaining)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitForDedupEntries(t *testing.T, shared *sharedDedupStorage, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		shared.mu.Lock()
		n := len(shared.entries)
		shared.mu.Unlock()
		if n >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d shared dedup records, want %d", n, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// A node that takes a soul over continues its failure streak and status clock
// from the replicated judgments instead of starting from zero.
func TestSeedFromJudgments(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	at := func(minutes int, status core.SoulStatus) *core.Judgment {
		return &core.Judgment{SoulID: "s", Status: status, Timestamp: base.Add(time.Duration(minutes) * time.Minute)}
	}
	cases := []struct {
		name       string
		judgments  []*core.Judgment
		preset     int // existing streak, -1 for none
		wantStreak int
		wantStatus core.SoulStatus
		wantSince  time.Time
	}{
		{"trailing failures, unordered", []*core.Judgment{at(3, core.SoulDead), at(1, core.SoulAlive), at(2, core.SoulDead), at(4, core.SoulDead)}, -1, 3, core.SoulDead, base.Add(2 * time.Minute)},
		{"recovered", []*core.Judgment{at(1, core.SoulDead), at(2, core.SoulAlive)}, -1, 0, core.SoulAlive, base.Add(2 * time.Minute)},
		{"all failing", []*core.Judgment{at(1, core.SoulDead), at(2, core.SoulDead)}, -1, 2, core.SoulDead, base.Add(time.Minute)},
		{"live state is kept", []*core.Judgment{at(1, core.SoulDead), at(2, core.SoulDead)}, 7, 7, core.SoulDead, base.Add(time.Minute)},
		{"no history", nil, -1, 0, "", time.Time{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewManager(nil, newTestLogger())
			m.rules["r"] = &core.AlertRule{ID: "r"}
			if tc.preset >= 0 {
				m.failureStreak["r:s"] = tc.preset
			}
			m.SeedFromJudgments("s", tc.judgments)
			if got := m.failureStreakCount("r", "s"); got != tc.wantStreak {
				t.Fatalf("streak = %d, want %d", got, tc.wantStreak)
			}
			hold, ok := m.statusSince["s"]
			if tc.wantStatus == "" {
				if ok {
					t.Fatalf("status clock seeded from no history: %+v", hold)
				}
				return
			}
			if !ok || hold.Status != tc.wantStatus || !hold.Since.Equal(tc.wantSince) {
				t.Fatalf("status clock = %+v (set %v), want %s since %v", hold, ok, tc.wantStatus, tc.wantSince)
			}
		})
	}
}
