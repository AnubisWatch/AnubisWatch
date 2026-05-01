package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"gopkg.in/yaml.v3"
)

func captureStdoutSystem(f func()) string {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func TestStatusCommand_NoStorage(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", filepath.Join(tmpDir, "does-not-exist"))

	output := captureStdoutSystem(statusCommand)
	if !strings.Contains(output, "AnubisWatch System Status") {
		t.Errorf("Expected status header, got: %s", output)
	}
	if !strings.Contains(output, "not accessible") {
		t.Errorf("Expected storage not accessible, got: %s", output)
	}
}

func TestExportCommand_NoArgs(t *testing.T) {
	oldArgs := os.Args
	os.Args = []string{"anubis", "export"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSystem(exportCommand)
	if !strings.Contains(output, "Export Configuration") {
		t.Errorf("Expected export header, got: %s", output)
	}
	if !strings.Contains(output, "souls") {
		t.Errorf("Expected souls subcommand, got: %s", output)
	}
}

func TestExportCommand_Souls(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	ws := &core.Workspace{ID: "default", Name: "Default"}
	if err := store.SaveWorkspace(ctx, ws); err != nil {
		t.Fatalf("Failed to save workspace: %v", err)
	}
	soul := &core.Soul{ID: "s1", Name: "test", Type: "http", Target: "https://example.com", WorkspaceID: "default"}
	if err := store.SaveSoul(ctx, soul); err != nil {
		t.Fatalf("Failed to save soul: %v", err)
	}
	store.Close()

	oldArgs := os.Args
	os.Args = []string{"anubis", "export", "souls"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSystem(exportCommand)
	if !strings.Contains(output, "test") {
		t.Errorf("Expected soul name in output, got: %s", output)
	}
	if !strings.Contains(output, "https://example.com") {
		t.Errorf("Expected target in output, got: %s", output)
	}
}

func TestExportCommand_Config(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "anubis.json")
	cfg := map[string]interface{}{"server": map[string]interface{}{"port": 8080}}
	data, _ := json.Marshal(cfg)
	os.WriteFile(configPath, data, 0644)

	os.Setenv("ANUBIS_CONFIG", configPath)
	defer os.Unsetenv("ANUBIS_CONFIG")

	oldArgs := os.Args
	os.Args = []string{"anubis", "export", "config"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSystem(exportCommand)
	if !strings.Contains(output, "8080") {
		t.Errorf("Expected port in config output, got: %s", output)
	}
}

func TestConfigCommand_NoArgs(t *testing.T) {
	oldArgs := os.Args
	os.Args = []string{"anubis", "config"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSystem(configCommand)
	if !strings.Contains(output, "Configuration Management") {
		t.Errorf("Expected config header, got: %s", output)
	}
	if !strings.Contains(output, "validate") {
		t.Errorf("Expected validate subcommand, got: %s", output)
	}
}

func TestConfigCommand_Path_WithConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "anubis.json")
	os.WriteFile(configPath, []byte("{}"), 0644)

	os.Setenv("ANUBIS_CONFIG", configPath)
	defer os.Unsetenv("ANUBIS_CONFIG")

	oldArgs := os.Args
	os.Args = []string{"anubis", "config", "path"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSystem(configCommand)
	if !strings.Contains(output, configPath) {
		t.Errorf("Expected config path in output, got: %s", output)
	}
}

func TestConfigCommand_Show(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "anubis.json")
	cfg := map[string]interface{}{"server": map[string]interface{}{"host": "0.0.0.0"}}
	data, _ := json.Marshal(cfg)
	os.WriteFile(configPath, data, 0644)

	os.Setenv("ANUBIS_CONFIG", configPath)
	defer os.Unsetenv("ANUBIS_CONFIG")

	oldArgs := os.Args
	os.Args = []string{"anubis", "config", "show"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSystem(configCommand)
	if !strings.Contains(output, "Current Configuration") {
		t.Errorf("Expected show header, got: %s", output)
	}
	if !strings.Contains(output, "0.0.0.0") {
		t.Errorf("Expected host in output, got: %s", output)
	}
}

func TestConfigCommand_Validate(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "anubis.json")
	cfg := core.Config{
		Server:  core.ServerConfig{Host: "127.0.0.1", Port: 8080},
		Storage: core.StorageConfig{Path: "/tmp/anubis"},
		Logging: core.LoggingConfig{Level: "info"},
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(configPath, data, 0644)

	os.Setenv("ANUBIS_CONFIG", configPath)
	defer os.Unsetenv("ANUBIS_CONFIG")

	oldArgs := os.Args
	os.Args = []string{"anubis", "config", "validate"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSystem(configCommand)
	if !strings.Contains(output, "Configuration is valid") {
		t.Errorf("Expected valid message, got: %s", output)
	}
	if !strings.Contains(output, "127.0.0.1") {
		t.Errorf("Expected host in output, got: %s", output)
	}
}

func TestConfigCommand_Set(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "anubis.json")
	cfg := map[string]interface{}{"server": map[string]interface{}{"port": 8080}}
	data, _ := json.Marshal(cfg)
	os.WriteFile(configPath, data, 0644)

	os.Setenv("ANUBIS_CONFIG", configPath)
	defer os.Unsetenv("ANUBIS_CONFIG")

	oldArgs := os.Args
	os.Args = []string{"anubis", "config", "set", "server.port", "9443"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSystem(configCommand)
	if !strings.Contains(output, "Set server.port = 9443") {
		t.Errorf("Expected set confirmation, got: %s", output)
	}

	// Verify file was updated
	updated, _ := os.ReadFile(configPath)
	if !strings.Contains(string(updated), "9443") {
		t.Errorf("Expected updated config to contain 9443, got: %s", string(updated))
	}
}

func TestLogsCommand_NoLogFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	oldArgs := os.Args
	os.Args = []string{"anubis", "logs"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSystem(logsCommand)
	if !strings.Contains(output, "Log file not found") {
		t.Errorf("Expected log not found message, got: %s", output)
	}
	if !strings.Contains(output, "Logs are written to stderr") {
		t.Errorf("Expected stderr hint, got: %s", output)
	}
}

func TestLogsCommand_WithLogFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	logDir := filepath.Join(tmpDir, "logs")
	os.MkdirAll(logDir, 0755)
	logPath := filepath.Join(logDir, "anubis.log")
	os.WriteFile(logPath, []byte("line1\nline2\nline3"), 0644)

	oldArgs := os.Args
	os.Args = []string{"anubis", "logs", "-n", "2"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSystem(logsCommand)
	if !strings.Contains(output, "line2") {
		t.Errorf("Expected line2 in output, got: %s", output)
	}
	if !strings.Contains(output, "line3") {
		t.Errorf("Expected line3 in output, got: %s", output)
	}
}

func TestConfigCommand_ValidateYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "anubis.yaml")
	cfg := core.Config{
		Server:  core.ServerConfig{Host: "127.0.0.1", Port: 8080},
		Storage: core.StorageConfig{Path: "/tmp/anubis"},
		Logging: core.LoggingConfig{Level: "info"},
	}
	data, _ := yaml.Marshal(cfg)
	os.WriteFile(configPath, data, 0644)

	os.Setenv("ANUBIS_CONFIG", configPath)
	defer os.Unsetenv("ANUBIS_CONFIG")

	oldArgs := os.Args
	os.Args = []string{"anubis", "config", "validate"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSystem(configCommand)
	if !strings.Contains(output, "Configuration is valid") {
		t.Errorf("Expected valid message, got: %s", output)
	}
}

func TestConfigCommand_Set_NestedKey(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "anubis.json")
	cfg := map[string]interface{}{"server": map[string]interface{}{"host": "0.0.0.0", "tls": map[string]interface{}{"enabled": false}}}
	data, _ := json.Marshal(cfg)
	os.WriteFile(configPath, data, 0644)

	os.Setenv("ANUBIS_CONFIG", configPath)
	defer os.Unsetenv("ANUBIS_CONFIG")

	oldArgs := os.Args
	os.Args = []string{"anubis", "config", "set", "server.tls.enabled", "true"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSystem(configCommand)
	if !strings.Contains(output, "Set server.tls.enabled = true") {
		t.Errorf("Expected set confirmation, got: %s", output)
	}

	updated, _ := os.ReadFile(configPath)
	if !strings.Contains(string(updated), "true") {
		t.Errorf("Expected updated config to contain true, got: %s", string(updated))
	}
}

func TestConfigCommand_Set_InvalidKeyFormat(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "anubis.json")
		os.WriteFile(configPath, []byte("{}"), 0644)
		os.Setenv("ANUBIS_CONFIG", configPath)
		os.Args = []string{"anubis", "config", "set", "invalidkey", "value"}
		configCommand()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestConfigCommand_Set_InvalidKeyFormat")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Invalid key format") {
		t.Errorf("Expected invalid key format error, got: %s", string(output))
	}
}

func TestConfigCommand_Set_UnknownSection(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "anubis.json")
		cfg := map[string]interface{}{"server": map[string]interface{}{"port": 8080}}
		data, _ := json.Marshal(cfg)
		os.WriteFile(configPath, data, 0644)
		os.Setenv("ANUBIS_CONFIG", configPath)
		os.Args = []string{"anubis", "config", "set", "unknown.key", "value"}
		configCommand()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestConfigCommand_Set_UnknownSection")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Unknown config section") {
		t.Errorf("Expected unknown section error, got: %s", string(output))
	}
}

