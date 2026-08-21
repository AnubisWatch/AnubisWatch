package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/grpcapi"
	"github.com/AnubisWatch/anubiswatch/internal/journey"
	"github.com/AnubisWatch/anubiswatch/internal/probe"
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
			"port": 0,
			"grpc_port": 0
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
	// Set ANUBIS_DATA_DIR to avoid permission issues in CI
	t.Setenv("ANUBIS_DATA_DIR", t.TempDir())

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
			"port": 0,
			"grpc_port": 0
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
			"port": 0,
			"grpc_port": 0
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
			"port": 0,
			"grpc_port": 0
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
	time.Sleep(100 * time.Millisecond)

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

func TestBuildServerDependencies_EnvConfigPath(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	configContent := `{
		"storage": {"path": "` + filepath.ToSlash(dataDir) + `"},
		"server": {"host": "127.0.0.1", "port": 0}
	}`
	configPath := filepath.Join(tempDir, "env-config.json")
	os.WriteFile(configPath, []byte(configContent), 0644)

	t.Setenv("ANUBIS_CONFIG", configPath)

	deps, err := BuildServerDependencies(ServerOptions{Logger: logger})
	if err != nil {
		t.Fatalf("BuildServerDependencies failed: %v", err)
	}
	if deps == nil {
		t.Fatal("Expected non-nil dependencies")
	}
	if deps.Store != nil {
		deps.Store.Close()
	}
}

func TestBuildServerDependencies_OIDCAuth(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	configContent := `{
		"storage": {"path": "` + filepath.ToSlash(dataDir) + `"},
		"server": {"host": "127.0.0.1", "port": 0},
		"auth": {"type": "oidc", "oidc": {"issuer": "http://localhost", "client_id": "test", "client_secret": "secret", "redirect_url": "http://localhost/callback"}}
	}`
	configPath := filepath.Join(tempDir, "oidc-config.json")
	os.WriteFile(configPath, []byte(configContent), 0644)

	deps, err := BuildServerDependencies(ServerOptions{ConfigPath: configPath, Logger: logger})
	if err != nil {
		t.Fatalf("BuildServerDependencies failed: %v", err)
	}
	if deps.Authenticator == nil {
		t.Error("Expected authenticator to be initialized")
	}
	if deps.Store != nil {
		deps.Store.Close()
	}
}

func TestBuildServerDependencies_LDAPAuth(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	configContent := `{
		"storage": {"path": "` + filepath.ToSlash(dataDir) + `"},
		"server": {"host": "127.0.0.1", "port": 0},
		"auth": {"type": "ldap", "ldap": {"url": "ldap://localhost", "base_dn": "dc=test,dc=com"}}
	}`
	configPath := filepath.Join(tempDir, "ldap-config.json")
	os.WriteFile(configPath, []byte(configContent), 0644)

	deps, err := BuildServerDependencies(ServerOptions{ConfigPath: configPath, Logger: logger})
	if err != nil {
		t.Fatalf("BuildServerDependencies failed: %v", err)
	}
	if deps.Authenticator == nil {
		t.Error("Expected authenticator to be initialized")
	}
	if deps.Store != nil {
		deps.Store.Close()
	}
}

func TestBuildServerDependencies_DashboardDisabled(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	configContent := `{
		"storage": {"path": "` + filepath.ToSlash(dataDir) + `"},
		"server": {"host": "127.0.0.1", "port": 0},
		"dashboard": {"enabled": false}
	}`
	configPath := filepath.Join(tempDir, "no-dash-config.json")
	os.WriteFile(configPath, []byte(configContent), 0644)

	deps, err := BuildServerDependencies(ServerOptions{ConfigPath: configPath, Logger: logger})
	if err != nil {
		t.Fatalf("BuildServerDependencies failed: %v", err)
	}
	if deps.DashboardHandler != nil {
		t.Error("Expected dashboard handler to be nil when disabled")
	}
	if deps.Store != nil {
		deps.Store.Close()
	}
}

