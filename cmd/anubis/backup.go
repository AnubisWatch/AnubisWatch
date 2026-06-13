package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/AnubisWatch/anubiswatch/internal/backup"
)

// errorLogger returns a logger that only outputs errors (for backup operations)
func errorLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
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
	mgr := backup.NewManager(store, getDataDir(), errorLogger())
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
	mgr := backup.NewManager(nil, getDataDir(), errorLogger())
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

	mgr := backup.NewManager(nil, getDataDir(), errorLogger())
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

	mgr := backup.NewManager(nil, getDataDir(), errorLogger())
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
