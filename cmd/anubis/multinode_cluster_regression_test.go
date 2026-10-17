package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/probe"
)

// TestMultiNodeClusterSharesWork boots three complete servers in one process --
// real storage, real REST, real Raft over TCPTransport on loopback -- and checks
// the cluster behaves as one monitoring system:
//
//   - a soul created through a follower's REST API is accepted (the write is
//     forwarded to the leader, not refused with 500) and is visible on every node;
//   - the soul is probed by exactly one node;
//   - its judgments are readable on every node;
//   - each failing judgment is notified once, not once per node, and the
//     incident it opens is visible on every node;
//   - when the owner stops, another node takes the soul over and keeps checking it.
func TestMultiNodeClusterSharesWork(t *testing.T) {
	if testing.Short() {
		t.Skip("boots a three-node cluster")
	}

	const (
		nodeCount     = 3
		adminEmail    = "admin@example.com"
		adminPassword = "ClusterPass1234!"
	)

	// TestMain points ANUBIS_DATA_DIR at one package-wide directory, and the
	// variable overrides storage.path; each node needs its own data directory.
	t.Setenv("ANUBIS_DATA_DIR", "")

	checks := newCheckCounter()
	restoreRegistry := newProbeRegistry
	t.Cleanup(func() { newProbeRegistry = restoreRegistry })

	notifications := newNotificationLog()
	webhook := httptest.NewServer(http.HandlerFunc(notifications.handle))
	defer webhook.Close()

	nodeIDs := make([]string, nodeCount)
	raftAddrs := make([]string, nodeCount)
	httpPorts := make([]int, nodeCount)
	for i := range nodeIDs {
		nodeIDs[i] = fmt.Sprintf("jackal-%d", i)
		raftAddrs[i] = fmt.Sprintf("127.0.0.1:%d", freeLoopbackPort(t))
		httpPorts[i] = freeLoopbackPort(t)
	}

	nodes := make([]*clusterTestNode, nodeCount)
	for i := range nodes {
		configPath := writeClusterNodeConfig(t, i, nodeIDs, raftAddrs, httpPorts[i], adminEmail, adminPassword)
		nodeID := nodeIDs[i]
		newProbeRegistry = func() *probe.CheckerRegistry {
			registry := probe.NewCheckerRegistry()
			registry.Register(&countingChecker{node: nodeID, counter: checks})
			return registry
		}
		nodes[i] = bootClusterNode(t, nodeID, configPath, httpPorts[i])
	}
	defer func() {
		for _, node := range nodes {
			node.stop(t)
		}
	}()

	leader := waitForClusterLeader(t, nodes, 15*time.Second)
	var follower *clusterTestNode
	for _, node := range nodes {
		if node != leader {
			follower = node
			break
		}
	}
	t.Logf("leader %s, writing through follower %s", leader.id, follower.id)

	client, _ := loginAsAdmin(t, follower.baseURL, adminEmail, adminPassword)

	channelID := postJSON(t, client, follower.baseURL+"/api/v1/channels", map[string]any{
		"name":    "cluster-webhook",
		"type":    "webhook",
		"enabled": true,
		"config":  map[string]any{"url": webhook.URL},
	})
	postJSON(t, client, follower.baseURL+"/api/v1/rules", map[string]any{
		"name":     "every-failure",
		"enabled":  true,
		"scope":    map[string]any{"type": "all"},
		"channels": []string{channelID},
		// failure_rate fires on every dead judgment; a 1ms cooldown turns off
		// deduplication, so the notification count tracks the judgment count.
		"conditions": []map[string]any{{"type": "failure_rate"}},
		"cooldown":   "1ms",
	})
	// A second rule with a long cooldown: it must notify once in total, even
	// after the soul moves to another node that keeps seeing it fail.
	cooldownNotifications := newNotificationLog()
	cooldownHook := httptest.NewServer(http.HandlerFunc(cooldownNotifications.handle))
	defer cooldownHook.Close()
	cooldownChannelID := postJSON(t, client, follower.baseURL+"/api/v1/channels", map[string]any{
		"name":    "cooldown-webhook",
		"type":    "webhook",
		"enabled": true,
		"config":  map[string]any{"url": cooldownHook.URL},
	})
	postJSON(t, client, follower.baseURL+"/api/v1/rules", map[string]any{
		"name":       "down-once-an-hour",
		"enabled":    true,
		"scope":      map[string]any{"type": "all"},
		"channels":   []string{cooldownChannelID},
		"conditions": []map[string]any{{"type": "consecutive_failures", "threshold": 2}},
		"cooldown":   "1h",
	})
	soulID := postJSON(t, client, follower.baseURL+"/api/v1/souls", map[string]any{
		"name":    "shared-soul",
		"type":    "tcp",
		"target":  "127.0.0.1:9",
		"weight":  "1s",
		"timeout": "1s",
	})

	// The follower answered only after applying the write: it reads its own write.
	if _, err := follower.deps.Store.GetSoulNoCtx(soulID); err != nil {
		t.Fatalf("the follower that accepted the soul cannot read it back: %v", err)
	}
	waitFor(t, 10*time.Second, "the soul to be visible on every node", func() bool {
		for _, node := range nodes {
			if _, err := node.deps.Store.GetSoulNoCtx(soulID); err != nil {
				return false
			}
		}
		return true
	})

	// Exactly one node probes the soul.
	waitFor(t, 15*time.Second, "three checks of the soul", func() bool { return checks.total(soulID) >= 3 })
	perNode := checks.byNode(soulID)
	if len(perNode) != 1 {
		t.Fatalf("the soul was probed by %d nodes (%v); with work sharing exactly one node owns it", len(perNode), perNode)
	}
	var owner *clusterTestNode
	for _, node := range nodes {
		if perNode[node.id] > 0 {
			owner = node
		}
	}
	t.Logf("owner %s ran %d checks", owner.id, perNode[owner.id])

	// Its judgments are readable on every node.
	waitFor(t, 10*time.Second, "judgments to replicate to every node", func() bool {
		for _, node := range nodes {
			judgments, err := node.deps.Store.ListJudgmentsNoCtx(soulID, time.Now().Add(-time.Hour), time.Now().Add(time.Hour), 100)
			if err != nil || len(judgments) < 2 {
				return false
			}
			for _, j := range judgments {
				if j.JackalID != owner.id {
					t.Fatalf("node %s holds a judgment from %s; only the owner %s probes the soul", node.id, j.JackalID, owner.id)
				}
			}
		}
		return true
	})

	// One notification per failing judgment, all from the owner.
	waitFor(t, 10*time.Second, "failure notifications", func() bool { return notifications.count() >= 2 })
	for message, n := range notifications.messages() {
		if n != 1 {
			t.Fatalf("judgment %q was notified %d times; alert rules must be evaluated once per judgment", message, n)
		}
		if !strings.Contains(message, owner.id) {
			t.Fatalf("notification %q did not come from the owner %s", message, owner.id)
		}
	}

	// The incident the owner opened is visible on every node.
	waitFor(t, 10*time.Second, "the incident to reach every node", func() bool {
		for _, node := range nodes {
			found := false
			for _, incident := range node.deps.AlertManager.ListIncidents() {
				if incident.SoulID == soulID {
					found = true
				}
			}
			if !found {
				return false
			}
		}
		return true
	})

	// A journey created through the follower runs on exactly one node, and its
	// runs are listed on every node.
	journeyTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer journeyTarget.Close()
	journeyID := postJSON(t, client, follower.baseURL+"/api/v1/journeys", map[string]any{
		"name":    "shared-journey",
		"enabled": true,
		"weight":  "300ms",
		"steps":   []map[string]any{{"name": "ping", "type": "http", "target": journeyTarget.URL, "timeout": "1s"}},
	})
	journeyRunners := func(among []*clusterTestNode) []string {
		var runners []string
		for _, node := range among {
			for _, id := range node.deps.JourneyExecutor.Running() {
				if id == journeyID {
					runners = append(runners, node.id)
				}
			}
		}
		return runners
	}
	waitFor(t, 10*time.Second, "the journey to be scheduled", func() bool { return len(journeyRunners(nodes)) > 0 })
	if runners := journeyRunners(nodes); len(runners) != 1 {
		t.Fatalf("the journey is scheduled on %v; exactly one node owns it", runners)
	}
	journeyOwner := journeyRunners(nodes)[0]
	waitFor(t, 10*time.Second, "journey runs to replicate to every node", func() bool {
		for _, node := range nodes {
			runs, err := node.deps.Store.QueryJourneyRuns(context.Background(), "default", journeyID, 0)
			if err != nil || len(runs) < 2 {
				return false
			}
			for _, run := range runs {
				if run.JackalID != journeyOwner {
					t.Fatalf("node %s holds a run of the journey by %s; only %s runs it", node.id, run.JackalID, journeyOwner)
				}
			}
		}
		return true
	})
	t.Logf("journey owner %s", journeyOwner)

	waitFor(t, 10*time.Second, "the cooldown rule to notify", func() bool { return cooldownNotifications.count() >= 1 })

	// Stop the owner: another node takes the soul over.
	before := checks.byNode(soulID)
	owner.stop(t)
	waitFor(t, 20*time.Second, "another node to take the soul over", func() bool {
		for node, n := range checks.byNode(soulID) {
			if node != owner.id && n > before[node] {
				return true
			}
		}
		return false
	})
	// Once ownership settled, exactly one surviving node keeps checking.
	time.Sleep(500 * time.Millisecond)
	settled := checks.byNode(soulID)
	time.Sleep(2500 * time.Millisecond)
	after := checks.byNode(soulID)
	probing := 0
	for _, node := range nodes {
		if node == owner {
			if after[node.id] != settled[node.id] {
				t.Fatalf("the stopped node %s kept checking", node.id)
			}
			continue
		}
		if after[node.id] > settled[node.id] {
			probing++
		}
	}
	if probing != 1 {
		t.Fatalf("after failover %d surviving nodes checked the soul (%v -> %v); exactly one should", probing, settled, after)
	}

	// The new owner kept seeing the soul fail, past the rule's threshold, yet
	// the cooldown the old owner started still holds: no second notification.
	waitFor(t, 10*time.Second, "the new owner to check the soul three more times", func() bool {
		for node, n := range checks.byNode(soulID) {
			if node != owner.id && n >= settled[node]+3 {
				return true
			}
		}
		return false
	})
	time.Sleep(300 * time.Millisecond) // let a would-be duplicate dispatch
	if n := cooldownNotifications.count(); n != 1 {
		t.Fatalf("the 1h-cooldown rule notified %d times across the failover (%v); the new owner must honour the old owner's cooldown", n, cooldownNotifications.messages())
	}

	// The journey is still scheduled on exactly one surviving node.
	survivors := make([]*clusterTestNode, 0, len(nodes)-1)
	for _, node := range nodes {
		if node != owner {
			survivors = append(survivors, node)
		}
	}
	waitFor(t, 10*time.Second, "one surviving node to run the journey", func() bool { return len(journeyRunners(survivors)) == 1 })
}

