package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// TestHeartbeatPingOnNonOwnerIsJudgedByOwner boots a three-node cluster and
// checks the heartbeat design end to end:
//
//   - a ping sent to a node that does not own the soul is accepted there;
//   - it is judged once, by the owner (the replicated ping record wakes the
//     owner's evaluator), and the judgment replicates to every node;
//   - when pings stop, only the owner records the missed-deadline dead
//     judgments, about one per interval -- never one per node.
func TestHeartbeatPingOnNonOwnerIsJudgedByOwner(t *testing.T) {
	if testing.Short() {
		t.Skip("boots a three-node cluster")
	}
	const (
		nodeCount     = 3
		adminEmail    = "admin@example.com"
		adminPassword = "ClusterPass1234!"
	)
	t.Setenv("ANUBIS_DATA_DIR", "")

	ids := make([]string, nodeCount)
	raftAddrs := make([]string, nodeCount)
	httpPorts := make([]int, nodeCount)
	for i := range ids {
		ids[i] = fmt.Sprintf("jackal-%d", i)
		raftAddrs[i] = fmt.Sprintf("127.0.0.1:%d", freeLoopbackPort(t))
		httpPorts[i] = freeLoopbackPort(t)
	}
	nodes := make([]*clusterTestNode, nodeCount)
	for i := range nodes {
		path := writeClusterNodeConfig(t, i, ids, raftAddrs, httpPorts[i], adminEmail, adminPassword, "hash")
		nodes[i] = bootClusterNode(t, ids[i], path, httpPorts[i])
	}
	defer func() {
		for _, n := range nodes {
			n.stop(t)
		}
	}()
	leader := waitForClusterLeader(t, nodes, 15*time.Second)
	var follower *clusterTestNode
	for _, n := range nodes {
		if n != leader {
			follower = n
			break
		}
	}
	client, _ := loginAsAdmin(t, follower.baseURL, adminEmail, adminPassword)

	soulID := postJSON(t, client, follower.baseURL+"/api/v1/souls", map[string]any{
		"name":      "nightly-backup",
		"type":      "heartbeat",
		"weight":    "2s",
		"heartbeat": map[string]any{"grace": "1s"},
	})

	var owner *clusterTestNode
	waitFor(t, 15*time.Second, "one node to own the heartbeat soul", func() bool {
		owner = nil
		count := 0
		for _, n := range nodes {
			if n.deps.ClusterWork.Owns(soulID) {
				owner, count = n, count+1
			}
		}
		return count == 1
	})
	var other *clusterTestNode
	for _, n := range nodes {
		if n != owner {
			other = n
			break
		}
	}
	t.Logf("owner %s, pinging %s", owner.id, other.id)

	resp, err := client.Get(follower.baseURL + "/api/v1/souls/" + soulID + "/heartbeat")
	if err != nil {
		t.Fatal(err)
	}
	var info struct {
		Token   string `json:"token"`
		PingURL string `json:"ping_url"`
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || json.Unmarshal(raw, &info) != nil || info.Token == "" {
		t.Fatalf("reveal = %d %s", resp.StatusCode, raw)
	}

	pingStart := time.Now()
	waitFor(t, 10*time.Second, "the non-owner to accept the ping", func() bool {
		r, err := http.Get(other.baseURL + "/api/v1/heartbeat/" + info.Token)
		if err != nil {
			return false
		}
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return r.StatusCode == http.StatusOK && strings.TrimSpace(string(body)) == "OK"
	})

	judgments := func(n *clusterTestNode) []*core.Judgment {
		js, _ := n.deps.Store.ListJudgmentsNoCtx(soulID, time.Now().Add(-time.Hour), time.Now().Add(time.Hour), 1000)
		sort.Slice(js, func(i, j int) bool { return js[i].Timestamp.Before(js[j].Timestamp) })
		return js
	}
	waitFor(t, 10*time.Second, "an alive judgment from the owner on every node", func() bool {
		for _, n := range nodes {
			found := false
			for _, j := range judgments(n) {
				if j.Status == core.SoulAlive && !j.Timestamp.Before(pingStart.Add(-time.Second)) {
					found = true
				}
			}
			if !found {
				return false
			}
		}
		return true
	})

	// No more pings: the owner records dead after interval + grace (3s) and
	// again every interval (2s).
	waitFor(t, 20*time.Second, "repeated dead judgments", func() bool {
		dead := 0
		for _, j := range judgments(owner) {
			if j.Status == core.SoulDead && j.Timestamp.After(pingStart) {
				dead++
			}
		}
		return dead >= 2
	})
	time.Sleep(time.Second) // let the last batch replicate

	for _, n := range nodes {
		alive := 0
		var lastDead time.Time
		for _, j := range judgments(n) {
			if j.JackalID != owner.id {
				t.Fatalf("%s holds a judgment from %s; only the owner %s may judge: %+v", n.id, j.JackalID, owner.id, j)
			}
			switch j.Status {
			case core.SoulAlive:
				alive++
			case core.SoulDead:
				if !lastDead.IsZero() && j.Timestamp.Sub(lastDead) < 1500*time.Millisecond {
					t.Fatalf("%s: dead judgments %s apart; want one per 2s interval", n.id, j.Timestamp.Sub(lastDead))
				}
				lastDead = j.Timestamp
			}
		}
		if alive != 1 {
			t.Fatalf("%s: %d alive judgments for one ping, want 1", n.id, alive)
		}
	}
}
