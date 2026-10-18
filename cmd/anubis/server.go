package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

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
	"github.com/AnubisWatch/anubiswatch/internal/telemetry"
)

// ServerDependencies holds all dependencies for the server
type ServerDependencies struct {
	Config            *core.Config
	Logger            *slog.Logger
	Store             *storage.CobaltDB
	TimeSeries        *storage.TimeSeriesStore
	Retention         *storage.RetentionManager
	Authenticator     api.Authenticator
	AlertManager      *alert.Manager
	ProbeEngine       *probe.Engine
	JourneyExecutor   *journey.Executor
	ClusterManager    *cluster.Manager
	RESTServer        *api.RESTServer
	GRPCServer        *grpcapi.Server
	StatusPageRepo    *statusPageRepository
	DashboardHandler  http.Handler
	StatusPageHandler http.Handler
	MCPServer         *api.MCPServer
	TracerProvider    *telemetry.TracerProvider
	// ClusterWork shares probing across the cluster; nil when standalone.
	ClusterWork *clusterWork
	// Journeys schedules stored journeys when standalone; nil in cluster mode.
	Journeys *standaloneJourneys
}

// Server represents the AnubisWatch server
type Server struct {
	deps   *ServerDependencies
	logger *slog.Logger
	stopCh chan struct{}

	// failOnce guards a single close of stopCh; startupErr records why a
	// critical component tore the server down so the process can exit non-zero.
	failOnce   sync.Once
	startupErr error
}

// NewServer creates a new Server instance
func NewServer(deps *ServerDependencies) *Server {
	return &Server{
		deps:   deps,
		logger: deps.Logger,
		stopCh: make(chan struct{}),
	}
}

// triggerShutdown records an optional failure reason and unblocks
// WaitForShutdown exactly once. A nil err denotes an ordinary requested stop.
func (s *Server) triggerShutdown(err error) {
	s.failOnce.Do(func() {
		s.startupErr = err
		close(s.stopCh)
	})
}

// StartupError returns the error that caused a critical component to trigger
// shutdown, or nil if the server stopped for a normal reason (signal). It is
// safe to read after WaitForShutdown returns.
func (s *Server) StartupError() error {
	return s.startupErr
}

// Start initializes and starts all server components
func (s *Server) Start(ctx context.Context) error {
	cfg := s.deps.Config
	logger := s.logger

	// Start storage maintenance: downsampling of check results into
	// coarser summaries, and deletion of data past its retention period.
	if s.deps.TimeSeries != nil {
		s.deps.TimeSeries.StartCompaction()
	}
	if s.deps.Retention != nil {
		r := cfg.Storage.TimeSeries.Retention
		logger.Info("data retention enabled",
			"raw", r.Raw.Duration, "1min", r.Minute.Duration, "5min", r.FiveMin.Duration,
			"1hour", r.Hour.Duration, "1day", r.Day)
		for _, w := range compactionRetentionConflicts(cfg.Storage.TimeSeries) {
			logger.Warn("retention deletes summaries before they are rolled up", "detail", w)
		}
		s.deps.Retention.Start()
	}

	// Start alert manager
	if s.deps.AlertManager != nil {
		if err := s.deps.AlertManager.Start(); err != nil {
			logger.Warn("failed to start alert manager", "err", err)
		} else {
			logger.Info("alert manager started")
			loadConfiguredAlerts(cfg, s.deps.AlertManager, logger)
		}
	}

	// Assign configured souls plus API-created souls persisted in storage. In
	// cluster mode ClusterWork assigns only the souls this node owns, once the
	// cluster manager is up.
	if s.deps.ProbeEngine != nil && s.deps.ClusterWork == nil {
		soulPtrs := make([]*core.Soul, 0, len(cfg.Souls))
		seen := make(map[string]struct{}, len(cfg.Souls))
		for i := range cfg.Souls {
			soul := &cfg.Souls[i]
			soulPtrs = append(soulPtrs, soul)
			if soul.ID != "" {
				seen[soul.ID] = struct{}{}
			}
		}

		workspaceIDs := allWorkspaceIDs(ctx, s.deps.Store, logger, "probe assignment")

		for _, workspaceID := range workspaceIDs {
			storedSouls, err := s.deps.Store.ListSouls(ctx, workspaceID, 0, 10000)
			if err != nil {
				logger.Warn("failed to load stored souls for probe assignment", "workspace", workspaceID, "err", err)
				continue
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
		}
	}
	// In cluster mode the ownership pass schedules each journey on its owner
	// only; standalone schedules every stored and configured journey.
	if s.deps.Journeys != nil {
		s.deps.Journeys.start(ctx)
	}

	// Start cluster manager.
	//
	// When the operator explicitly asked for clustering, a start failure must
	// abort startup rather than silently leave a standalone node behind: the
	// node would not join consensus, and nothing would signal that the cluster
	// it appears in is a fiction. Same reasoning as the REST bind failure
	// below — a misconfiguration has to look like a crash to supervisors.
	clusterRequired := cfg.Necropolis.Enabled || cfg.Necropolis.SingleNode || cfg.Necropolis.Raft.Bootstrap || len(cfg.Necropolis.Raft.Peers) > 0 || len(cfg.Necropolis.Raft.Join) > 0
	if clusterRequired && s.deps.ClusterManager == nil {
		return fmt.Errorf("cluster mode is enabled but the cluster manager was not initialized")
	}
	if s.deps.ClusterManager != nil {
		if err := s.deps.ClusterManager.Start(ctx); err != nil {
			if clusterRequired || s.deps.ClusterManager.IsClustered() {
				return fmt.Errorf("failed to start cluster manager: %w", err)
			}
			logger.Warn("failed to start standalone cluster manager", "err", err)
		} else {
			logger.Info("cluster manager initialized", "clustered", s.deps.ClusterManager.IsClustered())
		}
	}
	if s.deps.ClusterWork != nil {
		s.deps.ClusterWork.start()
	}

	// Start REST server
	if s.deps.RESTServer != nil {
		go func() {
			if err := s.deps.RESTServer.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Error("REST server failed", "err", err)
				// Signal shutdown so other components stop gracefully when the
				// REST API becomes unavailable, and record the failure so the
				// process exits non-zero — a bind failure (port in use, cert
				// unreadable) must look like a crash to supervisors/K8s, not a
				// clean exit.
				s.triggerShutdown(fmt.Errorf("REST server failed: %w", err))
			}
		}()
		logger.Info("REST API server initialized", "addr", fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port))
	}

	// Start gRPC server. A configured management endpoint is a required
	// subsystem: continuing after a bind failure would leave supervisors with a
	// falsely healthy process whose requested API is unavailable.
	if cfg.Server.GRPCPort > 0 {
		if s.deps.GRPCServer == nil {
			return fmt.Errorf("gRPC is enabled on port %d but the server was not initialized", cfg.Server.GRPCPort)
		}
		if err := s.deps.GRPCServer.Start(); err != nil {
			return fmt.Errorf("failed to start gRPC server: %w", err)
		}
		logger.Info("gRPC API server initialized", "addr", fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.GRPCPort))
	}

	logger.Info("AnubisWatch is ready. The judgment begins.")
	return nil
}