type clusterTestNode struct {
	id      string
	deps    *ServerDependencies
	server  *Server
	baseURL string
	once    sync.Once
}

func (n *clusterTestNode) stop(t *testing.T) {
	t.Helper()
	n.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := n.server.Stop(ctx); err != nil {
			t.Errorf("stopping %s: %v", n.id, err)
		}
	})
}

func bootClusterNode(t *testing.T, id, configPath string, port int) *clusterTestNode {
	t.Helper()
	level := slog.LevelError
	switch os.Getenv("ANUBIS_TEST_LOG_LEVEL") {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})).With("test_node", id)
	deps, err := BuildServerDependencies(ServerOptions{ConfigPath: configPath, Logger: logger})
	if err != nil {
		t.Fatalf("BuildServerDependencies(%s): %v", id, err)
	}
	if deps.ClusterWork == nil {
		t.Fatalf("%s did not enable cluster work sharing", id)
	}
	server := NewServer(deps)
	if err := server.Start(context.Background()); err != nil {
		t.Fatalf("Server.Start(%s): %v", id, err)
	}
	node := &clusterTestNode{id: id, deps: deps, server: server, baseURL: fmt.Sprintf("http://127.0.0.1:%d", port)}
	waitForHTTP(t, node.baseURL)
	return node
}

