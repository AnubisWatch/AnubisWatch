package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/acme"
	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/auth"
	"github.com/AnubisWatch/anubiswatch/internal/cluster"
	"github.com/AnubisWatch/anubiswatch/internal/core"
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

// initConfig handles the 'init' command with full flag support
func initConfig() {
	// Parse flags
	interactive := false
	configLocation := "local" // local, user, system
	configPath := ""

	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--interactive", "-i":
			interactive = true
		case "--location", "-l":
			if i+1 < len(os.Args) {
				configLocation = os.Args[i+1]
				i++
			}
		case "--output", "-o":
			if i+1 < len(os.Args) {
				configPath = os.Args[i+1]
				i++
			}
		case "--help", "-h":
			printInitHelp()
			return
		}
	}

	// Determine config path if not explicitly set
	if configPath == "" {
		switch configLocation {
		case "user":
			configPath = getUserConfigPath()
		case "system":
			configPath = getSystemConfigPath()
		default: // local
			configPath = "./anubis.json"
		}
	}

	// Check if exists
	if _, err := os.Stat(configPath); err == nil {
		fmt.Fprintf(os.Stderr, "⚠️  Config already exists: %s\n", configPath)
		fmt.Println("   Use --output to specify a different location")
		fmt.Println("   Or delete the existing file first")
		os.Exit(1)
	}

	// Ensure directory exists
	if err := ensureConfigDir(configPath); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Cannot create config directory: %v\n", err)
		os.Exit(1)
	}

	if interactive {
		initInteractiveWithPath(configPath)
	} else {
		initSimpleWithPath(configPath)
	}
}

func printInitHelp() {
	fmt.Println("Usage: anubis init [OPTIONS]")
	fmt.Println()
	fmt.Println("Initialize a new AnubisWatch instance")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  --interactive, -i     Interactive configuration wizard")
	fmt.Println("  --location, -l        Config location: local, user, system (default: local)")
	fmt.Println("  --output, -o          Specific config file path")
	fmt.Println("  --help, -h            Show this help")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  anubis init                          # Quick local setup")
	fmt.Println("  anubis init --interactive            # Full wizard")
	fmt.Println("  anubis init -l user                  # User-wide config")
	fmt.Println("  anubis init -o /etc/anubis/prod.json # Specific path")
	fmt.Println()
	fmt.Println("Config locations:")
	fmt.Printf("  local:  ./anubis.json\n")
	fmt.Printf("  user:   %s\n", getUserConfigPath())
	fmt.Printf("  system: %s\n", getSystemConfigPath())
	fmt.Println()
	fmt.Println("Multiple instances:")
	fmt.Println("  anubis init -o ./anubis-prod.json    # Production instance")
	fmt.Println("  anubis init -o ./anubis-staging.json # Staging instance")
	fmt.Println("  ANUBIS_CONFIG=./anubis-prod.json anubis serve")
}

