package raft

// Regression coverage for the snapshot restore that must not hold the node
// lock.
//
// handleInstallSnapshot replaced the whole keyspace while holding n.mu. A
// large restore therefore blocked the heartbeat path, AppendEntries and the
// election timer: the node could not answer the leader while it was
// installing the leader's snapshot, so a healthy leader looked dead and this
// node could start a competing election over a snapshot it had not finished
// installing.
//
// The restore now runs without n.mu. applyMu still serializes it against the
// apply path and snapshot capture, so the FSM state is replaced atomically
// with respect to committed entries.

import (
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// gatedFSM is an FSM whose Restore blocks until the test releases it, so the
// restore can be observed while it is in flight.
type gatedFSM struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once

	mu       sync.Mutex
	restored []byte
}

func newGatedFSM() *gatedFSM {
	return &gatedFSM{entered: make(chan struct{}), release: make(chan struct{})}
}

func (f *gatedFSM) Apply(*core.RaftLogEntry) interface{} { return nil }

func (f *gatedFSM) Snapshot() (core.FSMCommand, error) { return core.FSMCommand{}, nil }

func (f *gatedFSM) Restore(data []byte) error {
	f.once.Do(func() { close(f.entered) })
	<-f.release
	f.mu.Lock()
	f.restored = append([]byte(nil), data...)
	f.mu.Unlock()
	return nil
}

// restoredPayload returns the image the FSM was restored with.
func (f *gatedFSM) restoredPayload() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.restored...)
}

// nodeRestoringFlag reads the published restore flag under the node lock.
//
// The read is bounded: the regression this file covers is a restore that
// holds the node lock, so an unbounded read would hang the test instead of
// reporting the defect. It reports ok=false when the lock could not be taken
// in time, which is itself the failure the fix prevents.
func nodeRestoringFlag(n *Node, timeout time.Duration) (restoring bool, ok bool) {
	type result struct {
		value bool
	}
	ch := make(chan result, 1)
	go func() {
		n.mu.RLock()
		defer n.mu.RUnlock()
		ch <- result{value: n.restoring}
	}()
	select {
	case got := <-ch:
		return got.value, true
	case <-time.After(timeout):
		return false, false
	}
}

// TestInstallSnapshotDoesNotBlockHeartbeatsWhileRestoring is the core of
// the fix: while a restore is in flight the node must still answer the
// leader. The heartbeat is answered before the restore is released, which
// proves the restore holds no node lock.
func TestInstallSnapshotDoesNotBlockHeartbeatsWhileRestoring(t *testing.T) {
	fsm := newGatedFSM()
	n, err := NewNode(newTestRaftNodeConfig(), NewInMemoryLogStore(), NewInMemorySnapshotStore(), fsm, newTestRaftLogger())
	if err != nil {
		t.Fatalf("failed to create test node: %v", err)
	}
	n.currentTerm = 2

	payload := []byte(`{"ws/souls/value":"restored"}`)
	req := &core.InstallSnapshotRequest{
		Term:              2,
		LeaderID:          "leader",
		LastIncludedIndex: 3,
		LastIncludedTerm:  1,
		Data:              payload,
		Done:              true,
	}
	installDone := make(chan *core.InstallSnapshotResponse, 1)
	go func() { installDone <- n.handleInstallSnapshot(req) }()

	// The gate: the restore is in flight.
	select {
	case <-fsm.entered:
	case <-time.After(15 * time.Second):
		t.Fatal("the restore never started")
	}

	// The transition is published, so the node knows it must not campaign
	// while its state is being replaced. The read is bounded: a restore that
	// holds the node lock must be reported as the defect, not hang the test.
	restoring, ok := nodeRestoringFlag(n, 15*time.Second)
	if !ok {
		t.Fatal("the restore holds the node lock: the node cannot be inspected while it runs")
	}
	if !restoring {
		t.Fatal("the restore was not published as in flight")
	}

	// A heartbeat must be answered while the restore is in flight.
	heartbeatDone := make(chan *core.AppendEntriesResponse, 1)
	go func() {
		heartbeatDone <- n.handleAppendEntries(&core.AppendEntriesRequest{
			Term:         2,
			LeaderID:     "leader",
			PrevLogIndex: 0,
			PrevLogTerm:  0,
			LeaderCommit: 0,
		})
	}()
	select {
	case resp := <-heartbeatDone:
		if !resp.Success {
			t.Fatalf("the heartbeat was rejected: %+v", resp)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the heartbeat was blocked while the snapshot restore was in flight")
	}

	// The heartbeat was processed: the node learned who leads it.
	n.mu.RLock()
	leaderID := n.leaderID
	n.mu.RUnlock()
	if leaderID != "leader" {
		t.Fatalf("the heartbeat was not processed, leaderID is %q", leaderID)
	}

	// The restore is released and the install completes.
	close(fsm.release)
	select {
	case resp := <-installDone:
		if !resp.Success {
			t.Fatalf("the snapshot install failed: %+v", resp)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the snapshot install never finished")
	}

	if got := string(fsm.restoredPayload()); got != string(payload) {
		t.Fatalf("the FSM was restored with %q, want %q", got, payload)
	}
	if n.isRestoring() {
		t.Fatal("the node is still marked as restoring")
	}
	n.mu.RLock()
	lastApplied, commitIndex, logBase := n.lastApplied, n.commitIndex, n.logBase
	n.mu.RUnlock()
	if lastApplied != 3 || commitIndex != 3 || logBase != 3 {
		t.Fatalf("the snapshot boundary was not published: lastApplied %d commitIndex %d logBase %d", lastApplied, commitIndex, logBase)
	}
}
