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
	"github.com/AnubisWatch/anubiswatch/internal/backup"
	"github.com/AnubisWatch/anubiswatch/internal/cluster"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/probe"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
	"gopkg.in/yaml.v3"
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
			judgeCommand()
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
		case "backup":
			backupCommand()
		case "restore":
			restoreCommand()
		case "status":
			statusCommand()
		case "export":
			exportCommand()
		case "logs":
			logsCommand()
		case "config":
			configCommand()
		case "souls":
			soulsCommand()
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
  judge <name>    Force-check a specific soul
  judge --all     Force-check all souls now
  summon <addr>   Add node to cluster
  banish <id>     Remove node from cluster
  necropolis      Show cluster status
  version         Show version information
  health          Self health check
  backup          Create or manage backups
  restore         Restore from backup
  status          Show detailed system status
  export          Export configuration to JSON/YAML
  logs            View recent logs
  config          Configuration management
  souls           Souls management
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

func (a *restStorageAdapter) DeleteSoulNoCtx(id string) error {
	return a.store.DeleteSoul(context.Background(), "default", id)
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

// parseConfigValue converts a string value to an appropriate type for config
func parseConfigValue(s string) interface{} {
	// Try bool
	switch strings.ToLower(s) {
	case "true":
		return true
	case "false":
		return false
	}
	// Try int
	var i int
	if _, err := fmt.Sscanf(s, "%d", &i); err == nil && fmt.Sprintf("%d", i) == s {
		return i
	}
	// Try float
	var f float64
	if _, err := fmt.Sscanf(s, "%f", &f); err == nil && fmt.Sprintf("%g", f) == s {
		return f
	}
	// Default: string
	return s
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

// backupCommand handles backup subcommands
func backupCommand() {
	if len(os.Args) < 3 {
		fmt.Println("⚖️  Backup Management")
		fmt.Println("────────────────────────────────────────────")
		fmt.Println()
		fmt.Println("Usage: anubis backup <subcommand> [options]")
		fmt.Println()
		fmt.Println("Subcommands:")
		fmt.Println("  create              Create a new backup")
		fmt.Println("  list                List available backups")
		fmt.Println("  delete <file>       Delete a backup file")
		fmt.Println("  info <file>         Show backup information")
		fmt.Println()
		fmt.Println("Options for create:")
		fmt.Println("  --compress          Compress the backup (default: true)")
		fmt.Println("  --no-compress       Do not compress the backup")
		fmt.Println("  --include-history   Include judgment history")
		fmt.Println("  --output <path>     Custom output path")
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  anubis backup create")
		fmt.Println("  anubis backup list")
		fmt.Println("  anubis backup info anubis_backup_20260115_143022.json.gz")
		fmt.Println("  anubis backup delete anubis_backup_20260115_143022.json.gz")
		return
	}

	subcmd := os.Args[2]
	switch subcmd {
	case "create":
		backupCreate()
	case "list":
		backupList()
	case "delete":
		backupDelete()
	case "info":
		backupInfo()
	default:
		fmt.Fprintf(os.Stderr, "Unknown backup subcommand: %s\n", subcmd)
		fmt.Println("Run 'anubis backup' for usage information")
		os.Exit(1)
	}
}

// backupCreate creates a new backup
func backupCreate() {
	compress := true
	includeHistory := false
	outputPath := ""

	// Parse flags
	for i := 3; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--no-compress":
			compress = false
		case "--compress":
			compress = true
		case "--include-history":
			includeHistory = true
		case "--output", "-o":
			if i+1 < len(os.Args) {
				outputPath = os.Args[i+1]
				i++
			}
		}
	}

	fmt.Println("⚖️  Creating backup...")

	// Open storage
	store, err := openLocalStorage()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening storage: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	// Create backup manager
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	mgr := backup.NewManager(store, getDataDir(), logger)
	if err := mgr.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing backup manager: %v\n", err)
		os.Exit(1)
	}

	opts := backup.Options{
		Compress:         compress,
		IncludeJudgments: includeHistory,
		JudgmentDays:     7,
	}

	ctx := context.Background()
	backup, path, err := mgr.Create(ctx, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating backup: %v\n", err)
		os.Exit(1)
	}

	// Move to custom path if specified
	if outputPath != "" {
		if err := os.Rename(path, outputPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error moving backup to output path: %v\n", err)
			os.Exit(1)
		}
		path = outputPath
	}

	fmt.Println("✓ Backup created successfully")
	fmt.Println()
	fmt.Printf("File:      %s\n", filepath.Base(path))
	fmt.Printf("Path:      %s\n", path)
	fmt.Printf("Size:      %s\n", formatBytes(getFileSize(path)))
	fmt.Printf("Version:   %s\n", backup.Version)
	fmt.Printf("Created:   %s\n", backup.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Println()
	fmt.Println("Backup Contents:")
	fmt.Printf("  Workspaces:     %d\n", backup.Metadata.Workspaces)
	fmt.Printf("  Souls:          %d\n", backup.Metadata.Souls)
	fmt.Printf("  Alert Channels: %d\n", backup.Metadata.AlertChannels)
	fmt.Printf("  Alert Rules:    %d\n", backup.Metadata.AlertRules)
	fmt.Printf("  Status Pages:   %d\n", backup.Metadata.StatusPages)
	fmt.Printf("  Journeys:       %d\n", backup.Metadata.Journeys)
}

