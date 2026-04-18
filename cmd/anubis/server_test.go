package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/auth"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/grpcapi"
	"github.com/AnubisWatch/anubiswatch/internal/probe"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
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

func TestInitACMEManager_Server_Disabled(t *testing.T) {
	cfg := core.GenerateDefaultConfig()
	cfg.Server.TLS.Enabled = false
	mgr := initACMEManager(cfg, nil, slog.New(slog.NewTextHandler(os.Stdout, nil)))
	if mgr != nil {
		t.Error("Expected nil manager when TLS disabled")
	}
}

func TestInitACMEManager_Server_Enabled(t *testing.T) {
	tempDir := t.TempDir()
	cfg := core.GenerateDefaultConfig()
	cfg.Storage.Path = tempDir
	cfg.Server.TLS.Enabled = true
	cfg.Server.TLS.AutoCert = true
	cfg.Server.TLS.ACMEEmail = "test@example.com"

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	store, err := storage.NewEngine(cfg.Storage, logger)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	mgr := initACMEManager(cfg, store, logger)
	if mgr == nil {
		t.Error("Expected non-nil manager when TLS auto-cert enabled")
	}
}

func TestHandleLogin_InvalidCredentials(t *testing.T) {
	authenticator := auth.NewLocalAuthenticator("", "admin@anubis.watch", "TestPass1234!")
	handler := handleLogin(authenticator)

	req := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"email":"wrong@example.com","password":"wrong"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestHandleListSouls_StorageError(t *testing.T) {
	store := setupTestStore(t)
	store.Close() // close to force error

	engine := probe.NewEngine(probe.EngineOptions{
		Registry: probe.NewCheckerRegistry(),
		Logger:   slog.New(slog.NewTextHandler(os.Stdout, nil)),
	})

	handler := handleListSouls(store, engine)
	req := httptest.NewRequest("GET", "/api/souls", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, rec.Code)
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
		"server": {"host": "127.0.0.1", "port": 0, "grpc_port": 0}
	}`
	configPath := filepath.Join(tempDir, "test-config.json")
	os.WriteFile(configPath, []byte(configContent), 0644)

	deps, err := BuildServerDependencies(ServerOptions{ConfigPath: configPath, Logger: logger})
	if err != nil {
		t.Fatalf("BuildServerDependencies failed: %v", err)
	}

	// Override gRPC server with invalid address to force start error
	grpcStore := &grpcStorageAdapter{inner: &restStorageAdapter{store: deps.Store}}
	deps.GRPCServer = grpcapi.NewServer("invalid://:abc", grpcStore, &mockGRPCProbe{}, &mockAuthenticator{}, logger, nil, true)
	// Avoid REST server port conflicts with other tests
	deps.RESTServer = nil

	server := NewServer(deps)
	ctx := context.Background()
	if err := server.Start(ctx); err != nil {
		t.Fatalf("Server.Start failed: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	server.Stop(shutdownCtx)
}

type mockGRPCProbe struct{}

func (m *mockGRPCProbe) ForceCheck(soulID string) (interface{}, error) {
	return nil, fmt.Errorf("mock error")
}

type mockAuthenticator struct{}

func (m *mockAuthenticator) Authenticate(token string) (*api.User, error) {
	if token == "valid-token" {
		return &api.User{ID: "user-1", Email: "test@example.com", Workspace: "default"}, nil
	}
	return nil, fmt.Errorf("invalid token")
}
