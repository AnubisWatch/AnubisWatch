package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/probe"
)

// The SSRF policy is a process-wide global: BuildServerDependencies calls
// probe.ConfigureDefaultValidator, which replaces it. A probe engine that is
// still running from an earlier server reads that global on every check, so
// replacing it while that engine is live is a data race between the publish
// and the read.
//
// The race detector is the oracle for this regression: the project's own test
// target runs with -race (make test), and this test fails under -race against
// an unguarded global. It passes once the global is published atomically.

// ssrfRaceConfig writes a server config whose admin password is generated at
// runtime, so no credential is embedded in the test source.
func ssrfRaceConfig(t *testing.T, dir string, port int) string {
	t.Helper()

	secret := make([]byte, 18)
	if _, err := rand.Read(secret); err != nil {
		t.Fatalf("generating an admin password: %v", err)
	}
	adminPassword := "Aa1!" + base64.RawURLEncoding.EncodeToString(secret)

	cfg := map[string]any{
		"storage": map[string]any{"path": dir},
		"server":  map[string]any{"host": "127.0.0.1", "port": port, "grpc_port": 0},
		// The monitored target is a loopback test server.
		"security": map[string]any{"allow_private_targets": true},
		"auth": map[string]any{"type": "local", "local": map[string]any{
			"admin_email": "ssrf-race@example.com", "admin_password": adminPassword,
		}},
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "anubis.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestBuildSecondServerWhileFirstProbeEngineRuns builds a second server while
// the first server's probe engine is still checking souls. The second build
// republishes the process-wide SSRF validator, which the first engine's
// running checks read concurrently.
func TestBuildSecondServerWhileFirstProbeEngineRuns(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	firstDir := t.TempDir()
	firstCfg := ssrfRaceConfig(t, firstDir, 0)
	// ANUBIS_DATA_DIR overrides the configured storage path, and the package's
	// TestMain sets it globally. Point it at this build's own directory so the
	// two servers do not contend for the data-directory lock.
	t.Setenv("ANUBIS_DATA_DIR", firstDir)
	first, err := BuildServerDependencies(ServerOptions{
		ConfigPath: firstCfg,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("building the first server: %v", err)
	}
	defer first.Store.Close()
	defer first.ProbeEngine.Stop()
	if first.ClusterManager != nil {
		defer first.ClusterManager.Stop(context.Background())
	}

	judgments := make(chan *core.Judgment, 8)
	first.ProbeEngine.SetOnJudgment(func(j *core.Judgment) {
		select {
		case judgments <- j:
		default:
		}
	})

	// A live reader of the process-wide policy, exactly as the alert
	// dispatchers and probe checkers read it. It keeps reading across the
	// second build, so the publish is guaranteed to overlap a read.
	stopReader := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-stopReader:
				return
			default:
			}
			// A production read of the shared policy.
			_ = probe.ValidateTarget("http://127.0.0.1:1/")
		}
	}()
	defer func() {
		close(stopReader)
		<-readerDone
	}()

	// Assigning a soul starts the engine's runner goroutine, which reads the
	// SSRF validator on every check.
	first.ProbeEngine.AssignSouls([]*core.Soul{{
		ID:      "ssrf-race-soul",
		Name:    "ssrf race soul",
		Type:    core.CheckHTTP,
		Target:  target.URL,
		Weight:  core.Duration{Duration: time.Second},
		Timeout: core.Duration{Duration: 5 * time.Second},
		HTTP:    &core.HTTPConfig{Method: "GET", ValidStatus: []int{200}},
	}})

	// Wait until the engine has actually run a check, so its read of the
	// validator is known to have happened.
	select {
	case <-judgments:
	case <-time.After(30 * time.Second):
		t.Fatal("the first server's probe engine never judged the soul")
	}

	// Build the second server. It republishes the SSRF validator while the
	// first server's engine is still checking.
	secondDir := t.TempDir()
	secondCfg := ssrfRaceConfig(t, secondDir, 0)
	t.Setenv("ANUBIS_DATA_DIR", secondDir)
	second, err := BuildServerDependencies(ServerOptions{
		ConfigPath: secondCfg,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("building the second server: %v", err)
	}
	defer second.Store.Close()
	if second.ClusterManager != nil {
		defer second.ClusterManager.Stop(context.Background())
	}

	// Wait for another check to complete. Its read of the validator happens
	// after the second server republished it, and nothing orders that read
	// against the publish.
	select {
	case <-judgments:
	case <-time.After(30 * time.Second):
		t.Fatal("the first server's probe engine stopped checking souls")
	}
}
