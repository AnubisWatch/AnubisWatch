package raft

import (
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type gatedReplicationTransport struct {
	*mockTransport
	gate             chan struct{}
	blocked, healthy atomic.Int64
	requests         chan *core.AppendEntriesRequest
	failFirst        bool
}

func (tr *gatedReplicationTransport) SendAppendEntries(id string, req *core.AppendEntriesRequest) (*core.AppendEntriesResponse, error) {
	if id == "blocked" {
		attempt := tr.blocked.Add(1)
		tr.requests <- req
		<-tr.gate
		if tr.failFirst && attempt == 1 {
			return nil, errors.New("isolated peer")
		}
	} else {
		tr.healthy.Add(1)
	}
	return &core.AppendEntriesResponse{Term: req.Term, Success: true, MatchIndex: req.PrevLogIndex + uint64(len(req.Entries))}, nil
}

func replicationGateNode(t *testing.T) (*Node, *gatedReplicationTransport) {
	t.Helper()
	n := createTestNode(t)
	n.currentTerm = 1
	n.state.Store(core.StateLeader)
	n.peers["blocked"] = &Peer{ID: "blocked", nextIndex: 1}
	n.peers["healthy"] = &Peer{ID: "healthy", nextIndex: 1}
	tr := &gatedReplicationTransport{mockTransport: &mockTransport{}, gate: make(chan struct{}), requests: make(chan *core.AppendEntriesRequest, 128)}
	n.SetTransport(tr)
	return n, tr
}

func TestReplicationCoalescesBlockedPeerWithoutStallingHealthyPeer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		n, tr := replicationGateNode(t)
		released := false
		defer func() {
			if !released {
				close(tr.gate)
			}
		}()
		n.sendHeartbeats(0)
		synctest.Wait()
		for i := 0; i < 100; i++ {
			if i%2 == 0 {
				n.sendHeartbeats(0)
			} else {
				n.replicateLog()
			}
			synctest.Wait()
		}
		if tr.blocked.Load() != 1 || tr.healthy.Load() != 101 {
			t.Fatalf("one blocked RPC must coalesce while healthy peer progresses: blocked=%d healthy=%d", tr.blocked.Load(), tr.healthy.Load())
		}
		close(tr.gate)
		released = true
		synctest.Wait()
		n.replicateLog()
		synctest.Wait()
		if tr.blocked.Load() != 2 {
			t.Fatalf("completed peer must accept a fresh request: %d", tr.blocked.Load())
		}
	})
}

func TestReplicationErrorReleasesPeerAndCapturesFreshLog(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		n, tr := replicationGateNode(t)
		tr.failFirst = true
		n.sendHeartbeats(0)
		synctest.Wait()
		first := <-tr.requests
		if len(first.Entries) != 0 {
			t.Fatal("initial control should have an empty suffix")
		}
		if err := n.appendEntry(core.RaftLogEntry{Term: 1, Type: core.LogNoOp}); err != nil {
			close(tr.gate)
			t.Fatal(err)
		}
		n.replicateLog()
		synctest.Wait()
		close(tr.gate)
		synctest.Wait()
		n.sendHeartbeats(0)
		synctest.Wait()
		if tr.blocked.Load() != 2 {
			t.Fatalf("failed RPC must release its slot, without stale queued sends: %d", tr.blocked.Load())
		}
		retry := <-tr.requests
		if len(retry.Entries) != 1 || retry.Entries[0].Index != 1 {
			t.Fatalf("retry must capture current log: %+v", retry.Entries)
		}
	})
}

func TestReplicationSlotReleasedAfterStepdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		n, tr := replicationGateNode(t)
		n.sendHeartbeats(0)
		synctest.Wait()
		<-tr.requests
		n.mu.Lock()
		err := n.becomeFollower(2)
		n.mu.Unlock()
		close(tr.gate)
		synctest.Wait()
		if err != nil {
			t.Fatal(err)
		}
		n.state.Store(core.StateLeader)
		n.replicateLog()
		synctest.Wait()
		if tr.blocked.Load() != 2 {
			t.Fatalf("old leadership's completion retained the peer slot: %d", tr.blocked.Load())
		}
		if req := <-tr.requests; req.Term != 2 {
			t.Fatalf("new send must capture new term: %d", req.Term)
		}
	})
}

type gatedSnapshotList struct {
	SnapshotStore
	gate  chan struct{}
	calls atomic.Int64
}

func (s *gatedSnapshotList) List() ([]SnapshotMeta, error) {
	s.calls.Add(1)
	<-s.gate
	return nil, errors.New("snapshot unavailable")
}

func TestSnapshotAndLogReplicationSharePeerReservation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		n, tr := replicationGateNode(t)
		close(tr.gate)
		snapshot := &gatedSnapshotList{SnapshotStore: n.snapshot, gate: make(chan struct{})}
		n.snapshot = snapshot
		// The leader compacted through index 5: a peer that needs index 1 is
		// served by a snapshot. (nextIndex 0 without compaction is plain
		// replication from index 1, since replicateLog and sendHeartbeats
		// now share one path.)
		n.logBase, n.logBaseTerm = 5, 1
		n.log = []core.RaftLogEntry{{Index: 5, Term: 1}}
		n.peers["blocked"].nextIndex = 1
		n.peers["healthy"].nextIndex = 6
		n.sendHeartbeats(0)
		synctest.Wait()
		for i := 0; i < 20; i++ {
			n.sendHeartbeats(0)
			n.replicateLog()
			synctest.Wait()
		}
		calls, appends := snapshot.calls.Load(), tr.blocked.Load()
		close(snapshot.gate)
		synctest.Wait()
		if calls != 1 || appends != 0 {
			t.Fatalf("blocked snapshot must coalesce both dispatch paths: snapshots=%d appends=%d", calls, appends)
		}
		n.peers["blocked"].nextIndex = 6
		n.replicateLog()
		synctest.Wait()
		if tr.blocked.Load() != 1 {
			t.Fatalf("snapshot error did not release peer slot: %d", tr.blocked.Load())
		}
	})
}