func TestConfigCommand_UnknownSubcommandExits(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		os.Args = []string{"anubis", "config", "unknown"}
		configCommand()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestConfigCommand_UnknownSubcommandExits")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Unknown config subcommand") {
		t.Errorf("Expected unknown subcommand error, got: %s", string(output))
	}
}

func TestExportCommand_ConfigNotFound(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		tmpDir := t.TempDir()
		t.Setenv("ANUBIS_DATA_DIR", tmpDir)
		os.Unsetenv("ANUBIS_CONFIG")
		os.Args = []string{"anubis", "export", "config"}
		exportCommand()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestExportCommand_ConfigNotFound")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "No configuration file found") {
		t.Errorf("Expected no config error, got: %s", string(output))
	}
}

func TestConfigCommand_Path_NoConfig(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		tmpDir := t.TempDir()
		t.Setenv("ANUBIS_DATA_DIR", tmpDir)
		os.Unsetenv("ANUBIS_CONFIG")
		// Change to a temp dir so ./anubis.json doesn't exist
		os.Chdir(tmpDir)
		os.Args = []string{"anubis", "config", "path"}
		configCommand()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestConfigCommand_Path_NoConfig")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "./anubis.json") {
		t.Errorf("Expected default path in output, got: %s", string(output))
	}
}

