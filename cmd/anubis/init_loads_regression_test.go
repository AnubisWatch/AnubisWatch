package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// The config written by `anubis init` must load: a logging.output value the
// validator did not accept once made the server refuse to start right after init.
func TestGeneratedConfigLoads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "anubis.json")
	cfg := generateConfig(ConfigOptions{
		Host:            "0.0.0.0",
		HTTPPort:        8080,
		AdminEmail:      "admin@anubis.watch",
		AdminPassword:   generateSecurePassword(),
		DataDir:         filepath.Join(dir, "data"),
		RetentionDays:   90,
		EnableDashboard: true,
		DashboardTheme:  "dark",
		LogLevel:        "info",
		LogFormat:       "json",
	})
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := core.LoadConfig(path)
	if err != nil {
		t.Fatalf("generated config does not load: %v\n%s", err, cfg)
	}
	if loaded.Logging.Output != "both" {
		t.Errorf("logging.output = %q, want both", loaded.Logging.Output)
	}
	if loaded.Security.AllowPrivateTargets {
		t.Error("init must keep private targets blocked by default")
	}
	if _, _, err := newServerLogger(loaded); err != nil {
		t.Fatalf("logger from generated config: %v", err)
	}
}
