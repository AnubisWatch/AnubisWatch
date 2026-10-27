package main

import (
	"context"
	"encoding/json"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
)

func TestClusterManualJourneyRequiresAppliedHistory(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, clustered := range []bool{false, true} {
		dir := t.TempDir()
		t.Setenv("ANUBIS_DATA_DIR", dir)
		path := filepath.Join(dir, "cfg.json")
		cfg := map[string]any{"storage": map[string]any{"path": dir}, "auth": map[string]any{"type": "local", "local": map[string]any{"admin_email": "audit@fixture.test", "admin_password": "Fixture!Password123"}}, "necropolis": map[string]any{"enabled": clustered, "discovery": map[string]any{"mode": "manual"}, "node_name": "audit", "raft": map[string]any{"node_id": "audit"}}}
		raw, _ := json.Marshal(cfg)
		if e := os.WriteFile(path, raw, 0600); e != nil {
			t.Fatal(e)
		}
		deps, e := BuildServerDependencies(ServerOptions{ConfigPath: path, Logger: logger})
		if e != nil {
			t.Fatal("INVALID FIXTURE", e)
		}
		func() {
			defer deps.Store.Close()
			defer deps.Authenticator.Shutdown()
			defer deps.TracerProvider.Shutdown(context.Background())
			synctest.Test(t, func(t *testing.T) {
				config := &core.JourneyConfig{ID: "j", WorkspaceID: "default", Steps: []core.JourneyStep{{Type: core.CheckType("fixture-unsupported")}}}
				run, err := deps.JourneyExecutor.RunOnce(context.Background(), config)
				if run == nil {
					t.Fatal("INVALID FIXTURE: missing completed result")
				}
				_, readErr := deps.Store.GetJourneyRun(context.Background(), "default", "j", run.ID)
				if !clustered {
					if err != nil || readErr != nil {
						t.Fatal("INVALID CONTROL", err, readErr)
					}
					t.Log("CONTROL: standalone success has readable history")
					return
				}
				t.Logf("EXPECTED: cluster success requires stored history or returns failure ACTUAL: success=%v readable=%v", err == nil, readErr == nil)
				if err == nil && readErr != nil {
					t.Fatal("PROBLEM CONFIRMED")
				}
				t.Log("PROBLEM NOT REPRODUCED")
			})
		}()
	}
}
