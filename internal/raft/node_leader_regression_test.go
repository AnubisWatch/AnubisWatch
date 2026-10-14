package raft

import (
	"errors"
	"fmt"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type regressionLeaderCursorsChoice struct {
	kind, peerID string
	request      core.AppendEntriesRequest
}
type regressionLeaderCursorsTransport struct {
	*mockTransport
	choices chan regressionLeaderCursorsChoice
}

func (tr *regressionLeaderCursorsTransport) SendAppendEntries(peerID string, req *core.AppendEntriesRequest) (*core.AppendEntriesResponse, error) {
	tr.choices <- regressionLeaderCursorsChoice{kind: "append", peerID: peerID, request: *req}
	return nil, errors.New("fixture: no response")
}

type regressionLeaderCursorsSnapshots struct {
	SnapshotStore
	choices chan regressionLeaderCursorsChoice
}

func (s *regressionLeaderCursorsSnapshots) List() ([]SnapshotMeta, error) {
	s.choices <- regressionLeaderCursorsChoice{kind: "snapshot"}
	return nil, nil
}

func TestNodeLeaderInitializesPeerCursors(t *testing.T) {
	cases := []struct {
		name          string
		base          uint64
		prefix, peers int
		next, matched uint64
	}{
		{name: "fresh_peer", prefix: 1, peers: 1},
		{name: "initialized_control", prefix: 1, peers: 1, next: 2},
		{name: "stale_peer_cursors", prefix: 2, peers: 1, next: 20, matched: 19},
		{name: "empty_log", peers: 1},
		{name: "compacted_log", base: 5, prefix: 1, peers: 1},
		{name: "multiple_fresh_peers", prefix: 2, peers: 3},
		{name: "no_peers", prefix: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := createTestNode(t)
			n.currentTerm = 2
			if tc.base > 0 {
				n.logBase, n.logBaseTerm = tc.base, 1
				n.log = []core.RaftLogEntry{{Index: tc.base, Term: 1}}
			}
			for i := 0; i < tc.prefix; i++ {
				if err := n.appendEntry(core.RaftLogEntry{Term: 1, Type: core.LogCommand, Data: []byte(fmt.Sprintf("prefix-%d", i))}); err != nil {
					t.Fatal(err)
				}
			}
			previous := n.lastLogIndexLocked()
			previousTerm := n.getLogTerm(previous)
			for i := 0; i < tc.peers; i++ {
				id := fmt.Sprintf("peer-%d", i)
				n.peers[id] = &Peer{ID: id, nextIndex: tc.next, matchIndex: tc.matched}
				n.nextIndex[id], n.matchIndex[id] = tc.next, tc.matched
			}
			choices := make(chan regressionLeaderCursorsChoice, tc.peers)
			n.snapshot = &regressionLeaderCursorsSnapshots{SnapshotStore: NewInMemorySnapshotStore(), choices: choices}
			n.SetTransport(&regressionLeaderCursorsTransport{mockTransport: &mockTransport{}, choices: choices})
			n.becomeLeader()
			seen := make(map[string]bool)
			for i := 0; i < tc.peers; i++ {
				choice := <-choices
				if choice.kind != "append" {
					t.Fatalf("fresh leadership dispatched %s instead of append", choice.kind)
				}
				if seen[choice.peerID] {
					t.Fatalf("duplicate dispatch for %s", choice.peerID)
				}
				seen[choice.peerID] = true
				req := choice.request
				if req.PrevLogIndex != previous || req.PrevLogTerm != previousTerm || len(req.Entries) != 1 || req.Entries[0].Index != previous+1 || req.Entries[0].Type != core.LogNoOp {
					t.Fatalf("incorrect initial request: %+v", req)
				}
			}
			if n.State() != core.StateLeader || n.lastLogIndexLocked() != previous+1 {
				t.Fatal("leadership/no-op not initialized")
			}
			for id, peer := range n.peers {
				if peer.nextIndex != previous+1 || peer.matchIndex != 0 || n.nextIndex[id] != peer.nextIndex || n.matchIndex[id] != peer.matchIndex {
					t.Fatalf("cursor mismatch for %s: peer=%d/%d, maps=%d/%d", id, peer.nextIndex, peer.matchIndex, n.nextIndex[id], n.matchIndex[id])
				}
			}
		})
	}
}
