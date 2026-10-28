//go:build unix

// Command hasoak runs a local three-node HA fault-injection soak against real
// anubis processes on loopback. It never connects to a deployed instance.
//
// While a writer creates souls through random nodes, faults are injected one
// node at a time: SIGKILL followed by a restart on the same data directory,
// or SIGSTOP/SIGCONT, which isolates the node like a partition or a long GC
// pause (a stopped leader must step down when it wakes). After a quiet phase
// the run passes only if:
//
//   - every acknowledged write (201) exists on every node: zero lost writes;
//   - no soul is probed by more than one node at a time in the quiet phase;
//   - all nodes agree on one leader.
//
// Network partitions between running nodes need root (iptables); SIGSTOP is
// the unprivileged stand-in. Run it for hours before calling HA production
// ready: go run ./scripts/hasoak -duration 2h
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	adminEmail    = "soak@example.com"
	adminPassword = "HaSoak-Passw0rd-42"
	soulWeight    = 5 * time.Second
)

// sessionJar is shared by every node's client: HA sessions are cluster-wide,
// and cookies ignore the port, so one login serves all three nodes and a
// restarted node does not cost another login against the rate limit.
var sessionJar, _ = cookiejar.New(nil)

type options struct {
	binary, output  string
	duration, quiet time.Duration
	faultEvery      time.Duration
}

type node struct {
	id, dir, config, base string
	raftAddr              string
	cmd                   *exec.Cmd
	done                  chan struct{}
	client                *http.Client
	mu                    sync.Mutex
}

type result struct {
	Started            time.Time      `json:"started"`
	DurationSeconds    float64        `json:"duration_seconds"`
	Faults             map[string]int `json:"faults"`
	WriteAttempts      int            `json:"write_attempts"`
	Acknowledged       int            `json:"acknowledged_writes"`
	WriteErrors        map[string]int `json:"write_errors"`
	LostWrites         map[string]int `json:"lost_writes_per_node"`
	DualProbedSouls    []string       `json:"dual_probed_souls"`
	QuietSoulsChecked  int            `json:"quiet_souls_checked"`
	Leader             string         `json:"leader"`
	LeaderAgreement    bool           `json:"leader_agreement"`
	ServerErrors500    int            `json:"server_errors_500"`
	Passed             bool           `json:"passed"`
	Error              string         `json:"error,omitempty"`
	FaultLog           []string       `json:"fault_log"`
	MaxProbesPerWindow map[string]int `json:"max_probes_in_quiet_window,omitempty"`
}

func main() {
	var o options
	flag.StringVar(&o.binary, "binary", "", "anubis binary; built from the current source when empty")
	flag.StringVar(&o.output, "output", filepath.Join(".temp_files", "ha-soak-"+time.Now().UTC().Format("20060102T150405Z")), "new evidence directory")
	flag.DurationVar(&o.duration, "duration", 5*time.Minute, "fault-injection phase")
	flag.DurationVar(&o.quiet, "quiet", 60*time.Second, "fault-free phase before verification")
	flag.DurationVar(&o.faultEvery, "fault-every", 20*time.Second, "mean time between faults")
	flag.Parse()

	res, err := run(o)
	if err != nil {
		res.Error = err.Error()
		res.Passed = false
	}
	data, _ := json.MarshalIndent(res, "", "  ")
	_ = os.WriteFile(filepath.Join(o.output, "result.json"), data, 0o600)
	fmt.Println(string(data))
	if !res.Passed {
		os.Exit(1)
	}
}

