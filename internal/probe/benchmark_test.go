package probe

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// BenchmarkHTTPChecker_Judge benchmarks HTTP health checks
func BenchmarkHTTPChecker_Judge(b *testing.B) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status": "healthy"}`))
	}))
	defer server.Close()

	checker := NewHTTPChecker()
	soul := &core.Soul{
		ID:      "bench-http",
		Name:    "Benchmark HTTP",
		Type:    core.CheckHTTP,
		Target:  server.URL,
		Timeout: core.Duration{Duration: 5 * time.Second},
		HTTP: &core.HTTPConfig{
			Method:       "GET",
			ValidStatus:  []int{200},
			BodyContains: "healthy",
		},
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = checker.Judge(ctx, soul)
	}
}

// BenchmarkTCPChecker_Judge benchmarks TCP health checks
func BenchmarkTCPChecker_Judge(b *testing.B) {
	// Create test server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Write([]byte("Hello\n"))
			conn.Close()
		}
	}()

	checker := NewTCPChecker()
	soul := &core.Soul{
		ID:      "bench-tcp",
		Name:    "Benchmark TCP",
		Type:    core.CheckTCP,
		Target:  listener.Addr().String(),
		Timeout: core.Duration{Duration: 5 * time.Second},
		TCP: &core.TCPConfig{
			BannerMatch: "Hello",
		},
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = checker.Judge(ctx, soul)
	}
}

// BenchmarkCheckerRegistry_Get benchmarks registry lookups
func BenchmarkCheckerRegistry_Get(b *testing.B) {
	registry := NewCheckerRegistry()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = registry.Get(core.CheckHTTP)
	}
}

// BenchmarkEngine_AssignSouls benchmarks soul assignment
func BenchmarkEngine_AssignSouls(b *testing.B) {
	registry := NewCheckerRegistry()
	engine := NewEngine(EngineOptions{
		Registry: registry,
		NodeID:   "bench-node",
	})
	defer engine.Stop()

	souls := make([]*core.Soul, 100)
	for i := 0; i < 100; i++ {
		souls[i] = &core.Soul{
			ID:      fmt.Sprintf("soul-%d", i),
			Name:    fmt.Sprintf("Soul %d", i),
			Type:    core.CheckHTTP,
			Target:  "http://localhost:8080",
			Enabled: true,
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.AssignSouls(souls)
	}
}

// BenchmarkParallelChecks benchmarks parallel health checks
func BenchmarkParallelChecks(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1 * time.Millisecond) // Simulate some work
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := NewHTTPChecker()
	soul := &core.Soul{
		ID:      "bench-parallel",
		Name:    "Benchmark Parallel",
		Type:    core.CheckHTTP,
		Target:  server.URL,
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = checker.Judge(ctx, soul)
		}
	})
}
