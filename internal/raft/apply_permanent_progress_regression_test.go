package raft

// Regression coverage for the permanent-failure paths of the apply
// classification and for the snapshot restore that must not hold the node
// lock.
//
// A committed entry can fail to apply for two very different reasons. A
// transient storage failure may still succeed on a later attempt, so the node
// retries it (see apply_retry_regression_test.go). A deterministic failure --
// a rejected command, an unknown op, an undecodable payload -- can never
// succeed, so retrying it forever wedges the node at that index. A wedged node
// also stops snapshotting and compacting, because a snapshot may describe only
// the state that was actually applied.
//
// These tests pin the observable consequences of that distinction: the node
// keeps progressing after a permanent failure, compaction is not stalled
// behind it, the node does not campaign while its state is being replaced,
// and a snapshot that was overtaken by a newer term is not reported as
// accepted.

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// TestPermanentApplyFailureDoesNotStallSnapshotCompaction is the consequence
// the wedged node produced: a permanent failure must not stop the node from
// snapshotting and compacting.
//
// The node skipped the failing entry, so the applied index moved past it and a
// snapshot may describe the applied state. Before the classification existed,
// the failing entry kept the applied index behind, so no snapshot could ever
// be taken and the log was never compacted.
func TestPermanentApplyFailureDoesNotStallSnapshotCompaction(t *testing.T) {
	store := newFlakyStore("", 0)
	fsm := NewStorageFSM(store)
	// A handler rejects the command deterministically, the way a replicated
	// judgment that cannot be decoded is rejected.
	fsm.SetCommandHandler("judgments", func(cmd *core.FSMCommand) error {
		return errors.New("decode replicated judgment: invalid payload")
	})

	n, err := NewNode(newTestRaftNodeConfig(), NewInMemoryLogStore(), NewInMemorySnapshotStore(), fsm, newTestRaftLogger())
	if err != nil {
		t.Fatalf("failed to create test node: %v", err)
	}

	entry := batchEntry(t, 1, "node-a",
		core.FSMCommand{Op: core.FSMSet, Key: "good", Value: []byte("1")},
		core.FSMCommand{Op: core.FSMSet, Table: "judgments", Key: "bad", Value: []byte("{}")},
	)
	n.log = append(n.log, *entry)
	n.commitIndex = 1
	n.processCommitted(1)

	// The permanent failure did not wedge the node: the entry was consumed.
	n.mu.RLock()
	lastApplied := n.lastApplied
	n.mu.RUnlock()
	if lastApplied != 1 {
		t.Fatalf("lastApplied is %d, want 1: the permanent failure wedged the node", lastApplied)
	}
	if fsm.LastApplied() != 1 {
		t.Fatalf("the FSM applied index is %d, want 1", fsm.LastApplied())
	}
	if count := store.writeCount("good"); count != 1 {
		t.Fatalf("key \"good\" was written %d times, want 1", count)
	}

	// The consequence: the node can still snapshot and compact. A snapshot
	// describes the applied state, and the applied index moved past the
	// failed entry, so the snapshot is allowed to describe it.
	n.snapshotThreshold = 1
	n.maybeTakeSnapshot()

	n.mu.RLock()
	logBase := n.logBase
	n.mu.RUnlock()
	if logBase != 1 {
		t.Fatalf("logBase is %d, want 1: the permanent failure stalled compaction", logBase)
	}
	metas, err := n.snapshot.List()
	if err != nil {
		t.Fatalf("failed to list snapshots: %v", err)
	}
	if len(metas) != 1 || metas[0].Index != 1 {
		t.Fatalf("the permanent failure stalled snapshotting: %+v", metas)
	}
}