func run(o options) (*result, error) {
	res := &result{Started: time.Now().UTC(), Faults: map[string]int{}, WriteErrors: map[string]int{}, LostWrites: map[string]int{}, MaxProbesPerWindow: map[string]int{}}
	if err := os.MkdirAll(o.output, 0o700); err != nil {
		return res, err
	}
	out, err := filepath.Abs(o.output)
	if err != nil {
		return res, err
	}
	binary := o.binary
	if binary == "" {
		binary = filepath.Join(out, "anubis")
		build := exec.CommandContext(context.Background(), "go", "build", "-o", binary, "./cmd/anubis") // #nosec G204 -- output path is inside this harness's own temp directory
		build.Stdout, build.Stderr = os.Stderr, os.Stderr
		if err := build.Run(); err != nil {
			return res, fmt.Errorf("build: %w", err)
		}
	}

	// Probe target: counts hits per soul so dual probing is visible.
	var hitsMu sync.Mutex
	hits := map[string][]time.Time{}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/s/")
		hitsMu.Lock()
		hits[name] = append(hits[name], time.Now())
		hitsMu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	nodes, err := setup(out)
	if err != nil {
		return res, err
	}
	defer func() {
		for _, n := range nodes {
			n.kill(syscall.SIGCONT)
			n.kill(syscall.SIGKILL)
			n.wait(5 * time.Second)
		}
	}()
	for _, n := range nodes {
		if err := n.start(binary); err != nil {
			return res, err
		}
	}
	if _, err := waitLeader(nodes, 60*time.Second); err != nil {
		return res, err
	}

	// Writer: one create per second through a random node.
	acked := map[string]time.Time{}
	var ackMu sync.Mutex
	stopWriter := make(chan struct{})
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		seq := 0
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-stopWriter:
				return
			case <-tick.C:
			}
			seq++
			name := fmt.Sprintf("soak-%05d", seq)
			n := nodes[rand.IntN(len(nodes))]
			status, body, err := n.do("POST", "/api/v1/souls", map[string]any{
				"name": name, "type": "http", "target": target.URL + "/s/" + name,
				"enabled": true, "weight": soulWeight.String(), "timeout": "2s",
				"http": map[string]any{"method": "GET", "valid_status": []int{200}},
			})
			ackMu.Lock()
			res.WriteAttempts++
			switch {
			case err != nil:
				res.WriteErrors["transport"]++
			case status == http.StatusCreated:
				var soul struct {
					ID string `json:"id"`
				}
				if json.Unmarshal(body, &soul) == nil && soul.ID != "" {
					acked[soul.ID] = time.Now()
					res.Acknowledged++
				}
			default:
				res.WriteErrors[fmt.Sprint(status)]++
				if status == http.StatusInternalServerError {
					res.ServerErrors500++
				}
			}
			ackMu.Unlock()
		}
	}()

	// Faults: one node at a time, so a quorum always exists.
	deadline := time.Now().Add(o.duration)
	for time.Now().Before(deadline) {
		time.Sleep(o.faultEvery/2 + time.Duration(rand.Int64N(int64(o.faultEvery))))
		if !time.Now().Before(deadline) {
			break
		}
		victim := nodes[rand.IntN(len(nodes))]
		role := "follower"
		if leader, err := waitLeader(nodes, 10*time.Second); err == nil && (leader == victim.id || leader == victim.raftAddr) {
			role = "leader"
		}
		outage := 3*time.Second + time.Duration(rand.Int64N(int64(7*time.Second)))
		if rand.IntN(2) == 0 {
			res.Faults["sigkill_"+role]++
			res.FaultLog = append(res.FaultLog, fmt.Sprintf("%s kill -9 %s (%s), down %s", time.Now().Format(time.TimeOnly), victim.id, role, outage.Round(time.Second)))
			victim.kill(syscall.SIGKILL)
			victim.wait(10 * time.Second)
			time.Sleep(outage)
			if err := victim.start(binary); err != nil {
				return res, err
			}
		} else {
			res.Faults["sigstop_"+role]++
			res.FaultLog = append(res.FaultLog, fmt.Sprintf("%s SIGSTOP %s (%s) for %s", time.Now().Format(time.TimeOnly), victim.id, role, outage.Round(time.Second)))
			victim.kill(syscall.SIGSTOP)
			time.Sleep(outage)
			victim.kill(syscall.SIGCONT)
		}
		if _, err := waitLeader(nodes, 60*time.Second); err != nil {
			return res, fmt.Errorf("after fault: %w", err)
		}
	}
	close(stopWriter)
	<-writerDone

	// Quiet phase: no faults, then judge probing over its second half.
	quietStart := time.Now()
	time.Sleep(o.quiet)
	window := o.quiet / 2
	windowStart := quietStart.Add(o.quiet - window)
	expected := int(window / soulWeight)
	limit := expected + expected/2 + 1

	ackMu.Lock()
	defer ackMu.Unlock()
	hitsMu.Lock()
	for name, ts := range hits {
		count := 0
		for _, at := range ts {
			if at.After(windowStart) {
				count++
			}
		}
		if count == 0 {
			continue
		}
		res.QuietSoulsChecked++
		if count > limit {
			res.DualProbedSouls = append(res.DualProbedSouls, name)
			res.MaxProbesPerWindow[name] = count
		}
	}
	hitsMu.Unlock()
	sort.Strings(res.DualProbedSouls)

	leader, err := waitLeader(nodes, 30*time.Second)
	res.LeaderAgreement = err == nil
	res.Leader = leader

	// Every acknowledged soul must be on every node.
	for _, n := range nodes {
		have, err := n.soulIDs()
		if err != nil {
			return res, fmt.Errorf("%s: list souls: %w", n.id, err)
		}
		for id := range acked {
			if !have[id] {
				res.LostWrites[n.id]++
			}
		}
	}
	res.DurationSeconds = time.Since(res.Started).Seconds()
	lost := 0
	for _, v := range res.LostWrites {
		lost += v
	}
	res.Passed = lost == 0 && len(res.DualProbedSouls) == 0 && res.LeaderAgreement && res.Acknowledged > 0 && res.ServerErrors500 == 0
	return res, nil
}