func writeClusterNodeConfig(t *testing.T, index int, ids, raftAddrs []string, httpPort int, email, password string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ids[index])
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	peers := make([]map[string]any, 0, len(ids)-1)
	for j := range ids {
		if j != index {
			peers = append(peers, map[string]any{"id": ids[j], "address": raftAddrs[j], "region": "default", "role": "voter"})
		}
	}
	config := map[string]any{
		"storage":   map[string]any{"path": filepath.ToSlash(filepath.Join(dir, "data"))},
		"server":    map[string]any{"host": "127.0.0.1", "port": httpPort, "grpc_port": 0},
		"dashboard": map[string]any{"enabled": false},
		"security":  map[string]any{"allow_private_targets": true},
		"auth":      map[string]any{"type": "local", "local": map[string]any{"admin_email": email, "admin_password": password}},
		"necropolis": map[string]any{
			"enabled":        true,
			"node_name":      ids[index],
			"region":         "default",
			"bind_addr":      raftAddrs[index],
			"advertise_addr": raftAddrs[index],
			"cluster_secret": "multinode-test-cluster-secret",
			"discovery":      map[string]any{"mode": "manual"},
			"distribution":   map[string]any{"strategy": "hash"},
			"raft": map[string]any{
				"node_id":           ids[index],
				"bind_addr":         raftAddrs[index],
				"advertise_addr":    raftAddrs[index],
				"region":            "default",
				"bootstrap":         index == 0,
				"election_timeout":  "200ms",
				"heartbeat_timeout": "100ms",
				"commit_timeout":    "20ms",
				"peers":             peers,
			},
		},
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "anubis.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func waitForClusterLeader(t *testing.T, nodes []*clusterTestNode, timeout time.Duration) *clusterTestNode {
	t.Helper()
	var leader *clusterTestNode
	waitFor(t, timeout, "a leader every node agrees on", func() bool {
		leader = nil
		for _, node := range nodes {
			if node.deps.ClusterManager.IsLeader() {
				leader = node
			}
		}
		if leader == nil {
			return false
		}
		for _, node := range nodes {
			if node.deps.ClusterManager.Leader() != leader.id {
				return false
			}
		}
		return true
	})
	return leader
}