func serve() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: getLogLevel(),
	}))

	// Parse serve flags
	configFlag := ""
	for i, arg := range os.Args {
		if (arg == "--config" || arg == "-c") && i+1 < len(os.Args) {
			configFlag = os.Args[i+1]
			break
		}
		if strings.HasPrefix(arg, "--config=") {
			configFlag = strings.TrimPrefix(arg, "--config=")
			break
		}
	}

	// Determine config path
	configPath := configFlag
	if configPath == "" {
		configPath = findConfig()
	}

	instanceName := getInstanceName(configPath)

	logger.Info("⚖️  AnubisWatch — The Judgment Never Sleeps",
		"version", Version,
		"commit", Commit,
		"instance", instanceName,
		"config", configPath,
	)

	// Use the refactored server initialization
	opts := ServerOptions{
		ConfigPath: configPath,
		Logger:     logger,
	}

	deps, err := BuildServerDependencies(opts)
	if err != nil {
		logger.Error("failed to build server dependencies", "err", err)
		os.Exit(1)
	}

	server := NewServer(deps)

	// Start server
	ctx := context.Background()
	if err := server.Start(ctx); err != nil {
		logger.Error("failed to start server", "err", err)
		os.Exit(1)
	}

	// Wait for shutdown signal
	server.WaitForShutdown()

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Stop(shutdownCtx); err != nil {
		logger.Error("error during shutdown", "err", err)
	}
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
		fmt.Fprintf(os.Stderr, "Usage: anubis watch <target> [--name <name>] [--interval <duration>] [--type <http|tcp|icmp>]\n")
		os.Exit(1)
	}

	target := os.Args[2]
	name := target
	interval := 60 * time.Second
	checkType := "http"

	// Parse flags
	for i := 3; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--name":
			if i+1 < len(os.Args) {
				name = os.Args[i+1]
				i++
			}
		case "--interval", "-i":
			if i+1 < len(os.Args) {
				d, err := time.ParseDuration(os.Args[i+1])
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: invalid interval: %v\n", err)
					os.Exit(1)
				}
				interval = d
				i++
			}
		case "--type", "-t":
			if i+1 < len(os.Args) {
				checkType = os.Args[i+1]
				i++
			}
		}
	}

	// Determine check type from target if not specified
	if checkType == "http" && (strings.HasPrefix(target, "tcp://") || strings.HasPrefix(target, "tcp+tls://")) {
		checkType = "tcp"
	} else if checkType == "http" && strings.HasPrefix(target, "icmp://") {
		checkType = "icmp"
	}

	// Clean target prefix
	target = strings.TrimPrefix(target, "http://")
	target = strings.TrimPrefix(target, "https://")
	target = strings.TrimPrefix(target, "tcp://")
	target = strings.TrimPrefix(target, "tcp+tls://")
	target = strings.TrimPrefix(target, "icmp://")

	fmt.Printf("⚖️  Adding soul: %s (%s)\n", name, target)

	// Try API first (if server is running)
	apiURL := getAPIURL()
	token := getAPIToken()

	if token != "" {
		// Use API
		soulReq := map[string]interface{}{
			"name":        name,
			"target":      target,
			"type":        checkType,
			"interval":    interval.String(),
			"timeout":     "10s",
			"enabled":     true,
			"workspaceId": "default",
		}

		reqBody, _ := json.Marshal(soulReq)
		resp, err := httpPost(apiURL+"/api/v1/souls", "application/json", reqBody, token)
		if err == nil && resp.StatusCode == http.StatusCreated {
			resp.Body.Close()
			fmt.Println("✓ Soul added successfully via API")
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
	}

	// Fall back to direct storage access
	store, err := openLocalStorage()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot connect to API or open storage: %v\n", err)
		fmt.Println("\nMake sure AnubisWatch is running, or run from the data directory.")
		os.Exit(1)
	}
	defer store.Close()

	ctx := context.Background()

	// Determine soul type
	var soulType core.CheckType
	switch checkType {
	case "tcp":
		soulType = core.CheckTCP
	case "icmp":
		soulType = core.CheckICMP
	case "dns":
		soulType = core.CheckDNS
	case "smtp":
		soulType = core.CheckSMTP
	case "grpc":
		soulType = core.CheckGRPC
	case "websocket":
		soulType = core.CheckWebSocket
	case "tls":
		soulType = core.CheckTLS
	default:
		soulType = core.CheckHTTP
	}

	soul := &core.Soul{
		ID:          core.GenerateID(),
		Name:        name,
		Type:        soulType,
		Target:      target,
		Weight:      core.Duration{Duration: interval},
		Timeout:     core.Duration{Duration: 10 * time.Second},
		Enabled:     true,
		WorkspaceID: "default",
		Tags:        []string{"cli-added"},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := store.SaveSoul(ctx, soul); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving soul: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Soul added successfully (ID: %s)\n", soul.ID)
	fmt.Println("\nNote: The new soul will be picked up on next server restart,")
	fmt.Println("      or immediately if the server is running with hot-reload.")
}

