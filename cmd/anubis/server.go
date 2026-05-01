package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"syscall"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/acme"
	"github.com/AnubisWatch/anubiswatch/internal/alert"
	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/auth"
	"github.com/AnubisWatch/anubiswatch/internal/cluster"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/dashboard"
	"github.com/AnubisWatch/anubiswatch/internal/grpcapi"
	"github.com/AnubisWatch/anubiswatch/internal/journey"
	"github.com/AnubisWatch/anubiswatch/internal/probe"
	"github.com/AnubisWatch/anubiswatch/internal/statuspage"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

const (
	// maxRequestBodySize limits JSON request body size to prevent DoS (1MB)
	maxRequestBodySize = 1 << 20 // 1MB
)

// ServerDependencies holds all dependencies for the server
type ServerDependencies struct {
	Config            *core.Config
	Logger            *slog.Logger
	Store             *storage.CobaltDB
	Authenticator     api.Authenticator
	AlertManager      *alert.Manager
	ProbeEngine       *probe.Engine
	JourneyExecutor   *journey.Executor
	ClusterManager    *cluster.Manager
	RESTServer        *api.RESTServer
	GRPCServer        *grpcapi.Server
	StatusPageRepo    *statusPageRepository
	ACMEManager       interface{}
	DashboardHandler  http.Handler
	StatusPageHandler http.Handler
	MCPServer         *api.MCPServer
}

