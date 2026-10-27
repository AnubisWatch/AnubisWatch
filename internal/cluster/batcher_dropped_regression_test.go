package cluster

import (
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Overflowing commands used to be visible only in a log line written on the
// next successful flush (and reset by it). DroppedTotal counts them
// monotonically, across enqueue overflow and failed-flush requeue overflow.
func TestBatcherDroppedTotalIsMonotonic(t *testing.T) {
	applier := &fakeApplier{fail: true}
	b := NewBatcher(applier.apply, time.Hour, newTestLogger())
	b.maxPending = 3
	b.maxCommands = 2
	for i := 0; i < 5; i++ {
		b.Enqueue(core.FSMCommand{Op: core.FSMSet, Key: string(rune('a' + i))})
	}
	if got := b.DroppedTotal(); got != 2 {
		t.Fatalf("DroppedTotal after enqueue overflow = %d, want 2", got)
	}
	// A failed flush puts its batch back; the backlog is still at the limit,
	// so a further enqueue during the failure drops again.
	if err := b.flushOnce(); err == nil {
		t.Fatal("flush with a failing applier must fail")
	}
	b.Enqueue(core.FSMCommand{Op: core.FSMSet, Key: "z"})
	if got := b.DroppedTotal(); got != 3 {
		t.Fatalf("DroppedTotal = %d, want 3", got)
	}
	// A successful flush resets the per-log-line count but not the total.
	applier.mu.Lock()
	applier.fail = false
	applier.mu.Unlock()
	if err := b.flushOnce(); err != nil {
		t.Fatal(err)
	}
	if got := b.DroppedTotal(); got != 3 {
		t.Fatalf("DroppedTotal after a successful flush = %d, want 3 (monotonic)", got)
	}
}
