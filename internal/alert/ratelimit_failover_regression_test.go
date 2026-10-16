package alert

import (
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// sharedHistoryStore records shared history entries like the cluster adapter.
type sharedHistoryStore struct {
	*mockAlertStorage
	saved map[string]core.AlertHistoryEntry
}

func (s *sharedHistoryStore) SaveDedupState(_ string, entry *core.AlertHistoryEntry) error {
	s.saved[entry.Key] = *entry
	return nil
}
func (s *sharedHistoryStore) DeleteDedupState(_, key string) error { delete(s.saved, key); return nil }
func (s *sharedHistoryStore) ListDedupState() ([]*core.AlertHistoryEntry, error) {
	out := make([]*core.AlertHistoryEntry, 0, len(s.saved))
	for _, e := range s.saved {
		e := e
		out = append(out, &e)
	}
	return out, nil
}

// Channel rate-limit windows lived only in the owner's memory, so after a
// soul moved to another node its channel could be notified MaxAlerts more
// times within the same window.
func TestRateLimitWindowSurvivesOwnershipMove(t *testing.T) {
	store := &sharedHistoryStore{mockAlertStorage: &mockAlertStorage{}, saved: map[string]core.AlertHistoryEntry{}}
	channel := &core.AlertChannel{ID: "ch", RateLimit: core.RateLimitConfig{
		Enabled: true, MaxAlerts: 2, Window: core.Duration{Duration: time.Hour},
	}}
	event := &core.AlertEvent{SoulID: "soul", WorkspaceID: "default", Status: core.SoulDead}

	owner := NewManager(store, newTestLogger())
	for i := 0; i < 2; i++ {
		if owner.isRateLimited(channel, event) {
			t.Fatalf("alert %d limited on the original owner", i+1)
		}
	}
	key := rateLimitKey("ch", "soul", "")
	if got := store.saved[key].Count; got != 2 {
		t.Fatalf("shared window count = %d, want 2", got)
	}

	// The new owner loads the shared state (as on start or replicated apply).
	successor := NewManager(store, newTestLogger())
	successor.loadDedupState()
	if !successor.isRateLimited(channel, event) {
		t.Fatal("new owner sent past the channel's rate limit within the window")
	}
}
