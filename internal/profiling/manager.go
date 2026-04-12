// Package profiling provides runtime profiling and performance monitoring
package profiling

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"sync"
	"time"
)

// Manager handles profiling operations
type Manager struct {
	logger     *slog.Logger
	dataDir    string
	enabled    bool
	mu         sync.RWMutex
	cpuFile    *os.File
	traceFile  *os.File
	memProfile *MemProfile
	cpuProfile *CPUProfile
	goroutines *GoroutineStats
	gcStats    *GCStats
}

// MemProfile tracks memory statistics
type MemProfile struct {
	Alloc        uint64
	TotalAlloc   uint64
	Sys          uint64
	NumGC        uint32
	HeapAlloc    uint64
	HeapSys      uint64
	HeapIdle     uint64
	HeapInuse    uint64
	HeapReleased uint64
	StackInuse   uint64
	StackSys     uint64
	Timestamp    time.Time
}

// CPUProfile tracks CPU usage statistics
type CPUProfile struct {
	NumCPU       int
	NumGoroutine int
	NumCgoCall   int64
	Timestamp    time.Time
}

// GoroutineStats tracks goroutine information
type GoroutineStats struct {
	Count     int
	MaxCount  int
	AvgCount  int
	History   []int
	Timestamp time.Time
}

// GCStats tracks garbage collection statistics
type GCStats struct {
	NumGC      uint32
	PauseTotal time.Duration
	PauseAvg   time.Duration
	PauseMax   time.Duration
	PauseHist  []time.Duration
	Timestamp  time.Time
}

// Config for profiling manager
type Config struct {
	Enabled       bool
	DataDir       string
	CPUProfile    bool
	MemProfile    bool
	GoroutineDump bool
	Trace         bool
	Interval      time.Duration
}

// DefaultConfig returns default profiling configuration
func DefaultConfig() Config {
	return Config{
		Enabled:       false,
		DataDir:       "./profiles",
		CPUProfile:    true,
		MemProfile:    true,
		GoroutineDump: false,
		Trace:         false,
		Interval:      30 * time.Second,
	}
}

// NewManager creates a new profiling manager
func NewManager(cfg Config, logger *slog.Logger) *Manager {
	return &Manager{
		logger:     logger.With("component", "profiling"),
		dataDir:    cfg.DataDir,
		enabled:    cfg.Enabled,
		memProfile: &MemProfile{},
		cpuProfile: &CPUProfile{},
		goroutines: &GoroutineStats{
			History: make([]int, 0, 60),
		},
		gcStats: &GCStats{
			PauseHist: make([]time.Duration, 0, 100),
		},
	}
}

// Start begins profiling collection
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.enabled {
		return nil
	}

	// Create profiles directory
	if err := os.MkdirAll(m.dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create profiles directory: %w", err)
	}

	m.logger.Info("Starting profiling manager", "dir", m.dataDir)

	// Start background collection
	go m.collectLoop()

	return nil
}

// Stop ends profiling collection
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.enabled {
		return nil
	}

	// Close any active profiles
	if m.cpuFile != nil {
		pprof.StopCPUProfile()
		m.cpuFile.Close()
		m.cpuFile = nil
	}

	if m.traceFile != nil {
		trace.Stop()
		m.traceFile.Close()
		m.traceFile = nil
	}

	m.logger.Info("Profiling manager stopped")
	return nil
}

// collectLoop periodically collects runtime statistics
func (m *Manager) collectLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.collectStats()
		}
	}
}

// collectStats collects current runtime statistics
func (m *Manager) collectStats() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Memory stats
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	m.memProfile.Alloc = mem.Alloc
	m.memProfile.TotalAlloc = mem.TotalAlloc
	m.memProfile.Sys = mem.Sys
	m.memProfile.NumGC = mem.NumGC
	m.memProfile.HeapAlloc = mem.HeapAlloc
	m.memProfile.HeapSys = mem.HeapSys
	m.memProfile.HeapIdle = mem.HeapIdle
	m.memProfile.HeapInuse = mem.HeapInuse
	m.memProfile.HeapReleased = mem.HeapReleased
	m.memProfile.StackInuse = mem.StackInuse
	m.memProfile.StackSys = mem.StackSys
	m.memProfile.Timestamp = time.Now()

	// CPU/goroutine stats
	m.cpuProfile.NumCPU = runtime.NumCPU()
	m.cpuProfile.NumGoroutine = runtime.NumGoroutine()
	m.cpuProfile.NumCgoCall = runtime.NumCgoCall()
	m.cpuProfile.Timestamp = time.Now()

	// Goroutine history
	goroCount := runtime.NumGoroutine()
	m.goroutines.Count = goroCount
	if goroCount > m.goroutines.MaxCount {
		m.goroutines.MaxCount = goroCount
	}
	m.goroutines.History = append(m.goroutines.History, goroCount)
	if len(m.goroutines.History) > 60 {
		m.goroutines.History = m.goroutines.History[1:]
	}
	m.goroutines.Timestamp = time.Now()

	// GC stats
	m.gcStats.NumGC = mem.NumGC
	m.gcStats.PauseTotal = time.Duration(mem.PauseTotalNs)
	if mem.NumGC > 0 {
		m.gcStats.PauseAvg = time.Duration(mem.PauseTotalNs / uint64(mem.NumGC))
	}
	m.gcStats.Timestamp = time.Now()

	// Log if memory usage is high
	if mem.HeapAlloc > 1024*1024*1024 { // 1GB
		m.logger.Warn("High memory usage detected",
			"heap_alloc_mb", mem.HeapAlloc/1024/1024,
			"goroutines", goroCount)
	}
}

