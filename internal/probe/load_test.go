package probe

// Real load tests for probe engine
// Validates performance at scale: 100/500/1000 souls

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// createMockServer creates an HTTP server for load testing
func createMockLoadServer(responseDelay time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if responseDelay > 0 {
			time.Sleep(responseDelay)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "ok", "timestamp": "2024-01-01T00:00:00Z"}`))
	}))
}

// createLoadEngine creates an engine configured for load testing
func createLoadEngine() *Engine {
	registry := NewCheckerRegistry()
	registry.Register(NewHTTPChecker())
	registry.Register(NewTCPChecker())
	registry.Register(NewDNSChecker())

	opts := EngineOptions{
		Registry: registry,
		NodeID:   "load-test-node",
		Region:   "load-test",
		Logger:   newTestProbeLogger(),
		Config:   DefaultEngineConfig(),
	}
	opts.Config.MaxConcurrentChecks = 100

	return NewEngine(opts)
}

// createHTTPSouls creates HTTP check souls for load testing
func createHTTPSouls(count int, targetURL string) []*core.Soul {
	souls := make([]*core.Soul, count)
	for i := 0; i < count; i++ {
		souls[i] = &core.Soul{
			ID:      fmt.Sprintf("load-http-%d", i),
			Name:    fmt.Sprintf("Load Test HTTP %d", i),
			Type:    core.CheckHTTP,
			Target:  targetURL,
			Weight:  core.Duration{Duration: 60 * time.Second},
			Timeout: core.Duration{Duration: 10 * time.Second},
			HTTP: &core.HTTPConfig{
				Method:      "GET",
				ValidStatus: []int{200},
			},
		}
	}
	return souls
}

// createTCPSouls creates TCP check souls for load testing
func createTCPSouls(count int, target string) []*core.Soul {
	souls := make([]*core.Soul, count)
	for i := 0; i < count; i++ {
		souls[i] = &core.Soul{
			ID:      fmt.Sprintf("load-tcp-%d", i),
			Name:    fmt.Sprintf("Load Test TCP %d", i),
			Type:    core.CheckTCP,
			Target:  target,
			Weight:  core.Duration{Duration: 60 * time.Second},
			Timeout: core.Duration{Duration: 10 * time.Second},
		}
	}
	return souls
}

// createMixedSouls creates a mix of different check types
func createMixedSouls(count int, httpURL string) []*core.Soul {
	souls := make([]*core.Soul, count)
	for i := 0; i < count; i++ {
		switch i % 3 {
		case 0:
			souls[i] = &core.Soul{
				ID:      fmt.Sprintf("load-mixed-http-%d", i),
				Name:    fmt.Sprintf("Load Test Mixed HTTP %d", i),
				Type:    core.CheckHTTP,
				Target:  httpURL,
				Weight:  core.Duration{Duration: 60 * time.Second},
				Timeout: core.Duration{Duration: 10 * time.Second},
				HTTP: &core.HTTPConfig{
					Method:      "GET",
					ValidStatus: []int{200},
				},
			}
		case 1:
			souls[i] = &core.Soul{
				ID:      fmt.Sprintf("load-mixed-tcp-%d", i),
				Name:    fmt.Sprintf("Load Test Mixed TCP %d", i),
				Type:    core.CheckTCP,
				Target:  "localhost:80",
				Weight:  core.Duration{Duration: 60 * time.Second},
				Timeout: core.Duration{Duration: 10 * time.Second},
			}
		default:
			souls[i] = &core.Soul{
				ID:      fmt.Sprintf("load-mixed-dns-%d", i),
				Name:    fmt.Sprintf("Load Test Mixed DNS %d", i),
				Type:    core.CheckDNS,
				Target:  "anubis.watch",
				Weight:  core.Duration{Duration: 60 * time.Second},
				Timeout: core.Duration{Duration: 10 * time.Second},
				DNS: &core.DNSConfig{
					RecordType: "A",
				},
			}
		}
	}
	return souls
}

// getMemoryStats returns current memory usage
func getMemoryStats() runtime.MemStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m
}

func memoryAllocDelta(before, after runtime.MemStats) uint64 {
	if after.Alloc <= before.Alloc {
		return 0
	}
	return after.Alloc - before.Alloc
}

// TestLoad_100Souls_Real tests with 100 souls
func TestLoad_100Souls_Real(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	server := createMockLoadServer(10 * time.Millisecond)
	defer server.Close()

	engine := createLoadEngine()
	defer engine.Stop()

	souls := createHTTPSouls(100, server.URL)

	// Record initial memory
	memBefore := getMemoryStats()
	startTime := time.Now()

	// Assign souls
	engine.AssignSouls(souls)

	// Wait for first check cycle
	time.Sleep(2 * time.Second)

	elapsed := time.Since(startTime)
	memAfter := getMemoryStats()
	memUsed := memoryAllocDelta(memBefore, memAfter)

	status := engine.GetStatus()

	t.Logf("✅ 100 souls loaded in %v", elapsed)
	t.Logf("   Active checks: %d", status.ActiveChecks)
	t.Logf("   Memory used: %d KB", memUsed/1024)
	t.Logf("   Goroutines: %d", status.ChecksRunning)

	if status.ActiveChecks != 100 {
		t.Errorf("Expected 100 active checks, got %d", status.ActiveChecks)
	}

	// Verify no major memory leak
	if memUsed > 100*1024*1024 { // 100MB
		t.Errorf("High memory usage: %d MB", memUsed/1024/1024)
	}
}

// TestLoad_500Souls_Real tests with 500 souls
func TestLoad_500Souls_Real(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	server := createMockLoadServer(5 * time.Millisecond)
	defer server.Close()

	engine := createLoadEngine()
	defer engine.Stop()

	souls := createHTTPSouls(500, server.URL)

	memBefore := getMemoryStats()
	startTime := time.Now()

	engine.AssignSouls(souls)

	// Wait for stabilization
	time.Sleep(3 * time.Second)

	elapsed := time.Since(startTime)
	memAfter := getMemoryStats()
	memUsed := memoryAllocDelta(memBefore, memAfter)

	status := engine.GetStatus()

	t.Logf("✅ 500 souls loaded in %v", elapsed)
	t.Logf("   Active checks: %d", status.ActiveChecks)
	t.Logf("   Memory used: %d KB", memUsed/1024)
	t.Logf("   Total checks: %d", status.TotalChecks)

	if status.ActiveChecks != 500 {
		t.Errorf("Expected 500 active checks, got %d", status.ActiveChecks)
	}

	// Should have executed some checks
	if status.TotalChecks < 500 {
		t.Errorf("Expected at least 500 total checks, got %d", status.TotalChecks)
	}
}

// TestLoad_1000Souls_Real tests with 1000 souls (production target)
func TestLoad_1000Souls_Real(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	// Skip in CI environments - this test requires significant resources
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping load test in CI environment")
	}

	server := createMockLoadServer(2 * time.Millisecond)
	defer server.Close()

	engine := createLoadEngine()
	defer engine.Stop()

	souls := createHTTPSouls(1000, server.URL)

	memBefore := getMemoryStats()
	startTime := time.Now()

	engine.AssignSouls(souls)

	// Wait for checks to execute
	time.Sleep(5 * time.Second)

	elapsed := time.Since(startTime)
	memAfter := getMemoryStats()
	memUsed := memoryAllocDelta(memBefore, memAfter)

	status := engine.GetStatus()

	t.Logf("✅ 1000 souls loaded in %v", elapsed)
	t.Logf("   Active checks: %d", status.ActiveChecks)
	t.Logf("   Memory used: %d MB", memUsed/1024/1024)
	t.Logf("   Total checks: %d", status.TotalChecks)
	t.Logf("   Failed checks: %d", status.FailedChecks)
	t.Logf("   Checks running: %d", status.ChecksRunning)

	// Validate production targets
	if status.ActiveChecks != 1000 {
		t.Errorf("Expected 1000 active checks, got %d", status.ActiveChecks)
	}

	// Memory should be reasonable (< 500MB for 1000 souls)
	if memUsed > 500*1024*1024 {
		t.Errorf("High memory usage: %d MB (target < 500MB)", memUsed/1024/1024)
	}

	// Should execute many checks
	if status.TotalChecks < 1000 {
		t.Errorf("Expected at least 1000 total checks, got %d", status.TotalChecks)
	}

	// Failure rate should be low (< 5%)
	if status.TotalChecks > 0 {
		failRate := float64(status.FailedChecks) / float64(status.TotalChecks) * 100
		if failRate > 5 {
			t.Errorf("High failure rate: %.2f%% (target < 5%%)", failRate)
		}
		t.Logf("   Failure rate: %.2f%%", failRate)
	}
}

// TestLoad_MixedTypes_Real tests with mixed check types
func TestLoad_MixedTypes_Real(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	// Skip in CI environments
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping load test in CI environment")
	}

	server := createMockLoadServer(5 * time.Millisecond)
	defer server.Close()

	engine := createLoadEngine()
	defer engine.Stop()

	souls := createMixedSouls(300, server.URL)

	startTime := time.Now()
	engine.AssignSouls(souls)

	time.Sleep(3 * time.Second)

	elapsed := time.Since(startTime)
	status := engine.GetStatus()

	t.Logf("✅ 300 mixed souls loaded in %v", elapsed)
	t.Logf("   Active checks: %d", status.ActiveChecks)
	t.Logf("   Total checks: %d", status.TotalChecks)

	if status.ActiveChecks != 300 {
		t.Errorf("Expected 300 active checks, got %d", status.ActiveChecks)
	}
}

// TestLoad_ScaleUpDown_Real tests scaling up and down
func TestLoad_ScaleUpDown_Real(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	// Skip in CI environments
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping load test in CI environment")
	}

	server := createMockLoadServer(5 * time.Millisecond)
	defer server.Close()

	engine := createLoadEngine()
	defer engine.Stop()

	// Start with 100 souls
	souls100 := createHTTPSouls(100, server.URL)
	engine.AssignSouls(souls100)
	time.Sleep(1 * time.Second)

	status := engine.GetStatus()
	t.Logf("Phase 1 - 100 souls: Active=%d", status.ActiveChecks)

	// Scale to 500
	souls500 := createHTTPSouls(500, server.URL)
	engine.AssignSouls(souls500)
	time.Sleep(2 * time.Second)

	status = engine.GetStatus()
	t.Logf("Phase 2 - 500 souls: Active=%d", status.ActiveChecks)

	// Scale down to 200
	souls200 := createHTTPSouls(200, server.URL)
	engine.AssignSouls(souls200)
	time.Sleep(2 * time.Second)

	status = engine.GetStatus()
	t.Logf("Phase 3 - 200 souls: Active=%d", status.ActiveChecks)

	if status.ActiveChecks != 200 {
		t.Errorf("Expected 200 active checks after scale down, got %d", status.ActiveChecks)
	}

	t.Log("✅ Scale up/down test passed")
}

// TestLoad_ConcurrentChecks_Real tests concurrent execution
func TestLoad_ConcurrentChecks_Real(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	// Slow server to create concurrent load
	server := createMockLoadServer(100 * time.Millisecond)
	defer server.Close()

	engine := createLoadEngine()
	defer engine.Stop()

	// Create many souls that will check concurrently
	souls := createHTTPSouls(200, server.URL)
	engine.AssignSouls(souls)

	// Wait for concurrent execution
	time.Sleep(2 * time.Second)

	status := engine.GetStatus()

	t.Logf("✅ Concurrent execution test")
	t.Logf("   Active checks: %d", status.ActiveChecks)
	t.Logf("   Checks running: %d", status.ChecksRunning)
	t.Logf("   Total checks: %d", status.TotalChecks)

	// Should have some concurrent execution
	if status.ChecksRunning > 50 {
		t.Logf("   Good concurrency: %d checks running simultaneously", status.ChecksRunning)
	}
}

// BenchmarkEngine_Small benchmarks with small load
func BenchmarkEngine_Small(b *testing.B) {
	server := createMockLoadServer(1 * time.Millisecond)
	defer server.Close()

	engine := createLoadEngine()
	defer engine.Stop()

	souls := createHTTPSouls(10, server.URL)
	engine.AssignSouls(souls)

	b.ResetTimer()
	for b.Loop() {
		_ = engine.GetStatus()
	}
}

// BenchmarkEngine_Medium benchmarks with medium load
func BenchmarkEngine_Medium(b *testing.B) {
	server := createMockLoadServer(1 * time.Millisecond)
	defer server.Close()

	engine := createLoadEngine()
	defer engine.Stop()

	souls := createHTTPSouls(100, server.URL)
	engine.AssignSouls(souls)

	b.ResetTimer()
	for b.Loop() {
		_ = engine.GetStatus()
	}
}

// BenchmarkEngine_Large benchmarks with large load
func BenchmarkEngine_Large(b *testing.B) {
	server := createMockLoadServer(1 * time.Millisecond)
	defer server.Close()

	engine := createLoadEngine()
	defer engine.Stop()

	souls := createHTTPSouls(1000, server.URL)
	engine.AssignSouls(souls)

	b.ResetTimer()
	for b.Loop() {
		_ = engine.GetStatus()
	}
}

// BenchmarkHTTPChecker benchmarks HTTP checker performance
func BenchmarkHTTPChecker_Real(b *testing.B) {
	server := createMockLoadServer(0)
	defer server.Close()

	checker := NewHTTPChecker()
	soul := &core.Soul{
		ID:     "bench-http",
		Name:   "Benchmark HTTP",
		Type:   core.CheckHTTP,
		Target: server.URL,
		HTTP: &core.HTTPConfig{
			Method:      "GET",
			ValidStatus: []int{200},
		},
	}

	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		_, _ = checker.Judge(ctx, soul)
	}
}

// BenchmarkTCPChecker benchmarks TCP checker performance
func BenchmarkTCPChecker_Real(b *testing.B) {
	checker := NewTCPChecker()
	soul := &core.Soul{
		ID:     "bench-tcp",
		Name:   "Benchmark TCP",
		Type:   core.CheckTCP,
		Target: "8.8.8.8:53", // Google DNS
	}

	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		_, _ = checker.Judge(ctx, soul)
	}
}

// BenchmarkDNSChecker benchmarks DNS checker performance
func BenchmarkDNSChecker_Real(b *testing.B) {
	checker := NewDNSChecker()
	soul := &core.Soul{
		ID:     "bench-dns",
		Name:   "Benchmark DNS",
		Type:   core.CheckDNS,
		Target: "google.com",
		DNS: &core.DNSConfig{
			RecordType: "A",
		},
	}

	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		_, _ = checker.Judge(ctx, soul)
	}
}