func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	waitForOrDump(t, timeout, what, cond, nil)
}

// waitForOrDump is waitFor that logs dump() on timeout, so a CI failure shows
// the cluster state that caused it.
func waitForOrDump(t *testing.T, timeout time.Duration, what string, cond func() bool, dump func() string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			if dump != nil {
				t.Logf("cluster state at timeout:\n%s", dump())
			}
			t.Fatalf("timed out after %v waiting for %s", timeout, what)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// clusterStateDump describes each node's view: leader, live members (which
// drive ownership) and how many souls its probe engine runs.
func clusterStateDump(nodes ...*clusterTestNode) string {
	var b strings.Builder
	for _, node := range nodes {
		live := make([]string, 0)
		for _, m := range node.deps.ClusterManager.LiveMembers() {
			live = append(live, m.NodeID)
		}
		sort.Strings(live)
		fmt.Fprintf(&b, "  %s: leader=%q live=%v active_checks=%d\n",
			node.id, node.deps.ClusterManager.Leader(), live, node.deps.ProbeEngine.GetStatus().ActiveChecks)
	}
	return b.String()
}

// postJSON posts body and returns the "id" of the created resource.
func postJSON(t *testing.T, client *http.Client, url string, body any) string {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Post(url, "application/json", strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST %s answered %d: %s", url, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &created); err != nil || created.ID == "" {
		t.Fatalf("POST %s returned no id: %s", url, raw)
	}
	return created.ID
}

// countingChecker stands in for the TCP checker: it records which node ran
// each check and reports the soul dead, with a message naming the check.
type countingChecker struct {
	node    string
	counter *checkCounter
}

func (c *countingChecker) Type() core.CheckType { return core.CheckTCP }

func (c *countingChecker) Validate(*core.Soul) error { return nil }

func (c *countingChecker) Judge(_ context.Context, soul *core.Soul) (*core.Judgment, error) {
	n := c.counter.record(soul.ID, c.node)
	return &core.Judgment{
		SoulID:    soul.ID,
		Status:    core.SoulDead,
		Message:   fmt.Sprintf("check %d by %s", n, c.node),
		Duration:  time.Millisecond,
		Timestamp: time.Now().UTC(),
	}, nil
}

type checkCounter struct {
	mu     sync.Mutex
	counts map[string]map[string]int // soul -> node -> checks
}

func newCheckCounter() *checkCounter {
	return &checkCounter{counts: make(map[string]map[string]int)}
}

func (c *checkCounter) record(soulID, node string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.counts[soulID] == nil {
		c.counts[soulID] = make(map[string]int)
	}
	c.counts[soulID][node]++
	return c.counts[soulID][node]
}

func (c *checkCounter) byNode(soulID string) map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]int, len(c.counts[soulID]))
	for node, n := range c.counts[soulID] {
		out[node] = n
	}
	return out
}

func (c *checkCounter) total(soulID string) int {
	total := 0
	for _, n := range c.byNode(soulID) {
		total += n
	}
	return total
}

type notificationLog struct {
	mu   sync.Mutex
	seen map[string]int // message -> deliveries
}

func newNotificationLog() *notificationLog {
	return &notificationLog{seen: make(map[string]int)}
}

func (l *notificationLog) handle(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
		l.mu.Lock()
		l.seen[payload.Message]++
		l.mu.Unlock()
	}
	w.WriteHeader(http.StatusOK)
}

func (l *notificationLog) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	total := 0
	for _, n := range l.seen {
		total += n
	}
	return total
}

func (l *notificationLog) messages() map[string]int {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[string]int, len(l.seen))
	for k, v := range l.seen {
		out[k] = v
	}
	return out
}