// Stop gracefully shuts down the server
func (s *Server) Stop(ctx context.Context) error {
	logger := s.logger

	logger.Info("shutting down...")

	// Shutdown telemetry (flush traces)
	if s.deps.TracerProvider != nil {
		if err := s.deps.TracerProvider.Shutdown(ctx); err != nil {
			logger.Warn("failed to shutdown telemetry", "err", err)
		}
	}

	// Stop REST server
	if s.deps.RESTServer != nil {
		if err := s.deps.RESTServer.Stop(ctx); err != nil {
			logger.Warn("failed to stop REST server", "err", err)
		}
	}

	// Stop gRPC server, bounded by the shutdown deadline so a long-lived
	// streaming RPC cannot hang the process past its SIGTERM grace period.
	if s.deps.GRPCServer != nil {
		s.deps.GRPCServer.StopWithContext(ctx)
	}

	// Stop journey executors. In cluster mode halt the ownership loop first so
	// it cannot reschedule them.
	if s.deps.ClusterWork != nil {
		s.deps.ClusterWork.halt()
	}
	if s.deps.JourneyExecutor != nil {
		s.deps.JourneyExecutor.StopAll()
	}

	// Stop alert manager
	if s.deps.AlertManager != nil {
		if err := s.deps.AlertManager.Stop(); err != nil {
			logger.Warn("failed to stop alert manager", "err", err)
		}
	}

	// Flush replication still queued while consensus is up, then stop sharing.
	if s.deps.ClusterWork != nil {
		s.deps.ClusterWork.stop(ctx)
	}

	// Stop cluster manager
	if s.deps.ClusterManager != nil {
		if err := s.deps.ClusterManager.Stop(ctx); err != nil {
			logger.Warn("failed to stop cluster manager", "err", err)
		}
	}

	// Stop probe engine
	if s.deps.ProbeEngine != nil {
		s.deps.ProbeEngine.Stop()
	}

	// Shutdown authenticator
	if s.deps.Authenticator != nil {
		s.deps.Authenticator.Shutdown()
	}

	// Stop storage maintenance before the store it writes to closes.
	if s.deps.Retention != nil {
		s.deps.Retention.Stop()
	}
	if s.deps.TimeSeries != nil {
		s.deps.TimeSeries.StopCompaction()
	}

	// Close storage
	if s.deps.Store != nil {
		s.deps.Store.Close()
	}

	logger.Info("⚖️  AnubisWatch stopped. The judgment rests.")
	return nil
}

// WaitForShutdown blocks until a shutdown signal is received or StopCh is
// closed. StopCh is closed by component failures (e.g. REST server crash)
// to trigger graceful shutdown when a critical component becomes unavailable.
func (s *Server) WaitForShutdown() {
	shutdownCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	select {
	case <-shutdownCtx.Done():
	case <-s.stopCh:
	}
}

// ServerOptions holds options for building server dependencies
type ServerOptions struct {
	ConfigPath    string
	Logger        *slog.Logger
	SingleNode    bool
	Cluster       bool
	ClusterSet    bool
	Bootstrap     bool
	BootstrapSet  bool
	JoinAddrs     []string
	NodeName      string
	Region        string
	BindAddr      string
	AdvertiseAddr string

	// InsecureSkipVerify enables the K7 master switch that allows
	// per-soul InsecureSkipVerify=true on TLS checks. Defaults to
	// false (secure). When true, TLS verification is disabled for
	// any check that requests it, which is a MITM vector.
	InsecureSkipVerify bool
}

// probeController is the part of the probe engine the API surfaces drive:
// *probe.Engine standalone, *clusterProbe in cluster mode.
type probeController interface {
	UpsertSoul(soul *core.Soul)
	RemoveSoul(soulID string)
	GetStatus() *core.ProbeStatus
	ForceCheck(soulID string) (*core.Judgment, error)
}

// grpcProbeAdapter adapts probe.Engine to grpcapi.ProbeEngine
type grpcProbeAdapter struct {
	engine probeController
}

func (a *grpcProbeAdapter) ForceCheck(soulID string) (*core.Judgment, error) {
	return a.engine.ForceCheck(soulID)
}

func (a *grpcProbeAdapter) UpsertSoul(soul *core.Soul) {
	a.engine.UpsertSoul(soul)
}

func (a *grpcProbeAdapter) RemoveSoul(soulID string) {
	a.engine.RemoveSoul(soulID)
}

// grpcStorageAdapter wraps restStorageAdapter to provide concrete types for the
// gRPC Store interface. Every method simply forwards to the underlying storage
// — there are no more interface{} conversions or map-based fallbacks.
type grpcStorageAdapter struct {
	inner   *restStorageAdapter
	journey *journey.Executor
}

