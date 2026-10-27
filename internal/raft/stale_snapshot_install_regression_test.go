package raft

import (
	"encoding/json"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestInstallSnapshotIgnoresStaleAndRepeatedImages(t *testing.T) {
	for _, stale := range []uint64{1, 3} {
		t.Run(string(rune('0'+stale)), func(t *testing.T) {
			store := NewInMemoryStorage()
			fsm := NewStorageFSM(store)
			n, e := NewNode(newTestRaftNodeConfig(), NewInMemoryLogStore(), NewInMemorySnapshotStore(), fsm, newTestRaftLogger())
			if e != nil {
				t.Fatal(e)
			}
			n.currentTerm = 2
			payload, _ := json.Marshal(map[string][]byte{"ws/souls/value": []byte("fresh")})
			req := &core.InstallSnapshotRequest{Term: 2, LeaderID: "leader", LastIncludedIndex: 3, LastIncludedTerm: 1, Data: payload, Done: true}
			if r := n.handleInstallSnapshot(req); !r.Success {
				t.Fatal("fresh snapshot rejected")
			}
			if fsm.LastApplied() != 3 {
				t.Fatal("FSM snapshot boundary not advanced")
			}
			if e := store.Set("ws/souls/value", []byte("latest")); e != nil {
				t.Fatal(e)
			}
			req.LastIncludedIndex = stale
			req.Data, _ = json.Marshal(map[string][]byte{"ws/souls/value": []byte("old")})
			if r := n.handleInstallSnapshot(req); !r.Success {
				t.Fatal("obsolete snapshot should be acknowledged")
			}
			v, e := store.Get("ws/souls/value")
			if e != nil || string(v) != "latest" || n.lastApplied != 3 || n.commitIndex != 3 {
				t.Fatalf("stale image regressed state: %q %v indexes %d/%d", v, e, n.lastApplied, n.commitIndex)
			}
		})
	}
}

func TestInstallSnapshotRechecksBoundaryBetweenChunks(t *testing.T) {
	store := NewInMemoryStorage()
	fsm := NewStorageFSM(store)
	n, e := NewNode(newTestRaftNodeConfig(), NewInMemoryLogStore(), NewInMemorySnapshotStore(), fsm, newTestRaftLogger())
	if e != nil {
		t.Fatal(e)
	}
	n.currentTerm = 2
	payload, _ := json.Marshal(map[string][]byte{"k": []byte("old")})
	half := len(payload) / 2
	req := &core.InstallSnapshotRequest{Term: 2, LeaderID: "leader", LastIncludedIndex: 1, LastIncludedTerm: 1, Data: payload[:half]}
	if !n.handleInstallSnapshot(req).Success {
		t.Fatal("first chunk rejected")
	}
	// Hold the stale transfer's completion until newer state is committed.
	releaseTail := make(chan struct{})
	tailDone := make(chan *core.InstallSnapshotResponse, 1)
	tail := *req
	tail.Offset, tail.Data, tail.Done = uint64(half), payload[half:], true
	go func() {
		<-releaseTail
		tailDone <- n.handleInstallSnapshot(&tail)
	}()
	entry := batchEntry(t, 2, "", core.FSMCommand{Op: core.FSMSet, Key: "k", Value: []byte("latest")})
	n.log = append(n.log, core.RaftLogEntry{Index: 1, Term: 1, Type: core.LogNoOp}, *entry)
	n.commitIndex = 2
	n.processCommitted(2)
	close(releaseTail)
	if !(<-tailDone).Success {
		t.Fatal("stale tail should be acknowledged")
	}
	v, e := store.Get("k")
	if e != nil || string(v) != "latest" || n.lastApplied != 2 {
		t.Fatalf("stale transfer replaced committed state: %q %v", v, e)
	}
	if n.incomingSnapshot != nil {
		t.Fatal("obsolete transfer retained its buffered image")
	}
}