// openLocalStorage opens storage directly for CLI operations
func openLocalStorage() (*storage.CobaltDB, error) {
	dataDir := getDataDir()

	cfg := core.StorageConfig{
		Path: dataDir,
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	return storage.NewEngine(cfg, logger)
}

func showJudgments() {
	fmt.Println("⚖️  AnubisWatch — The Judgment Never Sleeps")
	fmt.Println("────────────────────────────────────────────")
	fmt.Println()

	// Try API first
	apiURL := getAPIURL()
	token := getAPIToken()

	var souls []*core.Soul

	if token != "" {
		resp, err := httpGet(apiURL+"/api/v1/souls", token)
		if err == nil && resp.StatusCode == http.StatusOK {
			if err := json.NewDecoder(resp.Body).Decode(&souls); err != nil {
				souls = nil
			}
			resp.Body.Close()
		}
	}

	// Fall back to direct storage access
	var store *storage.CobaltDB
	if souls == nil {
		var err error
		store, err = openLocalStorage()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: cannot connect to API or open storage: %v\n", err)
			fmt.Println("\nMake sure AnubisWatch is running, or run from the data directory.")
			os.Exit(1)
		}
		defer store.Close()

		ctx := context.Background()
		souls, err = store.ListSouls(ctx, "default", 0, 100)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing souls: %v\n", err)
			os.Exit(1)
		}
	}

	if len(souls) == 0 {
		fmt.Println("No souls configured yet.")
		fmt.Println("Run 'anubis watch <target>' to add your first soul.")
		return
	}

	// Print header
	fmt.Printf("%-24s %-10s %-10s %-12s %-20s\n", "Soul", "Status", "Latency", "Region", "Last Judged")
	fmt.Println("──────────────────────── ────────── ────────── ──────────── ────────────────────")

	// Get latest judgments for each soul
	ctx := context.Background()

	for _, soul := range souls {
		status := "unknown"
		latency := "-"
		region := soul.Region
		if region == "" {
			region = "default"
		}
		lastJudged := "never"

		// Try to get latest judgment
		if store != nil {
			j, err := storageGetLatestJudgment(store, ctx, soul.WorkspaceID, soul.ID)
			if err == nil {
				status = string(j.Status)
				latency = j.Duration.String()
				if j.Region != "" {
					region = j.Region
				}
				lastJudged = time.Since(j.Timestamp).Round(time.Second).String() + " ago"
			}
		}

		// Color status (using simple text indicators)
		statusIcon := "○"
		switch status {
		case "alive":
			statusIcon = "✓"
		case "dead":
			statusIcon = "✗"
		case "degraded":
			statusIcon = "~"
		}

		name := truncate(soul.Name, 22)
		fmt.Printf("%-24s %s %-8s %-10s %-12s %-20s\n", name, statusIcon, status, latency, region, lastJudged)
	}

	fmt.Println()
	fmt.Printf("Total souls: %d\n", len(souls))
}

func summonNode() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: anubis summon <address> [--name <node-id>] [--region <region>]\n")
		os.Exit(1)
	}

	addr := os.Args[2]
	nodeID := ""
	region := "default"

	// Parse flags
	for i := 3; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--name":
			if i+1 < len(os.Args) {
				nodeID = os.Args[i+1]
				i++
			}
		case "--region", "-r":
			if i+1 < len(os.Args) {
				region = os.Args[i+1]
				i++
			}
		}
	}

	// Generate node ID from address if not provided
	if nodeID == "" {
		nodeID = strings.ReplaceAll(addr, ":", "-")
		nodeID = strings.ReplaceAll(nodeID, ".", "-")
	}

	fmt.Printf("⚖️  Summoning Jackal at %s...\n", addr)

	// Try API first
	apiURL := getAPIURL()
	token := getAPIToken()

	if token != "" {
		reqBody := fmt.Sprintf(`{"node_id": "%s", "address": "%s", "region": "%s"}`, nodeID, addr, region)
		resp, err := httpPost(apiURL+"/api/v1/cluster/nodes", "application/json", []byte(reqBody), token)
		if err == nil && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated) {
			resp.Body.Close()
			fmt.Printf("✓ Node '%s' added to cluster successfully via API\n", nodeID)
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
	}

	// Fall back to direct storage
	store, err := openLocalStorage()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening storage: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.SaveJackal(ctx, nodeID, addr, region); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving node: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Node '%s' added to cluster configuration\n", nodeID)
	fmt.Println("\nNote: Cluster changes require a server restart to take full effect.")
}

func banishNode() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: anubis banish <node-id>\n")
		os.Exit(1)
	}

	nodeID := os.Args[2]
	fmt.Printf("⚖️  Banishing Jackal %s...\n", nodeID)

	// Try API first
	apiURL := getAPIURL()
	token := getAPIToken()

	if token != "" {
		req, err := http.NewRequest("DELETE", apiURL+"/api/v1/cluster/nodes/"+nodeID, nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := http.DefaultClient.Do(req)
			if err == nil && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent) {
				resp.Body.Close()
				fmt.Printf("✓ Node '%s' removed from cluster via API\n", nodeID)
				return
			}
			if resp != nil {
				resp.Body.Close()
			}
		}
	}

	// Fall back to direct storage
	store, err := openLocalStorage()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening storage: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	// Delete the node from storage
	key := fmt.Sprintf("system/jackals/%s", nodeID)
	if err := store.Delete(key); err != nil {
		fmt.Fprintf(os.Stderr, "Error removing node: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Node '%s' removed from cluster configuration\n", nodeID)
	fmt.Println("\nNote: Cluster changes require a server restart to take full effect.")
}

