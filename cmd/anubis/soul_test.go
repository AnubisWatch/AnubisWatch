package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func captureStdoutSoul(f func()) string {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	w.Close()
	os.Stdout = oldStdout

	var buf strings.Builder
	// io.Copy is not imported in this file; use a simple read loop
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

func TestSoulsCommand_NoArgs(t *testing.T) {
	oldArgs := os.Args
	os.Args = []string{"anubis", "souls"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSoul(soulsCommand)
	if !strings.Contains(output, "Souls Management") {
		t.Errorf("Expected souls header, got: %s", output)
	}
	if !strings.Contains(output, "export") {
		t.Errorf("Expected export subcommand, got: %s", output)
	}
}

func TestSoulsCommand_ExportJSON(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	ctx := context.Background()
	ws := &core.Workspace{ID: "default", Name: "Default"}
	store.SaveWorkspace(ctx, ws)
	soul := &core.Soul{ID: "s1", Name: "test-soul", Type: "http", Target: "https://example.com", WorkspaceID: "default"}
	store.SaveSoul(ctx, soul)
	store.Close()

	oldArgs := os.Args
	os.Args = []string{"anubis", "souls", "export"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSoul(soulsCommand)
	if !strings.Contains(output, "test-soul") {
		t.Errorf("Expected soul name in export, got: %s", output)
	}
	if !strings.Contains(output, "https://example.com") {
		t.Errorf("Expected target in export, got: %s", output)
	}
}

func TestSoulsCommand_ExportYAML(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	ctx := context.Background()
	ws := &core.Workspace{ID: "default", Name: "Default"}
	store.SaveWorkspace(ctx, ws)
	soul := &core.Soul{ID: "s2", Name: "yaml-soul", Type: "tcp", Target: "10.0.0.1:80", WorkspaceID: "default"}
	store.SaveSoul(ctx, soul)
	store.Close()

	oldArgs := os.Args
	os.Args = []string{"anubis", "souls", "export", "--format", "yaml"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSoul(soulsCommand)
	if !strings.Contains(output, "yaml-soul") {
		t.Errorf("Expected soul name in yaml export, got: %s", output)
	}
}

func TestSoulsCommand_ExportToFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)
	outPath := filepath.Join(tmpDir, "souls.json")

	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	ctx := context.Background()
	ws := &core.Workspace{ID: "default", Name: "Default"}
	store.SaveWorkspace(ctx, ws)
	store.Close()

	oldArgs := os.Args
	os.Args = []string{"anubis", "souls", "export", "--output", outPath}
	defer func() { os.Args = oldArgs }()

	captureStdoutSoul(soulsCommand)

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("Expected output file to exist: %v", err)
	}
	if len(data) == 0 {
		t.Error("Expected non-empty output file")
	}
}

func TestSoulsCommand_ImportJSON(t *testing.T) {
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

	importPath := filepath.Join(tmpDir, "import.json")
	souls := []*core.Soul{{ID: "imported-1", Name: "imported-soul", Type: "http", Target: "https://imported.com", WorkspaceID: "default"}}
	data, _ := json.Marshal(souls)
	os.WriteFile(importPath, data, 0644)

	oldArgs := os.Args
	os.Args = []string{"anubis", "souls", "import", importPath}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSoul(soulsCommand)
	if !strings.Contains(output, "imported-soul") {
		t.Errorf("Expected imported soul in output, got: %s", output)
	}
	if !strings.Contains(output, "Imported 1 souls") {
		t.Errorf("Expected import confirmation, got: %s", output)
	}
}

func TestSoulsCommand_ImportYAML(t *testing.T) {
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

	importPath := filepath.Join(tmpDir, "import.yaml")
	os.WriteFile(importPath, []byte("- id: yaml-import\n  name: yaml-soul\n  type: http\n  target: https://yaml.com\n  workspaceId: default\n"), 0644)

	oldArgs := os.Args
	os.Args = []string{"anubis", "souls", "import", importPath}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSoul(soulsCommand)
	if !strings.Contains(output, "yaml-soul") {
		t.Errorf("Expected yaml soul in output, got: %s", output)
	}
}

func TestSoulsCommand_ImportReplace(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	ctx := context.Background()
	ws := &core.Workspace{ID: "default", Name: "Default"}
	store.SaveWorkspace(ctx, ws)
	existing := &core.Soul{ID: "old-1", Name: "old-soul", Type: "http", Target: "https://old.com", WorkspaceID: "default"}
	store.SaveSoul(ctx, existing)
	store.Close()

	importPath := filepath.Join(tmpDir, "replace.json")
	souls := []*core.Soul{{ID: "new-1", Name: "new-soul", Type: "http", Target: "https://new.com", WorkspaceID: "default"}}
	data, _ := json.Marshal(souls)
	os.WriteFile(importPath, data, 0644)

	oldArgs := os.Args
	os.Args = []string{"anubis", "souls", "import", "--replace", importPath}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSoul(soulsCommand)
	if !strings.Contains(output, "new-soul") {
		t.Errorf("Expected new soul in output, got: %s", output)
	}
}

func TestSoulsCommand_Add(t *testing.T) {
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

	addPath := filepath.Join(tmpDir, "add.json")
	souls := []*core.Soul{{ID: "add-1", Name: "added-soul", Type: "http", Target: "https://added.com", WorkspaceID: "default"}}
	data, _ := json.Marshal(souls)
	os.WriteFile(addPath, data, 0644)

	oldArgs := os.Args
	os.Args = []string{"anubis", "souls", "add", addPath}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSoul(soulsCommand)
	if !strings.Contains(output, "Added 1 souls") {
		t.Errorf("Expected add confirmation, got: %s", output)
	}
}

func TestSoulsCommand_RemoveByName(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	ctx := context.Background()
	ws := &core.Workspace{ID: "default", Name: "Default"}
	store.SaveWorkspace(ctx, ws)
	soul := &core.Soul{ID: "rm-1", Name: "remove-me", Type: "http", Target: "https://rm.com", WorkspaceID: "default"}
	store.SaveSoul(ctx, soul)
	store.Close()

	oldArgs := os.Args
	os.Args = []string{"anubis", "souls", "remove", "remove-me"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSoul(soulsCommand)
	if !strings.Contains(output, "Soul removed") {
		t.Errorf("Expected removal confirmation, got: %s", output)
	}
	if !strings.Contains(output, "remove-me") {
		t.Errorf("Expected soul name in output, got: %s", output)
	}
}

func TestSoulsCommand_RemoveByID(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	ctx := context.Background()
	ws := &core.Workspace{ID: "default", Name: "Default"}
	store.SaveWorkspace(ctx, ws)
	soul := &core.Soul{ID: "rm-by-id", Name: "bye", Type: "http", Target: "https://bye.com", WorkspaceID: "default"}
	store.SaveSoul(ctx, soul)
	store.Close()

	oldArgs := os.Args
	os.Args = []string{"anubis", "souls", "remove", "rm-by-id"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSoul(soulsCommand)
	if !strings.Contains(output, "Soul removed") {
		t.Errorf("Expected removal confirmation, got: %s", output)
	}
}

func TestSoulsCommand_UnknownSubcommandExits(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		t.Setenv("ANUBIS_DATA_DIR", t.TempDir())
		os.Args = []string{"anubis", "souls", "unknown"}
		soulsCommand()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestSoulsCommand_UnknownSubcommandExits")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Unknown souls subcommand") {
		t.Errorf("Expected unknown subcommand error, got: %s", string(output))
	}
}

func TestQuickWatch_StorageFallback(t *testing.T) {
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
	os.Args = []string{"anubis", "watch", "https://example.com", "--name", "test-watch", "--interval", "30s"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSoul(quickWatch)
	if !strings.Contains(output, "Adding soul") {
		t.Errorf("Expected adding soul message, got: %s", output)
	}
	if !strings.Contains(output, "Soul added successfully") {
		t.Errorf("Expected success message, got: %s", output)
	}
}

func TestQuickWatch_InvalidInterval(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		t.Setenv("ANUBIS_DATA_DIR", t.TempDir())
		os.Args = []string{"anubis", "watch", "https://example.com", "--interval", "not-a-duration"}
		quickWatch()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestQuickWatch_InvalidInterval")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "invalid interval") {
		t.Errorf("Expected invalid interval error, got: %s", string(output))
	}
}

func TestQuickWatch_APISuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/souls" {
			t.Errorf("Expected /api/v1/souls, got %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	os.Setenv("ANUBIS_HOST", u.Hostname())
	os.Setenv("ANUBIS_PORT", u.Port())
	os.Setenv("ANUBIS_API_TOKEN", "test-token")
	defer func() {
		os.Unsetenv("ANUBIS_HOST")
		os.Unsetenv("ANUBIS_PORT")
		os.Unsetenv("ANUBIS_API_TOKEN")
	}()

	oldArgs := os.Args
	os.Args = []string{"anubis", "watch", "https://example.com", "--name", "api-soul"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSoul(quickWatch)
	if !strings.Contains(output, "Soul added successfully via API") {
		t.Errorf("Expected API success message, got: %s", output)
	}
}

func TestSoulsRemove_NotFound(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		t.Setenv("ANUBIS_DATA_DIR", t.TempDir())
		store, _ := openLocalStorage()
		ctx := context.Background()
		ws := &core.Workspace{ID: "default", Name: "Default"}
		store.SaveWorkspace(ctx, ws)
		store.Close()
		os.Args = []string{"anubis", "souls", "remove", "nonexistent"}
		soulsCommand()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestSoulsRemove_NotFound")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "not found") {
		t.Errorf("Expected not found error, got: %s", string(output))
	}
}

func TestSoulsAdd_MissingArgs(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		t.Setenv("ANUBIS_DATA_DIR", t.TempDir())
		store, _ := openLocalStorage()
		store.Close()
		os.Args = []string{"anubis", "souls", "add"}
		soulsCommand()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestSoulsAdd_MissingArgs")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Usage: anubis souls add") {
		t.Errorf("Expected usage message, got: %s", string(output))
	}
}

func TestSoulsAdd_FileNotFound(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		t.Setenv("ANUBIS_DATA_DIR", t.TempDir())
		store, _ := openLocalStorage()
		store.Close()
		os.Args = []string{"anubis", "souls", "add", "/nonexistent/file.json"}
		soulsCommand()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestSoulsAdd_FileNotFound")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Error reading file") {
		t.Errorf("Expected file not found error, got: %s", string(output))
	}
}