func (a *grpcStorageAdapter) GetSoulNoCtx(id string) (*core.Soul, error) {
	return a.inner.GetSoulNoCtx(id)
}
func (a *grpcStorageAdapter) ListSoulsNoCtx(ws string, o, l int) ([]*core.Soul, error) {
	return a.inner.ListSoulsNoCtx(ws, o, l)
}
func (a *grpcStorageAdapter) SaveSoulNoCtx(soul *core.Soul) error {
	return a.inner.SaveSoul(context.Background(), soul)
}
func (a *grpcStorageAdapter) DeleteSoulNoCtx(id string) error { return a.inner.DeleteSoulNoCtx(id) }
func (a *grpcStorageAdapter) ListJudgmentsNoCtx(soulID string, start, end time.Time, limit int) ([]*core.Judgment, error) {
	return a.inner.ListJudgmentsNoCtx(soulID, start, end, limit)
}
func (a *grpcStorageAdapter) GetChannelNoCtx(id string, ws string) (*core.AlertChannel, error) {
	return a.inner.GetChannelNoCtx(id, ws)
}
func (a *grpcStorageAdapter) ListChannelsNoCtx(ws string) ([]*core.AlertChannel, error) {
	return a.inner.ListChannelsNoCtx(ws)
}
func (a *grpcStorageAdapter) SaveChannelNoCtx(ch *core.AlertChannel) error {
	return a.inner.SaveChannelNoCtx(ch)
}
func (a *grpcStorageAdapter) DeleteChannelNoCtx(id string, ws string) error {
	return a.inner.DeleteChannelNoCtx(id, ws)
}
func (a *grpcStorageAdapter) GetRuleNoCtx(id string, ws string) (*core.AlertRule, error) {
	return a.inner.GetRuleNoCtx(id, ws)
}
func (a *grpcStorageAdapter) ListRulesNoCtx(ws string) ([]*core.AlertRule, error) {
	return a.inner.ListRulesNoCtx(ws)
}
func (a *grpcStorageAdapter) SaveRuleNoCtx(rule *core.AlertRule) error {
	return a.inner.SaveRuleNoCtx(rule)
}
func (a *grpcStorageAdapter) DeleteRuleNoCtx(id string, ws string) error {
	return a.inner.DeleteRuleNoCtx(id, ws)
}
func (a *grpcStorageAdapter) GetJourneyNoCtx(id string) (*core.JourneyConfig, error) {
	return a.inner.GetJourneyNoCtx(id)
}
func (a *grpcStorageAdapter) ListJourneysNoCtx(ws string, o, l int) ([]*core.JourneyConfig, error) {
	return a.inner.ListJourneysNoCtx(ws, o, l)
}
func (a *grpcStorageAdapter) SaveJourneyNoCtx(j *core.JourneyConfig) error {
	return a.inner.SaveJourneyNoCtx(j)
}
func (a *grpcStorageAdapter) DeleteJourneyNoCtx(id string) error {
	return a.inner.DeleteJourneyNoCtx(id)
}
func (a *grpcStorageAdapter) RunJourneyNoCtx(workspace, journeyID string) (*core.JourneyRun, error) {
	if a.journey == nil {
		return nil, fmt.Errorf("journey executor not available")
	}
	j, err := a.inner.store.GetJourney(context.Background(), workspace, journeyID)
	if err != nil {
		return nil, err
	}
	return a.journey.RunOnce(context.Background(), j)
}
func (a *grpcStorageAdapter) ListEvents(soulID string, limit int) ([]*core.AlertEvent, error) {
	return a.inner.store.ListAlertEvents(soulID, limit)
}
func (a *grpcStorageAdapter) ListJourneyRunsNoCtx(workspace, journeyID string, limit int) ([]*core.JourneyRun, error) {
	return a.inner.store.QueryJourneyRuns(context.Background(), workspace, journeyID, limit)
}
func (a *grpcStorageAdapter) GetJourneyRunNoCtx(workspace, journeyID, runID string) (*core.JourneyRun, error) {
	return a.inner.store.GetJourneyRun(context.Background(), workspace, journeyID, runID)
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
		warnUnimplementedConfig(cfg, logger)
	} else {
		logger.Info("no config file found, using defaults", "path", configPath)
		cfg = core.GenerateDefaultConfig()
		// Check environment variable for data dir override (for CI/tests)
		if envDir := os.Getenv("ANUBIS_DATA_DIR"); envDir != "" {
			cfg.Storage.Path = envDir
		}
	}
	applyServerOptionOverrides(cfg, opts)

	// Initialize telemetry
	telemetryConfig := telemetry.DefaultConfig()
	telemetryConfig.Enabled = cfg.Telemetry.Enabled
	telemetryConfig.Endpoint = cfg.Telemetry.Endpoint
	telemetryConfig.Environment = cfg.Environment
	telemetryConfig.SampleRate = cfg.Telemetry.SampleRate

	tracerProvider, err := telemetry.InitTracer(context.Background(), telemetryConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize telemetry: %w", err)
	}
	logger.Info("telemetry initialized", "enabled", telemetryConfig.Enabled)

	// Create data directory
	if err := os.MkdirAll(cfg.Storage.Path, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	// Initialize storage
	store, err := storage.NewEngine(cfg.Storage, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize storage: %w", err)
	}
	timeSeries := storage.NewTimeSeriesStore(store, cfg.Storage.TimeSeries, logger)
	retention := storage.NewRetentionManager(store, cfg.Storage.TimeSeries.Retention, cfg.Storage.Path, logger)

	// Initialize auth
	sessionPath := filepath.Join(cfg.Storage.Path, "sessions.json")
	adminEmail := "admin@anubis.watch"
	if cfg.Auth.Local.AdminEmail != "" {
		adminEmail = cfg.Auth.Local.AdminEmail
	}

	// CRITICAL: Never use a hardcoded default password. If no password is configured,
	// generate a random one and store it to a file so the operator must explicitly set it.
	adminPassword := cfg.Auth.Local.AdminPassword
	if adminPassword == "" {
		// Check if password file exists from previous generation
		passwordFile := filepath.Join(cfg.Storage.Path, ".admin_password")
		if data, err := os.ReadFile(passwordFile); err == nil {
			adminPassword = strings.TrimSpace(string(data))
			logger.Info("using existing admin password from file", "path", passwordFile)
		} else {
			// Generate new password
			b := make([]byte, 24)
			if _, err := rand.Read(b); err != nil {
				return nil, fmt.Errorf("failed to generate admin password: %w", err)
			}
			adminPassword = "Aa1!" + base64.RawURLEncoding.EncodeToString(b)
			// Store password to file (chmod 0600 so only owner can read)
			if err := os.WriteFile(passwordFile, []byte(adminPassword), 0600); err != nil {
				return nil, fmt.Errorf("failed to store admin password: %w", err)
			}
			logger.Warn("no admin password configured — random password generated and stored",
				"action", "set auth.local.admin_password in config",
				"password_file", passwordFile)
		}
	}

	var authenticator api.Authenticator
	switch cfg.Auth.Type {
	case "oidc":
		oidcAuth, err := auth.NewOIDCAuthenticator(cfg.Auth.OIDC, sessionPath, adminEmail, adminPassword)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize OIDC authenticator: %w", err)
		}
		authenticator = oidcAuth
		logger.Info("OIDC authentication initialized", "issuer", cfg.Auth.OIDC.Issuer)
	case "ldap":
		ldapAuth, err := auth.NewLDAPAuthenticator(cfg.Auth.LDAP, sessionPath, adminEmail, adminPassword)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize LDAP authenticator: %w", err)
		}
		authenticator = ldapAuth
		logger.Info("LDAP authentication initialized", "url", cfg.Auth.LDAP.URL)
	default:
		localAuth, err := auth.NewLocalAuthenticator(sessionPath, adminEmail, adminPassword)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize local authenticator: %w", err)
		}
		authenticator = localAuth
	}

	// SSRF policy for probes and alert dispatchers. Applied before either
	// starts so no check runs under the default policy.
	if err := probe.ConfigureDefaultValidator(cfg.Security.AllowPrivateTargets, cfg.Security.AllowedNetworks); err != nil {
		return nil, err
	}
	if probe.DefaultValidator.AllowPrivate {
		logger.Warn("SSRF protection relaxed: probes and alert channels may reach private and loopback addresses",
			"event", "ssrf_allow_private")
	}

	// Initialize cluster manager. When clustering was explicitly enabled, an
	// initialization failure is fatal; falling back to a standalone manager
	// would violate the requested topology without making the pod unhealthy.
	clusterMgr, err := cluster.NewManager(cfg.Necropolis, store, logger)
	if err != nil {
		if cfg.Necropolis.Enabled {
			return nil, fmt.Errorf("failed to initialize enabled cluster manager: %w", err)
		}
		logger.Warn("failed to initialize standalone cluster manager", "err", err)
		clusterMgr = nil
	}

	// In cluster mode monitoring work is shared: each soul is probed by its
	// owner only, and judgments and alert records replicate through Raft.
	var work *clusterWork
	if clusterMgr != nil && clusterMgr.IsClustered() {
		work = newClusterWork(clusterMgr, cfg, store, timeSeries, logger)
	}

	// Initialize alert manager
	alertStorage := &alertStorageAdapter{store: store, work: work}
	alertMgr := alert.NewManager(alertStorage, logger)

	// Initialize probe engine
	probeStore := &probeStorageAdapter{store: store, timeseries: timeSeries}
	if work != nil {
		probeStore.replicate = work.replicateJudgment
	}
	registry := newProbeRegistry()
	probeEngine := probe.NewEngine(probe.EngineOptions{
		Registry: registry,
		Store:    probeStore,
		Alerter:  alertMgr,
		NodeID:   cfg.Necropolis.NodeName,
		Region:   cfg.Necropolis.Region,
		Logger:   logger,
		Config: probe.EngineConfig{
			// K7: honour the master TLS-verification switch. The CLI
			// flag and env var have already been parsed into
			// ServerOptions; the config file's
			// security.allow_insecure_skip_verify is OR-ed in here so
			// any of the three sources can enable it.
			AllowInsecureSkipVerify: opts.InsecureSkipVerify || cfg.Security.AllowInsecureSkipVerify,
		},
	})

	if probeEngine.Config().AllowInsecureSkipVerify {
		logger.Warn("InsecureSkipVerify enabled at engine level — per-soul TLS-skip flags will be honoured",
			"event", "k7_insecure_skip_verify_enabled",
			// Source is the post-resolution view at server startup:
			// ServerOptions is populated from CLI flag, env var, or
			// config field. The exact origin is no longer recoverable
			// at this point, so we report the resolved fact rather
			// than the original input. Audit-grade traceability lives
			// in the applySecurityGate audit event per-check.
			"resolved", true,
		)
	}

	// Initialize journey executor
	journeyExec := journey.NewExecutor(store, logger)
	if work != nil {
		// Each journey runs on its owner only, and its runs replicate so every
		// node lists them; runs carry this node's ID like judgments do.
		journeyExec = journey.NewExecutorWithNodeID(store, logger, clusterMgr.NodeID(), cfg.Necropolis.Region)
		journeyExec.SetRunSaver(work.replicateJourneyRun)
		work.journeys = journeyExec
	}

	// The API surfaces schedule souls through probeAPI: the engine itself
	// standalone, an ownership-aware front in cluster mode.
	var probeAPI probeController = probeEngine
	if work != nil {
		work.engine = probeEngine
		work.alerts = alertMgr
		probeAPI = &clusterProbe{engine: probeEngine, work: work}
	}

	// Initialize REST server dependencies
	clusterAdapt := &clusterAdapter{mgr: clusterMgr}
	restStore := &restStorageAdapter{store: store}
	if clusterMgr != nil && clusterMgr.IsClustered() {
		restStore.replicator = clusterAdapt
	}
	var standaloneJourneySync *standaloneJourneys
	if work == nil {
		standaloneJourneySync = &standaloneJourneys{exec: journeyExec, cfg: cfg, store: store, logger: logger}
		restStore.onJourneyChange = standaloneJourneySync.sync
	}

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

	// Initialize status page handler. Private pages are gated on a session, so the
	// configured authenticator is adapted onto the handler's session check; without
	// this a private page is refused for everyone, including its owner.
	statusPageRepo := &statusPageRepository{store: store}
	statusPageHandler := statuspage.NewHandler(statusPageRepo).
		SetSessionValidator(statusPageSessionValidator{auth: authenticator})

	// Initialize MCP server
	mcpServer := api.NewMCPServer(restStore, probeAPI, alertMgr, logger)

	restServer := api.NewRESTServer(cfg.Server, cfg.Auth, restStore, probeAPI, alertMgr, authenticator, clusterAdapt, journeyExec, dashboardHandler, statusPageHandler, mcpServer, logger)
	// The Settings page reports the running storage path and raw-judgment
	// retention, and persists its presentation preferences in system config.
	restServer.SetRuntimeConfig(cfg.Storage.Path, cfg.Storage.TimeSeries.Retention.Raw.Duration)
	// Through restStore so a settings change replicates in cluster mode.
	restServer.SetSettingsStore(restStore)

	// Wire up WebSocket broadcasts for real-time updates: judgments from the
	// probe engine, incidents and dispatched alerts from Ma'at, and soul
	// writes from every API surface (they all share restStore).
	probeEngine.SetOnJudgment(restServer.OnJudgmentCallback())
	alertMgr.SetIncidentHook(restServer.OnIncidentCallback())
	alertMgr.SetAlertHook(restServer.OnAlertCallback())
	restStore.onSoulChange = restServer.OnSoulChangeCallback()
	if work != nil {
		// Judgments other nodes produce reach this node's live clients as they
		// replicate; this node's own are pushed by the probe engine above.
		work.onJudgment = restServer.OnJudgmentCallback()
	}

	// Initialize gRPC server
	var grpcServer *grpcapi.Server
	if cfg.Server.GRPCPort > 0 {
		grpcStore := &grpcStorageAdapter{inner: restStore, journey: journeyExec}
		// Build TLS config for gRPC server from server TLS config
		var grpcTLSConfig *tls.Config
		if cfg.Server.TLS.Enabled {
			cert, err := tls.LoadX509KeyPair(cfg.Server.TLS.Cert, cfg.Server.TLS.Key)
			if err != nil {
				return nil, fmt.Errorf("failed to load gRPC TLS certificate: %w", err)
			}
			grpcTLSConfig = &tls.Config{
				Certificates: []tls.Certificate{cert},
				MinVersion:   cfg.Server.TLS.ResolveMinVersion(),
			}
		}

		grpcServer = grpcapi.NewServer(
			grpcListenAddress(cfg.Server),
			grpcStore,
			&grpcProbeAdapter{engine: probeAPI},
			authenticator,
			logger,
			grpcTLSConfig,
			cfg.Server.GRPCReflection,
		).SetAlertRegistrar(alertMgr)
	}

	return &ServerDependencies{
		Config:            cfg,
		Logger:            logger,
		Store:             store,
		TimeSeries:        timeSeries,
		Retention:         retention,
		Authenticator:     authenticator,
		AlertManager:      alertMgr,
		ProbeEngine:       probeEngine,
		JourneyExecutor:   journeyExec,
		ClusterManager:    clusterMgr,
		RESTServer:        restServer,
		GRPCServer:        grpcServer,
		StatusPageRepo:    statusPageRepo,
		DashboardHandler:  dashboardHandler,
		StatusPageHandler: statusPageHandler,
		MCPServer:         mcpServer,
		TracerProvider:    tracerProvider,
		ClusterWork:       work,
		Journeys:          standaloneJourneySync,
	}, nil
}

