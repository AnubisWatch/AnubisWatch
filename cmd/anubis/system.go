package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

func selfHealth() {
	status := "healthy"
	// Check critical components
	checks := map[string]interface{}{
		"memory": checkMemory(),
		"uptime": time.Since(startTime).String(),
	}

	// The server itself: this command is the docker-compose healthcheck, so
	// it must fail when the server is down, not only when the data dir is.
	apiURL := getAPIURL()
	checks["api_url"] = apiURL
	if err := probeServerHealth(apiURL); err != nil {
		checks["server"] = "unreachable: " + err.Error()
		status = "unhealthy"
	} else {
		checks["server"] = "healthy"
	}

	// Check data directory accessibility
	dataDir := getDataDir()
	if info, err := os.Stat(dataDir); err == nil && info.IsDir() {
		checks["data_dir"] = "accessible"
	} else {
		checks["data_dir"] = "inaccessible"
		status = "unhealthy"
	}

	response := map[string]interface{}{
		"status":  status,
		"version": Version,
		"checks":  checks,
	}

	data, _ := json.MarshalIndent(response, "", "  ")
	fmt.Println(string(data))
	if status != "healthy" && !strings.HasSuffix(os.Args[0], ".test") {
		os.Exit(1)
	}
}

func systemSouls(ctx context.Context, store *storage.CobaltDB) ([]*core.Soul, error) {
	results, err := store.PrefixScan("")
	if err != nil {
		return nil, err
	}

	souls := make([]*core.Soul, 0)
	for key, data := range results {
		if data == nil || (!strings.Contains(key, "/souls/") && !strings.HasPrefix(key, "souls/")) {
			continue
		}
		var soul core.Soul
		if err := json.Unmarshal(data, &soul); err != nil {
			continue
		}
		if soul.WorkspaceID == "" {
			soul.WorkspaceID = core.WorkspaceIDFromContext(ctx)
		}
		souls = append(souls, &soul)
	}

	sort.Slice(souls, func(i, j int) bool {
		if souls[i].WorkspaceID == souls[j].WorkspaceID {
			return souls[i].ID < souls[j].ID
		}
		return souls[i].WorkspaceID < souls[j].WorkspaceID
	})

	return souls, nil
}

func systemWorkspaceIDs(ctx context.Context, store *storage.CobaltDB, souls []*core.Soul) []string {
	ids := map[string]struct{}{"default": {}}

	if workspaces, err := store.ListWorkspaces(ctx); err == nil {
		for _, ws := range workspaces {
			if ws != nil && ws.ID != "" {
				ids[ws.ID] = struct{}{}
			}
		}
	}
	for _, soul := range souls {
		if soul != nil && soul.WorkspaceID != "" {
			ids[soul.WorkspaceID] = struct{}{}
		}
	}

	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
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

	// Count souls across all workspaces.
	souls, err := systemSouls(ctx, store)
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
	channelCount := 0
	ruleCount := 0
	for _, workspaceID := range systemWorkspaceIDs(ctx, store, souls) {
		channels, _ := store.ListAlertChannels(workspaceID)
		rules, _ := store.ListAlertRules(workspaceID)
		channelCount += len(channels)
		ruleCount += len(rules)
	}
	fmt.Printf("Alerts:     %d channels, %d rules\n", channelCount, ruleCount)

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
		fmt.Println("  judgments    Export a soul's judgments from the server (CSV/NDJSON)")
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  anubis export souls > souls.json")
		fmt.Println("  anubis export config > config.json")
		fmt.Println("  anubis export judgments --soul ID --from 7d > judgments.csv")
		return
	}

	subcmd := os.Args[2]
	if subcmd == "judgments" {
		// Judgments come from the running server, not the local data dir.
		exportJudgmentsCommand(os.Args[3:])
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
	case "souls":
		souls, err := systemSouls(ctx, store)
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
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
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
				// Unparseable input leaves lines at 0; the default below applies.
				_, _ = fmt.Sscanf(os.Args[i+1], "%d", &lines)
				i++
			}
		case "-f", "--follow":
			follow = true
		}
	}

	fmt.Println("⚖️  AnubisWatch Logs")
	fmt.Println("────────────────────────────────────────────")
	fmt.Println()

	// Same path the server writes to (logging.file, or
	// <storage.path>/logs/anubis.log) when logging.output is file or both.
	logPath := serverLogFilePath(loadCLIConfig())

	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		fmt.Printf("Log file not found at %s\n", logPath)
		fmt.Println()
		fmt.Println("The server logs to stdout unless the config enables a log file:")
		fmt.Println(`  "logging": {"output": "both"}   (stdout + file; or "file" for file only)`)
		fmt.Println("Under systemd or Docker, use journalctl -u anubis / docker logs instead.")
		return
	}
	fmt.Printf("Showing last %d lines of %s\n", lines, logPath)
	fmt.Println()

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
	if lines_data[len(lines_data)-1] == "" {
		lines_data = lines_data[:len(lines_data)-1]
	}
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
		fmt.Println("-- Following log file. Press Ctrl+C to stop. --")
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		if err := followLogFile(ctx, logPath, int64(len(data)), os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "Error following log file: %v\n", err)
			os.Exit(1)
		}
	}
}

