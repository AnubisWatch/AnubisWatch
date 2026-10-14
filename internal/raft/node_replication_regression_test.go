package raft

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type regressionReplicationResult struct {
	request  core.AppendEntriesRequest
	response core.AppendEntriesResponse
}
type regressionReplicationTransport struct {
	*mockTransport
	follower *Node
	result   chan regressionReplicationResult
}

func (tr *regressionReplicationTransport) SendAppendEntries(_ string, req *core.AppendEntriesRequest) (*core.AppendEntriesResponse, error) {
	resp := tr.follower.handleAppendEntries(req)
	tr.result <- regressionReplicationResult{request: *req, response: *resp}
	return nil, errors.New("fixture: response withheld")
}

func TestNodeReplicationPreservesFollowerLogIndexes(t *testing.T) {
	cases := []struct {
		name                  string
		base                  uint64
		prefix, suffix, limit int
		matched               uint64
		zeroNext              bool
		repeats               int
	}{
		{name: "unknown_match", prefix: 1, suffix: 1, limit: 64},
		{name: "caught_up_control", prefix: 1, suffix: 1, limit: 64, matched: 1},
		{name: "match_behind_next", prefix: 3, suffix: 2, limit: 64, matched: 1},
		{name: "empty_append", prefix: 2, limit: 64},
		{name: "zero_next_control", suffix: 2, limit: 64, zeroNext: true},
		{name: "compacted_base", base: 5, prefix: 1, suffix: 2, limit: 64},
		{name: "bounded_batch", prefix: 1, suffix: 3, limit: 1},
		{name: "repeated_delivery", prefix: 1, suffix: 3, limit: 64, repeats: 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			leader := createTestNode(t)
			follower := createTestNode(t)
			leader.nodeID = "leader"
			follower.nodeID = "follower"
			leader.state.Store(core.StateLeader)
			leader.currentTerm = 2
			follower.currentTerm = 2
			leader.config.MaxAppendEntries = tc.limit
			if tc.base > 0 {
				leader.logBase, leader.logBaseTerm = tc.base, 1
				follower.logBase, follower.logBaseTerm = tc.base, 1
				leader.log = []core.RaftLogEntry{{Index: tc.base, Term: 1}}
				follower.log = []core.RaftLogEntry{{Index: tc.base, Term: 1}}
			}
			for i := 0; i < tc.prefix; i++ {
				entry := core.RaftLogEntry{Term: 1, Type: core.LogCommand, Data: []byte(fmt.Sprintf("prefix-%d", i))}
				if err := leader.appendEntry(entry); err != nil {
					t.Fatal(err)
				}
				if err := follower.appendEntry(entry); err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < tc.suffix; i++ {
				if err := leader.appendEntry(core.RaftLogEntry{Term: 2, Type: core.LogMembershipChange, Data: []byte(fmt.Sprintf(`{"phase":"final","fixture":%d}`, i))}); err != nil {
					t.Fatal(err)
				}
			}
			next := tc.base + uint64(tc.prefix) + 1
			if tc.zeroNext {
				next = 0
			}
			peer := &Peer{ID: "follower", nextIndex: next, matchIndex: tc.matched}
			leader.peers[peer.ID] = peer
			leader.nextIndex[peer.ID], leader.matchIndex[peer.ID] = peer.nextIndex, peer.matchIndex
			result := make(chan regressionReplicationResult, 1)
			leader.SetTransport(&regressionReplicationTransport{mockTransport: &mockTransport{}, follower: follower, result: result})
			repeats := tc.repeats
			if repeats == 0 {
				repeats = 1
			}
			for repeat := 0; repeat < repeats; repeat++ {
				leader.replicateLog()
				actual := <-result
				previous := tc.base + uint64(tc.prefix)
				previousTerm := leader.getLogTerm(previous)
				count := tc.suffix
				if count > tc.limit {
					count = tc.limit
				}
				expectedLast := previous + uint64(count)
				if actual.request.PrevLogIndex != previous || actual.request.PrevLogTerm != previousTerm || len(actual.request.Entries) != count {
					t.Fatalf("incorrect request: %+v", actual.request)
				}
				for i, entry := range actual.request.Entries {
					if entry.Index != previous+uint64(i)+1 {
						t.Fatalf("entry index %d follows previous %d", entry.Index, previous)
					}
				}
				if !actual.response.Success || actual.response.MatchIndex != expectedLast {
					t.Fatalf("incorrect follower response: %+v", actual.response)
				}
				if follower.lastLogIndexLocked() != expectedLast {
					t.Fatalf("follower tail: got %d, want %d", follower.lastLogIndexLocked(), expectedLast)
				}
				for index := tc.base + 1; index <= expectedLast; index++ {
					want, _ := leader.logEntry(index)
					got, ok := follower.logEntry(index)
					if !ok || !reflect.DeepEqual(got, want) {
						t.Fatalf("follower differs at %d: got %+v, want %+v", index, got, want)
					}
					var durable core.RaftLogEntry
					if err := follower.storage.GetLog(index, &durable); err != nil || !reflect.DeepEqual(durable, want) {
						t.Fatalf("durable follower entry %d: %+v, %v", index, durable, err)
					}
				}
			}
		})
	}
}