// TestPermanentApplyFailureOfUnknownOpIsConsumed covers the deterministic
// failure of an unknown op: no attempt can ever apply it, so the node must
// consume the entry instead of retrying it forever.
func TestPermanentApplyFailureOfUnknownOpIsConsumed(t *testing.T) {
	store := newFlakyStore("", 0)
	fsm := NewStorageFSM(store)

	n, err := NewNode(newTestRaftNodeConfig(), NewInMemoryLogStore(), NewInMemorySnapshotStore(), fsm, newTestRaftLogger())
	if err != nil {
		t.Fatalf("failed to create test node: %v", err)
	}

	// An op the FSM does not implement. It is deterministic: every attempt
	// rejects it the same way.
	entry := batchEntry(t, 1, "node-a",
		core.FSMCommand{Op: core.FSMSet, Key: "good", Value: []byte("1")},
		core.FSMCommand{Op: core.FSMOp(99), Key: "unknown"},
	)
	n.log = append(n.log, *entry)
	n.commitIndex = 1
	n.processCommitted(1)

	n.mu.RLock()
	lastApplied := n.lastApplied
	n.mu.RUnlock()
	if lastApplied != 1 {
		t.Fatalf("lastApplied is %d, want 1: the unknown op wedged the node", lastApplied)
	}
	if count := store.writeCount("good"); count != 1 {
		t.Fatalf("key \"good\" was written %d times, want 1", count)
	}
}

// TestNodeDoesNotCampaignWhileRestoringSnapshot covers the election side of
// the restore. A restore replaces the whole keyspace and can take a long
// time; the node cannot answer the leader while it runs, so a campaign during
// the restore would disrupt a healthy cluster over a snapshot that was still
// being installed.
func TestNodeDoesNotCampaignWhileRestoringSnapshot(t *testing.T) {
	fsm := newGatedFSM()
	n, err := NewNode(newTestRaftNodeConfig(), NewInMemoryLogStore(), NewInMemorySnapshotStore(), fsm, newTestRaftLogger())
	if err != nil {
		t.Fatalf("failed to create test node: %v", err)
	}
	n.currentTerm = 2

	payload := []byte(`{"ws/souls/value":"restored"}`)
	done := make(chan *core.InstallSnapshotResponse, 1)
	go func() {
		done <- n.handleInstallSnapshot(&core.InstallSnapshotRequest{
			Term:              2,
			LeaderID:          "leader",
			LastIncludedIndex: 3,
			LastIncludedTerm:  1,
			Data:              payload,
			Done:              true,
		})
	}()

	// The gate: the restore is in flight.
	select {
	case <-fsm.entered:
	case <-time.After(15 * time.Second):
		t.Fatal("the restore never started")
	}

	// The node must not campaign while its state is being replaced. The
	// campaign is bounded: a restore that holds the node lock must be
	// reported as the defect, not hang the test.
	campaignDone := make(chan struct{})
	go func() {
		n.startElection()
		close(campaignDone)
	}()
	select {
	case <-campaignDone:
	case <-time.After(15 * time.Second):
		t.Fatal("the campaign could not be decided while the restore was in flight: the restore holds the node lock")
	}
	n.mu.RLock()
	term := n.currentTerm
	state := n.state.Load().(core.RaftState)
	n.mu.RUnlock()
	if term != 2 || state == core.StateLeader {
		t.Fatalf("the node campaigned during the restore: term=%d state=%s", term, state)
	}

	close(fsm.release)
	select {
	case resp := <-done:
		if !resp.Success {
			t.Fatalf("the snapshot install failed: %+v", resp)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the snapshot install never finished")
	}

	// The control: once the restore is over, the node campaigns again, so
	// the check above was caused by the restore and not by the test setup.
	n.startElection()
	n.mu.RLock()
	term = n.currentTerm
	state = n.state.Load().(core.RaftState)
	n.mu.RUnlock()
	if term == 2 || state != core.StateLeader {
		t.Fatalf("the node did not campaign after the restore: term=%d state=%s", term, state)
	}
}

// TestInstallSnapshotRejectsStaleTermAfterRestore covers the term recheck of
// the restore. The destructive restore runs without the node lock, so a newer
// term can arrive while it is in flight. The image is already installed at
// that point, so the boundary is published and persisted -- but the request
// belongs to a term this node has left, and the stale leader must not be told
// the snapshot was accepted.
func TestInstallSnapshotRejectsStaleTermAfterRestore(t *testing.T) {
	fsm := newGatedFSM()
	stable := NewInMemoryStableStore()
	n, err := NewNodeWithStableStore(newTestRaftNodeConfig(), NewInMemoryLogStore(), stable, NewInMemorySnapshotStore(), fsm, newTestRaftLogger())
	if err != nil {
		t.Fatalf("failed to create test node: %v", err)
	}
	n.currentTerm = 2

	payload := []byte(`{"ws/souls/value":"restored"}`)
	done := make(chan *core.InstallSnapshotResponse, 1)
	go func() {
		done <- n.handleInstallSnapshot(&core.InstallSnapshotRequest{
			Term:              2,
			LeaderID:          "old-leader",
			LastIncludedIndex: 3,
			LastIncludedTerm:  1,
			Data:              payload,
			Done:              true,
		})
	}()

	// The gate: the restore is in flight.
	select {
	case <-fsm.entered:
	case <-time.After(15 * time.Second):
		t.Fatal("the restore never started")
	}

	// A newer leader takes over while the restore runs.
	n.handleHeartbeat(&core.HeartbeatRequest{Term: 3, LeaderID: "new-leader"})

	close(fsm.release)
	var resp *core.InstallSnapshotResponse
	select {
	case resp = <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the snapshot install never finished")
	}
	if resp.Success {
		t.Fatalf("the stale leader was told the snapshot was accepted: %+v", resp)
	}
	if resp.Term != 3 {
		t.Fatalf("the response reports term %d, want 3", resp.Term)
	}

	// The persisted hard state follows the term the node is now in, and
	// agrees with the image that was installed.
	raw, err := stable.Get(stableHardState)
	if err != nil {
		t.Fatalf("failed to read the persisted hard state: %v", err)
	}
	var state hardState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("failed to decode the persisted hard state: %v", err)
	}
	if state.CurrentTerm != 3 {
		t.Fatalf("the persisted term is %d, want 3", state.CurrentTerm)
	}
	if state.LastApplied != 3 || state.LogBase != 3 {
		t.Fatalf("the persisted boundary is applied=%d base=%d, want 3/3", state.LastApplied, state.LogBase)
	}
}