func grpcListenAddress(cfg core.ServerConfig) string {
	host := strings.TrimSpace(strings.Trim(cfg.Host, "[]"))
	return net.JoinHostPort(host, strconv.Itoa(cfg.GRPCPort))
}

func applyServerOptionOverrides(cfg *core.Config, opts ServerOptions) {
	applyEnvOverrides(cfg, opts)
	applyClusterDefaults(cfg, opts)
}

func applyEnvOverrides(cfg *core.Config, opts ServerOptions) {
	// Override server port from environment
	if envPort := os.Getenv("ANUBIS_HTTP_PORT"); envPort != "" {
		if port, err := strconv.Atoi(envPort); err == nil {
			cfg.Server.Port = port
		}
	}

	// Override node settings from environment
	if envNodeID := os.Getenv("ANUBIS_NODE_ID"); envNodeID != "" && opts.NodeName == "" {
		opts.NodeName = envNodeID
	}
	if envBind := os.Getenv("ANUBIS_BIND_ADDR"); envBind != "" && opts.BindAddr == "" {
		opts.BindAddr = envBind
	}
	if envRaftPort := os.Getenv("ANUBIS_RAFT_PORT"); envRaftPort != "" && opts.BindAddr == "" {
		opts.BindAddr = "0.0.0.0:" + envRaftPort
	}

	// Handle single-node mode
	if opts.SingleNode {
		cfg.Necropolis.Enabled = false
		cfg.Necropolis.Raft.Bootstrap = false
		cfg.Necropolis.Raft.Peers = nil
	}

	// Handle cluster mode
	if opts.ClusterSet {
		cfg.Necropolis.Enabled = opts.Cluster
	}
	if opts.Cluster || opts.Bootstrap || len(opts.JoinAddrs) > 0 {
		cfg.Necropolis.Enabled = true
	}
	if opts.BootstrapSet {
		cfg.Necropolis.Raft.Bootstrap = opts.Bootstrap
	}

	// Apply CLI options to config
	if opts.NodeName != "" {
		cfg.Necropolis.NodeName = opts.NodeName
		cfg.Necropolis.Raft.NodeID = opts.NodeName
	}
	if opts.Region != "" {
		cfg.Necropolis.Region = opts.Region
		cfg.Necropolis.Raft.Region = opts.Region
	}
	if opts.BindAddr != "" {
		cfg.Necropolis.BindAddr = opts.BindAddr
		cfg.Necropolis.Raft.BindAddr = opts.BindAddr
	}
	if opts.AdvertiseAddr != "" {
		cfg.Necropolis.AdvertiseAddr = opts.AdvertiseAddr
		cfg.Necropolis.Raft.AdvertiseAddr = opts.AdvertiseAddr
	}

	// Join addresses: the node asks these members to add it and learns the
	// real member IDs from them (see raft join.go), rather than listing them
	// as peers under IDs guessed from their addresses.
	for _, addr := range opts.JoinAddrs {
		if addr = strings.TrimSpace(addr); addr != "" {
			cfg.Necropolis.Raft.Join = append(cfg.Necropolis.Raft.Join, addr)
		}
	}
}

