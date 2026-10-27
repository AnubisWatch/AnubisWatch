// Command productionsoak runs an isolated real-server workload and restart check.
// It never connects to or modifies a deployed AnubisWatch instance.
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

type options struct {
	binary, output string
	duration       time.Duration
	souls          int
	minCadence     float64
	maxRSS         int64
}
type sample struct {
	At       time.Time          `json:"at"`
	Phase    string             `json:"phase"`
	RSSBytes int64              `json:"rss_bytes"`
	Metrics  map[string]float64 `json:"metrics"`
}
type result struct {
	Started               time.Time       `json:"started"`
	DurationSeconds       float64         `json:"duration_seconds"`
	Souls                 int             `json:"souls"`
	CadenceSeconds        int             `json:"cadence_seconds"`
	MinimumCadenceRatio   float64         `json:"minimum_cadence_ratio"`
	MaximumRSSBytes       int64           `json:"maximum_rss_bytes"`
	PeakTargetConcurrency int64           `json:"peak_target_concurrency"`
	TargetRequests        int64           `json:"target_requests"`
	MinimumJudgments      int             `json:"minimum_judgments_per_soul"`
	MaximumJudgments      int             `json:"maximum_judgments_per_soul"`
	TotalJudgments        int             `json:"total_judgments"`
	TransitionSouls       int             `json:"healthy_dead_healthy_souls"`
	RestartPreserved      int             `json:"restart_preserved_judgment_ids"`
	BinarySHA256          string          `json:"binary_sha256"`
	Version               json.RawMessage `json:"binary_version"`
	Samples               []sample        `json:"samples"`
	Passed                bool            `json:"passed"`
	Error                 string          `json:"error,omitempty"`
}

type process struct {
	command *exec.Cmd
	done    chan error
	log     *os.File
}

func start(binary, config, data, logPath string) (*process, error) {
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	// #nosec G204 -- operator-selected local candidate binary; no shell or remote input.
	cmd := exec.CommandContext(context.Background(), binary, "serve", "--single", "--config", config)
	env := os.Environ()
	for _, key := range []string{"ANUBIS_CONFIG", "ANUBIS_DATA_DIR", "ANUBIS_ADMIN_PASSWORD", "ANUBIS_HTTP_PORT", "ANUBIS_PORT", "ANUBIS_HOST", "ANUBIS_ENCRYPTION_KEY", "ANUBIS_SSRF_ALLOW_PRIVATE", "ANUBIS_LOG_LEVEL"} {
		clean := env[:0]
		for _, item := range env {
			if !strings.HasPrefix(item, key+"=") {
				clean = append(clean, item)
			}
		}
		env = clean
	}
	cmd.Env = append(env, "ANUBIS_CONFIG="+config, "ANUBIS_DATA_DIR="+data)
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		log.Close()
		return nil, err
	}
	p := &process{command: cmd, done: make(chan error, 1), log: log}
	go func() { p.done <- cmd.Wait(); log.Close() }()
	return p, nil
}
func (p *process) stop() error {
	if p == nil {
		return nil
	}
	select {
	case err := <-p.done:
		return err
	default:
	}
	if err := p.command.Process.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	select {
	case err := <-p.done:
		return err
	case <-time.After(45 * time.Second):
		_ = p.command.Process.Kill() // stop already failed its deadline
		if err := <-p.done; err != nil {
			return fmt.Errorf("server forced to stop after45s: %w", err)
		}
		return errors.New("server did not stop within45s")
	}
}
func request(client *http.Client, method, url string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequestWithContext(context.Background(), method, url, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s %s returned%d", method, req.URL.Path, response.StatusCode)
	}
	return data, nil
}
func awaitReady(ctx context.Context, client *http.Client, base string, p *process) error {
	deadline := time.NewTimer(40 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := request(client, "GET", base+"/ready", nil); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-p.done:
			return fmt.Errorf("server exited during startup: %v", err)
		case <-deadline.C:
			return errors.New("server not ready within40s")
		case <-ticker.C:
		}
	}
}
func login(client *http.Client, base, password string) error {
	body, _ := json.Marshal(map[string]string{"email": "soak@example.invalid", "password": password})
	_, err := request(client, "POST", base+"/api/v1/auth/login", strings.NewReader(string(body)))
	return err
}
func metricValues(data []byte) map[string]float64 {
	out := make(map[string]float64)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			continue
		}
		switch fields[0] {
		case "anubis_memory_alloc_bytes", "anubis_memory_sys_bytes", "anubis_goroutines", "anubis_judgments_total":
			if value, err := strconv.ParseFloat(fields[1], 64); err == nil {
				out[fields[0]] = value
			}
		}
	}
	return out
}
func rss(pid int) int64 {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) > 1 {
				n, _ := strconv.ParseInt(fields[1], 10, 64)
				return n * 1024
			}
		}
	}
	return 0
}
func openStore(path, key string) (*storage.CobaltDB, error) {
	return storage.NewEngine(core.StorageConfig{Path: path, Encryption: core.EncryptionConfig{Enabled: true, Key: key}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}
func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}
func randomValue() string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return "Aa1!" + hex.EncodeToString(b[:])
}