// backupList lists available backups
func backupList() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	mgr := backup.NewManager(nil, getDataDir(), logger)
	if err := mgr.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing backup manager: %v\n", err)
		os.Exit(1)
	}

	backups, err := mgr.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing backups: %v\n", err)
		os.Exit(1)
	}

	if len(backups) == 0 {
		fmt.Println("No backups found.")
		fmt.Println()
		fmt.Println("Create a backup with: anubis backup create")
		return
	}

	fmt.Println("⚖️  Available Backups")
	fmt.Println("────────────────────────────────────────────────────────────")
	fmt.Println()
	fmt.Printf("%-40s %-12s %-20s\n", "Filename", "Size", "Created")
	fmt.Println("────────────────────────────────────────────────────────────")

	for _, b := range backups {
		fmt.Printf("%-40s %-12s %-20s\n",
			truncate(b.Filename, 38),
			formatBytes(b.Size),
			b.CreatedAt.Format("2006-01-02 15:04"))
	}

	fmt.Println()
	fmt.Printf("Total: %d backups\n", len(backups))
}

// backupDelete deletes a backup file
func backupDelete() {
	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "Usage: anubis backup delete <filename>\n")
		os.Exit(1)
	}

	filename := os.Args[3]

	fmt.Printf("⚖️  Deleting backup: %s\n", filename)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	mgr := backup.NewManager(nil, getDataDir(), logger)
	if err := mgr.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing backup manager: %v\n", err)
		os.Exit(1)
	}

	if err := mgr.Delete(filename); err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting backup: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✓ Backup deleted successfully")
}

// backupInfo shows backup information
func backupInfo() {
	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "Usage: anubis backup info <filename>\n")
		os.Exit(1)
	}

	filename := os.Args[3]

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	mgr := backup.NewManager(nil, getDataDir(), logger)
	if err := mgr.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing backup manager: %v\n", err)
		os.Exit(1)
	}

	b, err := mgr.Get(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading backup: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("⚖️  Backup Information")
	fmt.Println("────────────────────────────────────────────")
	fmt.Println()
	fmt.Printf("File:      %s\n", filename)
	fmt.Printf("Version:   %s\n", b.Version)
	fmt.Printf("Type:      %s\n", b.BackupType)
	fmt.Printf("Created:   %s\n", b.CreatedAt.Format("2006-01-02 15:04:05 UTC"))
	fmt.Printf("Checksum:  %s...\n", b.Checksum[:16])
	fmt.Println()
	fmt.Println("Contents:")
	fmt.Printf("  Workspaces:     %d\n", b.Metadata.Workspaces)
	fmt.Printf("  Souls:          %d\n", b.Metadata.Souls)
	fmt.Printf("  Alert Channels: %d\n", b.Metadata.AlertChannels)
	fmt.Printf("  Alert Rules:    %d\n", b.Metadata.AlertRules)
	fmt.Printf("  Status Pages:   %d\n", b.Metadata.StatusPages)
	fmt.Printf("  Journeys:       %d\n", b.Metadata.Journeys)
}

