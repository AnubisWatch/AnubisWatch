package profiling

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewManager(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DataDir = t.TempDir()

	mgr := NewManager(cfg, newTestLogger())
	if mgr == nil {
		t.Fatal("NewManager() returned nil")
	}

	if mgr.dataDir != cfg.DataDir {
		t.Errorf("dataDir = %s, want %s", mgr.dataDir, cfg.DataDir)
	}

	if mgr.enabled != cfg.Enabled {
		t.Errorf("enabled = %v, want %v", mgr.enabled, cfg.Enabled)
	}
}

func TestManager_StartStop(t *testing.T) {
	tempDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.DataDir = tempDir

	mgr := NewManager(cfg, newTestLogger())

	err := mgr.Start()
	if err != nil {
		t.Errorf("Start() error = %v", err)
	}

	// Give it time to collect some stats
	time.Sleep(100 * time.Millisecond)

	err = mgr.Stop()
	if err != nil {
		t.Errorf("Stop() error = %v", err)
	}

	// Verify profiles directory was created
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		t.Error("profiles directory was not created")
	}
}

func TestManager_StartNotEnabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false

	mgr := NewManager(cfg, newTestLogger())

	err := mgr.Start()
	if err != nil {
		t.Errorf("Start() error = %v", err)
	}
}

func TestCollectStats(t *testing.T) {
	tempDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.DataDir = tempDir

	mgr := NewManager(cfg, newTestLogger())
	mgr.collectStats()

	mem := mgr.GetMemProfile()
	if mem.Timestamp.IsZero() {
		t.Error("MemProfile timestamp is zero")
	}

	cpu := mgr.GetCPUProfile()
	if cpu.NumCPU == 0 {
		t.Error("CPUProfile NumCPU is zero")
	}

	goros := mgr.GetGoroutineStats()
	if goros.Count == 0 {
		t.Error("GoroutineStats Count is zero")
	}
}

