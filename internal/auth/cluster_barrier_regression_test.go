package auth

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Every authenticated request in HA mode runs a quorum barrier. They used to
// run one at a time under the authenticator's mutex, so one slow barrier
// queued the node's whole API. Concurrent callers now share barriers.
func TestBarrierGroupCoalescesConcurrentCallers(t *testing.T) {
	var g barrierGroup
	var calls atomic.Int32
	release := make(chan struct{})
	started := make(chan struct{}, 8)
	fn := func() error {
		calls.Add(1)
		started <- struct{}{}
		<-release
		return nil
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _ = g.do(fn) }()
	<-started // the first barrier is running

	// These arrive while it runs: they must not reuse it (it started before
	// them), and all of them share the single next barrier.
	const waiters = 50
	for i := 0; i < waiters; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = g.do(fn) }()
	}
	time.Sleep(50 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("barriers started while the first runs = %d, want 1", got)
	}
	release <- struct{}{} // finish the first
	<-started             // the shared second one starts
	close(release)
	wg.Wait()
	if got := calls.Load(); got != 2 {
		t.Fatalf("barriers run for 51 concurrent callers = %d, want 2", got)
	}
}

// A caller that arrives after a barrier started waits for a later one, so it
// still sees every write acknowledged before its call.
func TestBarrierGroupNeverReusesAnEarlierBarrier(t *testing.T) {
	var g barrierGroup
	var seq atomic.Int64
	var startedAt sync.Map // call number -> sequence when it started
	release := make(chan struct{})
	first := make(chan struct{})
	fn := func() error {
		n := seq.Add(1)
		startedAt.Store(n, n)
		if n == 1 {
			close(first)
			<-release
		}
		return nil
	}
	done := make(chan struct{})
	go func() { _ = g.do(fn); close(done) }()
	<-first
	arrival := seq.Load()
	late := make(chan struct{})
	go func() { _ = g.do(fn); close(late) }()
	time.Sleep(20 * time.Millisecond)
	select {
	case <-late:
		t.Fatal("a caller that arrived during a barrier returned before a newer one ran")
	default:
	}
	close(release)
	<-done
	<-late
	if seq.Load() <= arrival {
		t.Fatalf("no barrier started after the late caller arrived (seq %d, arrival %d)", seq.Load(), arrival)
	}
}
