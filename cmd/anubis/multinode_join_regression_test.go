package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/probe"
)

// joinClusterStrategy is the ownership strategy the join test's cluster uses.
//
// The test asserts that the joining member receives a share of the souls. The
// default "hash" strategy cannot promise that: rendezvous hashing gives a
// member only the souls it outranks, and with a dozen souls over four
// members a member can win none of them -- the join then succeeds, the
// cluster is consistent, and the new member simply idles while the others
// keep every soul. The documented even strategies ("round-robin",
// "load-based") give every member an equal share, which is what this test
// needs to observe. The other cluster test keeps "hash", so both strategies
// stay covered.
const joinClusterStrategy = "load-based"

// TestMultiNodeClusterJoinByAddress grows a running three-node cluster by a
// fourth node that knows nothing but one member's Raft address
// (anubis serve --join <addr>). The joiner used to list that address as a peer
// under an ID guessed from it -- no member had that ID -- and, knowing no real
// member, elected itself leader of a one-node cluster. Now it must:
//
//   - never elect itself while it waits to be added;
//   - be admitted (the cluster has a secret, so the authenticated Join is
//     accepted) and learn every member's real ID from the answer;
//   - become a voter: with one original follower stopped, a write commits
//     only if the joiner acknowledges it;
//   - receive soul ownership, each soul still probed by exactly one node.
func TestMultiNodeClusterJoinByAddress(t *testing.T) {
	if testing.Short() {
		t.Skip("boots a four-node cluster")
	}

	const (
		adminEmail    = "admin@example.com"
		adminPassword = "ClusterPass1234!"
		soulCount     = 12
	)
	t.Setenv("ANUBIS_DATA_DIR", "")

	checks := newCheckCounter()
	restoreRegistry := newProbeRegistry
	t.Cleanup(func() { newProbeRegistry = restoreRegistry })
	useCountingRegistry := func(nodeID string) {
		newProbeRegistry = func() *probe.CheckerRegistry {
			registry := probe.NewCheckerRegistry()
			registry.Register(&countingChecker{node: nodeID, counter: checks})
			return registry
		}
	}

	ids := []string{"jackal-0", "jackal-1", "jackal-2"}
	raftAddrs := make([]string, len(ids))
	httpPorts := make([]int, len(ids))
	for i := range ids {
		raftAddrs[i] = fmt.Sprintf("127.0.0.1:%d", freeLoopbackPort(t))
		httpPorts[i] = freeLoopbackPort(t)
	}
	nodes := make([]*clusterTestNode, 0, 4)
	defer func() {
		for _, node := range nodes {
			node.stop(t)
		}
	}()
	for i := range ids {
		useCountingRegistry(ids[i])
		nodes = append(nodes, bootClusterNode(t, ids[i], writeClusterNodeConfig(t, i, ids, raftAddrs, httpPorts[i], adminEmail, adminPassword, joinClusterStrategy), httpPorts[i]))
	}
	leader := waitForClusterLeader(t, nodes, 15*time.Second)
	client, _ := loginAsAdmin(t, leader.baseURL, adminEmail, adminPassword)
	soulIDs := make([]string, 0, soulCount)
	for i := 0; i < soulCount; i++ {
		soulIDs = append(soulIDs, postJSON(t, client, leader.baseURL+"/api/v1/souls", map[string]any{
			"name": fmt.Sprintf("soul-%02d", i), "type": "tcp", "target": "127.0.0.1:9", "weight": "1s", "timeout": "1s",
		}))
	}

	// The fourth node knows one member's address, and nothing else.
	var follower *clusterTestNode
	for _, node := range nodes {
		if node != leader {
			follower = node
			break
		}
	}
	joinerID := "jackal-3"
	joinerRaft := fmt.Sprintf("127.0.0.1:%d", freeLoopbackPort(t))
	joinerHTTP := freeLoopbackPort(t)
	configPath := writeJoinNodeConfig(t, joinerID, joinerRaft, joinerHTTP, raftAddrs[indexOf(ids, follower.id)], adminEmail, adminPassword, joinClusterStrategy)
	useCountingRegistry(joinerID)
	joiner := bootClusterNode(t, joinerID, configPath, joinerHTTP)
	nodes = append(nodes, joiner)

	// It must not elect itself at any point while joining.
	var selfElected atomic.Bool
	stopWatch := make(chan struct{})
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		for {
			select {
			case <-stopWatch:
				return
			case <-time.After(5 * time.Millisecond):
				if joiner.deps.ClusterManager.IsLeader() {
					selfElected.Store(true)
				}
			}
		}
	}()

	waitFor(t, 60*time.Second, "every node to list the joiner and the joiner to list every member", func() bool {
		want := append(append([]string(nil), ids...), joinerID)
		sort.Strings(want)
		for _, node := range nodes {
			got := make([]string, 0, 4)
			for _, member := range node.deps.ClusterManager.Members() {
				got = append(got, member.ID)
			}
			sort.Strings(got)
			if fmt.Sprint(got) != fmt.Sprint(want) {
				return false
			}
		}
		return joiner.deps.ClusterManager.Leader() == leader.id
	})
	close(stopWatch)
	<-watchDone
	if selfElected.Load() {
		t.Fatal("the joining node elected itself leader before it was added to the cluster")
	}

	// It receives ownership: some souls are now probed by the joiner, and every
	// soul by exactly one node.
	// Ownership moves once the leader's replicated live-member list includes
	// the joiner; on a loaded CI runner that has taken longer than 20s.
	waitForOrDump(t, 60*time.Second, "the joiner to probe the souls it owns", func() bool {
		for _, id := range soulIDs {
			if checks.byNode(id)[joinerID] > 0 {
				return true
			}
		}
		return false
	}, func() string { return clusterStateDump(append(append([]*clusterTestNode(nil), nodes...), joiner)...) })
	time.Sleep(time.Second) // let the ownership hand-over settle
	before := make(map[string]map[string]int, len(soulIDs))
	for _, id := range soulIDs {
		before[id] = checks.byNode(id)
	}
	time.Sleep(2500 * time.Millisecond)
	joinerOwns := 0
	for _, id := range soulIDs {
		probing := []string{}
		for node, n := range checks.byNode(id) {
			if n > before[id][node] {
				probing = append(probing, node)
			}
		}
		if len(probing) != 1 {
			t.Fatalf("soul %s was probed by %v in a settled window; exactly one node owns it", id, probing)
		}
		if probing[0] == joinerID {
			joinerOwns++
		}
	}
	t.Logf("joiner owns %d of %d souls", joinerOwns, soulCount)
	if joinerOwns == 0 {
		t.Fatal("the joiner owns no soul")
	}

	// It is a voter: with one original follower down, leader + one original
	// is 2 of 4 -- a write commits only with the joiner's acknowledgement.
	waitFor(t, 30*time.Second, "the membership change to finish", func() bool {
		return len(leader.deps.ClusterManager.LiveMembers()) == 4
	})
	follower.stop(t)
	postJSON(t, client, leader.baseURL+"/api/v1/souls", map[string]any{
		"name": "after-join", "type": "tcp", "target": "127.0.0.1:9", "weight": "1s", "timeout": "1s",
	})
}

// writeJoinNodeConfig writes the config of a node that has no peers, does not
// bootstrap, and joins through joinAddr.
func writeJoinNodeConfig(t *testing.T, id, raftAddr string, httpPort int, joinAddr, email, password, strategy string) string {
	t.Helper()
	path := writeClusterNodeConfig(t, 0, []string{id}, []string{raftAddr}, httpPort, email, password, strategy)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	raft := config["necropolis"].(map[string]any)["raft"].(map[string]any)
	raft["bootstrap"] = false
	raft["peers"] = []any{}
	raft["join"] = []string{joinAddr}
	if data, err = json.MarshalIndent(config, "", "  "); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func indexOf(values []string, want string) int {
	for i, v := range values {
		if v == want {
			return i
		}
	}
	return -1
}