func applyClusterDefaults(cfg *core.Config, opts ServerOptions) {
	if !cfg.Necropolis.Enabled {
		return
	}

	if cfg.Necropolis.NodeName == "" {
		if hostname, err := os.Hostname(); err == nil && hostname != "" {
			cfg.Necropolis.NodeName = hostname
		} else {
			cfg.Necropolis.NodeName = "jackal-1"
		}
	}
	if cfg.Necropolis.Region == "" {
		cfg.Necropolis.Region = "default"
	}
	if cfg.Necropolis.Raft.NodeID == "" {
		cfg.Necropolis.Raft.NodeID = cfg.Necropolis.NodeName
	}
	if cfg.Necropolis.Raft.BindAddr == "" {
		cfg.Necropolis.Raft.BindAddr = cfg.Necropolis.BindAddr
	}
	if cfg.Necropolis.Raft.AdvertiseAddr == "" {
		cfg.Necropolis.Raft.AdvertiseAddr = cfg.Necropolis.AdvertiseAddr
	}
	if cfg.Necropolis.Raft.Region == "" {
		cfg.Necropolis.Raft.Region = cfg.Necropolis.Region
	}
}

// newProbeRegistry builds the checker registry for a probe engine. Tests
// replace it to observe which node runs a check.
var newProbeRegistry = probe.NewCheckerRegistry

// probeStorageAdapter adapts storage.CobaltDB to probe.Storage interface
type probeStorageAdapter struct {
	store *storage.CobaltDB
	// timeseries, when set, saves each judgment and folds it into the
	// 1-minute summary that compaction downsamples from.
	timeseries *storage.TimeSeriesStore
	// replicate, when set (cluster mode), queues the judgment for replication
	// instead of saving it here: every node, this one included, stores it when
	// the batch applies, so the soul's history is the same everywhere.
	replicate func(*core.Judgment) error
}

func (a *probeStorageAdapter) SaveJudgment(ctx context.Context, j *core.Judgment) error {
	if a.replicate != nil {
		return a.replicate(j)
	}
	if a.timeseries != nil {
		return a.timeseries.SaveJudgment(ctx, j)
	}
	return a.store.SaveJudgment(ctx, j)
}

func (a *probeStorageAdapter) GetSoul(ctx context.Context, workspaceID, soulID string) (*core.Soul, error) {
	return a.store.GetSoul(ctx, workspaceID, soulID)
}

func (a *probeStorageAdapter) ListSouls(ctx context.Context, workspaceID string) ([]*core.Soul, error) {
	return a.store.ListSouls(ctx, workspaceID, 0, 1000)
}

// mutationApplier is the local interface for submitting commands through Raft
// consensus. When non-nil the restStorageAdapter routes writes through it.
type mutationApplier interface {
	ApplyMutation(cmd core.FSMCommand, timeout time.Duration) (uint64, uint64, error)
}

// restStorageAdapter adapts storage.CobaltDB to api.Storage interface
type restStorageAdapter struct {
	store      *storage.CobaltDB
	replicator mutationApplier
	// onSoulChange, when set, is told about every successful soul write
	// ("created", "updated", "deleted"). REST, MCP and gRPC all write souls
	// through this adapter, so this one hook feeds the live soul_update stream.
	onSoulChange func(soul *core.Soul, action string)
	// onJourneyChange, when set, runs after a local journey write so the
	// standalone executor picks it up.
	onJourneyChange func()
}

// applyMutation routes an FSM command through Raft consensus when clustering
// is active; when replicator is nil it returns nil (caller should write
// locally).
func (a *restStorageAdapter) applyMutation(cmd core.FSMCommand) error {
	if a.replicator == nil {
		return nil
	}
	_, _, err := a.replicator.ApplyMutation(cmd, 30*time.Second)
	if err != nil {
		var raftErr *core.RaftError
		if errors.As(err, &raftErr) && raftErr.Code == core.ErrNotLeader {
			return err
		}
		return fmt.Errorf("consensus write failed: %w", err)
	}
	return nil
}

// Ping forwards to the underlying store so the REST readiness probe (/ready)
// can detect a closed/wedged storage engine. Without this forwarder the
// probe's Ping() interface assertion on the adapter would silently fail and
// readiness would always report storage "ok".
func (a *restStorageAdapter) Ping() error {
	return a.store.Ping()
}

func (a *restStorageAdapter) GetSoulNoCtx(id string) (*core.Soul, error) {
	return a.store.GetSoulNoCtx(id)
}

func (a *restStorageAdapter) ListSoulsNoCtx(workspace string, offset, limit int) ([]*core.Soul, error) {
	return a.store.ListSoulsNoCtx(workspace, offset, limit)
}

func (a *restStorageAdapter) SaveSoul(ctx context.Context, soul *core.Soul) error {
	action := "updated"
	if a.onSoulChange != nil {
		if existing, err := a.store.GetSoulNoCtx(soul.ID); err != nil || existing == nil {
			action = "created"
		}
	}
	var err error
	if a.replicator != nil {
		key := fmt.Sprintf("%s/souls/%s", soul.WorkspaceID, soul.ID)
		data, marshalErr := json.Marshal(soul)
		if marshalErr != nil {
			return marshalErr
		}
		err = a.applyMutation(core.FSMCommand{Op: core.FSMSet, Key: key, Value: data})
	} else {
		err = a.store.SaveSoul(ctx, soul)
	}
	if err == nil && a.onSoulChange != nil {
		a.onSoulChange(soul, action)
	}
	return err
}

func (a *restStorageAdapter) DeleteSoul(ctx context.Context, id string) error {
	soul, err := a.store.GetSoulNoCtx(id)
	if err != nil {
		return err
	}
	if a.replicator != nil {
		key := fmt.Sprintf("%s/souls/%s", soul.WorkspaceID, id)
		err = a.applyMutation(core.FSMCommand{Op: core.FSMDelete, Key: key})
	} else {
		err = a.store.DeleteSoul(ctx, soul.WorkspaceID, id)
	}
	if err == nil && a.onSoulChange != nil {
		a.onSoulChange(soul, "deleted")
	}
	return err
}

func (a *restStorageAdapter) DeleteSoulNoCtx(id string) error {
	return a.DeleteSoul(context.Background(), id)
}

func (a *restStorageAdapter) GetJudgmentNoCtx(id string) (*core.Judgment, error) {
	return a.store.GetJudgmentNoCtx(id)
}

func (a *restStorageAdapter) ListJudgmentsNoCtx(soulID string, start, end time.Time, limit int) ([]*core.Judgment, error) {
	return a.store.ListJudgmentsNoCtx(soulID, start, end, limit)
}

// TallyChecks lets the soul uptime endpoint count checks from hourly
// summaries instead of decoding a month of raw judgments per request.
func (a *restStorageAdapter) TallyChecks(soulID string, ranges []core.TimeRange) ([]core.CheckTally, error) {
	return a.store.TallyChecks(soulID, ranges)
}

func (a *restStorageAdapter) GetChannelNoCtx(id string, workspace string) (*core.AlertChannel, error) {
	return a.store.GetChannelNoCtx(id, workspace)
}

func (a *restStorageAdapter) ListChannelsNoCtx(workspace string) ([]*core.AlertChannel, error) {
	return a.store.ListChannelsNoCtx(workspace)
}

func (a *restStorageAdapter) SaveChannelNoCtx(ch *core.AlertChannel) error {
	if a.replicator != nil {
		// Alert channels are stored by the engine under "{ws}/alerts/channels/{id}"
		// (internal/storage/storage.go SaveAlertChannel/GetAlertChannel/
		// ListAlertChannels). Replicating under "{ws}/channels/{id}" wrote the
		// record where no read path looks, so a clustered create succeeded and
		// the channel was immediately unreadable. Use the canonical key.
		key := fmt.Sprintf("%s/alerts/channels/%s", ch.WorkspaceID, ch.ID)
		data, err := json.Marshal(ch)
		if err != nil {
			return err
		}
		return a.applyMutation(core.FSMCommand{Op: core.FSMSet, Key: key, Value: data})
	}
	return a.store.SaveChannelNoCtx(ch)
}