func TestConfigCommand_Show_NoConfig(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		tmpDir := t.TempDir()
		t.Setenv("ANUBIS_DATA_DIR", tmpDir)
		os.Unsetenv("ANUBIS_CONFIG")
		os.Chdir(tmpDir)
		os.Args = []string{"anubis", "config", "show"}
		configCommand()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestConfigCommand_Show_NoConfig")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "No configuration file found") {
		t.Errorf("Expected no config message, got: %s", string(output))
	}
}

func TestConfigCommand_Validate_NoConfig(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		tmpDir := t.TempDir()
		t.Setenv("ANUBIS_DATA_DIR", tmpDir)
		os.Unsetenv("ANUBIS_CONFIG")
		os.Chdir(tmpDir)
		os.Args = []string{"anubis", "config", "validate"}
		configCommand()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestConfigCommand_Validate_NoConfig")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "No configuration file found") {
		t.Errorf("Expected no config message, got: %s", string(output))
	}
}

func TestStatusCommand_WithJudgments(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	ctx := context.Background()
	ws := &core.Workspace{ID: "default", Name: "Default"}
	store.SaveWorkspace(ctx, ws)

	// Add souls with different judgment statuses
	aliveSoul := &core.Soul{ID: "s-alive", Name: "alive-soul", Type: "http", Target: "https://a.com", WorkspaceID: "default"}
	deadSoul := &core.Soul{ID: "s-dead", Name: "dead-soul", Type: "http", Target: "https://b.com", WorkspaceID: "default"}
	degradedSoul := &core.Soul{ID: "s-deg", Name: "degraded-soul", Type: "http", Target: "https://c.com", WorkspaceID: "default"}
	store.SaveSoul(ctx, aliveSoul)
	store.SaveSoul(ctx, deadSoul)
	store.SaveSoul(ctx, degradedSoul)

	// Add judgments
	now := time.Now()
	store.SaveJudgment(ctx, &core.Judgment{ID: "j1", SoulID: "s-alive", Status: core.SoulAlive, Timestamp: now, Duration: 100})
	store.SaveJudgment(ctx, &core.Judgment{ID: "j2", SoulID: "s-dead", Status: core.SoulDead, Timestamp: now, Duration: 200})
	store.SaveJudgment(ctx, &core.Judgment{ID: "j3", SoulID: "s-deg", Status: core.SoulDegraded, Timestamp: now, Duration: 300})

	store.Close()

	output := captureStdoutSystem(statusCommand)
	if !strings.Contains(output, "Alive:   1") {
		t.Errorf("Expected 1 alive soul, got: %s", output)
	}
	if !strings.Contains(output, "Dead:    1") {
		t.Errorf("Expected 1 dead soul, got: %s", output)
	}
	if !strings.Contains(output, "Degraded: 1") {
		t.Errorf("Expected 1 degraded soul, got: %s", output)
	}
	if !strings.Contains(output, "Total:     3") {
		t.Errorf("Expected 3 total souls, got: %s", output)
	}
}