// restoreCommand handles restore operations
func restoreCommand() {
	if len(os.Args) < 3 {
		fmt.Println("⚖️  Restore from Backup")
		fmt.Println("────────────────────────────────────────────")
		fmt.Println()
		fmt.Println("Usage: anubis restore <backup-file> [options]")
		fmt.Println()
		fmt.Println("Options:")
		fmt.Println("  --skip-souls        Do not restore souls")
		fmt.Println("  --skip-alerts       Do not restore alert channels/rules")
		fmt.Println("  --skip-status-pages Do not restore status pages")
		fmt.Println("  --skip-journeys     Do not restore journeys")
		fmt.Println("  --force             Continue on errors")
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  anubis restore anubis_backup_20260115_143022.json.gz")
		fmt.Println("  anubis restore backup.json --skip-alerts")
		fmt.Println()
		fmt.Println("⚠️  Warning: Restore will overwrite existing data!")
		return
	}

	backupFile := os.Args[2]

	skipSouls := false
	skipAlerts := false
	skipStatusPages := false
	skipJourneys := false
	force := false

	// Parse flags
	for i := 3; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--skip-souls":
			skipSouls = true
		case "--skip-alerts":
			skipAlerts = true
		case "--skip-status-pages":
			skipStatusPages = true
		case "--skip-journeys":
			skipJourneys = true
		case "--force":
			force = true
		}
	}

	fmt.Println("⚖️  Restore from Backup")
	fmt.Println("────────────────────────────────────────────")
	fmt.Println()
	fmt.Printf("Backup file: %s\n", backupFile)
	fmt.Println()

	// Verify backup file exists
	if _, err := os.Stat(backupFile); err != nil {
		fmt.Fprintf(os.Stderr, "Error: Backup file not found: %s\n", backupFile)
		os.Exit(1)
	}

	// Confirm with user
	if !force {
		fmt.Println("⚠️  Warning: This will overwrite existing data!")
		fmt.Print("Continue? [y/N]: ")
		var response string
		fmt.Scanln(&response)
		if response != "y" && response != "Y" {
			fmt.Println("Restore cancelled.")
			return
		}
		fmt.Println()
	}

	// Open storage
	store, err := openLocalStorage()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening storage: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	// Create backup manager
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	mgr := backup.NewManager(nil, getDataDir(), logger)

	opts := backup.RestoreOptions{
		IncludeWorkspaces:   true,
		IncludeSouls:        !skipSouls,
		IncludeAlerts:       !skipAlerts,
		IncludeStatusPages:  !skipStatusPages,
		IncludeJourneys:     !skipJourneys,
		IncludeSystemConfig: true,
		ContinueOnError:     force,
	}

	ctx := context.Background()

	fmt.Println("Restoring...")
	if err := mgr.Restore(ctx, store, backupFile, opts); err != nil {
		fmt.Fprintf(os.Stderr, "Error during restore: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("✓ Restore completed successfully")
	fmt.Println()
	fmt.Println("Note: Some changes may require a server restart to take full effect.")
}

// formatBytes formats byte size to human-readable string
func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// getFileSize returns the size of a file
func getFileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// statusCommand shows detailed system status
func statusCommand() {
	fmt.Println("⚖️  AnubisWatch System Status")
	fmt.Println("────────────────────────────────────────────")
	fmt.Println()

	// Version info
	fmt.Printf("Version:    %s\n", Version)
	fmt.Printf("Commit:     %s\n", Commit)
	fmt.Printf("Build Date: %s\n", BuildDate)
	fmt.Printf("Go Version: %s\n", getGoVersion())
	fmt.Println()

	// Data directory
	dataDir := getDataDir()
	fmt.Printf("Data Directory: %s\n", dataDir)

	// Check if data directory exists
	if info, err := os.Stat(dataDir); err == nil {
		fmt.Printf("  Status:   ✓ accessible\n")
		fmt.Printf("  Size:     %s\n", formatBytes(dirSize(dataDir)))
		fmt.Printf("  Modified: %s\n", info.ModTime().Format("2006-01-02 15:04:05"))
	} else {
		fmt.Printf("  Status:   ✗ not accessible (%v)\n", err)
	}
	fmt.Println()

	// Try to open storage for detailed stats
	store, err := openLocalStorage()
	if err != nil {
		fmt.Println("Storage:    Not accessible (server may not be running)")
		fmt.Println()
		fmt.Println("Run 'anubis serve' to start the server.")
		return
	}
	defer store.Close()

	ctx := context.Background()

	// Count souls
	souls, err := store.ListSouls(ctx, "default", 0, 10000)
	if err != nil {
		souls = []*core.Soul{}
	}

	alive := 0
	dead := 0
	degraded := 0
	for _, soul := range souls {
		j, err := storageGetLatestJudgment(store, ctx, soul.WorkspaceID, soul.ID)
		if err != nil {
			continue
		}
		switch j.Status {
		case core.SoulAlive:
			alive++
		case core.SoulDead:
			dead++
		case core.SoulDegraded:
			degraded++
		}
	}

	fmt.Println("Souls:")
	fmt.Printf("  Total:     %d\n", len(souls))
	fmt.Printf("  ✓ Alive:   %d\n", alive)
	fmt.Printf("  ✗ Dead:    %d\n", dead)
	fmt.Printf("  ~ Degraded: %d\n", degraded)
	fmt.Println()

	// Count workspaces
	workspaces, err := store.ListWorkspaces(ctx)
	if err != nil {
		workspaces = []*core.Workspace{}
	}
	fmt.Printf("Workspaces: %d\n", len(workspaces))

	// Count alert channels and rules
	channels, _ := store.ListAlertChannels()
	rules, _ := store.ListAlertRules()
	fmt.Printf("Alerts:     %d channels, %d rules\n", len(channels), len(rules))

	// Count status pages
	pages, _ := store.ListStatusPages()
	fmt.Printf("Status Pages: %d\n", len(pages))

	// Memory stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Println()
	fmt.Println("Memory:")
	fmt.Printf("  Allocated: %s\n", formatBytes(int64(m.Alloc)))
	fmt.Printf("  System:    %s\n", formatBytes(int64(m.Sys)))
	fmt.Printf("  GC Cycles: %d\n", m.NumGC)
	fmt.Printf("  Goroutines: %d\n", runtime.NumGoroutine())

	// Uptime
	fmt.Println()
	fmt.Printf("Uptime: %s\n", time.Since(startTime).Round(time.Second))
}

// dirSize calculates the total size of a directory
func dirSize(path string) int64 {
	var size int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size
}

// exportCommand exports configuration to various formats
func exportCommand() {
	if len(os.Args) < 3 {
		fmt.Println("⚖️  Export Configuration")
		fmt.Println("────────────────────────────────────────────")
		fmt.Println()
		fmt.Println("Usage: anubis export <subcommand>")
		fmt.Println()
		fmt.Println("Subcommands:")
		fmt.Println("  souls        Export all souls to JSON")
		fmt.Println("  config       Export server configuration")
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  anubis export souls > souls.json")
		fmt.Println("  anubis export config > config.json")
		return
	}

	subcmd := os.Args[2]

	store, err := openLocalStorage()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening storage: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	ctx := context.Background()

	switch subcmd {
	case "souls":
		souls, err := store.ListSouls(ctx, "default", 0, 10000)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing souls: %v\n", err)
			os.Exit(1)
		}

		output, err := json.MarshalIndent(souls, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error marshaling souls: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(output))

	case "config":
		// Try to find and export config
		configPath := findConfig()
		if configPath == "" {
			fmt.Fprintf(os.Stderr, "No configuration file found\n")
			os.Exit(1)
		}

		data, err := os.ReadFile(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading config: %v\n", err)
			os.Exit(1)
		}

		fmt.Println(string(data))

	default:
		fmt.Fprintf(os.Stderr, "Unknown export subcommand: %s\n", subcmd)
		fmt.Println("Run 'anubis export' for usage information")
		os.Exit(1)
	}
}