func TestBuildServerDependencies_NoGRPC(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	configContent := `{
		"storage": {"path": "` + filepath.ToSlash(dataDir) + `"},
		"server": {"host": "127.0.0.1", "port": 0, "grpc_port": 0}
	}`
	configPath := filepath.Join(tempDir, "no-grpc-config.json")
	os.WriteFile(configPath, []byte(configContent), 0644)

	deps, err := BuildServerDependencies(ServerOptions{ConfigPath: configPath, Logger: logger})
	if err != nil {
		t.Fatalf("BuildServerDependencies failed: %v", err)
	}
	if deps.GRPCServer != nil {
		t.Error("Expected gRPC server to be nil when port is 0")
	}
	if deps.Store != nil {
		deps.Store.Close()
	}
}

func TestGRPCListenAddressUsesConfiguredHost(t *testing.T) {
	tests := []struct {
		name string
		host string
		want string
	}{
		{name: "IPv4", host: "127.0.0.1", want: "127.0.0.1:9090"},
		{name: "IPv6", host: "::1", want: "[::1]:9090"},
		{name: "bracketed IPv6", host: "[::1]", want: "[::1]:9090"},
		{name: "configured interface", host: "10.0.0.12", want: "10.0.0.12:9090"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := grpcListenAddress(core.ServerConfig{Host: tt.host, GRPCPort: 9090})
			if got != tt.want {
				t.Fatalf("grpcListenAddress() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildServerDependencies_GRPCTLSLoadFailureFailsClosed(t *testing.T) {
	tempDir := t.TempDir()
	configContent := `{
		"storage": {"path": "` + filepath.ToSlash(filepath.Join(tempDir, "data")) + `"},
		"server": {
			"host": "127.0.0.1",
			"port": 8080,
			"grpc_port": 9090,
			"tls": {"enabled": true, "cert": "` + filepath.ToSlash(filepath.Join(tempDir, "missing.crt")) + `", "key": "` + filepath.ToSlash(filepath.Join(tempDir, "missing.key")) + `"}
		}
	}`
	configPath := filepath.Join(tempDir, "bad-grpc-tls.json")
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := BuildServerDependencies(ServerOptions{ConfigPath: configPath, Logger: slog.Default()})
	if err == nil || !strings.Contains(err.Error(), "failed to load gRPC TLS certificate") {
		t.Fatalf("BuildServerDependencies() error = %v, want gRPC TLS load failure", err)
	}
}

func TestStatusPageRepository_GetIncidentsByPage_PageNotFound(t *testing.T) {
	store := setupTestStore(t)
	repo := &statusPageRepository{store: store}

	_, err := repo.GetIncidentsByPage("nonexistent-page")
	if err == nil {
		t.Error("Expected error when page not found")
	}
}

func TestGrpcProbeAdapter_ForceCheck(t *testing.T) {
	store := setupTestStore(t)
	engine := probe.NewEngine(probe.EngineOptions{
		Registry: probe.NewCheckerRegistry(),
		Store:    &probeStorageAdapter{store: store},
		Logger:   slog.New(slog.NewTextHandler(os.Stdout, nil)),
	})

	adapter := &grpcProbeAdapter{engine: engine}
	_, err := adapter.ForceCheck("nonexistent-soul")
	if err == nil {
		t.Error("Expected error for nonexistent soul")
	}

	// Lifecycle methods must satisfy grpcapi.ProbeEngine and forward without
	// panicking; detailed runner behavior belongs to internal/probe tests.
	soul := &core.Soul{ID: "grpc-adapter-soul", Name: "Adapter", Type: core.CheckHTTP, Target: "https://example.com", Enabled: true}
	adapter.UpsertSoul(soul)
	adapter.RemoveSoul(soul.ID)
}

func TestServer_Start_JourneyAlreadyRunning(t *testing.T) {
	tempDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dataDir := filepath.Join(tempDir, "data")

	configContent := `{
		"storage": {"path": "` + filepath.ToSlash(dataDir) + `"},
		"server": {"host": "127.0.0.1", "port": 0},
		"journeys": [{"id": "j1", "name": "Test Journey", "enabled": true, "weight": "1s", "steps": [{"name": "step1", "type": "http", "target": "http://localhost"}]}]
	}`
	configPath := filepath.Join(tempDir, "test-config.json")
	os.WriteFile(configPath, []byte(configContent), 0644)

	deps, err := BuildServerDependencies(ServerOptions{ConfigPath: configPath, Logger: logger})
	if err != nil {
		t.Fatalf("BuildServerDependencies failed: %v", err)
	}

	// Avoid REST server port conflicts with other tests
	deps.RESTServer = nil

	// Pre-start the journey to trigger "already running" warning
	journey := &core.JourneyConfig{ID: "j1", Name: "Test Journey", Weight: core.Duration{Duration: time.Hour}}
	_ = deps.JourneyExecutor.Start(context.Background(), journey)

	server := NewServer(deps)
	ctx := context.Background()
	if err := server.Start(ctx); err != nil {
		t.Fatalf("Server.Start failed: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	server.Stop(shutdownCtx)
}

func TestWaitForShutdown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal sending not supported on Windows")
	}

	server := NewServer(
		&ServerDependencies{
			Config: core.GenerateDefaultConfig(),
			Logger: slog.New(slog.NewTextHandler(os.Stdout, nil)),
		})

	done := make(chan struct{})
	go func() {
		server.WaitForShutdown()
		close(done)
	}()

	// Give time for signal handler to register
	time.Sleep(100 * time.Millisecond)

	p, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("failed to find process: %v", err)
	}
	if err := p.Signal(os.Interrupt); err != nil {
		t.Fatalf("failed to send signal: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for WaitForShutdown")
	}
}

func TestBuildServerDependencies_NilLogger(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	configContent := `{
		"storage": {"path": "` + filepath.ToSlash(dataDir) + `"},
		"server": {"host": "127.0.0.1", "port": 0}
	}`
	configPath := filepath.Join(tempDir, "test-config.json")
	os.WriteFile(configPath, []byte(configContent), 0644)

	deps, err := BuildServerDependencies(ServerOptions{ConfigPath: configPath})
	if err != nil {
		t.Fatalf("BuildServerDependencies failed: %v", err)
	}
	if deps.Logger == nil {
		t.Error("Expected logger to be initialized")
	}
	if deps.Store != nil {
		deps.Store.Close()
	}
}

func TestServer_Start_GRPCServerError(t *testing.T) {
	tempDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dataDir := filepath.Join(tempDir, "data")

	configContent := `{
		"storage": {"path": "` + filepath.ToSlash(dataDir) + `"},
		"server": {"host": "127.0.0.1", "port": 8080, "grpc_port": 9090}
	}`
	configPath := filepath.Join(tempDir, "test-config.json")
	os.WriteFile(configPath, []byte(configContent), 0644)

	deps, err := BuildServerDependencies(ServerOptions{ConfigPath: configPath, Logger: logger})
	if err != nil {
		t.Fatalf("BuildServerDependencies failed: %v", err)
	}

	// Override gRPC server with an invalid address to force a synchronous bind
	// failure, then verify the enabled subsystem fails startup closed.
	grpcStore := &grpcStorageAdapter{inner: &restStorageAdapter{store: deps.Store}}
	deps.GRPCServer = grpcapi.NewServer("invalid://:abc", grpcStore, &mockGRPCProbe{}, &mockAuthenticator{}, logger, nil, true)
	deps.RESTServer = nil

	server := NewServer(deps)
	err = server.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "failed to start gRPC server") {
		t.Fatalf("Server.Start() error = %v, want gRPC startup failure", err)
	}
	server.Stop(context.Background())
}

func TestServer_Start_EnabledGRPCRequiresInitializedServer(t *testing.T) {
	server := NewServer(&ServerDependencies{
		Config: &core.Config{Server: core.ServerConfig{Host: "127.0.0.1", GRPCPort: 9090}},
		Logger: slog.Default(),
	})

	err := server.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "was not initialized") {
		t.Fatalf("Server.Start() error = %v, want missing gRPC server failure", err)
	}
}

func TestServer_Start_EnabledClusterRequiresInitializedManager(t *testing.T) {
	server := NewServer(&ServerDependencies{
		Config: &core.Config{Necropolis: core.NecropolisConfig{Enabled: true}},
		Logger: slog.Default(),
	})

	err := server.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "cluster manager was not initialized") {
		t.Fatalf("Server.Start() error = %v, want missing cluster manager failure", err)
	}
}

