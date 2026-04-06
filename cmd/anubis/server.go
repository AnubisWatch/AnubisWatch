package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/AnubisWatch/anubiswatch/internal/alert"
	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/auth"
	"github.com/AnubisWatch/anubiswatch/internal/cluster"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/dashboard"
	"github.com/AnubisWatch/anubiswatch/internal/journey"
	"github.com/AnubisWatch/anubiswatch/internal/probe"
	"github.com/AnubisWatch/anubiswatch/internal/statuspage"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

// ServerDependencies holds all dependencies for the server
type ServerDependencies struct {
	Config           *core.Config
	Logger           *slog.Logger
	Store            *storage.CobaltDB
	Authenticator    *auth.LocalAuthenticator
	AlertManager     *alert.Manager
	ProbeEngine      *probe.Engine
	JourneyExecutor  *journey.Executor
	ClusterManager   *cluster.Manager
	RESTServer       *api.RESTServer
	StatusPageRepo   *statusPageRepository
	ACMEManager      interface{}
	DashboardHandler http.Handler
	StatusPageHandler http.Handler
	MCPServer        *api.MCPServer
}

// Server represents the AnubisWatch server
type Server struct {
	deps *ServerDependencies
	logger *slog.Logger
}

// NewServer creates a new Server instance
func NewServer(deps *ServerDependencies) *Server {
	return &Server{
		deps:   deps,
		logger: deps.Logger,
	}
}

// Start initializes and starts all server components
func (s *Server) Start(ctx context.Context) error {
	cfg := s.deps.Config
	logger := s.logger

	// Start alert manager
	if s.deps.AlertManager != nil {
		if err := s.deps.AlertManager.Start(); err != nil {
			logger.Warn("failed to start alert manager", "err", err)
		} else {
			logger.Info("alert manager started")
		}
	}

	// Assign souls from config
	if len(cfg.Souls) > 0 {
		soulPtrs := make([]*core.Soul, len(cfg.Souls))
		for i := range cfg.Souls {
			soulPtrs[i] = &cfg.Souls[i]
		}
		s.deps.ProbeEngine.AssignSouls(soulPtrs)
		logger.Info("souls assigned", "count", len(cfg.Souls))
	}

	// Start journey executors
	for _, j := range cfg.Journeys {
		if j.Enabled {
			j.WorkspaceID = "default"
			if err := s.deps.Store.SaveJourney(ctx, &j); err != nil {
				logger.Warn("failed to save journey", "journey", j.Name, "err", err)
			}
			if err := s.deps.JourneyExecutor.Start(ctx, &j); err != nil {
				logger.Warn("failed to start journey", "journey", j.Name, "err", err)
			}
		}
	}

	// Start cluster manager
	if s.deps.ClusterManager != nil {
		if err := s.deps.ClusterManager.Start(ctx); err != nil {
			logger.Warn("failed to start cluster manager", "err", err)
		} else {
			logger.Info("cluster manager initialized", "clustered", s.deps.ClusterManager.IsClustered())
		}
	}

	// Start REST server
	if s.deps.RESTServer != nil {
		go func() {
			if err := s.deps.RESTServer.Start(); err != nil {
				logger.Error("REST server failed", "err", err)
			}
		}()
		logger.Info("REST API server initialized", "addr", fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port))
	}

	logger.Info("AnubisWatch is ready. The judgment begins.")
	return nil
}

// Stop gracefully shuts down the server
func (s *Server) Stop(ctx context.Context) error {
	logger := s.logger

	logger.Info("shutting down...")

	// Stop REST server
	if s.deps.RESTServer != nil {
		s.deps.RESTServer.Stop(ctx)
	}

	// Stop journey executors
	if s.deps.JourneyExecutor != nil {
		s.deps.JourneyExecutor.StopAll()
	}

	// Stop alert manager
	if s.deps.AlertManager != nil {
		s.deps.AlertManager.Stop()
	}

	// Stop cluster manager
	if s.deps.ClusterManager != nil {
		s.deps.ClusterManager.Stop(ctx)
	}

	// Stop probe engine
	if s.deps.ProbeEngine != nil {
		s.deps.ProbeEngine.Stop()
	}

	// Shutdown authenticator
	if s.deps.Authenticator != nil {
		s.deps.Authenticator.Shutdown()
	}

	// Close storage
	if s.deps.Store != nil {
		s.deps.Store.Close()
	}

	logger.Info("⚖️  AnubisWatch stopped. The judgment rests.")
	return nil
}

