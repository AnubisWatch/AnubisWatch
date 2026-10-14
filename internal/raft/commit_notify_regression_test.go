package raft

// Regression coverage for the commit-signal path in internal/raft/node.go.
//
// handleAppendEntries holds n.mu and ends in a commit-index advance, while
// checkCommit (leader path) does the same under its documented
// "Caller must hold n.mu.Lock()" contract. Both used to do a BLOCKING send on
// commitCh, whose only receive lives in the run loop at node.go:
//
//	case idx := <-n.commitCh:
//	    n.processCommitted(idx)
//
// The run loop is the same goroutine that dispatched these calls via
// handleRPC, so once commitCh (cap 16) filled, the loop blocked while holding
// n.mu and nothing could drain it again: the follower stopped answering
// AppendEntries, heartbeats and elections, and every goroutine needing n.mu was
// locked out.
//
// notifyCommit now signals without blocking, discarding a stale pending value
// when the buffer is full. That is safe because processCommitted applies every
// entry up to the index it receives, so only the newest pending index matters.

import (
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// fillCommitSignal saturates the commit-signal buffer.
func fillCommitSignal(n *Node) {
	for i := 0; i < cap(n.commitCh); i++ {
		n.commitCh <- uint64(i + 1)
	}
}

// leaderAppendEntries builds a request that advances the follower's commit index.
func leaderAppendEntries(entries int) *core.AppendEntriesRequest {
	req := &core.AppendEntriesRequest{
		Term:         1,
		LeaderID:     "leader-1",
		PrevLogIndex: 0,
		LeaderCommit: uint64(entries),
	}
	for i := 1; i <= entries; i++ {
		req.Entries = append(req.Entries, core.RaftLogEntry{Index: uint64(i), Term: 1})
	}
	return req
}

// runWithDeadline calls fn on a goroutine and reports whether it finished.
func runWithDeadline(t *testing.T, d time.Duration, fn func()) bool {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// TestAppendEntriesDoesNotBlockOnFullCommitSignal is the regression's core: a
// follower must keep answering the leader when its commit buffer is saturated.
func TestAppendEntriesDoesNotBlockOnFullCommitSignal(t *testing.T) {
	n := createTestNode(t)
	fillCommitSignal(n)

	if !runWithDeadline(t, 2*time.Second, func() { n.handleAppendEntries(leaderAppendEntries(2)) }) {
		t.Fatal("handleAppendEntries blocked on a full commitCh while holding n.mu; its " +
			"only consumer is the run loop goroutine that called it, so the follower deadlocks")
	}
}

// TestNodeMutexIsNotHeldAfterAppendEntries pins the blast radius: once the node
// handles an AppendEntries it must still be able to take n.mu.
func TestNodeMutexIsNotHeldAfterAppendEntries(t *testing.T) {
	n := createTestNode(t)
	fillCommitSignal(n)

	if !runWithDeadline(t, 2*time.Second, func() { n.handleAppendEntries(leaderAppendEntries(2)) }) {
		t.Fatal("handleAppendEntries blocked on a full commitCh; cannot check lock release")
	}
	if !runWithDeadline(t, time.Second, func() {
		n.mu.RLock()
		n.mu.RUnlock()
	}) {
		t.Fatal("n.mu is still held after AppendEntries; the follower cannot process " +
			"heartbeats, elections or any other RPC")
	}
}

// TestRepeatedAppendEntriesDoNotWedge walks the buffer past capacity the way a
// leader retrying heartbeats would.
func TestRepeatedAppendEntriesDoNotWedge(t *testing.T) {
	n := createTestNode(t)

	for i := 0; i < cap(n.commitCh)+8; i++ {
		req := leaderAppendEntries(2)
		if !runWithDeadline(t, 2*time.Second, func() { n.handleAppendEntries(req) }) {
			t.Fatalf("follower wedged on AppendEntries #%d of %d; the commit signal "+
				"buffer filled and the send had no concurrent consumer",
				i+1, cap(n.commitCh)+8)
		}
	}
}

// TestNotifyCommitKeepsNewestIndexWhenFull pins the coalescing contract: a
// saturated buffer must end up carrying the newest commit index, never a stale
// one, because processCommitted applies everything up to the index it gets.
func TestNotifyCommitKeepsNewestIndexWhenFull(t *testing.T) {
	n := createTestNode(t)
	fillCommitSignal(n)

	n.notifyCommit(9999)

	var newest uint64
	for len(n.commitCh) > 0 {
		newest = <-n.commitCh
	}
	if newest != 9999 {
		t.Fatalf("commit signal carried index %d after coalescing, want the newest 9999", newest)
	}
}

// TestNotifyCommitNeverBlocksWhenFull is the direct guard on the helper itself.
func TestNotifyCommitNeverBlocksWhenFull(t *testing.T) {
	n := createTestNode(t)
	fillCommitSignal(n)

	if !runWithDeadline(t, time.Second, func() {
		n.notifyCommit(42)
	}) {
		t.Fatal("notifyCommit blocked on a full commitCh; it must never block")
	}
}

// TestAppendEntriesWithEmptyCommitSignal is the control: the ordinary case with
// spare capacity must keep working unchanged.
func TestAppendEntriesWithEmptyCommitSignal(t *testing.T) {
	n := createTestNode(t)

	if !runWithDeadline(t, 2*time.Second, func() { n.handleAppendEntries(leaderAppendEntries(2)) }) {
		t.Fatal("control broken: handleAppendEntries blocked with an empty commitCh")
	}

	n.mu.RLock()
	commitIndex := n.commitIndex
	n.mu.RUnlock()
	if commitIndex != 2 {
		t.Fatalf("commitIndex = %d after AppendEntries with LeaderCommit=2, want 2", commitIndex)
	}
}

// TestCommitIndexStillAdvancesWhenSignalIsFull guards the other half: saturating
// the buffer must not cost the follower the commit-index advance itself.
func TestCommitIndexStillAdvancesWhenSignalIsFull(t *testing.T) {
	n := createTestNode(t)
	fillCommitSignal(n)

	if !runWithDeadline(t, 2*time.Second, func() { n.handleAppendEntries(leaderAppendEntries(2)) }) {
		t.Fatal("handleAppendEntries blocked on a full commitCh")
	}

	n.mu.RLock()
	commitIndex := n.commitIndex
	n.mu.RUnlock()
	if commitIndex != 2 {
		t.Fatalf("commitIndex = %d, want 2; the coalesced signal must not drop the advance",
			commitIndex)
	}
}
