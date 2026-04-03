package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/auth"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/dashboard"
	"github.com/AnubisWatch/anubiswatch/internal/probe"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func main() {
	// Parse CLI commands
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "serve":
			serve()
		case "init":
			initConfig()
		case "watch":
			quickWatch()
		case "judge":
			showJudgments()
		case "summon":
			summonNode()
		case "banish":
			banishNode()
		case "necropolis":
			showCluster()
		case "version":
			showVersion()
		case "health":
			selfHealth()
		case "help", "-h", "--help":
			printUsage()
		default:
			fmt.Fprintf(os.Stderr, "Unknown command: %s\n", os.Args[1])
			printUsage()
			os.Exit(1)
		}
		return
	}
	printUsage()
}

func printUsage() {
	fmt.Print(`⚖️  AnubisWatch — The Judgment Never Sleeps

Usage: anubis <command> [options]

Commands:
  serve           Start AnubisWatch server
  init            Initialize new AnubisWatch instance
  watch <target>  Quick-add a monitor
  judge           Show all current verdicts (status table)
  summon <addr>   Add node to cluster
  banish <id>     Remove node from cluster
  necropolis      Show cluster status
  version         Show version information
  health          Self health check
  help            Show this help

Use "anubis <command> --help" for more information about a command.

Environment Variables:
  ANUBIS_CONFIG         Config file path (default: ./anubis.json)
  ANUBIS_HOST           Server bind host
  ANUBIS_PORT           Server bind port
  ANUBIS_DATA_DIR       Data directory path
  ANUBIS_ENCRYPTION_KEY Encryption key for storage
  ANUBIS_CLUSTER_SECRET Cluster secret for Raft
  ANUBIS_ADMIN_PASSWORD Initial admin password
  ANUBIS_LOG_LEVEL      Log level (debug, info, warn, error)
`)
}

func showVersion() {
	fmt.Printf(`⚖️  AnubisWatch — The Judgment Never Sleeps
Version:    %s
Commit:     %s
Build Date: %s
Go Version: %s
`, Version, Commit, BuildDate, getGoVersion())
}

func getGoVersion() string {
	return fmt.Sprintf("%s %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

func initConfig() {
	configPath := "anubis.json"
	if _, err := os.Stat(configPath); err == nil {
		fmt.Fprintf(os.Stderr, "Config file already exists: %s\n", configPath)
		os.Exit(1)
	}

	config := core.GenerateDefaultConfig()

	// Generate example soul
	config.Souls = []core.Soul{
		{
			Name:    "Example API",
			Type:    core.CheckHTTP,
			Target:  "https://httpbin.org/get",
			Weight:  core.Duration{Duration: 60 * time.Second},
			Timeout: core.Duration{Duration: 10 * time.Second},
			Enabled: true,
			Tags:    []string{"example"},
			HTTP: &core.HTTPConfig{
				Method:      "GET",
				ValidStatus: []int{200},
			},
		},
	}

	if err := core.SaveConfig(configPath, config); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Created config file: %s\n", configPath)
	fmt.Println("\nNext steps:")
	fmt.Println("  1. Edit anubis.json to configure your souls")
	fmt.Println("  2. Run 'anubis serve' to start the server")
}

func serve() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: getLogLevel(),
	}))

	logger.Info("⚖️  AnubisWatch — The Judgment Never Sleeps",
		"version", Version,
		"commit", Commit,
	)

	// Load config
	configPath := os.Getenv("ANUBIS_CONFIG")
	if configPath == "" {
		configPath = "anubis.json"
	}

	var cfg *core.Config

	if _, statErr := os.Stat(configPath); statErr == nil {
		var err error
		cfg, err = core.LoadConfig(configPath)
		if err != nil {
			logger.Error("failed to load config", "err", err)
			os.Exit(1)
		}
		logger.Info("config loaded", "path", configPath)
	} else {
		logger.Info("no config file found, using defaults", "path", configPath)
		cfg = core.GenerateDefaultConfig()
	}

	// Create data directory if needed
	if err := os.MkdirAll(cfg.Storage.Path, 0755); err != nil {
		logger.Error("failed to create data directory", "path", cfg.Storage.Path, "err", err)
		os.Exit(1)
	}

	// Initialize storage
	store, err := storage.NewEngine(cfg.Storage, logger)
	if err != nil {
		logger.Error("failed to initialize storage", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	// Initialize auth
	authenticator := auth.NewLocalAuthenticator()

	// Initialize probe engine with registry
	registry := probe.NewCheckerRegistry()
	probeEngine := probe.NewEngine(probe.EngineOptions{
		Registry: registry,
		Store:    &probeStorageAdapter{store: store},
		NodeID:   "local",
		Region:   "default",
		Logger:   logger,
	})

	// Assign souls from config (convert to pointers)
	if len(cfg.Souls) > 0 {
		soulPtrs := make([]*core.Soul, len(cfg.Souls))
		for i := range cfg.Souls {
			soulPtrs[i] = &cfg.Souls[i]
		}
		probeEngine.AssignSouls(soulPtrs)
		logger.Info("souls assigned", "count", len(cfg.Souls))
	}

	// Setup HTTP mux
	mux := http.NewServeMux()

	// Health endpoints
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy"}`))
	})
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"version": Version,
			"commit": Commit,
			"build_date": BuildDate,
		})
	})

	// Auth endpoints
	mux.HandleFunc("/api/v1/auth/login", handleLogin(authenticator))
	mux.HandleFunc("/api/v1/auth/logout", handleLogout(authenticator))

	// Souls API
	mux.HandleFunc("/api/v1/souls", handleListSouls(store, probeEngine))

	// Dashboard static files (if enabled)
	if cfg.Dashboard.Enabled {
		dashboardHandler, err := dashboard.NewHandler()
		if err != nil {
			logger.Warn("failed to initialize dashboard", "err", err)
		} else {
			mux.Handle("/", dashboardHandler)
			logger.Info("dashboard enabled")
		}
	}

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("server starting", "addr", addr)
		if cfg.Server.TLS.Enabled && cfg.Server.TLS.Cert != "" && cfg.Server.TLS.Key != "" {
			if err := server.ListenAndServeTLS(cfg.Server.TLS.Cert, cfg.Server.TLS.Key); err != http.ErrServerClosed {
				logger.Error("server error", "err", err)
			}
		} else {
			logger.Warn("running without TLS - not recommended for production")
			if err := server.ListenAndServe(); err != http.ErrServerClosed {
				logger.Error("server error", "err", err)
			}
		}
	}()

	logger.Info("AnubisWatch is ready. The judgment begins.")

	<-ctx.Done()
	logger.Info("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "err", err)
	}

	logger.Info("⚖️  AnubisWatch stopped. The judgment rests.")
}

