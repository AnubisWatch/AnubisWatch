package raft

// A 2-hour HA soak restarted a node under ~2500 monitors: it stayed seconds
// behind the leader on replicated judgments, and because every authenticated
// request waited for a read barrier at the leader's commit index, its whole
// API answered in 5-9 s until the soak gave up on it. Authentication reads
// only the shared auth state, so its barrier now waits only for the last
// entry that wrote that state.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func commandEntry(t *testing.T, index uint64, cmd core.FSMCommand) core.RaftLogEntry {
	t.Helper()
	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatal(err)
	}
	return core.RaftLogEntry{Index: index, Term: 1, Type: core.LogCommand, Data: data}
}

func TestTableReadIndexStopsAtTheTablesLastEntry(t *testing.T) {
	fsm := NewStorageFSM(NewInMemoryStorage())
	n, err := NewNode(newTestRaftNodeConfig(), NewInMemoryLogStore(), NewInMemorySnapshotStore(), fsm, newTestRaftLogger())
	if err != nil {
		t.Fatalf("failed to create test node: %v", err)
	}
	n.log = append(n.log, commandEntry(t, 1, core.FSMCommand{Op: core.FSMSet, Table: "auth", Key: "system/auth/state", Value: []byte("1")}))
	for i := uint64(2); i <= 10; i++ {
		n.log = append(n.log, commandEntry(t, i, core.FSMCommand{Op: core.FSMSet, Table: "judgments", Key: "j", Value: []byte{byte(i)}}))
	}
	n.commitIndex = 10
	n.processCommitted(10)

	check := func(readIndex uint64, table string, want uint64) {
		t.Helper()
		got, err := n.tableReadIndex(readIndex, table, time.Second)
		if err != nil {
			t.Fatalf("tableReadIndex(%d, %q): %v", readIndex, table, err)
		}
		if got != want {
			t.Fatalf("tableReadIndex(%d, %q) = %d, want %d", readIndex, table, got, want)
		}
	}
	check(10, "auth", 1)
	check(10, "judgments", 10)
	check(10, "", 10)
	check(10, "never-written", 0)

	// A write of the table inside a replicated batch counts too.
	n.log = append(n.log, *batchEntry(t, 11, "node-a",
		core.FSMCommand{Op: core.FSMSet, Table: "judgments", Key: "j", Value: []byte("x")},
		core.FSMCommand{Op: core.FSMSet, Table: "auth", Key: "system/auth/state", Value: []byte("2")},
	))
	n.log = append(n.log, commandEntry(t, 12, core.FSMCommand{Op: core.FSMSet, Table: "judgments", Key: "j", Value: []byte("y")}))
	n.commitIndex = 12
	n.processCommitted(12)
	check(12, "auth", 11)
	// Never past the read index the caller was given.
	check(10, "judgments", 10)

	// Entries applied before this process started are not tracked per
	// table: the read waits for all of them.
	n.mu.Lock()
	n.tableScopeFloor = 12
	n.mu.Unlock()
	check(12, "auth", 12)
}

func TestFollowerTableReadBarrierSkipsUnrelatedWrites(t *testing.T) {
	nodes := startForwardCluster(t, 3, nil)
	leader, follower := waitForForwardLeader(t, nodes)

	auth := core.FSMCommand{Op: core.FSMSet, Table: "auth", Key: "system/auth/state", Value: []byte(`{"revision":1}`)}
	authIndex, _, _, err := leader.node.Apply(auth, 5*time.Second)
	if err != nil {
		t.Fatalf("leader auth write: %v", err)
	}
	for i := 0; i < 5; i++ {
		other := core.FSMCommand{Op: core.FSMSet, Key: "default/souls/other", Value: []byte{'0' + byte(i)}}
		if _, _, _, err := leader.node.Apply(other, 5*time.Second); err != nil {
			t.Fatalf("leader write: %v", err)
		}
	}

	index, err := follower.node.TableReadBarrierOrForward("auth", 5*time.Second)
	if err != nil {
		t.Fatalf("follower table read barrier: %v", err)
	}
	if index != authIndex {
		t.Fatalf("table read index = %d, want the auth write's index %d", index, authIndex)
	}
	if got, err := follower.store.Get(auth.Key); err != nil || string(got) != string(auth.Value) {
		t.Fatalf("after the barrier the follower cannot read the auth write: %q, %v", got, err)
	}

	full, err := follower.node.ReadBarrierOrForward(5 * time.Second)
	if err != nil {
		t.Fatalf("follower read barrier: %v", err)
	}
	if full <= authIndex {
		t.Fatalf("full read index %d not past the later writes (auth at %d)", full, authIndex)
	}
}
