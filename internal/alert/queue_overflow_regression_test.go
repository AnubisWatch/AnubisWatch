package alert

import (
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"testing"
	"time"
)

func TestQueueOverflowReleasesDedupReservation(t *testing.T) {
	shared := newSharedDedupStorage().node(failoverAlertStorage())
	manager := NewManager(shared, newTestLogger())
	manager.queue = make(chan *core.AlertEvent, 1)
	rule := &core.AlertRule{ID: "queue-rule", Cooldown: core.Duration{Duration: time.Hour}}
	event := func(id, soul string) *core.AlertEvent {
		return &core.AlertEvent{ID: id, RuleID: rule.ID, SoulID: soul, WorkspaceID: "default", Status: core.SoulDead}
	}
	first := event("first", "control")
	manager.enqueueAlert(rule, first)
	if len(manager.queue) != 1 {
		t.Fatal("control was not queued")
	}
	dropped := event("drop", "overflow")
	manager.enqueueAlert(rule, dropped)
	key := dedupKey(rule.ID, dropped.SoulID, dropped.Status)
	if _, exists := manager.history.Entries[key]; exists {
		t.Fatal("dropped event consumed local dedup")
	}
	shared.mu.Lock()
	_, exists := shared.entries[key]
	shared.mu.Unlock()
	if exists {
		t.Fatal("dropped event consumed shared dedup")
	}
	<-manager.queue
	manager.enqueueAlert(rule, dropped)
	if len(manager.queue) != 1 {
		t.Fatal("retry stayed suppressed after queue recovered")
	}
	<-manager.queue
	manager.enqueueAlert(rule, event("repeat", "overflow"))
	if len(manager.queue) != 0 {
		t.Fatal("accepted event was not deduplicated")
	}
}

// Force a previous delivery to complete only after its replacement owns the
// reservation. Releasing the stale event must not delete the replacement.
func TestLateDeliveryFailureKeepsNewDedupOwner(t *testing.T) {
	manager := NewManager(nil, newTestLogger())
	rule := &core.AlertRule{ID: "ownership", Cooldown: core.Duration{Duration: time.Hour}}
	first := &core.AlertEvent{ID: "old", RuleID: rule.ID, SoulID: "s", Status: core.SoulDead}
	next := *first
	next.ID = "new"
	manager.enqueueAlert(rule, first)
	<-manager.queue
	key := dedupKey(rule.ID, first.SoulID, first.Status)
	manager.history.Mu.Lock()
	manager.history.Entries[key].LastSent = time.Now().Add(-2 * time.Hour)
	manager.history.Mu.Unlock()
	release, done := make(chan struct{}), make(chan struct{})
	go func() { <-release; manager.releaseDedup(first); close(done) }()
	manager.enqueueAlert(rule, &next)
	<-manager.queue
	close(release)
	<-done
	manager.enqueueAlert(rule, &core.AlertEvent{ID: "third", RuleID: rule.ID, SoulID: "s", Status: core.SoulDead})
	if len(manager.queue) != 0 {
		t.Fatal("stale completion erased a newer dedup reservation")
	}
	manager.releaseDedup(&next)
	manager.enqueueAlert(rule, &core.AlertEvent{ID: "retry", RuleID: rule.ID, SoulID: "s", Status: core.SoulDead})
	if len(manager.queue) != 1 {
		t.Fatal("current owner's failure did not release its reservation")
	}
}