func TestSoulsAdd_ParseError(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		t.Setenv("ANUBIS_DATA_DIR", t.TempDir())
		store, _ := openLocalStorage()
		store.Close()
		tmpDir := t.TempDir()
		badFile := filepath.Join(tmpDir, "bad.json")
		os.WriteFile(badFile, []byte("not json"), 0644)
		os.Args = []string{"anubis", "souls", "add", badFile}
		soulsCommand()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestSoulsAdd_ParseError")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Error parsing JSON") {
		t.Errorf("Expected parse error, got: %s", string(output))
	}
}

func TestSoulsAdd_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	store.Close()

	emptyFile := filepath.Join(tmpDir, "empty.json")
	os.WriteFile(emptyFile, []byte("[]"), 0644)

	oldArgs := os.Args
	os.Args = []string{"anubis", "souls", "add", emptyFile}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSoul(soulsCommand)
	if !strings.Contains(output, "No souls found in input file") {
		t.Errorf("Expected empty file message, got: %s", output)
	}
}

func TestSoulsAdd_SkippedExisting(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	ctx := context.Background()
	ws := &core.Workspace{ID: "default", Name: "Default"}
	store.SaveWorkspace(ctx, ws)
	existing := &core.Soul{ID: "existing-1", Name: "existing-soul", Type: "http", Target: "https://old.com", WorkspaceID: "default"}
	store.SaveSoul(ctx, existing)
	store.Close()

	addFile := filepath.Join(tmpDir, "add.json")
	souls := []*core.Soul{{ID: "existing-1", Name: "existing-soul", Type: "http", Target: "https://new.com", WorkspaceID: "default"}}
	data, _ := json.Marshal(souls)
	os.WriteFile(addFile, data, 0644)

	oldArgs := os.Args
	os.Args = []string{"anubis", "souls", "add", addFile}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSoul(soulsCommand)
	if !strings.Contains(output, "skipped 1 (already exist)") {
		t.Errorf("Expected skip message, got: %s", output)
	}
}

