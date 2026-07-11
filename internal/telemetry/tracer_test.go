package telemetry

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestDefaultConfig(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "otel.example:4317")
	t.Setenv("OTEL_ENVIRONMENT", "staging")
	t.Setenv("OTEL_INSECURE", "true")

	cfg := DefaultConfig()
	if cfg.Enabled {
		t.Errorf("Enabled = true, want false (default)")
	}
	if cfg.Endpoint != "otel.example:4317" {
		t.Errorf("Endpoint = %q, want otel.example:4317", cfg.Endpoint)
	}
	if cfg.ServiceName != "anubiswatch" {
		t.Errorf("ServiceName = %q, want anubiswatch", cfg.ServiceName)
	}
	if cfg.ServiceVersion == "" {
		t.Errorf("ServiceVersion is empty")
	}
	if cfg.Environment != "staging" {
		t.Errorf("Environment = %q, want staging", cfg.Environment)
	}
	if !cfg.Insecure {
		t.Errorf("Insecure = false, want true")
	}
	if cfg.SampleRate != 1.0 {
		t.Errorf("SampleRate = %v, want 1.0", cfg.SampleRate)
	}
}

func TestDefaultConfig_NoEnv(t *testing.T) {
	os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	os.Unsetenv("OTEL_ENVIRONMENT")
	os.Unsetenv("OTEL_INSECURE")

	cfg := DefaultConfig()
	if cfg.Endpoint != "" {
		t.Errorf("Endpoint = %q, want empty when env unset", cfg.Endpoint)
	}
	if cfg.Environment != "" {
		t.Errorf("Environment = %q, want empty when env unset", cfg.Environment)
	}
	if cfg.Insecure {
		t.Errorf("Insecure = true, want false when env unset")
	}
}

func TestInitTracer_Disabled(t *testing.T) {
	ctx := context.Background()
	cfg := Config{Enabled: false}

	tp, err := InitTracer(ctx, cfg, newTestLogger())
	if err != nil {
		t.Fatalf("InitTracer(disabled) err = %v", err)
	}
	if tp == nil {
		t.Fatalf("InitTracer(disabled) returned nil provider wrapper")
	}
	if tp.provider != nil {
		t.Errorf("disabled tracer should have nil sdk provider, got %T", tp.provider)
	}

	// Shutdown on a disabled provider is a no-op and must not return an error.
	if err := tp.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown(disabled) err = %v, want nil", err)
	}
}

func TestInitTracer_Enabled(t *testing.T) {
	// Spin up a TCP listener so the exporter has an actual address to dial,
	// but never accept — the exporter uses a batcher so InitTracer returns
	// immediately without needing a real OTLP collector on the other side.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := Config{
		Enabled:        true,
		Endpoint:       listener.Addr().String(),
		ServiceName:    "test-svc",
		ServiceVersion: "v0.0.1",
		Environment:    "test",
		Insecure:       true,
		SampleRate:     1.0,
	}

	tp, err := InitTracer(ctx, cfg, newTestLogger())
	if err != nil {
		t.Fatalf("InitTracer(enabled) err = %v", err)
	}
	if tp == nil || tp.provider == nil {
		t.Fatalf("InitTracer(enabled) returned nil sdk provider")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	// Shutdown can produce a context-deadline error because no collector
	// is actually accepting — that's fine, we're testing the wiring, not
	// the export. Tolerate either nil or a context-deadline-style error.
	if err := tp.Shutdown(shutdownCtx); err != nil && !isExpectedShutdownErr(err) {
		t.Errorf("Shutdown returned unexpected err: %v", err)
	}
}

func TestInitTracer_DefaultEndpoint(t *testing.T) {
	// When config endpoint and env are both empty, InitTracer falls back to
	// localhost:4317 — assert it reaches the createExporter path without
	// panicking. We pass a context that's already canceled so the gRPC
	// dial returns quickly; the batcher swallows the error.
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cfg := Config{
		Enabled:    true,
		Endpoint:   "",
		Insecure:   true,
		SampleRate: 1.0,
	}
	tp, err := InitTracer(ctx, cfg, newTestLogger())
	if err != nil {
		t.Fatalf("InitTracer(default endpoint) err = %v", err)
	}
	if tp == nil || tp.provider == nil {
		t.Fatalf("InitTracer(default endpoint) returned nil provider")
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer shutdownCancel()
	_ = tp.Shutdown(shutdownCtx)
}

func TestInitTracer_SampleRates(t *testing.T) {
	cases := []struct {
		name string
		rate float64
	}{
		{"always", 1.0},
		{"never", 0.0},
		{"ratio", 0.5},
		{"above-one", 2.0},
		{"below-zero", -0.1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("listen: %v", err)
			}
			defer listener.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			cfg := Config{
				Enabled:    true,
				Endpoint:   listener.Addr().String(),
				Insecure:   true,
				SampleRate: tc.rate,
			}
			tp, err := InitTracer(ctx, cfg, newTestLogger())
			if err != nil {
				t.Fatalf("InitTracer(%v) err = %v", tc.rate, err)
			}
			if tp == nil {
				t.Fatalf("nil provider wrapper")
			}
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 1*time.Second)
			_ = tp.Shutdown(shutdownCtx)
			shutdownCancel()
		})
	}
}

func TestProvider_ReturnsEmptyWrapper(t *testing.T) {
	tp := Provider()
	if tp == nil {
		t.Fatalf("Provider() returned nil")
	}
	if tp.provider != nil {
		t.Errorf("Provider() should return empty wrapper, got non-nil sdk provider")
	}
	// Shutdown on an empty wrapper must not panic and must not error.
	if err := tp.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown(empty wrapper) err = %v, want nil", err)
	}
}

func TestInitTracer_EnabledResourceFailure(t *testing.T) {
	oldRes := tracerResource
	tracerResource = func(_ context.Context, _ ...resource.Option) (*resource.Resource, error) {
		return nil, io.ErrUnexpectedEOF
	}
	t.Cleanup(func() { tracerResource = oldRes })
	oldExp := tracerExporter
	tracerExporter = createExporter
	t.Cleanup(func() { tracerExporter = oldExp })

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Endpoint = "localhost:9999"
	if _, err := InitTracer(context.Background(), cfg, newTestLogger()); err == nil {
		t.Fatal("expected resource error")
	}
}

func TestInitTracer_EnabledExporterFailure(t *testing.T) {
	oldRes := tracerResource
	tracerResource = resource.New
	t.Cleanup(func() { tracerResource = oldRes })
	oldExp := tracerExporter
	tracerExporter = func(_ context.Context, _ string, _ bool) (sdktrace.SpanExporter, error) {
		return nil, io.EOF
	}
	t.Cleanup(func() { tracerExporter = oldExp })

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Endpoint = "localhost:9998"
	if _, err := InitTracer(context.Background(), cfg, newTestLogger()); err == nil {
		t.Fatal("expected exporter error")
	}
}

func isExpectedShutdownErr(err error) bool {
	if err == nil {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "context") ||
		strings.Contains(msg, "deadline") ||
		strings.Contains(msg, "canceled")
}