func showCluster() {
	fmt.Println("⚖️  AnubisWatch Necropolis — Cluster Status")
	fmt.Println("────────────────────────────────────────────")
	fmt.Println()

	// Try API first
	apiURL := getAPIURL()
	token := getAPIToken()

	var clusterData map[string]interface{}
	useAPI := false

	if token != "" {
		resp, err := httpGet(apiURL+"/api/v1/cluster/status", token)
		if err == nil && resp.StatusCode == http.StatusOK {
			if err := json.NewDecoder(resp.Body).Decode(&clusterData); err == nil {
				useAPI = true
			}
			resp.Body.Close()
		}
	}

	if useAPI {
		// Display API data
		isClustered, _ := clusterData["is_clustered"].(bool)
		nodeID, _ := clusterData["node_id"].(string)
		state, _ := clusterData["state"].(string)
		leader, _ := clusterData["leader"].(string)
		term, _ := clusterData["term"].(float64)
		peerCount, _ := clusterData["peer_count"].(float64)

		fmt.Printf("%-16s %s\n", "Raft State:", state)
		fmt.Printf("%-16s %s\n", "Current Node:", nodeID)
		fmt.Printf("%-16s %s\n", "Current Leader:", leader)
		fmt.Printf("%-16s %.0f\n", "Term:", term)
		fmt.Printf("%-16s %.0f\n", "Peer Count:", peerCount)
		fmt.Printf("%-16s %v\n", "Clustered:", isClustered)
	} else {
		// Fall back to storage
		store, err := openLocalStorage()
		if err != nil {
			fmt.Println("Raft State:     Standalone")
			fmt.Println("Current Leader: this node")
			fmt.Println("Term:           1")
			fmt.Println("Nodes:          1")
			fmt.Println()
			fmt.Println("Jackals:")
			fmt.Println("  ID              Region    Address           Role")
			fmt.Println("  ──────────────  ────────  ────────────────  ────────")
			fmt.Println("  (this node)     default   localhost:7946    Pharaoh")
			return
		}
		defer store.Close()

		ctx := context.Background()
		jackals, err := store.ListJackals(ctx)
		if err != nil {
			jackals = make(map[string]map[string]string)
		}

		// Get Raft state if available
		currentTerm, votedFor, _ := store.GetRaftState(ctx)
		if currentTerm == 0 {
			currentTerm = 1
		}

		nodeCount := len(jackals) + 1 // +1 for this node

		fmt.Printf("%-16s %s\n", "Raft State:", "Standalone")
		fmt.Printf("%-16s %d\n", "Term:", currentTerm)
		fmt.Printf("%-16s %d\n", "Nodes:", nodeCount)
		if votedFor != "" {
			fmt.Printf("%-16s %s\n", "Voted For:", votedFor)
		}
		fmt.Println()
		fmt.Println("Jackals:")
		fmt.Println("  ID              Region    Address           Role")
		fmt.Println("  ──────────────  ────────  ────────────────  ────────")

		// Show this node
		fmt.Printf("  %-14s  %-8s  %-16s  %-8s\n", "(this node)", "default", "localhost:7946", "Pharaoh")

		// Show other nodes
		for id, node := range jackals {
			region := node["region"]
			if region == "" {
				region = "default"
			}
			address := node["address"]
			if address == "" {
				address = "unknown"
			}
			fmt.Printf("  %-14s  %-8s  %-16s  %-8s\n", truncate(id, 14), region, address, "Jackal")
		}
	}

	fmt.Println()
	fmt.Println("Use 'anubis summon <address>' to add nodes to the cluster.")
}

func selfHealth() {
	// Check critical components
	checks := map[string]interface{}{
		"memory": checkMemory(),
		"uptime": time.Since(startTime).String(),
	}

	// Check HTTPS endpoint if configured
	if port := os.Getenv("ANUBIS_PORT"); port != "" {
		checks["https_port"] = port
	} else {
		checks["https_port"] = "8443"
	}

	// Check data directory accessibility
	dataDir := getDataDir()
	if info, err := os.Stat(dataDir); err == nil && info.IsDir() {
		checks["data_dir"] = "accessible"
	} else {
		checks["data_dir"] = "inaccessible"
	}

	response := map[string]interface{}{
		"status":  "healthy",
		"version": Version,
		"checks":  checks,
	}

	data, _ := json.MarshalIndent(response, "", "  ")
	fmt.Println(string(data))
}

var startTime = time.Now()

func checkMemory() map[string]interface{} {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return map[string]interface{}{
		"alloc_mb":   m.Alloc / 1024 / 1024,
		"sys_mb":     m.Sys / 1024 / 1024,
		"num_gc":     m.NumGC,
		"goroutines": runtime.NumGoroutine(),
	}
}

func getDataDir() string {
	if dir := os.Getenv("ANUBIS_DATA_DIR"); dir != "" {
		return dir
	}
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "anubis")
	default:
		return filepath.Join("/var", "lib", "anubis")
	}
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
