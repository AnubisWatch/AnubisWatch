package raft

import (
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// startJoinTestNode starts a real node over TCP. With no peers and no join
// addresses it elects itself (a one-node cluster); with join addresses it
// waits to be added.
func startJoinTestNode(t *testing.T, id, addr, secret string, join []string) *Node {
	t.Helper()
	cfg := core.RaftConfig{
		NodeID:           id,
		BindAddr:         addr,
		AdvertiseAddr:    addr,
		Region:           "default",
		ElectionTimeout:  core.Duration{Duration: 150 * time.Millisecond},
		HeartbeatTimeout: core.Duration{Duration: 50 * time.Millisecond},
		CommitTimeout:    core.Duration{Duration: 20 * time.Millisecond},
		MaxAppendEntries: 64,
		ClusterSecret:    secret,
		Join:             join,
	}
	node, err := NewNode(cfg, NewInMemoryLogStore(), NewInMemorySnapshotStore(), NewStorageFSM(NewInMemoryStorage()), newTestRaftLogger())
	if err != nil {
		t.Fatal(err)
	}
	transport, err := NewTCPTransport(addr, addr, nil, newTestRaftLogger())
	if err != nil {
		t.Fatal(err)
	}
	node.SetTransport(transport)
	if err := node.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = node.Stop() })
	return node
}

func waitUntil(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %v waiting for %s", timeout, what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func peerIDs(n *Node) map[string]core.RaftRole {
	ids := make(map[string]core.RaftRole)
	for _, p := range n.GetPeers() {
		ids[p.ID] = p.Role
	}
	return ids
}

// A node started with only a join address used to elect itself leader of a
// one-node cluster. It must wait instead; joining then depends on the
// cluster secret:
//   - with a shared secret its authenticated Join is admitted automatically;
//   - with another secret it is never admitted (and never self-elects);
//   - without a secret it is refused until an operator summons it by address
//     (Identify + AddPeer), after which its Join succeeds and it learns the
//     members' real IDs.
func TestJoinByAddress(t *testing.T) {
	cases := []struct {
		name          string
		clusterSecret string
		joinerSecret  string
		summon        bool
		wantJoined    bool
	}{
		{"shared secret admits automatically", "join-secret", "join-secret", false, true},
		{"wrong secret is never admitted", "join-secret", "other-secret", false, false},
		{"no secret waits for summon", "", "", false, false},
		{"no secret, summoned by address", "", "", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seedAddr, joinerAddr := reserveLoopbackAddr(t), reserveLoopbackAddr(t)
			seed := startJoinTestNode(t, "seed", seedAddr, tc.clusterSecret, nil)
			waitUntil(t, 5*time.Second, "the seed to lead", seed.IsLeader)

			joiner := startJoinTestNode(t, "joiner-real-id", joinerAddr, tc.joinerSecret, []string{seedAddr})
			if !joiner.AwaitingJoin() {
				t.Fatal("a node with only a join address is not waiting to be added")
			}

			if tc.summon {
				// What POST /api/v1/cluster/nodes does for `anubis summon <addr>`.
				identity, err := seed.IdentifyPeer(joinerAddr, 5*time.Second)
				if err != nil {
					t.Fatalf("identify: %v", err)
				}
				if identity.ID != "joiner-real-id" || !identity.AwaitingJoin {
					t.Fatalf("identify = %+v, want the joiner's real ID awaiting join", identity)
				}
				if err := seed.AddPeer(core.RaftPeer{ID: identity.ID, Address: joinerAddr, Region: identity.Region, Role: core.RoleVoter}); err != nil {
					t.Fatalf("add peer: %v", err)
				}
			}

			if tc.wantJoined {
				waitUntil(t, 15*time.Second, "the joiner to be added and learn the members", func() bool {
					return !joiner.AwaitingJoin() && peerIDs(joiner)["seed"] == core.RoleVoter && peerIDs(seed)["joiner-real-id"] == core.RoleVoter
				})
				waitUntil(t, 5*time.Second, "the joiner to follow the seed", func() bool { return joiner.Leader() == "seed" })
				if joiner.IsLeader() {
					t.Fatal("the joiner took over leadership instead of following")
				}
				// Two voters now: a write commits only with the joiner.
				if _, _, _, err := seed.Apply(core.FSMCommand{Op: core.FSMSet, Key: "k", Value: []byte(`"v"`)}, 5*time.Second); err != nil {
					t.Fatalf("write after join: %v", err)
				}
				return
			}

			// Never admitted: it keeps waiting, never elects itself, and the
			// seed does not list it.
			time.Sleep(1500 * time.Millisecond)
			if !joiner.AwaitingJoin() || joiner.IsLeader() || joiner.Term() != 0 {
				t.Fatalf("an unadmitted joiner left the wait (awaiting=%v leader=%v term=%d)", joiner.AwaitingJoin(), joiner.IsLeader(), joiner.Term())
			}
			if _, listed := peerIDs(seed)["joiner-real-id"]; listed {
				t.Fatal("the seed admitted a joiner it must refuse")
			}
		})
	}
}
