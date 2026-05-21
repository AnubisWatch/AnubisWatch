// Package telemetry provides OpenTelemetry tracing configuration for AnubisWatch.
package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Config holds telemetry configuration
type Config struct {
	Enabled        bool
	Endpoint       string  // OTLP collector address (e.g., "localhost:4317")
	ServiceName    string  // Service name for traces
	ServiceVersion string  // Service version
	Environment    string  // Deployment environment (production, staging, dev)
	Insecure       bool    // Use insecure connection (no TLS)
	SampleRate     float64 // Trace sampling rate (0.0-1.0, 1.0 = 100%)
}

// DefaultConfig returns default telemetry configuration
func DefaultConfig() Config {
	return Config{
		Enabled:        false, // Disabled by default
		Endpoint:       os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		ServiceName:    "anubiswatch",
		ServiceVersion: "1.0.0",
		Environment:    os.Getenv("OTEL_ENVIRONMENT"),
		Insecure:       os.Getenv("OTEL_INSECURE") == "true",
		SampleRate:     1.0,
	}
}

// TracerProvider wraps the OpenTelemetry tracer provider
type TracerProvider struct {
	provider *sdktrace.TracerProvider
	logger   *slog.Logger
}

// InitTracer initializes the global tracer provider.
// Returns a cleanup function that must be called on shutdown.
func InitTracer(ctx context.Context, cfg Config, logger *slog.Logger) (*TracerProvider, error) {
	if !cfg.Enabled {
		logger.Info("telemetry disabled, using no-op tracer")
		// Set a no-op tracer provider
		otel.SetTracerProvider(sdktrace.NewTracerProvider())
		return &TracerProvider{provider: nil, logger: logger}, nil
	}

	// Determine endpoint
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	}
	if endpoint == "" {
		endpoint = "localhost:4317" // Default OTLP gRPC endpoint
	}

	// Create resource with service info
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			semconv.DeploymentEnvironment(cfg.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create telemetry resource: %w", err)
	}

	// Configure OTLP exporter
	exporter, err := createExporter(ctx, endpoint, cfg.Insecure)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	// Create sampler based on sample rate
	var sampler sdktrace.Sampler
	if cfg.SampleRate >= 1.0 {
		sampler = sdktrace.AlwaysSample()
	} else if cfg.SampleRate <= 0.0 {
		sampler = sdktrace.NeverSample()
	} else {
		sampler = sdktrace.TraceIDRatioBased(cfg.SampleRate)
	}

	// Create tracer provider
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	// Set global tracer provider
	otel.SetTracerProvider(provider)

	// Set W3C trace context propagator
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	logger.Info("telemetry initialized",
		"endpoint", endpoint,
		"service", cfg.ServiceName,
		"environment", cfg.Environment,
		"sample_rate", cfg.SampleRate)

	return &TracerProvider{provider: provider, logger: logger}, nil
}

// createExporter creates an OTLP gRPC exporter
func createExporter(ctx context.Context, endpoint string, insecure bool) (sdktrace.SpanExporter, error) {
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithTimeout(10 * time.Second),
	}

	if insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	return otlptracegrpc.New(ctx, opts...)
}

// Shutdown gracefully shuts down the tracer provider
func (tp *TracerProvider) Shutdown(ctx context.Context) error {
	if tp.provider != nil {
		tp.logger.Info("shutting down telemetry")
		return tp.provider.Shutdown(ctx)
	}
	return nil
}

// Provider returns the global tracer provider
func Provider() *TracerProvider {
	return &TracerProvider{}
}