func TestImportSouls_MissingFile(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		t.Setenv("ANUBIS_DATA_DIR", t.TempDir())
		store, _ := openLocalStorage()
		store.Close()
		os.Args = []string{"anubis", "souls", "import"}
		soulsCommand()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestImportSouls_MissingFile")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "no input file specified") {
		t.Errorf("Expected missing file error, got: %s", string(output))
	}
}

func TestImportSouls_FileNotFound(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		t.Setenv("ANUBIS_DATA_DIR", t.TempDir())
		store, _ := openLocalStorage()
		store.Close()
		os.Args = []string{"anubis", "souls", "import", "/nonexistent.json"}
		soulsCommand()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestImportSouls_FileNotFound")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Error reading file") {
		t.Errorf("Expected file not found error, got: %s", string(output))
	}
}

func TestImportSouls_ReplaceMode(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	ctx := context.Background()
	ws := &core.Workspace{ID: "default", Name: "Default"}
	store.SaveWorkspace(ctx, ws)
	old := &core.Soul{ID: "old-1", Name: "old-soul", Type: "http", Target: "https://old.com", WorkspaceID: "default"}
	store.SaveSoul(ctx, old)
	store.Close()

	importFile := filepath.Join(tmpDir, "replace.json")
	souls := []*core.Soul{{ID: "new-1", Name: "new-soul", Type: "http", Target: "https://new.com", WorkspaceID: "default"}}
	data, _ := json.Marshal(souls)
	os.WriteFile(importFile, data, 0644)

	oldArgs := os.Args
	os.Args = []string{"anubis", "souls", "import", "--replace", importFile}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSoul(soulsCommand)
	if !strings.Contains(output, "Imported 1 souls") {
		t.Errorf("Expected import success, got: %s", output)
	}
	if !strings.Contains(output, "new-soul") {
		t.Errorf("Expected new soul in output, got: %s", output)
	}
}