// logsCommand shows recent logs
func logsCommand() {
	lines := 50
	follow := false

	// Parse flags
	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "-n", "--lines":
			if i+1 < len(os.Args) {
				fmt.Sscanf(os.Args[i+1], "%d", &lines)
				i++
			}
		case "-f", "--follow":
			follow = true
		}
	}

	// In a real implementation, this would read from log files
	// For now, show a message
	fmt.Println("⚖️  AnubisWatch Logs")
	fmt.Println("────────────────────────────────────────────")
	fmt.Println()
	fmt.Printf("Showing last %d lines...\n", lines)
	fmt.Println()

	// Check for log file
	dataDir := getDataDir()
	logPath := filepath.Join(dataDir, "logs", "anubis.log")

	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		fmt.Printf("Log file not found at %s\n", logPath)
		fmt.Println()
		fmt.Println("Logs are written to stderr by default.")
		fmt.Println("To save logs to file, redirect stderr:")
		fmt.Println("  anubis serve 2> anubis.log")
		return
	}

	// Read log file
	file, err := os.Open(logPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening log file: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	// Read last N lines (simplified implementation)
	data, err := os.ReadFile(logPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading log file: %v\n", err)
		os.Exit(1)
	}

	// Simple implementation: print last lines
	lines_data := strings.Split(string(data), "\n")
	start := len(lines_data) - lines
	if start < 0 {
		start = 0
	}

	for i := start; i < len(lines_data); i++ {
		if lines_data[i] != "" {
			fmt.Println(lines_data[i])
		}
	}

	if follow {
		fmt.Println()
		fmt.Println("-- Follow mode not implemented in this version --")
	}
}

