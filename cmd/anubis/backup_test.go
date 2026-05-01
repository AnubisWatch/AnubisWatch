package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func captureStdoutBackup(f func()) string {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	w.Close()
	os.Stdout = oldStdout

	var buf strings.Builder
	b := make([]byte, 1024)
	for {
		n, err := r.Read(b)
		if n > 0 {
			buf.Write(b[:n])
		}
		if err != nil {
			break
		}
	}
	return buf.String()
}

func TestBackupCommand_NoArgs(t *testing.T) {
	oldArgs := os.Args
	os.Args = []string{"anubis", "backup"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutBackup(backupCommand)
	if !strings.Contains(output, "Backup Management") {
		t.Errorf("Expected backup header, got: %s", output)
	}
	if !strings.Contains(output, "create") {
		t.Errorf("Expected create subcommand, got: %s", output)
	}
}

func TestBackupCreate(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	ctx := context.Background()
	ws := &core.Workspace{ID: "default", Name: "Default"}
	store.SaveWorkspace(ctx, ws)
	soul := &core.Soul{ID: "b1", Name: "backup-soul", Type: "http", Target: "https://example.com", WorkspaceID: "default"}
	store.SaveSoul(ctx, soul)
	store.Close()

	oldArgs := os.Args
	os.Args = []string{"anubis", "backup", "create"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutBackup(backupCreate)
	if !strings.Contains(output, "Backup created successfully") {
		t.Errorf("Expected success message, got: %s", output)
	}
	if !strings.Contains(output, "Workspaces:") {
		t.Errorf("Expected workspaces count, got: %s", output)
	}
}

func TestBackupCreate_WithOutput(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)
	outPath := filepath.Join(tmpDir, "custom_backup.tar.gz")

	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	ctx := context.Background()
	ws := &core.Workspace{ID: "default", Name: "Default"}
	store.SaveWorkspace(ctx, ws)
	store.Close()

	oldArgs := os.Args
	os.Args = []string{"anubis", "backup", "create", "--output", outPath}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutBackup(backupCreate)
	if !strings.Contains(output, "Backup created successfully") {
		t.Errorf("Expected success message, got: %s", output)
	}
	if _, err := os.Stat(outPath); os.IsNotExist(err) {
		t.Errorf("Expected output file at %s", outPath)
	}
}

func TestBackupList_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	oldArgs := os.Args
	os.Args = []string{"anubis", "backup", "list"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutBackup(backupList)
	if !strings.Contains(output, "No backups found") {
		t.Errorf("Expected empty message, got: %s", output)
	}
}

func TestBackupList_WithBackups(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	// Create a backup first
	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	ctx := context.Background()
	ws := &core.Workspace{ID: "default", Name: "Default"}
	store.SaveWorkspace(ctx, ws)
	store.Close()

	oldArgs := os.Args
	os.Args = []string{"anubis", "backup", "create"}
	captureStdoutBackup(backupCreate)

	// Now list
	os.Args = []string{"anubis", "backup", "list"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutBackup(backupList)
	if !strings.Contains(output, "Available Backups") {
		t.Errorf("Expected backup list header, got: %s", output)
	}
	if !strings.Contains(output, "Total: 1 backups") {
		t.Errorf("Expected total count, got: %s", output)
	}
}

func TestBackupDelete(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	// Create a backup first
	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	ctx := context.Background()
	ws := &core.Workspace{ID: "default", Name: "Default"}
	store.SaveWorkspace(ctx, ws)
	store.Close()

	oldArgs := os.Args
	os.Args = []string{"anubis", "backup", "create"}
	captureStdoutBackup(backupCreate)

	// Find the backup file
	entries, _ := os.ReadDir(filepath.Join(tmpDir, "backups"))
	if len(entries) == 0 {
		t.Fatal("Expected backup file to exist")
	}
	filename := entries[0].Name()

	// Delete it
	os.Args = []string{"anubis", "backup", "delete", filename}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutBackup(backupDelete)
	if !strings.Contains(output, "Backup deleted successfully") {
		t.Errorf("Expected delete confirmation, got: %s", output)
	}
}

func TestBackupInfo(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	// Create a backup first
	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	ctx := context.Background()
	ws := &core.Workspace{ID: "default", Name: "Default"}
	store.SaveWorkspace(ctx, ws)
	store.Close()

	oldArgs := os.Args
	os.Args = []string{"anubis", "backup", "create"}
	captureStdoutBackup(backupCreate)

	// Find the backup file
	entries, _ := os.ReadDir(filepath.Join(tmpDir, "backups"))
	if len(entries) == 0 {
		t.Fatal("Expected backup file to exist")
	}
	filename := entries[0].Name()

	// Get info
	os.Args = []string{"anubis", "backup", "info", filename}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutBackup(backupInfo)
	if !strings.Contains(output, "Backup Information") {
		t.Errorf("Expected info header, got: %s", output)
	}
	if !strings.Contains(output, filename) {
		t.Errorf("Expected filename in output, got: %s", output)
	}
}

func TestRestoreCommand_WithForce(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	// Create a backup first
	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	ctx := context.Background()
	ws := &core.Workspace{ID: "default", Name: "Default"}
	store.SaveWorkspace(ctx, ws)
	soul := &core.Soul{ID: "r1", Name: "restore-soul", Type: "http", Target: "https://restore.com", WorkspaceID: "default"}
	store.SaveSoul(ctx, soul)
	store.Close()

	oldArgs := os.Args
	os.Args = []string{"anubis", "backup", "create", "--output", filepath.Join(tmpDir, "backup.tar.gz")}
	captureStdoutBackup(backupCreate)

	backupPath := filepath.Join(tmpDir, "backup.tar.gz")

	// Restore with force
	os.Args = []string{"anubis", "restore", backupPath, "--force"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutBackup(restoreCommand)
	if !strings.Contains(output, "Restore completed successfully") {
		t.Errorf("Expected restore success, got: %s", output)
	}
}

func TestBackupCommand_UnknownSubcommandExits(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		t.Setenv("ANUBIS_DATA_DIR", t.TempDir())
		os.Args = []string{"anubis", "backup", "unknown"}
		backupCommand()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestBackupCommand_UnknownSubcommandExits")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Unknown backup subcommand") {
		t.Errorf("Expected unknown subcommand error, got: %s", string(output))
	}
}

func TestBackupCreate_NoCompress(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	ctx := context.Background()
	ws := &core.Workspace{ID: "default", Name: "Default"}
	store.SaveWorkspace(ctx, ws)
	store.Close()

	oldArgs := os.Args
	os.Args = []string{"anubis", "backup", "create", "--no-compress"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutBackup(backupCreate)
	if !strings.Contains(output, "Backup created successfully") {
		t.Errorf("Expected success message, got: %s", output)
	}
}

func TestRestoreCommand_NoArgs(t *testing.T) {
	oldArgs := os.Args
	os.Args = []string{"anubis", "restore"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutBackup(restoreCommand)
	if !strings.Contains(output, "Restore from Backup") {
		t.Errorf("Expected restore header, got: %s", output)
	}
	if !strings.Contains(output, "--force") {
		t.Errorf("Expected force flag, got: %s", output)
	}
}

func TestBackupDelete_MissingArgs(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		t.Setenv("ANUBIS_DATA_DIR", t.TempDir())
		os.Args = []string{"anubis", "backup", "delete"}
		backupDelete()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestBackupDelete_MissingArgs")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Usage: anubis backup delete") {
		t.Errorf("Expected usage message, got: %s", string(output))
	}
}

func TestExportCommand_UnknownSubcommand(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		t.Setenv("ANUBIS_DATA_DIR", t.TempDir())
		store, _ := openLocalStorage()
		store.Close()
		os.Args = []string{"anubis", "export", "unknown"}
		exportCommand()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestExportCommand_UnknownSubcommand")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Unknown export subcommand") {
		t.Errorf("Expected unknown subcommand error, got: %s", string(output))
	}
}

func TestBackupCreate_NoCompressInfo(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	ctx := context.Background()
	ws := &core.Workspace{ID: "default", Name: "Default"}
	store.SaveWorkspace(ctx, ws)
	store.Close()

	oldArgs := os.Args
	os.Args = []string{"anubis", "backup", "create", "--no-compress"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutBackup(backupCreate)
	if !strings.Contains(output, "Backup created successfully") {
		t.Errorf("Expected success message, got: %s", output)
	}
}

func TestBackupDelete_WithInfo(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	ctx := context.Background()
	ws := &core.Workspace{ID: "default", Name: "Default"}
	store.SaveWorkspace(ctx, ws)
	store.Close()

	oldArgs := os.Args
	os.Args = []string{"anubis", "backup", "create"}
	captureStdoutBackup(backupCreate)

	entries, _ := os.ReadDir(filepath.Join(tmpDir, "backups"))
	if len(entries) == 0 {
		t.Fatal("Expected backup file to exist")
	}
	filename := entries[0].Name()

	os.Args = []string{"anubis", "backup", "delete", filename}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutBackup(backupDelete)
	if !strings.Contains(output, "Backup deleted successfully") {
		t.Errorf("Expected delete confirmation, got: %s", output)
	}
	if !strings.Contains(output, filename) {
		t.Errorf("Expected filename in output, got: %s", output)
	}
}

func TestRestoreCommand_NoForce(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		tmpDir := t.TempDir()
		t.Setenv("ANUBIS_DATA_DIR", tmpDir)

		store, err := openLocalStorage()
		if err != nil {
			return
		}
		ctx := context.Background()
		ws := &core.Workspace{ID: "default", Name: "Default"}
		store.SaveWorkspace(ctx, ws)
		store.Close()

		os.Args = []string{"anubis", "backup", "create", "--output", filepath.Join(tmpDir, "backup.tar.gz")}
		captureStdoutBackup(backupCreate)

		backupPath := filepath.Join(tmpDir, "backup.tar.gz")
		os.Args = []string{"anubis", "restore", backupPath}
		restoreCommand()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestRestoreCommand_NoForce")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Restore cancelled.") {
		t.Errorf("Expected force warning, got: %s", string(output))
	}
}

func TestBackupCommand_List(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	ctx := context.Background()
	ws := &core.Workspace{ID: "default", Name: "Default"}
	store.SaveWorkspace(ctx, ws)
	store.Close()

	oldArgs := os.Args
	os.Args = []string{"anubis", "backup", "list"}
	defer func() { os.Args = oldArgs }()

	captureStdoutBackup(backupCreate)

	os.Args = []string{"anubis", "backup", "list"}
	output := captureStdoutBackup(backupCommand)
	if !strings.Contains(output, "Available Backups") {
		t.Errorf("Expected backup list header, got: %s", output)
	}
}

func TestBackupCommand_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	ctx := context.Background()
	ws := &core.Workspace{ID: "default", Name: "Default"}
	store.SaveWorkspace(ctx, ws)
	store.Close()

	oldArgs := os.Args
	os.Args = []string{"anubis", "backup", "create"}
	captureStdoutBackup(backupCreate)

	entries, _ := os.ReadDir(filepath.Join(tmpDir, "backups"))
	if len(entries) == 0 {
		t.Fatal("Expected backup file to exist")
	}
	filename := entries[0].Name()

	os.Args = []string{"anubis", "backup", "delete", filename}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutBackup(backupCommand)
	if !strings.Contains(output, "Backup deleted successfully") {
		t.Errorf("Expected delete confirmation, got: %s", output)
	}
}

func TestBackupCommand_Info(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	ctx := context.Background()
	ws := &core.Workspace{ID: "default", Name: "Default"}
	store.SaveWorkspace(ctx, ws)
	store.Close()

	oldArgs := os.Args
	os.Args = []string{"anubis", "backup", "create"}
	captureStdoutBackup(backupCreate)

	entries, _ := os.ReadDir(filepath.Join(tmpDir, "backups"))
	if len(entries) == 0 {
		t.Fatal("Expected backup file to exist")
	}
	filename := entries[0].Name()

	os.Args = []string{"anubis", "backup", "info", filename}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutBackup(backupCommand)
	if !strings.Contains(output, "Backup Information") {
		t.Errorf("Expected info header, got: %s", output)
	}
}

func TestBackupCreate_WithFlags(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	ctx := context.Background()
	ws := &core.Workspace{ID: "default", Name: "Default"}
	store.SaveWorkspace(ctx, ws)
	store.Close()

	oldArgs := os.Args
	os.Args = []string{"anubis", "backup", "create", "--compress", "--include-history"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutBackup(backupCreate)
	if !strings.Contains(output, "Backup created successfully") {
		t.Errorf("Expected success message, got: %s", output)
	}
}

func TestRestoreCommand_WithSkipFlags(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	ctx := context.Background()
	ws := &core.Workspace{ID: "default", Name: "Default"}
	store.SaveWorkspace(ctx, ws)
	soul := &core.Soul{ID: "r1", Name: "restore-soul", Type: "http", Target: "https://restore.com", WorkspaceID: "default"}
	store.SaveSoul(ctx, soul)
	store.Close()

	oldArgs := os.Args
	os.Args = []string{"anubis", "backup", "create", "--output", filepath.Join(tmpDir, "backup.tar.gz")}
	captureStdoutBackup(backupCreate)

	backupPath := filepath.Join(tmpDir, "backup.tar.gz")

	os.Args = []string{"anubis", "restore", backupPath, "--force", "--skip-alerts", "--skip-status-pages", "--skip-journeys", "--skip-souls"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutBackup(restoreCommand)
	if !strings.Contains(output, "Restore completed successfully") {
		t.Errorf("Expected restore success, got: %s", output)
	}
}

func TestBackupInfo_MissingArgs(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		t.Setenv("ANUBIS_DATA_DIR", t.TempDir())
		os.Args = []string{"anubis", "backup", "info"}
		backupInfo()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestBackupInfo_MissingArgs")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Usage: anubis backup info") {
		t.Errorf("Expected usage message, got: %s", string(output))
	}
}

func TestBackupDelete_NonExistent(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		tmpDir := t.TempDir()
		t.Setenv("ANUBIS_DATA_DIR", tmpDir)
		os.Args = []string{"anubis", "backup", "delete", "nonexistent.tar.gz"}
		backupDelete()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestBackupDelete_NonExistent")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Error deleting backup") {
		t.Errorf("Expected delete error, got: %s", string(output))
	}
}