func followLogFile(ctx context.Context, logPath string, offset int64, out io.Writer) error {
	file, err := os.Open(logPath)
	if err != nil {
		return err
	}
	defer func() { file.Close() }()

	// A rotation between the initial read and this open leaves a new, shorter
	// file at logPath; follow it from its start instead of past its end.
	if info, err := file.Stat(); err == nil && info.Size() < offset {
		offset = 0
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return err
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	buf := make([]byte, 4096)
	for {
		n, err := file.Read(buf)
		if n > 0 {
			if _, writeErr := out.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
		}
		if err == nil {
			continue
		}
		if err != io.EOF {
			return err
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}

		// The server rotates the log by renaming it; once the path names a
		// different file, print what was written to the old one before the
		// rename, then continue with the new file from its start.
		if rotated(file, logPath) {
			next, err := os.Open(logPath)
			if err != nil {
				continue // mid-rotation: the new file isn't there yet
			}
			if _, err := io.Copy(out, file); err != nil {
				next.Close()
				return err
			}
			file.Close()
			file = next
		}
	}
}

// rotated reports whether logPath now names a different file than f.
func rotated(f *os.File, logPath string) bool {
	cur, err := f.Stat()
	if err != nil {
		return false
	}
	now, err := os.Stat(logPath)
	if err != nil {
		return false
	}
	return !os.SameFile(cur, now)
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
		configPath := findConfigFromArgs(os.Args)
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			fmt.Println("✗ No configuration file found")
			fmt.Println()
			fmt.Println("Create one with: anubis init")
			os.Exit(1)
		}

		fmt.Printf("Validating: %s\n", configPath)
		fmt.Println()

		cfg, err := core.LoadConfig(configPath)
		if err != nil {
			fmt.Printf("✗ Invalid configuration: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("✓ Configuration is valid")
		fmt.Println()
		fmt.Printf("Server:   %s:%d\n", cfg.Server.Host, cfg.Server.Port)
		fmt.Printf("Data Dir: %s\n", cfg.Storage.Path)
		fmt.Printf("Log Level: %s\n", cfg.Logging.Level)

	case "show":
		configPath := findConfigFromArgs(os.Args)
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
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
		if hasArg(os.Args, "--reveal") {
			fmt.Println(string(data))
		} else {
			fmt.Println(redactConfigSecrets(string(data)))
			fmt.Println()
			fmt.Println("(secrets masked; use --reveal to show them)")
		}

	case "path":
		configPath := findConfigFromArgs(os.Args)
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

		configPath := findConfigFromArgs(os.Args)
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
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&cfg); err != nil {
			fmt.Fprintf(os.Stderr, "✗ Invalid config JSON: %v\n", err)
			os.Exit(1)
		}
		if err := decoder.Decode(new(interface{})); err != io.EOF {
			fmt.Fprintln(os.Stderr, "✗ Invalid config JSON: unexpected trailing data")
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

// configSecretPattern matches JSON or YAML keys that hold credentials, along
// with their scalar value.
var configSecretPattern = regexp.MustCompile(`(?im)((?:^|[\s{,])"?(?:[a-z0-9_]*(?:password|secret|token|apikey|webhook_url)|(?:[a-z0-9_]*_)?key)"?\s*:\s*)("(?:[^"\\]|\\.)*"|'[^']*'|[^\s,}#]+)`)

// redactConfigSecrets masks credential values in a config file for display.
// Environment references such as "${ANUBIS_ADMIN_PASSWORD}" and empty values
// are left visible because they reveal nothing.
func redactConfigSecrets(text string) string {
	var out strings.Builder
	last := 0
	for _, m := range configSecretPattern.FindAllStringSubmatchIndex(text, -1) {
		start, end := m[0], m[1]
		if start < last {
			continue // inside a value an earlier match already masked
		}
		out.WriteString(text[last:start])
		last = end
		prefix, raw := text[m[2]:m[3]], text[m[4]:m[5]]
		value := strings.Trim(raw, `"'`)
		if value == "" || strings.HasPrefix(value, "${") {
			out.WriteString(text[start:end])
			continue
		}
		out.WriteString(prefix + `"********"`)
		if raw[0] == '"' || raw[0] == '\'' {
			continue
		}

		// The pattern's unquoted branch stops at the first space, but a YAML
		// plain scalar runs to the end of the line and a block scalar
		// (`key: |`) to the end of its indented body. Mask the rest too.
		keyStart := m[2]
		if strings.ContainsRune(" \t\r\n{,", rune(text[keyStart])) {
			keyStart++
		}
		lineStart := strings.LastIndexByte(text[:keyStart], '\n') + 1
		lineEnd := len(text)
		if i := strings.IndexByte(text[end:], '\n'); i >= 0 {
			lineEnd = end + i
		}
		if strings.ContainsAny(text[lineStart:keyStart], "{[") {
			continue // flow context: `,` and `}` already end the value
		}
		if configBlockScalarIndicator.MatchString(raw) {
			out.WriteString(text[end:lineEnd])
			indent := keyStart - lineStart
			lastContent := lineEnd
			for pos := lineEnd; pos < len(text); {
				ls := pos + 1
				le := len(text)
				if i := strings.IndexByte(text[ls:], '\n'); i >= 0 {
					le = ls + i
				}
				line := text[ls:le]
				if strings.TrimSpace(line) != "" {
					if len(line)-len(strings.TrimLeft(line, " \t")) <= indent {
						break
					}
					lastContent = le
				}
				pos = le
			}
			last = lastContent
			continue
		}
		rest := text[end:lineEnd]
		if rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
			continue
		}
		words := rest
		for i := 1; i < len(rest); i++ {
			if rest[i] == '#' && (rest[i-1] == ' ' || rest[i-1] == '\t') {
				words = rest[:i]
				break
			}
		}
		words = strings.TrimRight(words, " \t\r")
		if t := strings.TrimLeft(words, " \t"); t != "" && !strings.ContainsRune(",}]", rune(t[0])) {
			last = end + len(words)
		}
	}
	out.WriteString(text[last:])
	return out.String()
}

// configBlockScalarIndicator matches a YAML block scalar header: | or >, with
// optional chomping (+/-) and indentation digit.
var configBlockScalarIndicator = regexp.MustCompile(`^[|>](?:[+-][0-9]?|[0-9][+-]?)?$`)

func hasArg(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// probeServerHealth GETs <apiURL>/health. Certificate verification is skipped
// only for loopback addresses, where the server commonly runs with a
// self-signed certificate and there is no network path to spoof.
func probeServerHealth(apiURL string) error {
	u, err := url.Parse(apiURL)
	if err != nil {
		return err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		// #nosec G402 -- loopback-only liveness probe.
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	client := &http.Client{Timeout: 3 * time.Second, Transport: transport}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, apiURL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}