// GetMemProfile returns current memory profile
func (m *Manager) GetMemProfile() *MemProfile {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.memProfile
}

// GetCPUProfile returns current CPU profile
func (m *Manager) GetCPUProfile() *CPUProfile {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cpuProfile
}

// GetGoroutineStats returns goroutine statistics
func (m *Manager) GetGoroutineStats() *GoroutineStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.goroutines
}

// GetGCStats returns GC statistics
func (m *Manager) GetGCStats() *GCStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.gcStats
}

// StartCPUProfile starts CPU profiling
func (m *Manager) StartCPUProfile(duration time.Duration) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cpuFile != nil {
		return "", fmt.Errorf("CPU profiling already active")
	}

	filename := filepath.Join(m.dataDir, fmt.Sprintf("cpu_%s.pprof", time.Now().Format("20060102_150405")))
	f, err := os.Create(filename)
	if err != nil {
		return "", fmt.Errorf("failed to create CPU profile file: %w", err)
	}

	if err := pprof.StartCPUProfile(f); err != nil {
		f.Close()
		return "", fmt.Errorf("failed to start CPU profile: %w", err)
	}

	m.cpuFile = f
	m.logger.Info("Started CPU profiling", "file", filename, "duration", duration)

	// Stop after duration
	go func() {
		time.Sleep(duration)
		m.StopCPUProfile()
	}()

	return filename, nil
}

// StopCPUProfile stops CPU profiling
func (m *Manager) StopCPUProfile() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cpuFile == nil {
		return nil
	}

	pprof.StopCPUProfile()
	m.cpuFile.Close()
	m.cpuFile = nil

	m.logger.Info("Stopped CPU profiling")
	return nil
}

// WriteHeapProfile writes current heap profile to file
func (m *Manager) WriteHeapProfile() (string, error) {
	filename := filepath.Join(m.dataDir, fmt.Sprintf("heap_%s.pprof", time.Now().Format("20060102_150405")))
	f, err := os.Create(filename)
	if err != nil {
		return "", fmt.Errorf("failed to create heap profile file: %w", err)
	}
	defer f.Close()

	if err := pprof.WriteHeapProfile(f); err != nil {
		return "", fmt.Errorf("failed to write heap profile: %w", err)
	}

	m.logger.Info("Wrote heap profile", "file", filename)
	return filename, nil
}

// WriteGoroutineProfile writes goroutine profile to file
func (m *Manager) WriteGoroutineProfile() (string, error) {
	filename := filepath.Join(m.dataDir, fmt.Sprintf("goroutine_%s.pprof", time.Now().Format("20060102_150405")))
	f, err := os.Create(filename)
	if err != nil {
		return "", fmt.Errorf("failed to create goroutine profile file: %w", err)
	}
	defer f.Close()

	if err := pprof.Lookup("goroutine").WriteTo(f, 0); err != nil {
		return "", fmt.Errorf("failed to write goroutine profile: %w", err)
	}

	m.logger.Info("Wrote goroutine profile", "file", filename)
	return filename, nil
}

// StartTrace starts execution tracing
func (m *Manager) StartTrace(duration time.Duration) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.traceFile != nil {
		return "", fmt.Errorf("trace already active")
	}

	filename := filepath.Join(m.dataDir, fmt.Sprintf("trace_%s.out", time.Now().Format("20060102_150405")))
	f, err := os.Create(filename)
	if err != nil {
		return "", fmt.Errorf("failed to create trace file: %w", err)
	}

	if err := trace.Start(f); err != nil {
		f.Close()
		return "", fmt.Errorf("failed to start trace: %w", err)
	}

	m.traceFile = f
	m.logger.Info("Started execution trace", "file", filename, "duration", duration)

	// Stop after duration
	go func() {
		time.Sleep(duration)
		m.StopTrace()
	}()

	return filename, nil
}

// StopTrace stops execution tracing
func (m *Manager) StopTrace() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.traceFile == nil {
		return nil
	}

	trace.Stop()
	m.traceFile.Close()
	m.traceFile = nil

	m.logger.Info("Stopped execution trace")
	return nil
}

// GetAllStats returns all current statistics
func (m *Manager) GetAllStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return map[string]interface{}{
		"memory":     m.memProfile,
		"cpu":        m.cpuProfile,
		"goroutines": m.goroutines,
		"gc":         m.gcStats,
		"timestamp":  time.Now(),
	}
}

// GenerateReport creates a performance report
func (m *Manager) GenerateReport() (*Report, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	report := &Report{
		Timestamp:  time.Now(),
		MemStats:   *m.memProfile,
		CPUStats:   *m.cpuProfile,
		Goroutines: *m.goroutines,
		GCStats:    *m.gcStats,
	}

	// Calculate derived metrics
	if len(m.goroutines.History) > 0 {
		sum := 0
		for _, v := range m.goroutines.History {
			sum += v
		}
		report.Goroutines.AvgCount = sum / len(m.goroutines.History)
	}

	return report, nil
}

// Report contains performance data
type Report struct {
	Timestamp  time.Time
	MemStats   MemProfile
	CPUStats   CPUProfile
	Goroutines GoroutineStats
	GCStats    GCStats
}

// WriteToFile saves report to JSON file
func (r *Report) WriteToFile(path string) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