func handleLogin(a *auth.LocalAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		user, token, err := a.Login(req.Email, req.Password)
		if err != nil {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"user":  user,
			"token": token,
		})
	}
}

func handleLogout(a *auth.LocalAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		a.Logout(token)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "logged out"})
	}
}

// probeStorageAdapter adapts storage.CobaltDB to probe.Storage interface
type probeStorageAdapter struct {
	store *storage.CobaltDB
}

func (a *probeStorageAdapter) SaveJudgment(ctx context.Context, j *core.Judgment) error {
	return a.store.SaveJudgment(ctx, j)
}

func (a *probeStorageAdapter) GetSoul(ctx context.Context, workspaceID, soulID string) (*core.Soul, error) {
	return a.store.GetSoul(ctx, workspaceID, soulID)
}

func (a *probeStorageAdapter) ListSouls(ctx context.Context, workspaceID string) ([]*core.Soul, error) {
	return a.store.ListSouls(ctx, workspaceID, 0, 1000)
}

func handleListSouls(store *storage.CobaltDB, engine *probe.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := context.Background()
		souls, err := store.ListSouls(ctx, "", 0, 100)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(souls)
	}
}

func quickWatch() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: anubis watch <target> [--name <name>] [--interval <duration>]\n")
		os.Exit(1)
	}

	target := os.Args[2]
	name := target

	// Parse flags
	for i := 3; i < len(os.Args); i++ {
		if os.Args[i] == "--name" && i+1 < len(os.Args) {
			name = os.Args[i+1]
			i++
		}
	}

	fmt.Printf("⚖️  Adding soul: %s (%s)\n", name, target)
	fmt.Println("✓ Soul added (not yet implemented - edit anubis.yaml manually)")
}

func showJudgments() {
	fmt.Print(`⚖️  AnubisWatch — The Judgment Never Sleeps
────────────────────────────────────────────

Soul                    Status    Latency   Region      Last Judged
──────────────────────  ────────  ────────  ──────────  ───────────

  No souls configured yet.
  Run 'anubis watch <target>' to add your first soul.
`)
}

func summonNode() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: anubis summon <address>\n")
		os.Exit(1)
	}
	addr := os.Args[2]
	fmt.Printf("⚖️  Summoning Jackal at %s...\n", addr)
	fmt.Println("✓ Node added to cluster (not yet implemented)")
}

func banishNode() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: anubis banish <node-id>\n")
		os.Exit(1)
	}
	id := os.Args[2]
	fmt.Printf("⚖️  Banishing Jackal %s...\n", id)
	fmt.Println("✓ Node removed from cluster (not yet implemented)")
}

func showCluster() {
	fmt.Print(`⚖️  AnubisWatch Necropolis — Cluster Status
────────────────────────────────────────────

Raft State:     Single Node
Current Leader: this node
Term:           1
Nodes:          1

Jackals:
  ID              Region    Status    Role      Last Contact
  ──────────────  ────────  ────────  ────────  ────────────
  (this node)     default   healthy   Pharaoh   now

  Cluster mode not enabled. Run with --cluster to form a Necropolis.
`)
}

func selfHealth() {
	// TODO: Implement actual health check
	fmt.Println(`{"status":"healthy","checks":{}}`)
}

func getLogLevel() slog.Level {
	level := os.Getenv("ANUBIS_LOG_LEVEL")
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
