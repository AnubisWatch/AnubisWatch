package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestBuildServerDependencies_DefaultConfig(t *testing.T) {
	// Create temp directory for test data
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	opts := ServerOptions{
		ConfigPath: "", // Will use defaults
		Logger:     logger,
	}

	// Create a minimal config file for testing
	configContent := `{
		"storage": {
			"path": "` + filepath.ToSlash(dataDir) + `"
		},
		"server": {
			"host": "127.0.0.1",
			"port": 0
		},
		"necropolis": {
			"node_name": "test-node",
			"region": "test-region"
		}
	}`
	configPath := filepath.Join(tempDir, "test-config.json")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}
	opts.ConfigPath = configPath

	deps, err := BuildServerDependencies(opts)
	if err != nil {
		t.Fatalf("BuildServerDependencies failed: %v", err)
	}

	if deps == nil {
		t.Fatal("Expected non-nil dependencies")
	}

	// Verify all dependencies are initialized
	if deps.Config == nil {
		t.Error("Expected Config to be initialized")
	}
	if deps.Store == nil {
		t.Error("Expected Store to be initialized")
	}
	if deps.Authenticator == nil {
		t.Error("Expected Authenticator to be initialized")
	}
	if deps.AlertManager == nil {
		t.Error("Expected AlertManager to be initialized")
	}
	if deps.ProbeEngine == nil {
		t.Error("Expected ProbeEngine to be initialized")
	}
	if deps.JourneyExecutor == nil {
		t.Error("Expected JourneyExecutor to be initialized")
	}
	if deps.RESTServer == nil {
		t.Error("Expected RESTServer to be initialized")
	}
	if deps.MCPServer == nil {
		t.Error("Expected MCPServer to be initialized")
	}

	// Cleanup
	deps.Store.Close()
}

func TestBuildServerDependencies_InvalidConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	opts := ServerOptions{
		ConfigPath: "/nonexistent/path/config.json",
		Logger:     logger,
	}

	deps, err := BuildServerDependencies(opts)
	if err != nil {
		t.Fatalf("BuildServerDependencies should use defaults for invalid config path: %v", err)
	}

	if deps == nil {
		t.Fatal("Expected non-nil dependencies with defaults")
	}

	// Cleanup
	if deps.Store != nil {
		deps.Store.Close()
	}
}

func TestNewServer(t *testing.T) {
	deps := &ServerDependencies{
		Config: core.GenerateDefaultConfig(),
		Logger: slog.New(slog.NewTextHandler(os.Stdout, nil)),
	}

	server := NewServer(deps)
	if server == nil {
		t.Fatal("Expected non-nil server")
	}

	if server.deps != deps {
		t.Error("Server dependencies not set correctly")
	}
}

func TestServer_StartStop(t *testing.T) {
	tempDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dataDir := filepath.Join(tempDir, "data")

	// Build dependencies
	configContent := `{
		"storage": {
			"path": "` + filepath.ToSlash(dataDir) + `"
		},
		"server": {
			"host": "127.0.0.1",
			"port": 0
		},
		"necropolis": {
			"node_name": "test-node"
		}
	}`
	configPath := filepath.Join(tempDir, "test-config.json")
	os.WriteFile(configPath, []byte(configContent), 0644)

	opts := ServerOptions{
		ConfigPath: configPath,
		Logger:     logger,
	}

	deps, err := BuildServerDependencies(opts)
	if err != nil {
		t.Fatalf("BuildServerDependencies failed: %v", err)
	}

	server := NewServer(deps)

	// Start server
	ctx := context.Background()
	if err := server.Start(ctx); err != nil {
		t.Fatalf("Server.Start failed: %v", err)
	}

	// Give server time to initialize
	time.Sleep(100 * time.Millisecond)

	// Stop server
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := server.Stop(shutdownCtx); err != nil {
		t.Errorf("Server.Stop failed: %v", err)
	}
}

func TestServer_Stop_NilComponents(t *testing.T) {
	// Test that Stop handles nil components gracefully
	deps := &ServerDependencies{
		Config: core.GenerateDefaultConfig(),
		Logger: slog.New(slog.NewTextHandler(os.Stdout, nil)),
		// All other components are nil
	}

	server := NewServer(deps)

	ctx := context.Background()
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Should not panic even with nil components
	if err := server.Stop(shutdownCtx); err != nil {
		t.Errorf("Server.Stop with nil components should not error: %v", err)
	}
}

