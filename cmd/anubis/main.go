package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
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
		case "judge", "check":
			judgeCommand()
		case "summon":
			summonNode()
		case "banish":
			banishNode()
		case "necropolis", "cluster-status":
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
		case "souls", "soul":
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
  check <name>    Alias for judge <name>
  summon <addr>   Add node to cluster
  banish <id>     Remove node from cluster
  necropolis      Show cluster status
  cluster-status  Alias for necropolis
  version         Show version information
  health          Self health check
  backup          Create or manage backups
  restore         Restore from backup
  status          Show detailed system status
  export          Export configuration to JSON/YAML
  logs            View recent logs
  config          Configuration management
  souls           Souls management
  soul            Alias for souls
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

	serveOpts := parseServeOptions(os.Args)

	// Determine config path
	configPath := serveOpts.ConfigPath
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
		ConfigPath:    configPath,
		Logger:        logger,
		SingleNode:    serveOpts.SingleNode,
		Cluster:       serveOpts.Cluster,
		ClusterSet:    serveOpts.ClusterSet,
		Bootstrap:     serveOpts.Bootstrap,
		BootstrapSet:  serveOpts.BootstrapSet,
		JoinAddrs:     serveOpts.JoinAddrs,
		NodeName:      serveOpts.NodeName,
		Region:        serveOpts.Region,
		BindAddr:      serveOpts.BindAddr,
		AdvertiseAddr: serveOpts.AdvertiseAddr,
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

type serveOptions struct {
	ConfigPath    string
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
}

func parseServeOptions(args []string) serveOptions {
	opts := serveOptions{ConfigPath: configPathFromArgs(args)}
	for i := 1; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--single":
			opts.SingleNode = true
		case arg == "--cluster":
			opts.Cluster = true
			opts.ClusterSet = true
		case strings.HasPrefix(arg, "--cluster="):
			opts.Cluster = parseBoolFlagValue(strings.TrimPrefix(arg, "--cluster="))
			opts.ClusterSet = true
		case arg == "--bootstrap":
			opts.Cluster = true
			opts.ClusterSet = true
			opts.Bootstrap = true
			opts.BootstrapSet = true
		case strings.HasPrefix(arg, "--bootstrap="):
			opts.Bootstrap = parseBoolFlagValue(strings.TrimPrefix(arg, "--bootstrap="))
			opts.BootstrapSet = true
			if opts.Bootstrap {
				opts.Cluster = true
				opts.ClusterSet = true
			}
		case arg == "--join" && i+1 < len(args):
			opts.Cluster = true
			opts.ClusterSet = true
			opts.JoinAddrs = append(opts.JoinAddrs, args[i+1])
			i++
		case strings.HasPrefix(arg, "--join="):
			opts.Cluster = true
			opts.ClusterSet = true
			opts.JoinAddrs = append(opts.JoinAddrs, strings.TrimPrefix(arg, "--join="))
		case (arg == "--node-name" || arg == "--node-id") && i+1 < len(args):
			opts.NodeName = args[i+1]
			i++
		case strings.HasPrefix(arg, "--node-name="):
			opts.NodeName = strings.TrimPrefix(arg, "--node-name=")
		case strings.HasPrefix(arg, "--node-id="):
			opts.NodeName = strings.TrimPrefix(arg, "--node-id=")
		case arg == "--region" && i+1 < len(args):
			opts.Region = args[i+1]
			i++
		case strings.HasPrefix(arg, "--region="):
			opts.Region = strings.TrimPrefix(arg, "--region=")
		case (arg == "--bind" || arg == "--bind-addr") && i+1 < len(args):
			opts.BindAddr = args[i+1]
			i++
		case strings.HasPrefix(arg, "--bind="):
			opts.BindAddr = strings.TrimPrefix(arg, "--bind=")
		case strings.HasPrefix(arg, "--bind-addr="):
			opts.BindAddr = strings.TrimPrefix(arg, "--bind-addr=")
		case arg == "--advertise-addr" && i+1 < len(args):
			opts.AdvertiseAddr = args[i+1]
			i++
		case strings.HasPrefix(arg, "--advertise-addr="):
			opts.AdvertiseAddr = strings.TrimPrefix(arg, "--advertise-addr=")
		}
	}
	return opts
}

func parseBoolFlagValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func raftPeerFromJoinAddr(addr string) core.RaftPeer {
	id := addr
	if host, _, err := net.SplitHostPort(addr); err == nil && host != "" {
		id = host
	}
	id = strings.NewReplacer(":", "-", ".", "-", "[", "", "]", "").Replace(id)
	if id == "" {
		id = addr
	}
	return core.RaftPeer{
		ID:      id,
		Address: addr,
		Role:    core.RoleVoter,
	}
}