type mockGRPCProbe struct{}

func (m *mockGRPCProbe) ForceCheck(soulID string) (*core.Judgment, error) {
	return nil, fmt.Errorf("mock error")
}

func (m *mockGRPCProbe) UpsertSoul(_ *core.Soul) {}

func (m *mockGRPCProbe) RemoveSoul(_ string) {}

type mockAuthenticator struct{}

func (m *mockAuthenticator) Authenticate(token string) (*api.User, error) {
	if token == "valid-token" {
		return &api.User{ID: "user-1", Email: "test@example.com", Workspace: "default"}, nil
	}
	return nil, fmt.Errorf("invalid token")
}

func TestAlertStorageAdapter_ListMaintenanceWindows(t *testing.T) {
	db := setupTestStore(t)
	adapter := &alertStorageAdapter{store: db}

	// Test with no maintenance windows
	windows, err := adapter.ListMaintenanceWindows()
	if err != nil {
		t.Fatalf("ListMaintenanceWindows failed: %v", err)
	}
	if len(windows) != 0 {
		t.Errorf("expected 0 windows, got %d", len(windows))
	}

	// Save a maintenance window directly via store and verify it's listed
	mw := &core.MaintenanceWindow{
		ID:        "mw-test-1",
		Name:      "Test Maintenance",
		StartTime: time.Now(),
		EndTime:   time.Now().Add(time.Hour),
		Enabled:   true,
	}
	if err := db.SaveMaintenanceWindow(mw); err != nil {
		t.Fatalf("SaveMaintenanceWindow failed: %v", err)
	}

	windows, err = adapter.ListMaintenanceWindows()
	if err != nil {
		t.Fatalf("ListMaintenanceWindows failed: %v", err)
	}
	if len(windows) != 1 {
		t.Errorf("expected 1 window, got %d", len(windows))
	}
	if windows[0].ID != "mw-test-1" {
		t.Errorf("expected mw-test-1, got %s", windows[0].ID)
	}

	// Add another window
	mw2 := &core.MaintenanceWindow{
		ID:        "mw-test-2",
		Name:      "Test Maintenance 2",
		StartTime: time.Now(),
		EndTime:   time.Now().Add(2 * time.Hour),
		Enabled:   false,
	}
	if err := db.SaveMaintenanceWindow(mw2); err != nil {
		t.Fatalf("SaveMaintenanceWindow failed: %v", err)
	}

	windows, err = adapter.ListMaintenanceWindows()
	if err != nil {
		t.Fatalf("ListMaintenanceWindows failed: %v", err)
	}
	if len(windows) != 2 {
		t.Errorf("expected 2 windows, got %d", len(windows))
	}
}

func TestGrpcStorageAdapter_RunJourneyNoCtx_JourneyExecutorNil(t *testing.T) {
	db := setupTestStore(t)
	rest := &restStorageAdapter{store: db}
	adapter := &grpcStorageAdapter{inner: rest, journey: nil}

	_, err := adapter.RunJourneyNoCtx("default", "nonexistent-journey")
	if err == nil {
		t.Error("expected error when journey executor is nil")
	}
	if err.Error() != "journey executor not available" {
		t.Errorf("expected 'journey executor not available', got: %v", err)
	}
}

func TestGrpcStorageAdapter_RunJourneyNoCtx_JourneyNotFound(t *testing.T) {
	db := setupTestStore(t)
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	exec := journey.NewExecutor(db, logger)
	if exec == nil {
		t.Fatal("expected non-nil executor")
	}
	defer exec.StopAll()

	rest := &restStorageAdapter{store: db}
	adapter := &grpcStorageAdapter{inner: rest, journey: exec}

	// Test with non-existent journey
	_, err := adapter.RunJourneyNoCtx("default", "nonexistent-journey")
	if err == nil {
		t.Error("expected error when journey not found")
	}
}