// configCommand handles configuration management
func configCommand() {
	if len(os.Args) < 3 {
		fmt.Println("⚖️  Configuration Management")
		fmt.Println("────────────────────────────────────────────")
		fmt.Println()
		fmt.Println("Usage: anubis config <subcommand>")
		fmt.Println()
		fmt.Println("Subcommands:")
		fmt.Println("  validate    Validate configuration file")
		fmt.Println("  show        Show current configuration")
		fmt.Println("  path        Show configuration file path")
		fmt.Println("  set <key> <value>  Set a configuration value")
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  anubis config validate")
		fmt.Println("  anubis config show")
		fmt.Println("  anubis config path")
		fmt.Println(`  anubis config set server.port 9443`)
		fmt.Println(`  anubis config set logging.level debug`)
		return
	}

	subcmd := os.Args[2]

	switch subcmd {
	case "validate":
		configPath := findConfig()
		if configPath == "" {
			fmt.Println("✗ No configuration file found")
			fmt.Println()
			fmt.Println("Create one with: anubis init")
			os.Exit(1)
		}

		fmt.Printf("Validating: %s\n", configPath)
		fmt.Println()

		data, err := os.ReadFile(configPath)
		if err != nil {
			fmt.Printf("✗ Error reading config: %v\n", err)
			os.Exit(1)
		}

		var cfg core.Config
		if err := json.Unmarshal(data, &cfg); err != nil {
			// Try YAML
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				fmt.Printf("✗ Invalid configuration: %v\n", err)
				os.Exit(1)
			}
		}

		fmt.Println("✓ Configuration is valid")
		fmt.Println()
		fmt.Printf("Server:   %s:%d\n", cfg.Server.Host, cfg.Server.Port)
		fmt.Printf("Data Dir: %s\n", cfg.Storage.Path)
		fmt.Printf("Log Level: %s\n", cfg.Logging.Level)

	case "show":
		configPath := findConfig()
		if configPath == "" {
			fmt.Println("No configuration file found")
			fmt.Println()
			fmt.Println("Create one with: anubis init")
			os.Exit(1)
		}

		data, err := os.ReadFile(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading config: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("⚖️  Current Configuration")
		fmt.Println("────────────────────────────────────────────")
		fmt.Println()
		fmt.Printf("File: %s\n", configPath)
		fmt.Println()
		fmt.Println(string(data))

	case "path":
		configPath := findConfig()
		if configPath == "" {
			// Show search paths
			fmt.Println("Configuration file not found.")
			fmt.Println()
			fmt.Println("Search paths:")
			fmt.Println("  ./anubis.json")
			fmt.Println("  ./anubis.yaml")
			fmt.Println("  ~/.config/anubis/anubis.json")
			fmt.Println("  /etc/anubis/anubis.json")
			fmt.Println()
			fmt.Println("Or set ANUBIS_CONFIG environment variable")
		} else {
			fmt.Println(configPath)
		}

	case "set":
		if len(os.Args) < 5 {
			fmt.Fprintf(os.Stderr, "Usage: anubis config set <key> <value>\n")
			fmt.Println()
			fmt.Println("Supported keys:")
			fmt.Println("  server.host          Server bind host")
			fmt.Println("  server.port          Server bind port")
			fmt.Println("  server.tls.enabled   Enable TLS (true/false)")
			fmt.Println("  logging.level        Log level (debug/info/warn/error)")
			fmt.Println("  storage.path         Data directory path")
			fmt.Println("  probe.workers        Number of probe workers")
			fmt.Println()
			fmt.Println("Examples:")
			fmt.Println(`  anubis config set server.port 9443`)
			fmt.Println(`  anubis config set logging.level debug`)
			fmt.Println(`  anubis config set server.tls.enabled true`)
			os.Exit(1)
		}

		key := os.Args[3]
		value := os.Args[4]

		configPath := findConfig()
		if configPath == "" {
			fmt.Println("✗ No configuration file found")
			fmt.Println("Create one with: anubis init")
			os.Exit(1)
		}

		data, err := os.ReadFile(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "✗ Error reading config: %v\n", err)
			os.Exit(1)
		}

		var cfg map[string]interface{}
		if err := json.Unmarshal(data, &cfg); err != nil {
			fmt.Fprintf(os.Stderr, "✗ Invalid config JSON: %v\n", err)
			os.Exit(1)
		}

		parts := strings.SplitN(key, ".", 2)
		if len(parts) != 2 {
			fmt.Fprintf(os.Stderr, "✗ Invalid key format: %s\n", key)
			fmt.Println("Key must be in format: section.key (e.g., server.port)")
			os.Exit(1)
		}
		section, subkey := parts[0], parts[1]

		sectionMap, ok := cfg[section].(map[string]interface{})
		if !ok {
			fmt.Fprintf(os.Stderr, "✗ Unknown config section: %s\n", section)
			os.Exit(1)
		}

		// Parse nested keys (e.g., tls.enabled)
		subParts := strings.SplitN(subkey, ".", 3)
		if len(subParts) > 1 {
			nested, ok := sectionMap[subParts[0]].(map[string]interface{})
			if !ok {
				nested = make(map[string]interface{})
				sectionMap[subParts[0]] = nested
			}
			// Handle up to 3 levels deep
			if len(subParts) == 3 {
				deepNested, ok := nested[subParts[1]].(map[string]interface{})
				if !ok {
					deepNested = make(map[string]interface{})
					nested[subParts[1]] = deepNested
				}
				deepNested[subParts[2]] = parseConfigValue(value)
			} else {
				nested[subParts[1]] = parseConfigValue(value)
			}
		} else {
			sectionMap[subkey] = parseConfigValue(value)
		}

		// Write back
		output, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "✗ Error marshaling config: %v\n", err)
			os.Exit(1)
		}

		if err := os.WriteFile(configPath, append(output, '\n'), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "✗ Error writing config: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("✓ Set %s = %v\n", key, value)
		fmt.Println("  Note: Some changes may require a server restart.")

	default:
		fmt.Fprintf(os.Stderr, "Unknown config subcommand: %s\n", subcmd)
		fmt.Println("Run 'anubis config' for usage information")
		os.Exit(1)
	}
}