func TestLogsCommand_FollowFlag(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	logDir := filepath.Join(tmpDir, "logs")
	os.MkdirAll(logDir, 0755)
	logPath := filepath.Join(logDir, "anubis.log")
	os.WriteFile(logPath, []byte("log line"), 0644)

	oldArgs := os.Args
	os.Args = []string{"anubis", "logs", "--follow"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSystem(logsCommand)
	if !strings.Contains(output, "Follow mode not implemented") {
		t.Errorf("Expected follow message, got: %s", output)
	}
}

func TestSelfHealth_InaccessibleDataDir(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		t.Setenv("ANUBIS_DATA_DIR", "Z:/nonexistent/path/for/sure")
		selfHealth()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestSelfHealth_InaccessibleDataDir")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "inaccessible") {
		t.Errorf("Expected inaccessible message, got: %s", string(output))
	}
}

func TestConfigCommand_Set_MissingConfig(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		tmpDir := t.TempDir()
		t.Setenv("ANUBIS_DATA_DIR", tmpDir)
		os.Unsetenv("ANUBIS_CONFIG")
		os.Chdir(tmpDir)
		os.Args = []string{"anubis", "config", "set", "server.port", "9443"}
		configCommand()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestConfigCommand_Set_MissingConfig")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Error reading config") {
		t.Errorf("Expected no config error, got: %s", string(output))
	}
}

func TestStatusCommand_StorageError(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", filepath.Join(tmpDir, "not-a-dir"))

	output := captureStdoutSystem(statusCommand)
	if !strings.Contains(output, "not accessible") {
		t.Errorf("Expected not accessible, got: %s", output)
	}
}
func TestLogsCommand_FollowWithLines(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	logDir := filepath.Join(tmpDir, "logs")
	os.MkdirAll(logDir, 0755)
	logPath := filepath.Join(logDir, "anubis.log")
	os.WriteFile(logPath, []byte("line1\nline2\nline3\nline4\nline5"), 0644)

	oldArgs := os.Args
	os.Args = []string{"anubis", "logs", "-f", "-n", "2"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSystem(logsCommand)
	if !strings.Contains(output, "line4") {
		t.Errorf("Expected line4 in output, got: %s", output)
	}
	if !strings.Contains(output, "Follow mode not implemented") {
		t.Errorf("Expected follow message, got: %s", output)
	}
}

func TestConfigCommand_Set_MissingArgs(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "anubis.json")
		os.WriteFile(configPath, []byte("{}"), 0644)
		os.Setenv("ANUBIS_CONFIG", configPath)
		os.Args = []string{"anubis", "config", "set"}
		configCommand()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestConfigCommand_Set_MissingArgs")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Usage: anubis config set") {
		t.Errorf("Expected usage message, got: %s", string(output))
	}
}

func TestConfigCommand_Validate_InvalidJSON(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "anubis.json")
		os.WriteFile(configPath, []byte("not valid json"), 0644)
		os.Setenv("ANUBIS_CONFIG", configPath)
		os.Args = []string{"anubis", "config", "validate"}
		configCommand()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestConfigCommand_Validate_InvalidJSON")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Invalid configuration") {
		t.Errorf("Expected invalid config error, got: %s", string(output))
	}
}

func TestConfigCommand_Set_InvalidJSON(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "anubis.json")
		os.WriteFile(configPath, []byte("not json"), 0644)
		os.Setenv("ANUBIS_CONFIG", configPath)
		os.Args = []string{"anubis", "config", "set", "server.port", "9443"}
		configCommand()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestConfigCommand_Set_InvalidJSON")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Invalid config JSON") {
		t.Errorf("Expected invalid JSON error, got: %s", string(output))
	}
}