// Server represents the AnubisWatch server
type Server struct {
	deps   *ServerDependencies
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

	// Assign configured souls plus API-created souls persisted in storage.
	if s.deps.ProbeEngine != nil {
		soulPtrs := make([]*core.Soul, 0, len(cfg.Souls))
		seen := make(map[string]struct{}, len(cfg.Souls))
		for i := range cfg.Souls {
			soul := &cfg.Souls[i]
			soulPtrs = append(soulPtrs, soul)
			if soul.ID != "" {
				seen[soul.ID] = struct{}{}
			}
		}

		storedSouls, err := s.deps.Store.ListSouls(ctx, "default", 0, 1000)
		if err != nil {
			logger.Warn("failed to load stored souls for probe assignment", "err", err)
		}
		for _, soul := range storedSouls {
			if soul == nil {
				continue
			}
			if _, exists := seen[soul.ID]; exists && soul.ID != "" {
				continue
			}
			soulPtrs = append(soulPtrs, soul)
			if soul.ID != "" {
				seen[soul.ID] = struct{}{}
			}
		}

		if len(soulPtrs) > 0 {
			s.deps.ProbeEngine.AssignSouls(soulPtrs)
			logger.Info("souls assigned", "count", len(soulPtrs))
		}
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

	// Start gRPC server
	if s.deps.GRPCServer != nil {
		if err := s.deps.GRPCServer.Start(); err != nil {
			logger.Warn("failed to start gRPC server", "err", err)
		} else {
			logger.Info("gRPC API server initialized", "addr", cfg.Server.GRPCPort)
		}
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

	// Stop gRPC server
	if s.deps.GRPCServer != nil {
		s.deps.GRPCServer.Stop()
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

// grpcProbeAdapter adapts probe.Engine to grpcapi.ProbeEngine
type grpcProbeAdapter struct {
	engine *probe.Engine
}

func (a *grpcProbeAdapter) ForceCheck(soulID string) (interface{}, error) {
	return a.engine.ForceCheck(soulID)
}

// grpcStorageAdapter wraps restStorageAdapter to return interface{} for gRPC compatibility
type grpcStorageAdapter struct {
	inner *restStorageAdapter
}

func (a *grpcStorageAdapter) GetSoulNoCtx(id string) (interface{}, error) {
	return a.inner.GetSoulNoCtx(id)
}
func (a *grpcStorageAdapter) ListSoulsNoCtx(ws string, o, l int) ([]interface{}, error) {
	souls, err := a.inner.ListSoulsNoCtx(ws, o, l)
	if err != nil {
		return nil, err
	}
	result := make([]interface{}, len(souls))
	for i, s := range souls {
		result[i] = s
	}
	return result, nil
}
func (a *grpcStorageAdapter) SaveSoulNoCtx(s interface{}) error {
	soul, ok := s.(*core.Soul)
	if !ok {
		return fmt.Errorf("invalid soul type: %T", s)
	}
	return a.inner.SaveSoul(context.Background(), soul)
}
func (a *grpcStorageAdapter) DeleteSoulNoCtx(id string) error { return a.inner.DeleteSoulNoCtx(id) }
func (a *grpcStorageAdapter) ListJudgmentsNoCtx(soulID string, start, end time.Time, limit int) ([]interface{}, error) {
	judgments, err := a.inner.ListJudgmentsNoCtx(soulID, start, end, limit)
	if err != nil {
		return nil, err
	}
	result := make([]interface{}, len(judgments))
	for i, j := range judgments {
		result[i] = j
	}
	return result, nil
}
func (a *grpcStorageAdapter) GetChannelNoCtx(id string, ws string) (interface{}, error) {
	return a.inner.GetChannelNoCtx(id, ws)
}
func (a *grpcStorageAdapter) ListChannelsNoCtx(ws string) ([]interface{}, error) {
	channels, err := a.inner.ListChannelsNoCtx(ws)
	if err != nil {
		return nil, err
	}
	result := make([]interface{}, len(channels))
	for i, c := range channels {
		result[i] = c
	}
	return result, nil
}
func (a *grpcStorageAdapter) SaveChannelNoCtx(ch interface{}) error {
	channel, ok := ch.(*core.AlertChannel)
	if !ok {
		return fmt.Errorf("invalid channel type: %T", ch)
	}
	return a.inner.SaveChannelNoCtx(channel)
}
func (a *grpcStorageAdapter) DeleteChannelNoCtx(id string, ws string) error {
	return a.inner.DeleteChannelNoCtx(id, ws)
}
func (a *grpcStorageAdapter) GetRuleNoCtx(id string, ws string) (interface{}, error) {
	return a.inner.GetRuleNoCtx(id, ws)
}
func (a *grpcStorageAdapter) ListRulesNoCtx(ws string) ([]interface{}, error) {
	rules, err := a.inner.ListRulesNoCtx(ws)
	if err != nil {
		return nil, err
	}
	result := make([]interface{}, len(rules))
	for i, r := range rules {
		result[i] = r
	}
	return result, nil
}
func (a *grpcStorageAdapter) SaveRuleNoCtx(rule interface{}) error {
	r, ok := rule.(*core.AlertRule)
	if !ok {
		return fmt.Errorf("invalid rule type: %T", rule)
	}
	return a.inner.SaveRuleNoCtx(r)
}
func (a *grpcStorageAdapter) DeleteRuleNoCtx(id string, ws string) error {
	return a.inner.DeleteRuleNoCtx(id, ws)
}
func (a *grpcStorageAdapter) GetJourneyNoCtx(id string) (interface{}, error) {
	return a.inner.GetJourneyNoCtx(id)
}
func (a *grpcStorageAdapter) ListJourneysNoCtx(ws string, o, l int) ([]interface{}, error) {
	journeys, err := a.inner.ListJourneysNoCtx(ws, o, l)
	if err != nil {
		return nil, err
	}
	result := make([]interface{}, len(journeys))
	for i, j := range journeys {
		result[i] = j
	}
	return result, nil
}
func (a *grpcStorageAdapter) SaveJourneyNoCtx(j interface{}) error {
	journey, ok := j.(*core.JourneyConfig)
	if !ok {
		return fmt.Errorf("invalid journey type: %T", j)
	}
	return a.inner.SaveJourneyNoCtx(journey)
}
func (a *grpcStorageAdapter) DeleteJourneyNoCtx(id string) error {
	return a.inner.DeleteJourneyNoCtx(id)
}
func (a *grpcStorageAdapter) ListEvents(soulID string, limit int) ([]interface{}, error) {
	events, err := a.inner.store.ListAlertEvents(soulID, limit)
	if err != nil {
		return nil, err
	}
	result := make([]interface{}, len(events))
	for i, e := range events {
		result[i] = e
	}
	return result, nil
}
func (a *grpcStorageAdapter) ListJourneyRunsNoCtx(journeyID string, limit int) ([]interface{}, error) {
	runs, err := a.inner.store.QueryJourneyRuns(context.Background(), "default", journeyID, limit)
	if err != nil {
		return nil, err
	}
	result := make([]interface{}, len(runs))
	for i, r := range runs {
		result[i] = r
	}
	return result, nil
}
func (a *grpcStorageAdapter) GetJourneyRunNoCtx(journeyID, runID string) (interface{}, error) {
	return a.inner.store.GetJourneyRun(context.Background(), "default", journeyID, runID)
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
		// Check environment variable for data dir override (for CI/tests)
		if envDir := os.Getenv("ANUBIS_DATA_DIR"); envDir != "" {
			cfg.Storage.Path = envDir
		}
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
	adminEmail := "admin@anubis.watch"
	if cfg.Auth.Local.AdminEmail != "" {
		adminEmail = cfg.Auth.Local.AdminEmail
	}

	// CRITICAL: Never use a hardcoded default password. If no password is configured,
	// generate a random one and log it so the operator must explicitly set it.
	adminPassword := cfg.Auth.Local.AdminPassword
	if adminPassword == "" {
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			return nil, fmt.Errorf("failed to generate admin password: %w", err)
		}
		adminPassword = base64.RawURLEncoding.EncodeToString(b)
		logger.Warn("no admin password configured — random password generated",
			"action", "set auth.local.admin_password in config",
			"password", adminPassword)
	}

	var authenticator api.Authenticator
	switch cfg.Auth.Type {
	case "oidc":
		authenticator = auth.NewOIDCAuthenticator(cfg.Auth.OIDC, sessionPath, adminEmail, adminPassword)
		logger.Info("OIDC authentication initialized", "issuer", cfg.Auth.OIDC.Issuer)
	case "ldap":
		authenticator = auth.NewLDAPAuthenticator(cfg.Auth.LDAP, sessionPath, adminEmail, adminPassword)
		logger.Info("LDAP authentication initialized", "url", cfg.Auth.LDAP.URL)
	default:
		authenticator = auth.NewLocalAuthenticator(sessionPath, adminEmail, adminPassword)
	}

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
	clusterMgr, err := cluster.NewManager(cfg.Necropolis, store, logger)
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

	restServer := api.NewRESTServer(cfg.Server, cfg.Auth, restStore, probeEngine, alertMgr, authenticator, clusterAdapt, journeyExec, dashboardHandler, statusPageHandler, mcpServer, logger)

	// Wire up WebSocket broadcast for real-time judgment updates
	probeEngine.SetOnJudgment(restServer.OnJudgmentCallback())

	// Initialize gRPC server
	var grpcServer *grpcapi.Server
	if cfg.Server.GRPCPort > 0 {
		grpcStore := &grpcStorageAdapter{inner: restStore}
		// Build TLS config for gRPC server from server TLS config
		var grpcTLSConfig *tls.Config
		if cfg.Server.TLS.Enabled && cfg.Server.TLS.Cert != "" && cfg.Server.TLS.Key != "" {
			cert, err := tls.LoadX509KeyPair(cfg.Server.TLS.Cert, cfg.Server.TLS.Key)
			if err == nil {
				grpcTLSConfig = &tls.Config{
					Certificates: []tls.Certificate{cert},
					MinVersion:   tls.VersionTLS12,
				}
			}
		}

		grpcServer = grpcapi.NewServer(
			fmt.Sprintf(":%d", cfg.Server.GRPCPort),
			grpcStore,
			&grpcProbeAdapter{engine: probeEngine},
			authenticator,
			logger,
			grpcTLSConfig,
			cfg.Server.GRPCReflection,
		)
	}

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
		GRPCServer:        grpcServer,
		StatusPageRepo:    statusPageRepo,
		ACMEManager:       acmeMgr,
		DashboardHandler:  dashboardHandler,
		StatusPageHandler: statusPageHandler,
		MCPServer:         mcpServer,
	}, nil
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

// restStorageAdapter adapts storage.CobaltDB to api.Storage interface
type restStorageAdapter struct {
	store *storage.CobaltDB
}

func (a *restStorageAdapter) GetSoulNoCtx(id string) (*core.Soul, error) {
	return a.store.GetSoulNoCtx(id)
}

func (a *restStorageAdapter) ListSoulsNoCtx(workspace string, offset, limit int) ([]*core.Soul, error) {
	return a.store.ListSoulsNoCtx(workspace, offset, limit)
}

func (a *restStorageAdapter) SaveSoul(ctx context.Context, soul *core.Soul) error {
	return a.store.SaveSoul(ctx, soul)
}

func (a *restStorageAdapter) DeleteSoul(ctx context.Context, id string) error {
	return a.store.DeleteSoul(ctx, "default", id)
}

func (a *restStorageAdapter) DeleteSoulNoCtx(id string) error {
	return a.store.DeleteSoul(context.Background(), "default", id)
}

func (a *restStorageAdapter) GetJudgmentNoCtx(id string) (*core.Judgment, error) {
	return a.store.GetJudgmentNoCtx(id)
}

func (a *restStorageAdapter) ListJudgmentsNoCtx(soulID string, start, end time.Time, limit int) ([]*core.Judgment, error) {
	return a.store.ListJudgmentsNoCtx(soulID, start, end, limit)
}

func (a *restStorageAdapter) GetChannelNoCtx(id string, workspace string) (*core.AlertChannel, error) {
	return a.store.GetChannelNoCtx(id, workspace)
}

func (a *restStorageAdapter) ListChannelsNoCtx(workspace string) ([]*core.AlertChannel, error) {
	return a.store.ListChannelsNoCtx(workspace)
}

func (a *restStorageAdapter) SaveChannelNoCtx(ch *core.AlertChannel) error {
	return a.store.SaveChannelNoCtx(ch)
}

func (a *restStorageAdapter) DeleteChannelNoCtx(id string, workspace string) error {
	return a.store.DeleteChannelNoCtx(id, workspace)
}

func (a *restStorageAdapter) GetRuleNoCtx(id string, workspace string) (*core.AlertRule, error) {
	return a.store.GetRuleNoCtx(id, workspace)
}

func (a *restStorageAdapter) ListRulesNoCtx(workspace string) ([]*core.AlertRule, error) {
	return a.store.ListRulesNoCtx(workspace)
}

func (a *restStorageAdapter) SaveRuleNoCtx(rule *core.AlertRule) error {
	return a.store.SaveRuleNoCtx(rule)
}

func (a *restStorageAdapter) DeleteRuleNoCtx(id string, workspace string) error {
	return a.store.DeleteRuleNoCtx(id, workspace)
}

func (a *restStorageAdapter) GetWorkspaceNoCtx(id string) (*core.Workspace, error) {
	return a.store.GetWorkspaceNoCtx(id)
}

func (a *restStorageAdapter) ListWorkspacesNoCtx() ([]*core.Workspace, error) {
	return a.store.ListWorkspacesNoCtx()
}

func (a *restStorageAdapter) SaveWorkspaceNoCtx(ws *core.Workspace) error {
	return a.store.SaveWorkspaceNoCtx(ws)
}

func (a *restStorageAdapter) DeleteWorkspaceNoCtx(id string) error {
	return a.store.DeleteWorkspaceNoCtx(id)
}

func (a *restStorageAdapter) GetStatsNoCtx(workspace string, start, end time.Time) (*core.Stats, error) {
	return a.store.GetStatsNoCtx(workspace, start, end)
}

// StatusPage methods

func (a *restStorageAdapter) GetStatusPageNoCtx(id string) (*core.StatusPage, error) {
	return a.store.GetStatusPageNoCtx(id)
}

func (a *restStorageAdapter) ListStatusPagesNoCtx() ([]*core.StatusPage, error) {
	return a.store.ListStatusPagesNoCtx()
}

func (a *restStorageAdapter) SaveStatusPageNoCtx(page *core.StatusPage) error {
	return a.store.SaveStatusPageNoCtx(page)
}

func (a *restStorageAdapter) DeleteStatusPageNoCtx(id string) error {
	return a.store.DeleteStatusPageNoCtx(id)
}

// Journey methods
func (a *restStorageAdapter) GetJourneyNoCtx(id string) (*core.JourneyConfig, error) {
	return a.store.GetJourneyNoCtx(id)
}

func (a *restStorageAdapter) ListJourneysNoCtx(workspace string, offset, limit int) ([]*core.JourneyConfig, error) {
	return a.store.ListJourneysNoCtx(workspace, offset, limit)
}

func (a *restStorageAdapter) SaveJourneyNoCtx(journey *core.JourneyConfig) error {
	return a.store.SaveJourneyNoCtx(journey)
}

func (a *restStorageAdapter) DeleteJourneyNoCtx(id string) error {
	return a.store.DeleteJourneyNoCtx(id)
}

func (a *restStorageAdapter) GetDashboardNoCtx(id string) (*core.CustomDashboard, error) {
	return a.store.GetDashboardNoCtx(id)
}

func (a *restStorageAdapter) ListDashboardsNoCtx() ([]*core.CustomDashboard, error) {
	return a.store.ListDashboardsNoCtx()
}

func (a *restStorageAdapter) SaveDashboardNoCtx(dashboard *core.CustomDashboard) error {
	return a.store.SaveDashboardNoCtx(dashboard)
}

func (a *restStorageAdapter) DeleteDashboardNoCtx(id string) error {
	return a.store.DeleteDashboardNoCtx(id)
}

// MaintenanceWindow methods
func (a *restStorageAdapter) GetMaintenanceWindow(id string) (*core.MaintenanceWindow, error) {
	return a.store.GetMaintenanceWindow(id)
}

func (a *restStorageAdapter) ListMaintenanceWindows() ([]*core.MaintenanceWindow, error) {
	return a.store.ListMaintenanceWindows()
}

func (a *restStorageAdapter) SaveMaintenanceWindow(w *core.MaintenanceWindow) error {
	return a.store.SaveMaintenanceWindow(w)
}

func (a *restStorageAdapter) DeleteMaintenanceWindow(id string) error {
	return a.store.DeleteMaintenanceWindow(id)
}

// clusterAdapter adapts cluster.Manager to api.ClusterManager interface
type clusterAdapter struct {
	mgr *cluster.Manager
}

func (a *clusterAdapter) IsLeader() bool {
	if a.mgr == nil {
		return false
	}
	return a.mgr.IsLeader()
}

func (a *clusterAdapter) Leader() string {
	if a.mgr == nil {
		return ""
	}
	return a.mgr.Leader()
}

func (a *clusterAdapter) IsClustered() bool {
	if a.mgr == nil {
		return false
	}
	return a.mgr.IsClustered()
}

func (a *clusterAdapter) GetStatus() *api.ClusterStatus {
	if a.mgr == nil {
		return &api.ClusterStatus{IsClustered: false, NodeID: "standalone", State: "standalone"}
	}
	cs := a.mgr.GetStatus()
	if cs == nil {
		return &api.ClusterStatus{IsClustered: false, NodeID: "standalone", State: "standalone"}
	}
	return &api.ClusterStatus{
		IsClustered: cs.IsClustered,
		NodeID:      cs.NodeID,
		State:       cs.State,
		Leader:      cs.Leader,
		Term:        cs.Term,
		PeerCount:   cs.PeerCount,
	}
}

// alertStorageAdapter adapts storage.CobaltDB to alert.AlertStorage interface
type alertStorageAdapter struct {
	store *storage.CobaltDB
}

func (a *alertStorageAdapter) SaveChannel(ch *core.AlertChannel) error {
	return a.store.SaveAlertChannel(ch)
}

func (a *alertStorageAdapter) GetChannel(id string, workspace string) (*core.AlertChannel, error) {
	return a.store.GetAlertChannel(id, workspace)
}

func (a *alertStorageAdapter) ListChannels(workspace string) ([]*core.AlertChannel, error) {
	return a.store.ListAlertChannels(workspace)
}

func (a *alertStorageAdapter) DeleteChannel(id string, workspace string) error {
	return a.store.DeleteAlertChannel(id, workspace)
}

func (a *alertStorageAdapter) SaveRule(rule *core.AlertRule) error {
	return a.store.SaveAlertRule(rule)
}

func (a *alertStorageAdapter) GetRule(id string, workspace string) (*core.AlertRule, error) {
	return a.store.GetAlertRule(id, workspace)
}

func (a *alertStorageAdapter) ListRules(workspace string) ([]*core.AlertRule, error) {
	return a.store.ListAlertRules(workspace)
}

func (a *alertStorageAdapter) DeleteRule(id string, workspace string) error {
	return a.store.DeleteAlertRule(id, workspace)
}

func (a *alertStorageAdapter) SaveEvent(event *core.AlertEvent) error {
	return a.store.SaveAlertEvent(event)
}

func (a *alertStorageAdapter) ListEvents(soulID string, limit int) ([]*core.AlertEvent, error) {
	return a.store.ListAlertEvents(soulID, limit)
}

func (a *alertStorageAdapter) SaveIncident(incident *core.Incident) error {
	return a.store.SaveIncident(incident)
}

func (a *alertStorageAdapter) GetIncident(id string) (*core.Incident, error) {
	return a.store.GetIncident(id)
}

func (a *alertStorageAdapter) ListActiveIncidents() ([]*core.Incident, error) {
	return a.store.ListActiveIncidents()
}

// statusPageRepository implements statuspage.Repository
type statusPageRepository struct {
	store *storage.CobaltDB
}

func (r *statusPageRepository) GetStatusPageByDomain(domain string) (*core.StatusPage, error) {
	return r.store.GetStatusPageByDomain(domain)
}

func (r *statusPageRepository) GetStatusPageBySlug(slug string) (*core.StatusPage, error) {
	return r.store.GetStatusPageBySlug(slug)
}

func (r *statusPageRepository) GetSoul(id string) (*core.Soul, error) {
	return r.store.GetSoulNoCtx(id)
}

func (r *statusPageRepository) GetSoulJudgments(soulID string, limit int) ([]core.Judgment, error) {
	return r.store.GetSoulJudgments(soulID, limit)
}

func (r *statusPageRepository) GetIncidentsByPage(pageID string) ([]core.Incident, error) {
	// Get the status page to find associated souls
	page, err := r.store.GetStatusPageBySlug(pageID)
	if err != nil {
		// Try by ID if slug lookup fails
		page, err = r.store.GetStatusPageNoCtx(pageID)
		if err != nil {
			return nil, err
		}
	}

	// Create a set of soul IDs for this page
	soulSet := make(map[string]bool)
	for _, soulID := range page.Souls {
		soulSet[soulID] = true
	}

	// Get all active incidents and filter by page's souls
	active, err := r.store.ListActiveIncidents()
	if err != nil {
		return nil, err
	}

	// Convert []*core.Incident to []core.Incident, filtering by page's souls
	result := make([]core.Incident, 0, len(active))
	for _, inc := range active {
		if inc != nil && soulSet[inc.SoulID] {
			result = append(result, *inc)
		}
	}
	return result, nil
}

func (r *statusPageRepository) GetUptimeHistory(soulID string, days int) ([]core.UptimeDay, error) {
	return r.store.GetUptimeHistory(soulID, days)
}

func (r *statusPageRepository) SaveSubscription(sub *core.StatusPageSubscription) error {
	return r.store.SaveStatusPageSubscription(sub)
}

func (r *statusPageRepository) GetSubscriptionsByPage(pageID string) ([]*core.StatusPageSubscription, error) {
	return r.store.GetSubscriptionsByPage(pageID)
}

func (r *statusPageRepository) DeleteSubscription(subscriptionID string) error {
	return r.store.DeleteStatusPageSubscription(subscriptionID)
}

// storageGetLatestJudgment retrieves the most recent judgment for a soul
func storageGetLatestJudgment(store *storage.CobaltDB, ctx context.Context, workspaceID, soulID string) (*core.Judgment, error) {
	if workspaceID == "" {
		workspaceID = "default"
	}

	// Scan with prefix to find latest
	prefix := fmt.Sprintf("%s/judgments/%s/", workspaceID, soulID)
	results, err := store.PrefixScan(prefix)
	if err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return nil, &core.NotFoundError{Entity: "judgment", ID: soulID}
	}

	// Find latest (keys are sorted, so last one is latest)
	var latest *core.Judgment
	var latestKey string
	for key, data := range results {
		if data == nil {
			continue
		}
		var j core.Judgment
		if err := json.Unmarshal(data, &j); err != nil {
			continue
		}
		if key > latestKey {
			latest = &j
			latestKey = key
		}
	}

	if latest == nil {
		return nil, &core.NotFoundError{Entity: "judgment", ID: soulID}
	}

	return latest, nil
}

// initACMEManager initializes the ACME manager for Let's Encrypt
func initACMEManager(cfg *core.Config, store *storage.CobaltDB, logger *slog.Logger) *acme.Manager {
	if !cfg.Server.TLS.Enabled || !cfg.Server.TLS.AutoCert {
		return nil
	}

	acmeCfg := acme.Config{
		Enabled:   true,
		Provider:  acme.ProviderLetsEncrypt,
		Email:     cfg.Server.TLS.ACMEEmail,
		AcceptTOS: true,
		CertPath:  path.Join(cfg.Storage.Path, "acme"),
	}

	mgr, err := acme.NewManager(store, acmeCfg)
	if err != nil {
		logger.Warn("failed to initialize ACME manager", "err", err)
		return nil
	}

	logger.Info("ACME manager initialized", "provider", acmeCfg.Provider, "email", acmeCfg.Email)
	return mgr
}