func (a *restStorageAdapter) DeleteChannelNoCtx(id string, workspace string) error {
	if a.replicator != nil {
		// Same canonical key as SaveChannelNoCtx; the old "{ws}/channels/{id}"
		// form left the stored record orphaned.
		return a.applyMutation(core.FSMCommand{Op: core.FSMDelete, Key: fmt.Sprintf("%s/alerts/channels/%s", workspace, id)})
	}
	return a.store.DeleteChannelNoCtx(id, workspace)
}

func (a *restStorageAdapter) GetRuleNoCtx(id string, workspace string) (*core.AlertRule, error) {
	return a.store.GetRuleNoCtx(id, workspace)
}

func (a *restStorageAdapter) ListRulesNoCtx(workspace string) ([]*core.AlertRule, error) {
	return a.store.ListRulesNoCtx(workspace)
}

func (a *restStorageAdapter) SaveRuleNoCtx(rule *core.AlertRule) error {
	if a.replicator != nil {
		// Alert rules are stored by the engine under "{ws}/alerts/rules/{id}"
		// (internal/storage/storage.go SaveAlertRule/GetAlertRule/
		// ListAlertRules). Replicating under "{ws}/rules/{id}" wrote the record
		// where no read path looks, so a clustered create succeeded and the rule
		// was immediately unreadable. Use the canonical key.
		key := fmt.Sprintf("%s/alerts/rules/%s", rule.WorkspaceID, rule.ID)
		data, err := json.Marshal(rule)
		if err != nil {
			return err
		}
		return a.applyMutation(core.FSMCommand{Op: core.FSMSet, Key: key, Value: data})
	}
	return a.store.SaveRuleNoCtx(rule)
}

func (a *restStorageAdapter) DeleteRuleNoCtx(id string, workspace string) error {
	if a.replicator != nil {
		// Same canonical key as SaveRuleNoCtx; the old "{ws}/rules/{id}" form
		// left the stored record orphaned.
		return a.applyMutation(core.FSMCommand{Op: core.FSMDelete, Key: fmt.Sprintf("%s/alerts/rules/%s", workspace, id)})
	}
	return a.store.DeleteRuleNoCtx(id, workspace)
}

func (a *restStorageAdapter) GetWorkspaceNoCtx(id string) (*core.Workspace, error) {
	return a.store.GetWorkspaceNoCtx(id)
}

func (a *restStorageAdapter) ListWorkspacesNoCtx() ([]*core.Workspace, error) {
	return a.store.ListWorkspacesNoCtx()
}

func (a *restStorageAdapter) SaveWorkspaceNoCtx(ws *core.Workspace) error {
	if a.replicator != nil {
		// Every read path in CobaltDB resolves workspaces through the
		// workspace-less "workspaces/{id}" key (engine.go GetWorkspace,
		// ListWorkspaces, SaveWorkspace, DeleteWorkspace). Replicating under
		// "{ws}/workspaces/{ws}" wrote the record where nothing reads it, so a
		// clustered create answered 201 and the workspace was immediately
		// unreadable. Use the same canonical key the engine uses.
		key := "workspaces/" + ws.ID
		data, err := json.Marshal(ws)
		if err != nil {
			return err
		}
		return a.applyMutation(core.FSMCommand{Op: core.FSMSet, Key: key, Value: data})
	}
	return a.store.SaveWorkspaceNoCtx(ws)
}

func (a *restStorageAdapter) DeleteWorkspaceNoCtx(id string) error {
	if a.replicator != nil {
		// Same canonical key as SaveWorkspaceNoCtx; deleting the old
		// "{id}/workspaces/{id}" form left the stored record orphaned.
		return a.applyMutation(core.FSMCommand{Op: core.FSMDelete, Key: "workspaces/" + id})
	}
	return a.store.DeleteWorkspaceNoCtx(id)
}

func (a *restStorageAdapter) GetStatsNoCtx(workspace string, start, end time.Time) (*core.Stats, error) {
	return a.store.GetStatsNoCtx(workspace, start, end)
}

// statusPageSessionValidator adapts the configured authenticator to the status
// page's session check: a private page is visible to a session whose user belongs
// to the page's workspace, with the empty workspace meaning "default" exactly as
// it does elsewhere.
type statusPageSessionValidator struct{ auth api.Authenticator }

func (v statusPageSessionValidator) ValidateStatusPageSession(token, workspaceID string) bool {
	if v.auth == nil || token == "" {
		return false
	}
	user, err := v.auth.Authenticate(token)
	if err != nil || user == nil {
		return false
	}
	return statusPageWorkspace(user.Workspace) == statusPageWorkspace(workspaceID)
}

func statusPageWorkspace(workspace string) string {
	return workspaceOrDefault(workspace)
}

// workspaceOrDefault maps the empty workspace to "default", as storage does.
func workspaceOrDefault(workspace string) string {
	if workspace == "" {
		return "default"
	}
	return workspace
}

// StatusPage methods

func (a *restStorageAdapter) GetStatusPageNoCtx(id string) (*core.StatusPage, error) {
	return a.store.GetStatusPageNoCtx(id)
}

func (a *restStorageAdapter) ListStatusPagesNoCtx() ([]*core.StatusPage, error) {
	return a.store.ListStatusPagesNoCtx()
}

func (a *restStorageAdapter) SaveStatusPageNoCtx(page *core.StatusPage) error {
	if a.replicator != nil {
		key := fmt.Sprintf("%s/statuspages/%s", page.WorkspaceID, page.ID)
		data, err := json.Marshal(page)
		if err != nil {
			return err
		}
		return a.applyMutation(core.FSMCommand{Op: core.FSMSet, Key: key, Value: data})
	}
	return a.store.SaveStatusPageNoCtx(page)
}

func (a *restStorageAdapter) DeleteStatusPageNoCtx(id string) error {
	page, err := a.store.GetStatusPageNoCtx(id)
	if err != nil {
		return err
	}
	if a.replicator != nil {
		return a.applyMutation(core.FSMCommand{Op: core.FSMDelete, Key: fmt.Sprintf("%s/statuspages/%s", page.WorkspaceID, id)})
	}
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
	if a.replicator != nil {
		key := fmt.Sprintf("%s/journeys/%s", journey.WorkspaceID, journey.ID)
		data, err := json.Marshal(journey)
		if err != nil {
			return err
		}
		return a.applyMutation(core.FSMCommand{Op: core.FSMSet, Key: key, Value: data})
	}
	if err := a.store.SaveJourneyNoCtx(journey); err != nil {
		return err
	}
	if a.onJourneyChange != nil {
		a.onJourneyChange()
	}
	return nil
}

func (a *restStorageAdapter) DeleteJourneyNoCtx(id string) error {
	journey, err := a.store.GetJourneyNoCtx(id)
	if err != nil {
		return err
	}
	if a.replicator != nil {
		return a.applyMutation(core.FSMCommand{Op: core.FSMDelete, Key: fmt.Sprintf("%s/journeys/%s", journey.WorkspaceID, id)})
	}
	if err := a.store.DeleteJourneyNoCtx(id); err != nil {
		return err
	}
	if a.onJourneyChange != nil {
		a.onJourneyChange()
	}
	return nil
}

// GetSystemConfig reads a system config entry from the local store, which
// replicated writes keep current on every node.
func (a *restStorageAdapter) GetSystemConfig(ctx context.Context, key string) ([]byte, error) {
	return a.store.GetSystemConfig(ctx, key)
}

// SaveSystemConfig replicates the entry in cluster mode (same key the store
// uses) so, e.g., dashboard settings agree across nodes.
func (a *restStorageAdapter) SaveSystemConfig(ctx context.Context, key string, value []byte) error {
	if a.replicator != nil {
		return a.applyMutation(core.FSMCommand{Op: core.FSMSet, Key: "system/config/" + key, Value: value})
	}
	return a.store.SaveSystemConfig(ctx, key, value)
}