// soulsCommand handles souls management subcommands
func soulsCommand() {
	if len(os.Args) < 3 {
		fmt.Println("⚖️  Souls Management")
		fmt.Println("────────────────────────────────────────────")
		fmt.Println()
		fmt.Println("Usage: anubis souls <subcommand> [options]")
		fmt.Println()
		fmt.Println("Subcommands:")
		fmt.Println("  export       Export all souls to JSON or YAML")
		fmt.Println("  import       Import souls from JSON or YAML file")
		fmt.Println("  add <file>   Add souls from a YAML/JSON file (merge)")
		fmt.Println("  remove <name|id>  Remove a soul by name or ID")
		fmt.Println()
		fmt.Println("Options for export:")
		fmt.Println("  --format json|yaml   Output format (default: json)")
		fmt.Println("  --output <file>      Write to file instead of stdout")
		fmt.Println()
		fmt.Println("Options for import:")
		fmt.Println("  --replace            Replace existing souls (default: merge)")
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  anubis souls export > souls.json")
		fmt.Println("  anubis souls export --format yaml --output souls.yaml")
		fmt.Println("  anubis souls import souls.json")
		fmt.Println("  anubis souls import --replace souls.yaml")
		fmt.Println("  anubis souls add monitors.yaml")
		fmt.Println("  anubis souls remove my-api")
		fmt.Println("  anubis souls remove soul_abc123")
		return
	}

	subcmd := os.Args[2]

	// add and remove can work with storage directly
	if subcmd == "add" {
		store, err := openLocalStorage()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening storage: %v\n", err)
			os.Exit(1)
		}
		defer store.Close()
		ctx := context.Background()
		soulsAdd(store, ctx)
		return
	}

	if subcmd == "remove" {
		store, err := openLocalStorage()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening storage: %v\n", err)
			os.Exit(1)
		}
		defer store.Close()
		ctx := context.Background()
		soulsRemove(store, ctx)
		return
	}

	store, err := openLocalStorage()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening storage: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	ctx := context.Background()

	switch subcmd {
	case "export":
		exportSouls(store, ctx)
	case "import":
		importSouls(store, ctx)
	default:
		fmt.Fprintf(os.Stderr, "Unknown souls subcommand: %s\n", subcmd)
		fmt.Println("Run 'anubis souls' for usage information")
		os.Exit(1)
	}
}

// exportSouls exports souls to JSON or YAML
func exportSouls(store *storage.CobaltDB, ctx context.Context) {
	format := "json"
	outputPath := ""

	for i := 3; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--format", "-f":
			if i+1 < len(os.Args) {
				format = os.Args[i+1]
				i++
			}
		case "--output", "-o":
			if i+1 < len(os.Args) {
				outputPath = os.Args[i+1]
				i++
			}
		}
	}

	if format != "json" && format != "yaml" {
		fmt.Fprintf(os.Stderr, "Error: unsupported format %q (use json or yaml)\n", format)
		os.Exit(1)
	}

	souls, err := store.ListSouls(ctx, "default", 0, 10000)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing souls: %v\n", err)
		os.Exit(1)
	}

	var output []byte
	if format == "yaml" {
		output, err = yaml.Marshal(souls)
	} else {
		output, err = json.MarshalIndent(souls, "", "  ")
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling souls: %v\n", err)
		os.Exit(1)
	}

	if outputPath != "" {
		if err := os.WriteFile(outputPath, output, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Exported %d souls to %s\n", len(souls), outputPath)
	} else {
		fmt.Println(string(output))
	}
}