func run(ctx context.Context, o options, summary *result) (retErr error) {
	if o.souls < 1 || o.souls > 10000 {
		return errors.New("souls must be between1 and10000")
	}
	if o.duration < 12*time.Second {
		return errors.New("duration must be at least12s to exercise three phases")
	}
	if o.minCadence <= 0 || o.minCadence > 1 {
		return errors.New("minimum cadence ratio must be in(0,1]")
	}
	if o.maxRSS <= 0 {
		return errors.New("max-rss-mib must be positive")
	}
	if _, err := os.Stat(filepath.Join(o.output, "result.json")); err == nil {
		return errors.New("output already contains result.json; use a new evidence directory")
	}
	if err := os.MkdirAll(o.output, 0700); err != nil {
		return err
	}
	defer func() {
		summary.Error = ""
		if retErr != nil {
			summary.Error = retErr.Error()
		}
		if err := writeJSON(filepath.Join(o.output, "result.json"), summary); retErr == nil && err != nil {
			retErr = err
		}
	}()
	work, err := os.MkdirTemp("", "anubis-soak-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	if o.binary == "" {
		o.binary = filepath.Join(work, "anubis")
		// #nosec G204 -- output is inside this harness's owned temporary directory.
		cmd := exec.CommandContext(ctx, "go", "build", "-o", o.binary, "./cmd/anubis")
		if data, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("build binary: %w: %s", err, data)
		}
	}
	o.binary, err = filepath.Abs(o.binary)
	if err != nil {
		return err
	}
	bytes, err := os.ReadFile(o.binary)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(bytes)
	summary.BinarySHA256 = hex.EncodeToString(hash[:])
	// #nosec G204 -- operator-selected candidate executable, invoked without a shell.
	version, err := exec.CommandContext(ctx, o.binary, "version", "--json").Output()
	if err != nil {
		return err
	}
	summary.Version = json.RawMessage(version)
	var phase atomic.Int64
	var active, peak, total atomic.Int64
	counts := make([][3]int, o.souls)
	var countMu sync.Mutex
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/"))
		if err != nil || index < 0 || index >= o.souls {
			http.NotFound(w, r)
			return
		}
		p := int(phase.Load())
		now := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); now > old; old = peak.Load() {
			if peak.CompareAndSwap(old, now) {
				break
			}
		}
		total.Add(1)
		countMu.Lock()
		counts[index][p]++
		countMu.Unlock()
		time.Sleep(10 * time.Millisecond)
		if p == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, "injected bounded failure")
			return
		}
		_, _ = io.WriteString(w, "healthy")
	}))
	defer target.Close()
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	key, password := randomValue(), randomValue()
	dataPath := filepath.Join(work, "data")
	configPath := filepath.Join(work, "anubis.json")
	cfg := map[string]any{"environment": "development", "server": map[string]any{"host": "127.0.0.1", "port": port, "grpc_port": 0, "metrics_auth": true}, "storage": map[string]any{"path": dataPath, "encryption": map[string]any{"enabled": true, "key": key}}, "auth": map[string]any{"enabled": true, "type": "local", "local": map[string]string{"admin_email": "soak@example.invalid", "admin_password": password}}, "security": map[string]any{"allow_private_targets": true}, "dashboard": map[string]bool{"enabled": false}, "necropolis": map[string]bool{"enabled": false}, "logging": map[string]string{"level": "error", "format": "json"}}
	if err := writeJSON(configPath, cfg); err != nil {
		return err
	}
	db, err := openStore(dataPath, key)
	if err != nil {
		return err
	}
	for i := 0; i < o.souls; i++ {
		soul := &core.Soul{ID: fmt.Sprintf("soak-%05d", i), WorkspaceID: "default", Name: fmt.Sprintf("Soak%05d", i), Type: core.CheckHTTP, Target: target.URL + "/" + strconv.Itoa(i), Enabled: true, Weight: core.Duration{Duration: time.Second}, Timeout: core.Duration{Duration: 5 * time.Second}, HTTP: &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}}}
		if err := db.SaveSoul(context.Background(), soul); err != nil {
			db.Close()
			return err
		}
	}
	if err := db.Close(); err != nil {
		return err
	}
	p, err := start(o.binary, configPath, dataPath, filepath.Join(o.output, "server.log"))
	if err != nil {
		return err
	}
	defer func() {
		if p != nil {
			_ = p.stop()
		}
	}()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	if err := awaitReady(ctx, client, base, p); err != nil {
		return err
	}
	if err := login(client, base, password); err != nil {
		return err
	}
	summary.Started = time.Now().UTC()
	summary.Souls = o.souls
	summary.DurationSeconds = o.duration.Seconds()
	summary.CadenceSeconds = 1
	summary.MinimumCadenceRatio = o.minCadence
	failAt := o.duration / 2
	recoverAt := failAt + 3*time.Second
	startTime := time.Now()
	// Metrics enumeration can be expensive under sustained history. Keep target
	// transitions on independent timers so a scrape cannot extend the failure.
	phaseContext, cancelPhases := context.WithCancel(ctx)
	phaseDone := make(chan struct{})
	go func() {
		defer close(phaseDone)
		wait := func(duration time.Duration) bool {
			timer := time.NewTimer(duration)
			defer timer.Stop()
			select {
			case <-phaseContext.Done():
				return false
			case <-timer.C:
				return true
			}
		}
		if !wait(failAt) {
			return
		}
		phase.Store(1)
		fmt.Printf("PHASE injected-failure elapsed=%s\n", time.Since(startTime).Round(time.Millisecond))
		if !wait(3 * time.Second) {
			return
		}
		phase.Store(2)
		fmt.Printf("PHASE recovered elapsed=%s\n", time.Since(startTime).Round(time.Millisecond))
	}()
	defer func() { cancelPhases(); <-phaseDone }()
	nextSample := time.Duration(0)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	takeSample := func(label string) error {
		data, err := request(client, "GET", base+"/metrics", nil)
		if err != nil {
			return fmt.Errorf("sample metrics: %w", err)
		}
		item := sample{At: time.Now().UTC(), Phase: label, RSSBytes: rss(p.command.Process.Pid), Metrics: metricValues(data)}
		summary.Samples = append(summary.Samples, item)
		if item.RSSBytes > summary.MaximumRSSBytes {
			summary.MaximumRSSBytes = item.RSSBytes
		}
		if item.RSSBytes > o.maxRSS*1024*1024 {
			return fmt.Errorf("RSS%d exceeds explicit%dMiB acceptance limit", item.RSSBytes, o.maxRSS)
		}
		if n := item.Metrics["anubis_goroutines"]; n > float64(o.souls*10+1000) {
			return fmt.Errorf("goroutines%.0f exceed explicit budget%d", n, o.souls*10+1000)
		}
		return nil
	}
	fmt.Printf("SOAK START souls=%d cadence=1s duration=%s min-cadence=%.0f%% max-target-concurrency=100 max-RSS=%dMiB\n", o.souls, o.duration, o.minCadence*100, o.maxRSS)
	for time.Since(startTime) < o.duration {
		elapsed := time.Since(startTime)
		label := []string{"healthy", "injected-failure", "recovered"}[phase.Load()]
		if elapsed >= nextSample {
			if err := takeSample(label); err != nil {
				return err
			}
			fmt.Printf("SAMPLE elapsed=%s requests=%d peak=%d RSS=%.1fMiB goroutines=%.0f\n", elapsed.Round(time.Second), total.Load(), peak.Load(), float64(summary.MaximumRSSBytes)/1024/1024, summary.Samples[len(summary.Samples)-1].Metrics["anubis_goroutines"])
			nextSample = elapsed + 30*time.Second
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-p.done:
			p = nil
			return fmt.Errorf("server exited during soak: %v", err)
		case <-ticker.C:
		}
	}
	phase.Store(2)
	if err := takeSample("completed"); err != nil {
		return err
	}
	if err := p.stop(); err != nil {
		return fmt.Errorf("stop after soak: %w", err)
	}
	p = nil
	summary.TargetRequests = total.Load()
	summary.PeakTargetConcurrency = peak.Load()
	if peak.Load() > 100 {
		return fmt.Errorf("target concurrency%d exceeds engine limit100", peak.Load())
	}
	db, err = openStore(dataPath, key)
	if err != nil {
		return err
	}
	minimum := int(o.duration.Seconds() * o.minCadence)
	summary.MinimumJudgments = int(^uint(0) >> 1)
	retained := make([]*core.Judgment, o.souls)
	for i := 0; i < o.souls; i++ {
		id := fmt.Sprintf("soak-%05d", i)
		history, err := db.QueryJudgments(context.Background(), "default", id, summary.Started.Add(-time.Minute), time.Now().Add(time.Minute), 0)
		if err != nil {
			db.Close()
			return err
		}
		if len(history) < minimum {
			db.Close()
			return fmt.Errorf("%s persisted%d judgments, acceptance minimum%d", id, len(history), minimum)
		}
		if len(history) < summary.MinimumJudgments {
			summary.MinimumJudgments = len(history)
		}
		if len(history) > summary.MaximumJudgments {
			summary.MaximumJudgments = len(history)
		}
		summary.TotalJudgments += len(history)
		healthyBefore, dead, recovered := false, false, false
		for _, j := range history {
			if retained[i] == nil || j.Timestamp.After(retained[i].Timestamp) {
				retained[i] = j
			}
			if j.Status == core.SoulDead {
				dead = true
			}
			if j.Status == core.SoulAlive {
				if j.Timestamp.Before(summary.Started.Add(failAt)) {
					healthyBefore = true
				}
				if j.Timestamp.After(summary.Started.Add(recoverAt)) {
					recovered = true
				}
			}
		}
		countMu.Lock()
		observed := counts[i]
		countMu.Unlock()
		if !healthyBefore || !dead || !recovered || observed[0] == 0 || observed[1] == 0 || observed[2] == 0 {
			db.Close()
			return fmt.Errorf("%s did not complete healthy→dead→healthy; target phase counts=%v", id, observed)
		}
		summary.TransitionSouls++
	}
	if err := db.Close(); err != nil {
		return err
	}
	p, err = start(o.binary, configPath, dataPath, filepath.Join(o.output, "restart.log"))
	if err != nil {
		return err
	}
	jar, _ = cookiejar.New(nil)
	client.Jar = jar
	if err := awaitReady(ctx, client, base, p); err != nil {
		return err
	}
	if err := login(client, base, password); err != nil {
		return err
	}
	// Wait on actual requests from every restored runner, not only a fixed delay.
	beforeRestart := make([]int, o.souls)
	countMu.Lock()
	for i := range counts {
		beforeRestart[i] = counts[i][2]
	}
	countMu.Unlock()
	until := time.Now().Add(20 * time.Second)
	for {
		countMu.Lock()
		complete := true
		for i := range counts {
			if counts[i][2] <= beforeRestart[i] {
				complete = false
				break
			}
		}
		countMu.Unlock()
		if complete {
			break
		}
		if time.Now().After(until) {
			return errors.New("not every Soul ran after restart within20s")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	if err := p.stop(); err != nil {
		return err
	}
	p = nil
	db, err = openStore(dataPath, key)
	if err != nil {
		return err
	}
	defer db.Close()
	souls, err := db.ListSouls(context.Background(), "default", 0, 0)
	if err != nil {
		return err
	}
	if len(souls) != o.souls {
		return fmt.Errorf("restart retained%d Souls, want%d", len(souls), o.souls)
	}
	for _, j := range retained {
		actual, err := db.GetJudgment(context.Background(), "default", j.SoulID, j.Timestamp)
		if err != nil || actual.ID != j.ID {
			return fmt.Errorf("restart lost retained judgment for%s: %v", j.SoulID, err)
		}
		summary.RestartPreserved++
	}
	for _, name := range []string{"server.log", "restart.log"} {
		data, err := os.ReadFile(filepath.Join(o.output, name))
		if err != nil {
			return err
		}
		if strings.Contains(string(data), `"level":"ERROR"`) {
			return fmt.Errorf("server ERROR log found in%s", name)
		}
	}
	summary.Passed = true
	fmt.Printf("SOAK PASSED total-judgments=%d per-soul=%d..%d transitions=%d restart-preserved=%d peak-concurrency=%d max-RSS=%.1fMiB\n", summary.TotalJudgments, summary.MinimumJudgments, summary.MaximumJudgments, summary.TransitionSouls, summary.RestartPreserved, summary.PeakTargetConcurrency, float64(summary.MaximumRSSBytes)/1024/1024)
	return nil
}
func main() {
	var o options
	flag.StringVar(&o.binary, "binary", "", "existing anubis binary; otherwise build current source into an owned temporary directory")
	flag.StringVar(&o.output, "output", filepath.Join(".temp_files", "production-soak-"+time.Now().UTC().Format("20060102T150405Z")), "new evidence directory (logs and result.json; never real production data)")
	flag.DurationVar(&o.duration, "duration", 180*time.Second, "soak duration; at least12s; default180s")
	flag.IntVar(&o.souls, "souls", 100, "number of isolated local HTTP Souls at1s cadence (1..10000)")
	flag.Float64Var(&o.minCadence, "min-cadence", .8, "minimum persisted samples per Soul as fraction of expected1s cadence")
	flag.Int64Var(&o.maxRSS, "max-rss-mib", 2048, "explicit process RSS acceptance ceiling in MiB on Linux")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var summary result
	if err := run(ctx, o, &summary); err != nil {
		fmt.Fprintln(os.Stderr, "SOAK FAILED:", err)
		os.Exit(1)
	}
}
