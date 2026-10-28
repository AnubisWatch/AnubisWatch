package raft

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type leadershipFenceTransport struct {
	*mockTransport
	follower *Node
	received chan *core.AppendEntriesRequest
}

func (tr *leadershipFenceTransport) SendAppendEntries(_ string, req *core.AppendEntriesRequest) (*core.AppendEntriesResponse, error) {
	resp := tr.follower.handleAppendEntries(req)
	tr.received <- req
	return resp, nil
}
func TestReplicationRequestCaptureFencedAfterStepdown(t *testing.T) {
	for _, path := range []string{"heartbeat", "replication"} {
		t.Run(path, func(t *testing.T) {
			for _, steppedDown := range []bool{false, true} {
				t.Run(fmt.Sprintf("stepped_down_%v", steppedDown), func(t *testing.T) {
					sender, receiver := createTestNode(t), createTestNode(t)
					sender.nodeID = "old-leader"
					sender.currentTerm = 1
					sender.state.Store(core.StateLeader)
					receiver.nodeID = "receiver"
					receiver.currentTerm = 1
					prefix := core.RaftLogEntry{Term: 1, Type: core.LogNoOp}
					if err := sender.appendEntry(prefix); err != nil {
						t.Fatal(err)
					}
					staleData, _ := json.Marshal(core.FSMCommand{Op: core.FSMSet, Key: "record", Value: []byte("stale-value")})
					if err := sender.appendEntry(core.RaftLogEntry{Term: 1, Type: core.LogCommand, Data: staleData}); err != nil {
						t.Fatal(err)
					}
					if err := receiver.appendEntry(prefix); err != nil {
						t.Fatal(err)
					}
					if steppedDown {
						// The new leader has committed a replacement at index2 in term2.
						receiver.currentTerm = 2
						committedData, _ := json.Marshal(core.FSMCommand{Op: core.FSMSet, Key: "record", Value: []byte("committed-value")})
						if err := receiver.appendEntry(core.RaftLogEntry{Term: 2, Type: core.LogCommand, Data: committedData}); err != nil {
							t.Fatal(err)
						}
						receiver.commitIndex, receiver.lastApplied = 2, 1
					}
					peer := &Peer{ID: "receiver", nextIndex: 2}
					sender.peers[peer.ID] = peer
					sender.nextIndex[peer.ID] = 2
					tr := &leadershipFenceTransport{mockTransport: &mockTransport{}, follower: receiver, received: make(chan *core.AppendEntriesRequest, 1)}
					sender.SetTransport(tr)
					checked, returned := make(chan struct{}), make(chan struct{})
					// Force the run-loop's caller check before stepdown, and capture after it.
					sender.mu.Lock()
					go func() {
						defer close(returned)
						if sender.IsLeader() {
							close(checked)
							if path == "heartbeat" {
								sender.sendHeartbeats(0)
							} else {
								sender.replicateLog()
							}
						}
					}()
					<-checked
					if steppedDown {
						if err := sender.becomeFollower(2); err != nil {
							t.Fatal(err)
						}
					}
					sender.mu.Unlock()
					<-returned
					select {
					case req := <-tr.received:
						if steppedDown {
							receiver.mu.RLock()
							entry, ok := receiver.logEntry(2)
							receiver.mu.RUnlock()
							receiver.processCommitted(2)
							value, _ := receiver.fsm.(*StorageFSM).store.Get("record")
							t.Fatalf("EXPECTED: no request after stepdown, committed-value applied; ACTUAL: emitted term%d as %s; receiver entry=%+v exists=%v value=%q; PROBLEM CONFIRMED", req.Term, req.LeaderID, entry, ok, value)
						}
						if req.Term != 1 || len(req.Entries) != 1 {
							t.Fatalf("invalid unaffected control: %+v", req)
						}
					case <-time.After(100 * time.Millisecond):
						if !steppedDown {
							t.Fatal("unaffected leader control did not replicate")
						}
						receiver.mu.RLock()
						entry, ok := receiver.logEntry(2)
						receiver.mu.RUnlock()
						if !ok || entry.Term != 2 {
							t.Fatal("committed entry changed after fenced sender")
						}
						receiver.processCommitted(2)
						value, err := receiver.fsm.(*StorageFSM).store.Get("record")
						if err != nil || string(value) != "committed-value" {
							t.Fatalf("committed data changed: value=%q err=%v", value, err)
						}
					}
				})
			}
		})
	}
}