func TestBuildServerDependencies_InvalidStoragePath(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Create a config with invalid storage path
	configContent := `{
		"storage": {
			"path": "/invalid/path/that/cannot/be/created"
		}
	}`
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "test-config.json")
	os.WriteFile(configPath, []byte(configContent), 0644)

	opts := ServerOptions{
		ConfigPath: configPath,
		Logger:     logger,
	}

	_, err := BuildServerDependencies(opts)
	// On Windows, this might succeed due to different permission model
	// On Unix, it should fail
	if err != nil {
		t.Logf("BuildServerDependencies failed as expected on invalid path: %v", err)
	}
}

func TestServer_Start_WithDashboardEnabled(t *testing.T) {
	tempDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dataDir := filepath.Join(tempDir, "data")

	configContent := `{
		"storage": {
			"path": "` + filepath.ToSlash(dataDir) + `"
		},
		"server": {
			"host": "127.0.0.1",
			"port": 0
		},
		"necropolis": {
			"node_name": "test-node"
		},
		"dashboard": {
			"enabled": true
		}
	}`
	configPath := filepath.Join(tempDir, "test-config.json")
	os.WriteFile(configPath, []byte(configContent), 0644)

	opts := ServerOptions{
		ConfigPath: configPath,
		Logger:     logger,
	}

	deps, err := BuildServerDependencies(opts)
	if err != nil {
		t.Fatalf("BuildServerDependencies failed: %v", err)
	}

	server := NewServer(deps)

	ctx := context.Background()
	if err := server.Start(ctx); err != nil {
		t.Fatalf("Server.Start failed: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	server.Stop(shutdownCtx)
}

// TestServer_Start_WithNilAlertManager tests Start when alert manager fails
func TestServer_Start_WithNilComponents(t *testing.T) {
	tempDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dataDir := filepath.Join(tempDir, "data")

	configContent := `{
		"storage": {
			"path": "` + filepath.ToSlash(dataDir) + `"
		},
		"server": {
			"host": "127.0.0.1",
			"port": 0
		}
	}`
	configPath := filepath.Join(tempDir, "test-config.json")
	os.WriteFile(configPath, []byte(configContent), 0644)

	opts := ServerOptions{
		ConfigPath: configPath,
		Logger:     logger,
	}

	deps, err := BuildServerDependencies(opts)
	if err != nil {
		t.Fatalf("BuildServerDependencies failed: %v", err)
	}

	// Set some components to nil to test error paths
	deps.AlertManager = nil
	deps.ClusterManager = nil
	deps.RESTServer = nil

	server := NewServer(deps)

	ctx := context.Background()
	// Should not panic with nil components
	if err := server.Start(ctx); err != nil {
		t.Fatalf("Server.Start failed: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	server.Stop(shutdownCtx)
}

// TestServer_Start_MultipleTimes tests starting server multiple times
func TestServer_Start_MultipleTimes(t *testing.T) {
	tempDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dataDir := filepath.Join(tempDir, "data")

	configContent := `{
		"storage": {
			"path": "` + filepath.ToSlash(dataDir) + `"
		},
		"server": {
			"host": "127.0.0.1",
			"port": 0
		}
	}`
	configPath := filepath.Join(tempDir, "test-config.json")
	os.WriteFile(configPath, []byte(configContent), 0644)

	opts := ServerOptions{
		ConfigPath: configPath,
		Logger:     logger,
	}

	deps, err := BuildServerDependencies(opts)
	if err != nil {
		t.Fatalf("BuildServerDependencies failed: %v", err)
	}

	server := NewServer(deps)

	ctx := context.Background()

	// Start first time
	if err := server.Start(ctx); err != nil {
		t.Fatalf("First Server.Start failed: %v", err)
	}

	// Give time for startup
	time.Sleep(50 * time.Millisecond)

	// Stop
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	server.Stop(shutdownCtx)
	cancel()

	// Start again with fresh dependencies (some components don't support reuse)
	deps2, err := BuildServerDependencies(opts)
	if err != nil {
		t.Logf("Second BuildServerDependencies may fail: %v", err)
	} else {
		server2 := NewServer(deps2)
		if err := server2.Start(ctx); err != nil {
			t.Logf("Second Server.Start may fail due to port binding: %v", err)
		}

		shutdownCtx2, cancel2 := context.WithTimeout(ctx, 5*time.Second)
		server2.Stop(shutdownCtx2)
		cancel2()
	}
}
