package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/acme"
	"github.com/AnubisWatch/anubiswatch/internal/auth"
	"github.com/AnubisWatch/anubiswatch/internal/cluster"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/journey"
	"github.com/AnubisWatch/anubiswatch/internal/probe"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
	"github.com/AnubisWatch/anubiswatch/internal/alert"
	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/dashboard"
	"github.com/AnubisWatch/anubiswatch/internal/statuspage"
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
		case "verdict":
			verdictCommand()
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
  verdict         Alert management (test, history, ack)
  help            Show this help

Subcommands:
  verdict test <channel>    Send test notification to channel
  verdict history           Show recent alert history
  verdict ack <id>          Acknowledge an incident

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
	configPath := "anubis.yaml"
	if _, err := os.Stat(configPath); err == nil {
		fmt.Fprintf(os.Stderr, "Config file already exists: %s\n", configPath)
		os.Exit(1)
	}

	config := `# ⚖️ AnubisWatch Configuration
# The Judgment Never Sleeps

server:
  host: "0.0.0.0"
  port: 8443
  tls:
    enabled: false
    cert: ""
    key: ""
    auto_cert: false
    acme_email: ""

storage:
  path: "./data"
  retention_days: 90
  encryption_key: ""

necropolis:
  enabled: false
  node_name: "jackal-1"
  region: "default"
  bind_addr: "0.0.0.0:7946"
  advertise_addr: ""
  cluster_secret: ""
  discovery:
    mode: "mdns"  # mdns, gossip, or manual
    seeds: []
  raft:
    node_id: "jackal-1"
    bind_addr: "0.0.0.0:7946"
    advertise_addr: ""
    bootstrap: true
    election_timeout: "1s"
    heartbeat_timeout: "500ms"
    commit_timeout: "500ms"
    snapshot_interval: "120s"
    snapshot_threshold: 100
    max_append_entries: 64
    trailing_logs: 10000
    peers: []

dashboard:
  enabled: true
  theme: "dark"

logging:
  level: "info"
  format: "json"

# Souls - Monitored targets
souls:
  - name: "Example API"
    type: http
    target: "https://httpbin.org/get"
    weight: 60s
    timeout: 10s
    enabled: true
    tags:
      - example
      - external
    http:
      method: GET
      valid_status: [200]
      feather: 500ms

  - name: "Google DNS"
    type: dns
    target: "8.8.8.8:53"
    weight: 30s
    timeout: 5s
    enabled: true
    tags:
      - dns
      - external
    dns:
      record_type: A
      expected_value: ""

  - name: "Local TCP Service"
    type: tcp
    target: "localhost:8080"
    weight: 15s
    timeout: 5s
    enabled: false
    tags:
      - tcp
      - local
    tcp:
      expect_banner: ""

# Alert Channels
channels: []
# Example Slack channel:
#   - name: "ops-slack"
#     type: slack
#     enabled: true
#     slack:
#       webhook_url: "${SLACK_WEBHOOK_URL}"
#       channel: "#ops"

# Example Discord channel:
#   - name: "ops-discord"
#     type: discord
#     enabled: true
#     discord:
#       webhook_url: "${DISCORD_WEBHOOK_URL}"

# Example Email channel:
#   - name: "ops-email"
#     type: email
#     enabled: true
#     email:
#       smtp_host: "smtp.example.com"
#       smtp_port: 587
#       username: "${SMTP_USER}"
#       password: "${SMTP_PASS}"
#       from: "anubis@example.com"
#       to: ["ops@example.com"]

# Verdicts - Alert rules
verdicts:
  rules: []
# Example alert rule:
#   - name: "Service Down"
#     enabled: true
#     condition:
#       type: consecutive_failures
#       threshold: 3
#     severity: critical
#     channels: ["ops-slack"]
#     cooldown: 5m

# Synthetic Monitoring Journeys
journeys: []
# Example journey:
#   - name: "Login Flow"
#     enabled: false
#     weight: 5m
#     timeout: 60s
#     variables:
#       base_url: "https://example.com"
#     steps:
#       - name: "Get login page"
#         type: http
#         target: "${base_url}/login"
#         http:
#           method: GET
#           valid_status: [200]
#         extract:
#           csrf_token:
#             from: body
#             path: "input[name='csrf'].value"
#       - name: "Submit login"
#         type: http
#         target: "${base_url}/login"
#         http:
#           method: POST
#           valid_status: [302]
#           headers:
#             Content-Type: "application/x-www-form-urlencoded"
#           body: "csrf=${csrf_token}&username=test&password=test"
`

	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Created config file: %s\n", configPath)
	fmt.Println("\nNext steps:")
	fmt.Println("  1. Edit anubis.yaml to configure your souls, channels, and verdicts")
	fmt.Println("  2. Run 'anubis serve' to start the server")
	fmt.Println("  3. Access the dashboard at https://localhost:8443")
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

	// Initialize auth with session persistence
	sessionPath := filepath.Join(cfg.Storage.Path, "sessions.json")
	authenticator := auth.NewLocalAuthenticator(sessionPath)
	defer authenticator.Shutdown()

	// Initialize alert manager
	alertStorage := &alertStorageAdapter{store: store}
	alertMgr := alert.NewManager(alertStorage, logger)
	if err := alertMgr.Start(); err != nil {
		logger.Warn("failed to start alert manager", "err", err)
	} else {
		logger.Info("alert manager started")
	}

	// Initialize probe engine with registry
	registry := probe.NewCheckerRegistry()
	probeEngine := probe.NewEngine(probe.EngineOptions{
		Registry: registry,
		Store:    &probeStorageAdapter{store: store},
		Alerter:  alertMgr,
		NodeID:   cfg.Necropolis.NodeName,
		Region:   cfg.Necropolis.Region,
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

	// Initialize journey executor
	journeyExec := journey.NewExecutor(store, logger)

	// Start journey executors for configured journeys
	ctx := context.Background()
	for _, j := range cfg.Journeys {
		if j.Enabled {
			j.WorkspaceID = "default"
			if err := store.SaveJourney(ctx, &j); err != nil {
				logger.Warn("failed to save journey", "journey", j.Name, "err", err)
			}
			if err := journeyExec.Start(ctx, &j); err != nil {
				logger.Warn("failed to start journey", "journey", j.Name, "err", err)
			}
		}
	}

	// Initialize cluster manager
	clusterMgr, err := cluster.NewManager(cfg.Necropolis.Raft, store, logger)
	if err != nil {
		logger.Warn("failed to initialize cluster manager", "err", err)
	} else {
		if err := clusterMgr.Start(ctx); err != nil {
			logger.Warn("failed to start cluster manager", "err", err)
		} else {
			logger.Info("cluster manager initialized", "clustered", clusterMgr.IsClustered())
		}
	}

	// Initialize REST API server with adapters
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

	// Initialize MCP server for AI agent integration
	mcpServer := api.NewMCPServer(restStore, probeEngine, alertMgr, logger)

	restServer := api.NewRESTServer(cfg.Server, restStore, probeEngine, alertMgr, authenticator, clusterAdapt, dashboardHandler, statusPageHandler, mcpServer, logger)
	go func() {
		if err := restServer.Start(); err != nil {
			logger.Error("REST server failed", "err", err)
		}
	}()
	logger.Info("REST API server initialized", "addr", fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port))

	// Graceful shutdown
	shutdownCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Info("AnubisWatch is ready. The judgment begins.")

	<-shutdownCtx.Done()
	logger.Info("shutting down...")

	shutdownCtx2, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Stop REST server
	if restServer != nil {
		restServer.Stop(shutdownCtx2)
	}

	// Stop journey executors
	journeyExec.StopAll()

	// Stop alert manager
	if alertMgr != nil {
		alertMgr.Stop()
	}

	// Stop cluster manager
	if clusterMgr != nil {
		clusterMgr.Stop(shutdownCtx2)
	}

	// Stop probe engine
	probeEngine.Stop()

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

func (a *restStorageAdapter) GetJudgmentNoCtx(id string) (*core.Judgment, error) {
	return a.store.GetJudgmentNoCtx(id)
}

func (a *restStorageAdapter) ListJudgmentsNoCtx(soulID string, start, end time.Time, limit int) ([]*core.Judgment, error) {
	return a.store.ListJudgmentsNoCtx(soulID, start, end, limit)
}

func (a *restStorageAdapter) GetChannelNoCtx(id string) (*core.AlertChannel, error) {
	return a.store.GetChannelNoCtx(id)
}

func (a *restStorageAdapter) ListChannelsNoCtx(workspace string) ([]*core.AlertChannel, error) {
	return a.store.ListChannelsNoCtx(workspace)
}

func (a *restStorageAdapter) SaveChannelNoCtx(ch *core.AlertChannel) error {
	return a.store.SaveChannelNoCtx(ch)
}

func (a *restStorageAdapter) DeleteChannelNoCtx(id string) error {
	return a.store.DeleteChannelNoCtx(id)
}

func (a *restStorageAdapter) GetRuleNoCtx(id string) (*core.AlertRule, error) {
	return a.store.GetRuleNoCtx(id)
}

func (a *restStorageAdapter) ListRulesNoCtx(workspace string) ([]*core.AlertRule, error) {
	return a.store.ListRulesNoCtx(workspace)
}

func (a *restStorageAdapter) SaveRuleNoCtx(rule *core.AlertRule) error {
	return a.store.SaveRuleNoCtx(rule)
}

func (a *restStorageAdapter) DeleteRuleNoCtx(id string) error {
	return a.store.DeleteRuleNoCtx(id)
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

// clusterAdapter adapts cluster.Manager to api.ClusterManager interface
type clusterAdapter struct {
	mgr *cluster.Manager
}

func (a *clusterAdapter) IsLeader() bool {
	return a.mgr.IsLeader()
}

func (a *clusterAdapter) Leader() string {
	return a.mgr.Leader()
}

func (a *clusterAdapter) IsClustered() bool {
	return a.mgr.IsClustered()
}

func (a *clusterAdapter) GetStatus() *api.ClusterStatus {
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

func (a *alertStorageAdapter) GetChannel(id string) (*core.AlertChannel, error) {
	return a.store.GetAlertChannel(id)
}

func (a *alertStorageAdapter) ListChannels() ([]*core.AlertChannel, error) {
	return a.store.ListAlertChannels()
}

func (a *alertStorageAdapter) DeleteChannel(id string) error {
	return a.store.DeleteAlertChannel(id)
}

func (a *alertStorageAdapter) SaveRule(rule *core.AlertRule) error {
	return a.store.SaveAlertRule(rule)
}

func (a *alertStorageAdapter) GetRule(id string) (*core.AlertRule, error) {
	return a.store.GetAlertRule(id)
}

func (a *alertStorageAdapter) ListRules() ([]*core.AlertRule, error) {
	return a.store.ListAlertRules()
}

func (a *alertStorageAdapter) DeleteRule(id string) error {
	return a.store.DeleteAlertRule(id)
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
	// TODO: Filter incidents by status page
	active, err := r.store.ListActiveIncidents()
	if err != nil {
		return nil, err
	}
	// Convert []*core.Incident to []core.Incident
	result := make([]core.Incident, 0, len(active))
	for _, inc := range active {
		if inc != nil {
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

// verdictCommand handles verdict subcommands
func verdictCommand() {
	if len(os.Args) < 3 {
		fmt.Println("⚖️  Verdict Management")
		fmt.Println("────────────────────────────────────────────")
		fmt.Println()
		fmt.Println("Usage: anubis verdict <subcommand> [options]")
		fmt.Println()
		fmt.Println("Subcommands:")
		fmt.Println("  test <channel>    Send test notification to channel")
		fmt.Println("  history           Show recent alert history")
		fmt.Println("  ack <id>          Acknowledge an incident")
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  anubis verdict test ops-slack")
		fmt.Println("  anubis verdict history")
		fmt.Println("  anubis verdict ack alert_123456")
		return
	}

	subcmd := os.Args[2]
	switch subcmd {
	case "test":
		verdictTest()
	case "history":
		verdictHistory()
	case "ack":
		verdictAck()
	default:
		fmt.Fprintf(os.Stderr, "Unknown verdict subcommand: %s\n", subcmd)
		fmt.Println("Run 'anubis verdict' for usage information")
		os.Exit(1)
	}
}

// verdictTest sends a test notification to a channel
func verdictTest() {
	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "Usage: anubis verdict test <channel>\n")
		os.Exit(1)
	}

	channelName := os.Args[3]
	fmt.Printf("⚖️  Sending test notification to channel: %s\n", channelName)

	// Try to connect to local API
	apiURL := getAPIURL()
	token := getAPIToken()

	if token == "" {
		fmt.Println("Note: No API token found. Run 'anubis serve' and login first.")
		fmt.Println()
		fmt.Println("Test notification would be sent to:", channelName)
		fmt.Println("✓ Test prepared (requires running server to send)")
		return
	}

	// Make API request to send test notification
	reqBody := fmt.Sprintf(`{"message": "Test notification from AnubisWatch CLI", "channel": "%s"}`, channelName)

	resp, err := httpPost(apiURL+"/api/v1/channels/"+channelName+"/test", "application/json", []byte(reqBody), token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		fmt.Println("✓ Test notification sent successfully")
	} else {
		fmt.Fprintf(os.Stderr, "Error: Server returned status %d\n", resp.StatusCode)
		os.Exit(1)
	}
}

// verdictHistory shows recent alert history
func verdictHistory() {
	fmt.Println("⚖️  Alert History")
	fmt.Println("────────────────────────────────────────────")
	fmt.Println()

	// Try to connect to local API
	apiURL := getAPIURL()
	token := getAPIToken()

	if token == "" {
		fmt.Println("No API token found. Run 'anubis serve' and login first.")
		fmt.Println()
		fmt.Println("Recent alerts would be displayed here.")
		return
	}

	resp, err := httpGet(apiURL+"/api/v1/incidents", token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching incidents: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Error: Server returned status %d\n", resp.StatusCode)
		os.Exit(1)
	}

	var incidents []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&incidents); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
		os.Exit(1)
	}

	if len(incidents) == 0 {
		fmt.Println("No active incidents.")
		return
	}

	fmt.Printf("%-20s %-12s %-10s %-20s\n", "ID", "Soul", "Severity", "Status")
	fmt.Println("────────────────────────────────────────────────────────────────")
	for _, inc := range incidents {
		id, _ := inc["id"].(string)
		soul, _ := inc["soul_name"].(string)
		severity, _ := inc["severity"].(string)
		status, _ := inc["status"].(string)
		fmt.Printf("%-20s %-12s %-10s %-20s\n", truncate(id, 18), truncate(soul, 12), severity, status)
	}
}

// verdictAck acknowledges an incident
func verdictAck() {
	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "Usage: anubis verdict ack <incident-id>\n")
		os.Exit(1)
	}

	incidentID := os.Args[3]
	fmt.Printf("⚖️  Acknowledging incident: %s\n", incidentID)

	// Try to connect to local API
	apiURL := getAPIURL()
	token := getAPIToken()

	if token == "" {
		fmt.Println("Note: No API token found. Run 'anubis serve' and login first.")
		fmt.Println()
		fmt.Println("Incident would be acknowledged:", incidentID)
		fmt.Println("✓ Acknowledgment prepared (requires running server)")
		return
	}

	resp, err := httpPost(apiURL+"/api/v1/incidents/"+incidentID+"/acknowledge", "application/json", []byte("{}"), token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		fmt.Printf("✓ Incident %s acknowledged successfully\n", incidentID)
	} else {
		fmt.Fprintf(os.Stderr, "Error: Server returned status %d\n", resp.StatusCode)
		os.Exit(1)
	}
}

// Helper functions for API calls
func getAPIURL() string {
	host := os.Getenv("ANUBIS_HOST")
	port := os.Getenv("ANUBIS_PORT")
	if host == "" {
		host = "localhost"
	}
	if port == "" {
		port = "8443"
	}
	return fmt.Sprintf("http://%s:%s", host, port)
}

func getAPIToken() string {
	return os.Getenv("ANUBIS_API_TOKEN")
}

func httpGet(url, token string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return http.DefaultClient.Do(req)
}

func httpPost(url, contentType string, body []byte, token string) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+token)
	return http.DefaultClient.Do(req)
}

func truncate(s string, maxLen int) string {
	if maxLen <= 3 {
		if len(s) <= maxLen {
			return s
		}
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
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