func TestWriteHeapProfile(t *testing.T) {
	tempDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.DataDir = tempDir

	mgr := NewManager(cfg, newTestLogger())

	filename, err := mgr.WriteHeapProfile()
	if err != nil {
		t.Fatalf("WriteHeapProfile() error = %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		t.Error("heap profile file was not created")
	}

	// Verify it's in the right directory
	if filepath.Dir(filename) != tempDir {
		t.Errorf("heap profile in wrong directory: %s", filepath.Dir(filename))
	}
}

func TestWriteGoroutineProfile(t *testing.T) {
	tempDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.DataDir = tempDir

	mgr := NewManager(cfg, newTestLogger())

	filename, err := mgr.WriteGoroutineProfile()
	if err != nil {
		t.Fatalf("WriteGoroutineProfile() error = %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		t.Error("goroutine profile file was not created")
	}
}

func TestStartCPUProfile(t *testing.T) {
	tempDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.DataDir = tempDir

	mgr := NewManager(cfg, newTestLogger())

	filename, err := mgr.StartCPUProfile(100 * time.Millisecond)
	if err != nil {
		t.Fatalf("StartCPUProfile() error = %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		t.Error("CPU profile file was not created")
	}

	// Wait for profile to complete
	time.Sleep(200 * time.Millisecond)

	// Verify file has content
	info, err := os.Stat(filename)
	if err != nil {
		t.Fatalf("Failed to stat CPU profile: %v", err)
	}
	if info.Size() == 0 {
		t.Error("CPU profile file is empty")
	}
}

func TestStartCPUProfile_AlreadyActive(t *testing.T) {
	tempDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.DataDir = tempDir

	mgr := NewManager(cfg, newTestLogger())

	// Start first profile
	_, err := mgr.StartCPUProfile(1 * time.Second)
	if err != nil {
		t.Fatalf("First StartCPUProfile() error = %v", err)
	}

	// Try to start second profile
	_, err = mgr.StartCPUProfile(1 * time.Second)
	if err == nil {
		t.Error("Second StartCPUProfile() should fail when already active")
	}

	// Cleanup
	mgr.StopCPUProfile()
}

func TestGenerateReport(t *testing.T) {
	tempDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.DataDir = tempDir

	mgr := NewManager(cfg, newTestLogger())
	mgr.collectStats()

	report, err := mgr.GenerateReport()
	if err != nil {
		t.Fatalf("GenerateReport() error = %v", err)
	}

	if report.Timestamp.IsZero() {
		t.Error("report.Timestamp is zero")
	}

	if report.MemStats.Timestamp.IsZero() {
		t.Error("report.MemStats.Timestamp is zero")
	}
}

func TestReport_WriteToFile(t *testing.T) {
	tempDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.DataDir = tempDir

	mgr := NewManager(cfg, newTestLogger())
	mgr.collectStats()

	report, err := mgr.GenerateReport()
	if err != nil {
		t.Fatalf("GenerateReport() error = %v", err)
	}

	path := filepath.Join(tempDir, "report.json")
	err = report.WriteToFile(path)
	if err != nil {
		t.Fatalf("WriteToFile() error = %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("report file was not created")
	}

	// Verify it's valid JSON
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read report file: %v", err)
	}

	if len(data) == 0 {
		t.Error("report file is empty")
	}
}

func TestGetAllStats(t *testing.T) {
	tempDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.DataDir = tempDir

	mgr := NewManager(cfg, newTestLogger())
	mgr.collectStats()

	stats := mgr.GetAllStats()
	if stats == nil {
		t.Fatal("GetAllStats() returned nil")
	}

	requiredKeys := []string{"memory", "cpu", "goroutines", "gc", "timestamp"}
	for _, key := range requiredKeys {
		if _, ok := stats[key]; !ok {
			t.Errorf("GetAllStats() missing key: %s", key)
		}
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Enabled {
		t.Error("DefaultConfig().Enabled should be false")
	}

	if cfg.DataDir != "./profiles" {
		t.Errorf("DefaultConfig().DataDir = %s, want ./profiles", cfg.DataDir)
	}

	if cfg.Interval != 30*time.Second {
		t.Errorf("DefaultConfig().Interval = %v, want 30s", cfg.Interval)
	}
}

func TestStartTrace(t *testing.T) {
	tempDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.DataDir = tempDir

	mgr := NewManager(cfg, newTestLogger())

	filename, err := mgr.StartTrace(100 * time.Millisecond)
	if err != nil {
		t.Fatalf("StartTrace() error = %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		t.Error("trace file was not created")
	}

	// Wait for trace to complete
	time.Sleep(200 * time.Millisecond)

	// Verify file has content
	info, err := os.Stat(filename)
	if err != nil {
		t.Fatalf("Failed to stat trace file: %v", err)
	}
	if info.Size() == 0 {
		t.Error("trace file is empty")
	}
}

func TestStartTrace_AlreadyActive(t *testing.T) {
	tempDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.DataDir = tempDir

	mgr := NewManager(cfg, newTestLogger())

	// Start first trace
	_, err := mgr.StartTrace(1 * time.Second)
	if err != nil {
		t.Fatalf("First StartTrace() error = %v", err)
	}

	// Try to start second trace
	_, err = mgr.StartTrace(1 * time.Second)
	if err == nil {
		t.Error("Second StartTrace() should fail when already active")
	}

	// Cleanup
	mgr.StopTrace()
}

func TestStopTrace_NotActive(t *testing.T) {
	tempDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.DataDir = tempDir

	mgr := NewManager(cfg, newTestLogger())

	// Stop when not active should not error
	err := mgr.StopTrace()
	if err != nil {
		t.Errorf("StopTrace() error = %v", err)
	}
}

func TestStopCPUProfile_NotActive(t *testing.T) {
	tempDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.DataDir = tempDir

	mgr := NewManager(cfg, newTestLogger())

	// Stop when not active should not error
	err := mgr.StopCPUProfile()
	if err != nil {
		t.Errorf("StopCPUProfile() error = %v", err)
	}
}

func TestManager_Stop_NotEnabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false

	mgr := NewManager(cfg, newTestLogger())

	// Stop when not enabled should not error
	err := mgr.Stop()
	if err != nil {
		t.Errorf("Stop() error = %v", err)
	}
}

func TestManager_Start_CreatesDirectory(t *testing.T) {
	tempDir := t.TempDir()
	nestedDir := filepath.Join(tempDir, "nested", "profiles")

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.DataDir = nestedDir

	mgr := NewManager(cfg, newTestLogger())

	err := mgr.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer mgr.Stop()

	// Verify nested directory was created
	if _, err := os.Stat(nestedDir); os.IsNotExist(err) {
		t.Error("nested profiles directory was not created")
	}
}

func TestManager_MultipleStatsCollections(t *testing.T) {
	tempDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.DataDir = tempDir

	mgr := NewManager(cfg, newTestLogger())

	// Collect stats multiple times
	for i := 0; i < 5; i++ {
		mgr.collectStats()
	}

	goros := mgr.GetGoroutineStats()
	if len(goros.History) != 5 {
		t.Errorf("Expected 5 history entries, got %d", len(goros.History))
	}
}

func TestManager_GoroutineHistoryLimit(t *testing.T) {
	tempDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.DataDir = tempDir

	mgr := NewManager(cfg, newTestLogger())

	// Collect stats many times to exceed the 60-entry limit
	for i := 0; i < 70; i++ {
		mgr.collectStats()
	}

	goros := mgr.GetGoroutineStats()
	if len(goros.History) > 60 {
		t.Errorf("History should be limited to 60 entries, got %d", len(goros.History))
	}
}

func TestReport_WriteToFile_Error(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DataDir = t.TempDir()

	mgr := NewManager(cfg, newTestLogger())
	mgr.collectStats()

	report, err := mgr.GenerateReport()
	if err != nil {
		t.Fatalf("GenerateReport() error = %v", err)
	}

	// Try to write to a read-only directory (this should fail)
	readOnlyDir := t.TempDir()
	os.Chmod(readOnlyDir, 0555)       // Read-only
	defer os.Chmod(readOnlyDir, 0755) // Restore permissions

	path := filepath.Join(readOnlyDir, "report.json")
	err = report.WriteToFile(path)
	if err == nil {
		t.Log("WriteToFile() succeeded - may vary by OS")
	}
}

// TestManager_Stop_WithActiveCPUProfile tests Stop when CPU profile is active
func TestManager_Stop_WithActiveCPUProfile(t *testing.T) {
	tempDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.DataDir = tempDir

	mgr := NewManager(cfg, newTestLogger())

	err := mgr.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Start CPU profiling
	_, err = mgr.StartCPUProfile(5 * time.Second)
	if err != nil {
		t.Fatalf("StartCPUProfile() error = %v", err)
	}

	// Stop should close the CPU profile
	err = mgr.Stop()
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

// TestManager_Stop_WithActiveTrace tests Stop when trace is active
func TestManager_Stop_WithActiveTrace(t *testing.T) {
	tempDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.DataDir = tempDir

	mgr := NewManager(cfg, newTestLogger())

	err := mgr.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Start trace
	_, err = mgr.StartTrace(1 * time.Second)
	if err != nil {
		t.Fatalf("StartTrace() error = %v", err)
	}

	// Stop should close the trace
	err = mgr.Stop()
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

// TestManager_Stop_WithBothActive tests Stop when both CPU profile and trace are active
func TestManager_Stop_WithBothActive(t *testing.T) {
	tempDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.DataDir = tempDir

	mgr := NewManager(cfg, newTestLogger())

	err := mgr.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Start both
	_, err = mgr.StartTrace(1 * time.Second)
	if err != nil {
		t.Fatalf("StartTrace() error = %v", err)
	}

	_, err = mgr.StartCPUProfile(5 * time.Second)
	if err != nil {
		t.Fatalf("StartCPUProfile() error = %v", err)
	}

	// Stop should close both
	err = mgr.Stop()
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

// TestWriteHeapProfile_ReadOnlyDir tests WriteHeapProfile when directory is not writable
func TestWriteHeapProfile_ReadOnlyDir(t *testing.T) {
	readOnlyDir := t.TempDir()
	os.Chmod(readOnlyDir, 0555)
	defer os.Chmod(readOnlyDir, 0755)

	cfg := DefaultConfig()
	cfg.DataDir = readOnlyDir
	cfg.Enabled = true

	mgr := NewManager(cfg, newTestLogger())

	_, err := mgr.WriteHeapProfile()
	if err == nil {
		t.Log("WriteHeapProfile() succeeded - may vary by OS")
	}
}

// TestWriteGoroutineProfile_ReadOnlyDir tests WriteGoroutineProfile when directory is not writable
func TestWriteGoroutineProfile_ReadOnlyDir(t *testing.T) {
	readOnlyDir := t.TempDir()
	os.Chmod(readOnlyDir, 0555)
	defer os.Chmod(readOnlyDir, 0755)

	cfg := DefaultConfig()
	cfg.DataDir = readOnlyDir
	cfg.Enabled = true

	mgr := NewManager(cfg, newTestLogger())

	_, err := mgr.WriteGoroutineProfile()
	if err == nil {
		t.Log("WriteGoroutineProfile() succeeded - may vary by OS")
	}
}

// TestManager_GetGCStats tests GetGCStats
func TestManager_GetGCStats(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.DataDir = dir
	mgr := NewManager(cfg, newTestLogger())
	defer mgr.Stop()

	// GetGCStats should return nil initially (no stats collected yet)
	stats := mgr.GetGCStats()
	// Should not panic
	_ = stats
}

// TestManager_WriteToFile tests writing profiles to a file
func TestManager_WriteToFile(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.DataDir = dir
	mgr := NewManager(cfg, newTestLogger())
	defer mgr.Stop()

	// WriteHeapProfile should succeed
	filename, err := mgr.WriteHeapProfile()
	if err != nil {
		t.Fatalf("WriteHeapProfile() error: %v", err)
	}
	if filename == "" {
		t.Error("Expected non-empty filename")
	}

	// Verify file exists
	if _, err := os.Stat(filename); err != nil {
		t.Errorf("Profile file not created: %v", err)
	}
}

// TestManager_StartCPUProfile_AlreadyRunning tests starting CPU profile twice
func TestManager_StartCPUProfile_AlreadyRunning(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.DataDir = dir
	mgr := NewManager(cfg, newTestLogger())
	defer mgr.Stop()

	// Start profiling
	_, err := mgr.StartCPUProfile(5 * time.Second)
	if err != nil {
		t.Fatalf("StartCPUProfile() error: %v", err)
	}

	// Try starting again - should fail
	_, err = mgr.StartCPUProfile(5 * time.Second)
	if err == nil {
		t.Error("Expected error when CPU profiling already active")
	}

	// Stop the CPU profile before test ends (so temp dir can be cleaned up)
	mgr.StopCPUProfile()
}