// importSouls imports souls from a JSON or YAML file
func importSouls(store *storage.CobaltDB, ctx context.Context) {
	replace := false
	filePath := ""

	for i := 3; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--replace":
			replace = true
		default:
			if !strings.HasPrefix(os.Args[i], "-") {
				filePath = os.Args[i]
			}
		}
	}

	if filePath == "" {
		fmt.Fprintf(os.Stderr, "Error: no input file specified\n")
		fmt.Println("Usage: anubis souls import [--replace] <file.json|file.yaml>")
		os.Exit(1)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	var souls []*core.Soul
	if strings.HasSuffix(filePath, ".yaml") || strings.HasSuffix(filePath, ".yml") {
		if err := yaml.Unmarshal(data, &souls); err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing YAML: %v\n", err)
			os.Exit(1)
		}
	} else {
		if err := json.Unmarshal(data, &souls); err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing JSON: %v\n", err)
			os.Exit(1)
		}
	}

	if len(souls) == 0 {
		fmt.Println("No souls found in input file")
		return
	}

	// If replace mode, delete existing souls first
	if replace {
		existing, _ := store.ListSouls(ctx, "default", 0, 10000)
		for _, s := range existing {
			store.DeleteSoul(ctx, "default", s.ID)
		}
	}

	imported := 0
	skipped := 0
	for _, soul := range souls {
		if soul.ID == "" {
			soul.ID = core.GenerateID()
		}
		if soul.WorkspaceID == "" {
			soul.WorkspaceID = "default"
		}
		soul.UpdatedAt = time.Now()
		if soul.CreatedAt.IsZero() {
			soul.CreatedAt = time.Now()
		}

		// Check if soul already exists
		_, err := store.GetSoulNoCtx(soul.ID)
		if err == nil && !replace {
			skipped++
			continue
		}

		if err := store.SaveSoul(ctx, soul); err != nil {
			fmt.Fprintf(os.Stderr, "Error importing soul %q: %v\n", soul.Name, err)
			continue
		}
		imported++
	}

	fmt.Printf("✓ Imported %d souls", imported)
	if skipped > 0 {
		fmt.Printf(", skipped %d (already exist)", skipped)
	}
	fmt.Println()
	fmt.Println()
	for _, soul := range souls {
		fmt.Printf("  %s  %s (%s)\n", soul.ID, soul.Name, soul.Type)
	}
}

// judgeCommand handles the judge command with subcommands and flags
func judgeCommand() {
	// Check for --help flag
	for i := 2; i < len(os.Args); i++ {
		if os.Args[i] == "--help" || os.Args[i] == "-h" {
			printJudgeHelp()
			return
		}
	}

	// Check for --all flag
	for i := 2; i < len(os.Args); i++ {
		if os.Args[i] == "--all" || os.Args[i] == "-a" {
			judgeAll()
			return
		}
	}

	// Check for a soul name argument (anubis judge <name>)
	if len(os.Args) >= 3 && !strings.HasPrefix(os.Args[2], "-") {
		judgeSingle(os.Args[2])
		return
	}

	// No flags or args — show judgments table
	showJudgments()
}

func printJudgeHelp() {
	fmt.Println("⚖️  Judgment Management")
	fmt.Println("────────────────────────────────────────────")
	fmt.Println()
	fmt.Println("Usage: anubis judge [options]")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  <name>      Force-check a specific soul by name or ID")
	fmt.Println("  --all, -a   Force-check all souls now")
	fmt.Println("  (no args)   Show current verdicts table")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  anubis judge                # Show status table")
	fmt.Println("  anubis judge my-api         # Force-check 'my-api' soul")
	fmt.Println("  anubis judge --all          # Check all souls immediately")
}

// judgeSingle force-checks a specific soul by name or ID
func judgeSingle(nameOrID string) {
	fmt.Printf("⚖️  Force-checking soul: %s\n", nameOrID)

	apiURL := getAPIURL()
	token := getAPIToken()

	if token == "" {
		fmt.Println("Error: No API token found. Set ANUBIS_API_TOKEN environment variable.")
		fmt.Println("       The server must be running to trigger force checks.")
		os.Exit(1)
	}

	// First, find the soul by name or ID
	resp, err := httpGet(apiURL+"/api/v1/souls", token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to API: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Error: API returned status %d\n", resp.StatusCode)
		os.Exit(1)
	}

	var souls []*core.Soul
	if err := json.NewDecoder(resp.Body).Decode(&souls); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
		os.Exit(1)
	}

	// Find matching soul
	var targetSoul *core.Soul
	for _, s := range souls {
		if s.ID == nameOrID || s.Name == nameOrID {
			targetSoul = s
			break
		}
	}

	if targetSoul == nil {
		fmt.Fprintf(os.Stderr, "Error: soul '%s' not found\n", nameOrID)
		os.Exit(1)
	}

	// Trigger force check
	resp, err = httpPost(apiURL+"/api/v1/souls/"+targetSoul.ID+"/check", "application/json", []byte("{}"), token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error triggering check: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Error: API returned status %d\n", resp.StatusCode)
		os.Exit(1)
	}

	var judgment core.Judgment
	if err := json.NewDecoder(resp.Body).Decode(&judgment); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
		os.Exit(1)
	}

	statusIcon := "○"
	switch judgment.Status {
	case core.SoulAlive:
		statusIcon = "✓"
	case core.SoulDead:
		statusIcon = "✗"
	case core.SoulDegraded:
		statusIcon = "~"
	}

	fmt.Printf("✓ Check complete: %s %s (%s)\n", statusIcon, judgment.Status, judgment.Duration)
	fmt.Printf("  Duration: %s\n", judgment.Duration)
	fmt.Printf("  Timestamp: %s\n", judgment.Timestamp.Format("2006-01-02 15:04:05"))
	if judgment.Message != "" {
		fmt.Printf("  Message: %s\n", judgment.Message)
	}
}

