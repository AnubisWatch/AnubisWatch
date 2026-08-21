package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
	"gopkg.in/yaml.v3"
)

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
			if err := store.DeleteSoul(ctx, "default", s.ID); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to delete soul %s: %v\n", s.ID, err)
			}
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