// TestPermanentFailureIsNotRetriedForever pins the retry side of the
// classification: a deterministic failure is decided exactly once. The node
// must not re-attempt it, and it must not leave a retry pending that would
// re-apply the entry it already consumed.
func TestPermanentFailureIsNotRetriedForever(t *testing.T) {
	store := newFlakyStore("", 0)
	fsm := NewStorageFSM(store)

	var mu sync.Mutex
	attempts := 0
	fsm.SetCommandHandler("judgments", func(cmd *core.FSMCommand) error {
		mu.Lock()
		attempts++
		mu.Unlock()
		return errors.New("decode replicated judgment: invalid payload")
	})

	n, err := NewNode(newTestRaftNodeConfig(), NewInMemoryLogStore(), NewInMemorySnapshotStore(), fsm, newTestRaftLogger())
	if err != nil {
		t.Fatalf("failed to create test node: %v", err)
	}

	entry := batchEntry(t, 1, "node-a",
		core.FSMCommand{Op: core.FSMSet, Table: "judgments", Key: "bad", Value: []byte("{}")},
	)
	n.log = append(n.log, *entry)
	n.commitIndex = 1
	n.processCommitted(1)

	n.mu.RLock()
	lastApplied := n.lastApplied
	n.mu.RUnlock()
	if lastApplied != 1 {
		t.Fatalf("lastApplied is %d, want 1", lastApplied)
	}

	// No retry may be pending for a failure that can never succeed.
	select {
	case <-n.applyRetryCh:
		t.Fatal("a permanent failure scheduled an apply retry")
	default:
	}

	mu.Lock()
	got := attempts
	mu.Unlock()
	if got != 1 {
		t.Fatalf("the permanent failure was attempted %d times, want exactly 1", got)
	}
}