func (a *restStorageAdapter) GetDashboardNoCtx(id string) (*core.CustomDashboard, error) {
	return a.store.GetDashboardNoCtx(id)
}

func (a *restStorageAdapter) ListDashboardsNoCtx() ([]*core.CustomDashboard, error) {
	return a.store.ListDashboardsNoCtx()
}

func (a *restStorageAdapter) SaveDashboardNoCtx(dashboard *core.CustomDashboard) error {
	if a.replicator != nil {
		key := fmt.Sprintf("%s/dashboards/%s", dashboard.WorkspaceID, dashboard.ID)
		data, err := json.Marshal(dashboard)
		if err != nil {
			return err
		}
		return a.applyMutation(core.FSMCommand{Op: core.FSMSet, Key: key, Value: data})
	}
	return a.store.SaveDashboardNoCtx(dashboard)
}

func (a *restStorageAdapter) DeleteDashboardNoCtx(id string) error {
	dashboard, err := a.store.GetDashboardNoCtx(id)
	if err != nil {
		return nil // not found — idempotent
	}
	if a.replicator != nil {
		return a.applyMutation(core.FSMCommand{Op: core.FSMDelete, Key: fmt.Sprintf("%s/dashboards/%s", dashboard.WorkspaceID, id)})
	}
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
	if a.replicator != nil {
		key := fmt.Sprintf("%s/maintenance/%s", w.WorkspaceID, w.ID)
		data, err := json.Marshal(w)
		if err != nil {
			return err
		}
		return a.applyMutation(core.FSMCommand{Op: core.FSMSet, Key: key, Value: data})
	}
	return a.store.SaveMaintenanceWindow(w)
}

func (a *restStorageAdapter) DeleteMaintenanceWindow(id string) error {
	w, err := a.store.GetMaintenanceWindow(id)
	if err != nil {
		return err
	}
	if a.replicator != nil {
		return a.applyMutation(core.FSMCommand{Op: core.FSMDelete, Key: fmt.Sprintf("%s/maintenance/%s", w.WorkspaceID, id)})
	}
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

// ClusterPeers lists this node and its Raft peers for GET /api/v1/cluster/peers.
func (a *clusterAdapter) ClusterPeers() []api.ClusterPeer {
	if a.mgr == nil {
		return nil
	}
	members := a.mgr.Members()
	peers := make([]api.ClusterPeer, 0, len(members))
	for _, member := range members {
		lastContact := ""
		if !member.LastContact.IsZero() {
			lastContact = member.LastContact.UTC().Format(time.RFC3339)
		}
		peers = append(peers, api.ClusterPeer{
			ID:          member.ID,
			Name:        member.Name,
			Address:     member.Address,
			State:       member.State,
			LastContact: lastContact,
			IsLeader:    member.IsLeader,
			Region:      member.Region,
		})
	}
	return peers
}

func (a *clusterAdapter) ApplyMutation(cmd core.FSMCommand, timeout time.Duration) (uint64, uint64, error) {
	if a.mgr == nil {
		return 0, 0, &core.RaftError{Code: core.ErrNotLeader, Message: "cluster manager not available"}
	}
	return a.mgr.ApplyMutation(cmd, timeout)
}

// AddNode adds a voting member at runtime (POST /api/v1/cluster/nodes).
func (a *clusterAdapter) AddNode(id, address, region string) error {
	if a.mgr == nil {
		return api.ErrClusterDisabled
	}
	return clusterMembershipError(a.mgr.AddNode(core.RaftPeer{ID: id, Address: address, Region: region, Role: core.RoleVoter}))
}

// AddNodeByAddress adds the node at a Raft address as a voter, learning its
// node ID from it (POST /api/v1/cluster/nodes without node_id).
func (a *clusterAdapter) AddNodeByAddress(address, region string) (string, string, error) {
	if a.mgr == nil {
		return "", "", api.ErrClusterDisabled
	}
	peer, err := a.mgr.AddNodeByAddress(address, region)
	if errors.Is(err, cluster.ErrIdentifyFailed) {
		return "", "", fmt.Errorf("%w: %v", api.ErrClusterNodeUnreachable, err)
	}
	return peer.ID, peer.Region, clusterMembershipError(err)
}

// RemoveNode removes a member at runtime (DELETE /api/v1/cluster/nodes/:id).
func (a *clusterAdapter) RemoveNode(id string) error {
	if a.mgr == nil {
		return api.ErrClusterDisabled
	}
	return clusterMembershipError(a.mgr.RemoveNode(id))
}

// clusterMembershipError translates cluster membership errors into the api
// package's, which it maps onto HTTP statuses.
func clusterMembershipError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, cluster.ErrNotClustered):
		return api.ErrClusterDisabled
	case errors.Is(err, cluster.ErrNodeExists):
		return api.ErrClusterNodeExists
	case errors.Is(err, cluster.ErrNodeUnknown):
		return api.ErrClusterNodeAbsent
	case errors.Is(err, cluster.ErrRemoveLeader):
		return api.ErrClusterIsLeader
	}
	return err
}

// alertStorageAdapter adapts storage.CobaltDB to alert.AlertStorage interface
type alertStorageAdapter struct {
	store *storage.CobaltDB
	// work, when set and the cluster is running, makes alert records
	// replicate: channels and rules synchronously (they are API writes),
	// incidents and events through the batcher (they are written on the probe
	// path, which must not wait on consensus).
	work *clusterWork
}

// replicated reports whether writes must go through Raft. Before the cluster
// manager starts (configured alerts load first) records are written locally;
// every node loads the same configuration.
func (a *alertStorageAdapter) replicated() bool {
	return a.work != nil && a.work.mgr.Running()
}

func (a *alertStorageAdapter) applySync(cmd core.FSMCommand) error {
	if _, _, err := a.work.mgr.ApplyMutation(cmd, 30*time.Second); err != nil {
		return fmt.Errorf("consensus write failed: %w", err)
	}
	return nil
}

func (a *alertStorageAdapter) SaveChannel(ch *core.AlertChannel) error {
	if a.replicated() {
		data, err := json.Marshal(ch)
		if err != nil {
			return err
		}
		key := fmt.Sprintf("%s/alerts/channels/%s", workspaceOrDefault(ch.WorkspaceID), ch.ID)
		return a.applySync(core.FSMCommand{Op: core.FSMSet, Key: key, Value: data})
	}
	return a.store.SaveAlertChannel(ch)
}

func (a *alertStorageAdapter) GetChannel(id string, workspace string) (*core.AlertChannel, error) {
	return a.store.GetAlertChannel(id, workspace)
}

func (a *alertStorageAdapter) ListChannels(workspace string) ([]*core.AlertChannel, error) {
	return a.store.ListAlertChannels(workspace)
}

func (a *alertStorageAdapter) DeleteChannel(id string, workspace string) error {
	if a.replicated() {
		key := fmt.Sprintf("%s/alerts/channels/%s", workspaceOrDefault(workspace), id)
		return a.applySync(core.FSMCommand{Op: core.FSMDelete, Key: key})
	}
	return a.store.DeleteAlertChannel(id, workspace)
}

func (a *alertStorageAdapter) SaveRule(rule *core.AlertRule) error {
	if a.replicated() {
		data, err := json.Marshal(rule)
		if err != nil {
			return err
		}
		key := fmt.Sprintf("%s/alerts/rules/%s", workspaceOrDefault(rule.WorkspaceID), rule.ID)
		return a.applySync(core.FSMCommand{Op: core.FSMSet, Key: key, Value: data})
	}
	return a.store.SaveAlertRule(rule)
}