// judgeAll force-checks all souls immediately
func judgeAll() {
	fmt.Println("⚖️  Force-checking all souls...")

	apiURL := getAPIURL()
	token := getAPIToken()

	if token == "" {
		fmt.Println("Error: No API token found. Set ANUBIS_API_TOKEN environment variable.")
		fmt.Println("       The server must be running to trigger force checks.")
		os.Exit(1)
	}

	// Get all souls
	resp, err := httpGet(apiURL+"/api/v1/souls", token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to API: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Error: API returned status %d\n", resp.StatusCode)
		os.Exit(1)
	}

	var souls []*core.Soul
	if err := json.NewDecoder(resp.Body).Decode(&souls); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
		os.Exit(1)
	}

	if len(souls) == 0 {
		fmt.Println("No souls configured.")
		return
	}

	fmt.Printf("Found %d souls, triggering checks...\n\n", len(souls))

	successCount := 0
	failCount := 0

	for _, soul := range souls {
		resp, err := httpPost(apiURL+"/api/v1/souls/"+soul.ID+"/check", "application/json", []byte("{}"), token)
		if err != nil || resp.StatusCode != http.StatusOK {
			failCount++
			if resp != nil {
				resp.Body.Close()
			}
			fmt.Printf("  ✗ %s: failed to trigger\n", soul.Name)
			continue
		}
		resp.Body.Close()
		successCount++
		fmt.Printf("  ✓ %s: check triggered\n", soul.Name)
	}

	fmt.Println()
	fmt.Printf("Results: %d triggered, %d failed\n", successCount, failCount)
	fmt.Println("Checks run asynchronously. Use 'anubis judge' to view results.")
}

// soulsAdd adds souls from a YAML/JSON file (merge mode)
func soulsAdd(store *storage.CobaltDB, ctx context.Context) {
	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "Usage: anubis souls add <file.yaml|file.json>\n")
		os.Exit(1)
	}

	filePath := os.Args[3]

	data, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	var souls []*core.Soul
	if strings.HasSuffix(filePath, ".yaml") || strings.HasSuffix(filePath, ".yml") {
		if err := yaml.Unmarshal(data, &souls); err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing YAML: %v\n", err)
			os.Exit(1)
		}
	} else {
		if err := json.Unmarshal(data, &souls); err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing JSON: %v\n", err)
			os.Exit(1)
		}
	}

	if len(souls) == 0 {
		fmt.Println("No souls found in input file")
		return
	}

	added := 0
	skipped := 0
	for _, soul := range souls {
		if soul.ID == "" {
			soul.ID = core.GenerateID()
		}
		if soul.WorkspaceID == "" {
			soul.WorkspaceID = "default"
		}
		soul.UpdatedAt = time.Now()
		if soul.CreatedAt.IsZero() {
			soul.CreatedAt = time.Now()
		}

		// Skip if soul already exists
		_, err := store.GetSoulNoCtx(soul.ID)
		if err == nil {
			skipped++
			continue
		}

		if err := store.SaveSoul(ctx, soul); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving soul %q: %v\n", soul.Name, err)
			continue
		}
		added++
	}

	fmt.Printf("✓ Added %d souls", added)
	if skipped > 0 {
		fmt.Printf(", skipped %d (already exist)", skipped)
	}
	fmt.Println()
}

// soulsRemove removes a soul by name or ID
func soulsRemove(store *storage.CobaltDB, ctx context.Context) {
	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "Usage: anubis souls remove <name|id>\n")
		os.Exit(1)
	}

	nameOrID := os.Args[3]

	// List all souls to find the match
	souls, err := store.ListSouls(ctx, "default", 0, 10000)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing souls: %v\n", err)
		os.Exit(1)
	}

	var targetSoul *core.Soul
	for _, s := range souls {
		if s.ID == nameOrID || s.Name == nameOrID {
			targetSoul = s
			break
		}
	}

	if targetSoul == nil {
		fmt.Fprintf(os.Stderr, "Error: soul '%s' not found\n", nameOrID)
		os.Exit(1)
	}

	if err := store.DeleteSoul(ctx, "default", targetSoul.ID); err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting soul: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Soul removed: %s (%s)\n", targetSoul.Name, targetSoul.ID)
}