func TestQuickWatch_APIError(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		u, _ := url.Parse(server.URL)
		os.Setenv("ANUBIS_HOST", u.Hostname())
		os.Setenv("ANUBIS_PORT", u.Port())
		os.Setenv("ANUBIS_API_TOKEN", "test-token")

		os.Args = []string{"anubis", "watch", "https://example.com", "--name", "api-error-soul"}
		quickWatch()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestQuickWatch_APIError")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Soul added successfully") {
		t.Errorf("Expected error message, got: %s", string(output))
	}
}

func TestExportSouls_Empty(t *testing.T) {
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
	os.Args = []string{"anubis", "souls", "export"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSoul(soulsCommand)
	if !strings.Contains(output, "[]") {
		t.Errorf("Expected empty message, got: %s", output)
	}
}

func TestSoulsRemove_MissingArgs(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		t.Setenv("ANUBIS_DATA_DIR", t.TempDir())
		store, _ := openLocalStorage()
		store.Close()
		os.Args = []string{"anubis", "souls", "remove"}
		soulsCommand()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestSoulsRemove_MissingArgs")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Usage: anubis souls remove") {
		t.Errorf("Expected usage message, got: %s", string(output))
	}
}

func TestQuickWatch_WithInterval(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	oldArgs := os.Args
	os.Args = []string{"anubis", "watch", "https://interval.com", "--name", "interval-soul", "--interval", "30s"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSoul(quickWatch)
	if !strings.Contains(output, "Soul added successfully") {
		t.Errorf("Expected success message, got: %s", output)
	}
}

func TestQuickWatch_WithMinutesInterval(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	oldArgs := os.Args
	os.Args = []string{"anubis", "watch", "https://minutes.com", "--name", "min-soul", "--interval", "5m"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSoul(quickWatch)
	if !strings.Contains(output, "Soul added successfully") {
		t.Errorf("Expected success message, got: %s", output)
	}
}


func TestQuickWatch_TCPType(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	oldArgs := os.Args
	os.Args = []string{"anubis", "watch", "tcp://example.com:8080", "--name", "tcp-soul", "--type", "tcp"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSoul(quickWatch)
	if !strings.Contains(output, "tcp-soul") {
		t.Errorf("Expected soul name, got: %s", output)
	}
	if !strings.Contains(output, "Soul added successfully") {
		t.Errorf("Expected success message, got: %s", output)
	}
}

func TestSoulsCommand_UnknownSubcommand(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		tmpDir := t.TempDir()
		t.Setenv("ANUBIS_DATA_DIR", tmpDir)
		store, _ := openLocalStorage()
		store.Close()
		os.Args = []string{"anubis", "souls", "unknown"}
		soulsCommand()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestSoulsCommand_UnknownSubcommand")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Unknown souls subcommand") {
		t.Errorf("Expected unknown subcommand error, got: %s", string(output))
	}
}

func TestExportSouls_UnsupportedFormat(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		tmpDir := t.TempDir()
		t.Setenv("ANUBIS_DATA_DIR", tmpDir)
		store, _ := openLocalStorage()
		ctx := context.Background()
		ws := &core.Workspace{ID: "default", Name: "Default"}
		store.SaveWorkspace(ctx, ws)
		store.Close()

		os.Args = []string{"anubis", "souls", "export", "--format", "xml"}
		soulsCommand()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestExportSouls_UnsupportedFormat")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "unsupported format") {
		t.Errorf("Expected unsupported format error, got: %s", string(output))
	}
}

func TestQuickWatch_ICMPType(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	oldArgs := os.Args
	os.Args = []string{"anubis", "watch", "icmp://8.8.8.8", "--name", "icmp-soul"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSoul(quickWatch)
	if !strings.Contains(output, "icmp-soul") {
		t.Errorf("Expected soul name, got: %s", output)
	}
	if !strings.Contains(output, "Soul added successfully") {
		t.Errorf("Expected success message, got: %s", output)
	}
}

func TestQuickWatch_NoName(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	oldArgs := os.Args
	os.Args = []string{"anubis", "watch", "https://noname.example.com"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSoul(quickWatch)
	if !strings.Contains(output, "noname.example.com") {
		t.Errorf("Expected target as name, got: %s", output)
	}
}

func TestQuickWatch_API201(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	os.Setenv("ANUBIS_HOST", u.Hostname())
	os.Setenv("ANUBIS_PORT", u.Port())
	os.Setenv("ANUBIS_API_TOKEN", "test-token")
	defer func() {
		os.Unsetenv("ANUBIS_HOST")
		os.Unsetenv("ANUBIS_PORT")
		os.Unsetenv("ANUBIS_API_TOKEN")
	}()

	oldArgs := os.Args
	os.Args = []string{"anubis", "watch", "https://api-success.com", "--name", "api-soul"}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSoul(quickWatch)
	if !strings.Contains(output, "via API") {
		t.Errorf("Expected API success message, got: %s", output)
	}
}

func TestSoulsCommand_Import(t *testing.T) {
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

	importPath := filepath.Join(tmpDir, "souls.json")
	data, _ := json.Marshal([]*core.Soul{{ID: "imp1", Name: "imported", Type: "http", Target: "https://import.com", WorkspaceID: "default"}})
	os.WriteFile(importPath, data, 0644)

	oldArgs := os.Args
	os.Args = []string{"anubis", "souls", "import", importPath}
	defer func() { os.Args = oldArgs }()

	output := captureStdoutSoul(soulsCommand)
	if !strings.Contains(output, "imported") && !strings.Contains(output, "Import complete") {
		t.Errorf("Expected import success message, got: %s", output)
	}
}