func (a *alertStorageAdapter) GetRule(id string, workspace string) (*core.AlertRule, error) {
	return a.store.GetAlertRule(id, workspace)
}

func (a *alertStorageAdapter) ListRules(workspace string) ([]*core.AlertRule, error) {
	return a.store.ListAlertRules(workspace)
}

func (a *alertStorageAdapter) DeleteRule(id string, workspace string) error {
	if a.replicated() {
		key := fmt.Sprintf("%s/alerts/rules/%s", workspaceOrDefault(workspace), id)
		return a.applySync(core.FSMCommand{Op: core.FSMDelete, Key: key})
	}
	return a.store.DeleteAlertRule(id, workspace)
}

func (a *alertStorageAdapter) SaveEvent(event *core.AlertEvent) error {
	if a.replicated() {
		if event.ID == "" {
			event.ID = core.GenerateID()
		}
		event.WorkspaceID = workspaceOrDefault(event.WorkspaceID)
		key := fmt.Sprintf("%s/alerts/events/%s/%d/%s", event.WorkspaceID, event.SoulID, event.Timestamp.UnixNano(), event.ID)
		return a.work.replicateAsync(key, event)
	}
	return a.store.SaveAlertEvent(event)
}

func (a *alertStorageAdapter) ListEvents(soulID string, limit int) ([]*core.AlertEvent, error) {
	return a.store.ListAlertEvents(soulID, limit)
}

func (a *alertStorageAdapter) SaveIncident(incident *core.Incident) error {
	if a.replicated() {
		incident.WorkspaceID = workspaceOrDefault(incident.WorkspaceID)
		key := fmt.Sprintf("%s/alerts/incidents/%s", incident.WorkspaceID, incident.ID)
		return a.work.replicateAsync(key, incident)
	}
	return a.store.SaveIncident(incident)
}

// SaveIncidentDurable is used for operator actions (acknowledge, resolve):
// unlike SaveIncident it returns only once the change is committed, so the
// API never reports an ack that a later batch failure loses
// (alert.DurableIncidentStore).
func (a *alertStorageAdapter) SaveIncidentDurable(incident *core.Incident) error {
	if !a.replicated() {
		return a.store.SaveIncident(incident)
	}
	incident.WorkspaceID = workspaceOrDefault(incident.WorkspaceID)
	data, err := json.Marshal(incident)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("%s/alerts/incidents/%s", incident.WorkspaceID, incident.ID)
	return a.applySync(core.FSMCommand{Op: core.FSMSet, Key: key, Value: data})
}

// SaveDedupState shares a notification dedup record across the cluster, so
// the node that takes a soul over keeps honouring the rule cooldown (see
// alert.DedupStateStore). Standalone dedup state stays in memory, as before.
func (a *alertStorageAdapter) SaveDedupState(workspaceID string, entry *core.AlertHistoryEntry) error {
	if a.work == nil {
		return nil
	}
	return a.work.replicateAsync(dedupStateKey(workspaceID, entry.Key), entry)
}

// DeleteDedupState shares the release of a dedup record.
func (a *alertStorageAdapter) DeleteDedupState(workspaceID, key string) error {
	if a.work == nil {
		return nil
	}
	a.work.batcher.Enqueue(core.FSMCommand{Op: core.FSMDelete, Key: dedupStateKey(workspaceID, key), Origin: a.work.mgr.NodeID()})
	return nil
}

// ListDedupState returns the shared dedup records of every workspace.
func (a *alertStorageAdapter) ListDedupState() ([]*core.AlertHistoryEntry, error) {
	if a.work == nil || a.store == nil {
		return nil, nil
	}
	var entries []*core.AlertHistoryEntry
	for _, ws := range allWorkspaceIDs(context.Background(), a.store, nil, "dedup state") {
		records, err := a.store.PrefixScan(dedupStateKey(ws, ""))
		if err != nil {
			return nil, err
		}
		for _, data := range records {
			var entry core.AlertHistoryEntry
			if json.Unmarshal(data, &entry) == nil && entry.Key != "" {
				entries = append(entries, &entry)
			}
		}
	}
	return entries, nil
}

func dedupStateKey(workspaceID, key string) string {
	return fmt.Sprintf("%s/alerts/dedup/%s", workspaceOrDefault(workspaceID), key)
}

func (a *alertStorageAdapter) GetIncident(id string) (*core.Incident, error) {
	return a.store.GetIncident(id)
}

func (a *alertStorageAdapter) ListActiveIncidents() ([]*core.Incident, error) {
	return a.store.ListActiveIncidents()
}

func (a *alertStorageAdapter) ListMaintenanceWindows() ([]*core.MaintenanceWindow, error) {
	return a.store.ListMaintenanceWindows()
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

// ListMaintenanceWindows lets public pages show souls under maintenance as
// embalmed (statuspage.MaintenanceSource).
func (r *statusPageRepository) ListMaintenanceWindows() ([]*core.MaintenanceWindow, error) {
	return r.store.ListMaintenanceWindows()
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

	// Unpadded decimal timestamp keys do not sort chronologically.
	var latest *core.Judgment
	for _, data := range results {
		if data == nil {
			continue
		}
		var j core.Judgment
		if err := json.Unmarshal(data, &j); err != nil {
			continue
		}
		if latest == nil || j.Timestamp.After(latest.Timestamp) {
			latest = &j
		}
	}

	if latest == nil {
		return nil, &core.NotFoundError{Entity: "judgment", ID: soulID}
	}

	return latest, nil
}

// compactionRetentionConflicts reports each resolution whose retention ends
// before compaction can roll its buckets up. A bucket is rolled up once its
// whole span is past the compaction threshold, so the source level must be
// kept for at least threshold + one target bucket, or the rollup is built
// from a partly deleted bucket.
func compactionRetentionConflicts(tsCfg core.TimeSeriesConfig) []string {
	c, r := tsCfg.Compaction, tsCfg.Retention
	levels := []struct {
		src, tgt  string
		threshold time.Duration
		retention time.Duration
		span      time.Duration
	}{
		{"1min", "5min", c.MinuteToFive.Duration, r.Minute.Duration, 5 * time.Minute},
		{"5min", "1hour", c.FiveToHour.Duration, r.FiveMin.Duration, time.Hour},
		{"1hour", "1day", c.HourToDay.Duration, r.Hour.Duration, 24 * time.Hour},
	}
	var conflicts []string
	for _, l := range levels {
		if l.threshold <= 0 || l.retention <= 0 {
			continue // compaction disabled, or the level is kept forever
		}
		if l.retention < l.threshold+l.span {
			conflicts = append(conflicts, fmt.Sprintf(
				"%s summaries are kept %s but rolled up into %s only after %s; keep them at least %s",
				l.src, l.retention, l.tgt, l.threshold, l.threshold+l.span))
		}
	}
	return conflicts
}

// warnUnimplementedConfig flags config sections that parse but have no
// effect yet, so operators do not rely on them.
func warnUnimplementedConfig(cfg *core.Config, logger *slog.Logger) {
	if cfg.Tenants.Enabled {
		logger.Warn("tenants.* is not enforced yet: workspace quotas and isolation settings are ignored")
	}
	if cfg.Dashboard.Branding.Logo != "" || (cfg.Dashboard.Branding.Title != "" && cfg.Dashboard.Branding.Title != "AnubisWatch") {
		logger.Warn("dashboard.branding is not applied to the dashboard yet")
	}
	if len(cfg.Verdicts.Escalation) > 0 {
		logger.Warn("verdicts.escalation is ignored; set escalation on individual alert rules instead")
	}
	if cfg.Necropolis.Distribution.Redundancy > 1 {
		logger.Warn("necropolis.distribution.redundancy is ignored: each soul is probed by exactly one node")
	}
}