// WaitForShutdown blocks until shutdown signal is received
func (s *Server) WaitForShutdown() {
	shutdownCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-shutdownCtx.Done()
}

// ServerOptions holds options for building server dependencies
type ServerOptions struct {
	ConfigPath string
	Logger     *slog.Logger
}

// BuildServerDependencies builds all server dependencies
func BuildServerDependencies(opts ServerOptions) (*ServerDependencies, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}

	// Load config
	configPath := opts.ConfigPath
	if configPath == "" {
		configPath = os.Getenv("ANUBIS_CONFIG")
		if configPath == "" {
			configPath = "anubis.json"
		}
	}

	var cfg *core.Config
	if _, statErr := os.Stat(configPath); statErr == nil {
		var err error
		cfg, err = core.LoadConfig(configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load config: %w", err)
		}
		logger.Info("config loaded", "path", configPath)
	} else {
		logger.Info("no config file found, using defaults", "path", configPath)
		cfg = core.GenerateDefaultConfig()
	}

	// Create data directory
	if err := os.MkdirAll(cfg.Storage.Path, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	// Initialize storage
	store, err := storage.NewEngine(cfg.Storage, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize storage: %w", err)
	}

	// Initialize auth
	sessionPath := filepath.Join(cfg.Storage.Path, "sessions.json")
	authenticator := auth.NewLocalAuthenticator(sessionPath)

	// Initialize alert manager
	alertStorage := &alertStorageAdapter{store: store}
	alertMgr := alert.NewManager(alertStorage, logger)

	// Initialize probe engine
	registry := probe.NewCheckerRegistry()
	probeEngine := probe.NewEngine(probe.EngineOptions{
		Registry: registry,
		Store:    &probeStorageAdapter{store: store},
		Alerter:  alertMgr,
		NodeID:   cfg.Necropolis.NodeName,
		Region:   cfg.Necropolis.Region,
		Logger:   logger,
	})

	// Initialize journey executor
	journeyExec := journey.NewExecutor(store, logger)

	// Initialize cluster manager
	clusterMgr, err := cluster.NewManager(cfg.Necropolis.Raft, store, logger)
	if err != nil {
		logger.Warn("failed to initialize cluster manager", "err", err)
		clusterMgr = nil
	}

	// Initialize REST server dependencies
	restStore := &restStorageAdapter{store: store}
	clusterAdapt := &clusterAdapter{mgr: clusterMgr}

	// Initialize dashboard handler
	var dashboardHandler http.Handler
	if cfg.Dashboard.Enabled {
		dh, err := dashboard.NewHandler()
		if err != nil {
			logger.Warn("failed to initialize dashboard", "err", err)
		} else {
			dashboardHandler = dh
			logger.Info("dashboard handler initialized")
		}
	}

	// Initialize status page handler
	statusPageRepo := &statusPageRepository{store: store}
	acmeMgr := initACMEManager(cfg, store, logger)
	statusPageHandler := statuspage.NewHandler(statusPageRepo, acmeMgr)

	// Initialize MCP server
	mcpServer := api.NewMCPServer(restStore, probeEngine, alertMgr, logger)

	restServer := api.NewRESTServer(cfg.Server, restStore, probeEngine, alertMgr, authenticator, clusterAdapt, dashboardHandler, statusPageHandler, mcpServer, logger)

	return &ServerDependencies{
		Config:            cfg,
		Logger:            logger,
		Store:             store,
		Authenticator:     authenticator,
		AlertManager:      alertMgr,
		ProbeEngine:       probeEngine,
		JourneyExecutor:   journeyExec,
		ClusterManager:    clusterMgr,
		RESTServer:        restServer,
		StatusPageRepo:    statusPageRepo,
		ACMEManager:       acmeMgr,
		DashboardHandler:  dashboardHandler,
		StatusPageHandler: statusPageHandler,
		MCPServer:         mcpServer,
	}, nil
}