func freePort() int {
	l, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func setup(out string) ([]*node, error) {
	const count = 3
	ids := make([]string, count)
	raft := make([]string, count)
	ports := make([]int, count)
	for i := range ids {
		ids[i] = fmt.Sprintf("jackal-%d", i)
		raft[i] = fmt.Sprintf("127.0.0.1:%d", freePort())
		ports[i] = freePort()
	}
	nodes := make([]*node, count)
	for i := range nodes {
		dir := filepath.Join(out, ids[i])
		if err := os.MkdirAll(filepath.Join(dir, "data"), 0o700); err != nil {
			return nil, err
		}
		peers := []map[string]any{}
		for j := range ids {
			if j != i {
				peers = append(peers, map[string]any{"id": ids[j], "address": raft[j], "region": "default", "role": "voter"})
			}
		}
		cfg := map[string]any{
			"storage":   map[string]any{"path": filepath.Join(dir, "data")},
			"server":    map[string]any{"host": "127.0.0.1", "port": ports[i], "grpc_port": 0},
			"dashboard": map[string]any{"enabled": false},
			"security":  map[string]any{"allow_private_targets": true},
			"logging":   map[string]any{"level": "info", "output": "stdout"},
			"auth":      map[string]any{"type": "local", "local": map[string]any{"admin_email": adminEmail, "admin_password": adminPassword}},
			"necropolis": map[string]any{
				"enabled": true, "node_name": ids[i], "region": "default",
				"bind_addr": raft[i], "advertise_addr": raft[i],
				"cluster_secret": "ha-soak-cluster-secret-0123456789", // #nosec G101 -- throwaway local cluster
				"discovery":      map[string]any{"mode": "manual"},
				"raft": map[string]any{
					"node_id": ids[i], "bind_addr": raft[i], "advertise_addr": raft[i], "region": "default",
					"bootstrap": i == 0, "election_timeout": "1s", "heartbeat_timeout": "300ms", "commit_timeout": "50ms",
					"peers": peers,
				},
			},
		}
		data, _ := json.MarshalIndent(cfg, "", "  ")
		path := filepath.Join(dir, "anubis.json")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return nil, err
		}
		nodes[i] = &node{id: ids[i], raftAddr: raft[i], dir: dir, config: path, base: fmt.Sprintf("http://127.0.0.1:%d", ports[i])}
	}
	return nodes, nil
}

func (n *node) start(binary string) error {
	logf, err := os.OpenFile(filepath.Join(n.dir, "anubis.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(context.Background(), binary, "serve", "--config", n.config) // #nosec G204 -- local soak binary
	env := []string{}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "ANUBIS_") {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env, "ANUBIS_CONFIG="+n.config)
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		logf.Close()
		return err
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); logf.Close(); close(done) }()
	n.mu.Lock()
	n.cmd, n.done = cmd, done
	n.client = &http.Client{Timeout: 5 * time.Second, Jar: sessionJar}
	n.mu.Unlock()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if status, _, err := n.raw("GET", "/health", nil); err == nil && status == http.StatusOK {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("%s did not become healthy", n.id)
}

func (n *node) kill(sig syscall.Signal) {
	n.mu.Lock()
	cmd := n.cmd
	n.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Signal(sig)
	}
}

func (n *node) wait(d time.Duration) {
	n.mu.Lock()
	done := n.done
	n.mu.Unlock()
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(d):
	}
}

func (n *node) login() error {
	status, _, err := n.raw("POST", "/api/v1/auth/login", map[string]any{"email": adminEmail, "password": adminPassword})
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("login: status %d", status)
	}
	return nil
}

func (n *node) raw(method, path string, body any) (int, []byte, error) {
	var r io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		r = bytes.NewReader(data)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, n.base+path, r)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	n.mu.Lock()
	client := n.client
	n.mu.Unlock()
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return resp.StatusCode, data, err
}

// do sends an authenticated request, logging in again after a restart.
func (n *node) do(method, path string, body any) (int, []byte, error) {
	status, data, err := n.raw(method, path, body)
	if err == nil && status == http.StatusUnauthorized {
		if lerr := n.login(); lerr != nil {
			return status, data, nil
		}
		return n.raw(method, path, body)
	}
	return status, data, err
}

func (n *node) soulIDs() (map[string]bool, error) {
	ids := map[string]bool{}
	for offset := 0; ; offset += 100 {
		status, data, err := n.do("GET", fmt.Sprintf("/api/v1/souls?limit=100&offset=%d", offset), nil)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("status %d: %s", status, data)
		}
		var page struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
			Pagination struct {
				HasMore bool `json:"has_more"`
			} `json:"pagination"`
		}
		if err := json.Unmarshal(data, &page); err != nil {
			return nil, err
		}
		for _, s := range page.Data {
			ids[s.ID] = true
		}
		if !page.Pagination.HasMore {
			return ids, nil
		}
		time.Sleep(700 * time.Millisecond) // stay under the per-IP rate limit
	}
}

// waitLeader returns the leader every reachable node agrees on.
func waitLeader(nodes []*node, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		leaders := map[string]int{}
		for _, n := range nodes {
			status, data, err := n.do("GET", "/api/v1/cluster/status", nil)
			if err != nil || status != http.StatusOK {
				continue
			}
			var st struct {
				Leader string `json:"leader"`
			}
			if json.Unmarshal(data, &st) == nil && st.Leader != "" {
				leaders[st.Leader]++
			}
		}
		if len(leaders) == 1 {
			for l, c := range leaders {
				if c == len(nodes) {
					return l, nil
				}
				last = l
			}
		}
		time.Sleep(time.Second)
	}
	return last, errors.New("nodes did not agree on a leader")
}
