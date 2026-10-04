package probe

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/http2"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/protobuf/proto"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// gRPCChecker implements gRPC health checks
type gRPCChecker struct{}

func hasNonEmptyStringValue(values map[string]string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

// NewGRPCChecker creates a new gRPC checker
func NewGRPCChecker() *gRPCChecker {
	return &gRPCChecker{}
}

// Type returns the protocol identifier
func (c *gRPCChecker) Type() core.CheckType {
	return core.CheckGRPC
}

// Validate checks configuration
func (c *gRPCChecker) Validate(soul *core.Soul) error {
	if soul.Target == "" {
		return configError("target", "target host:port is required")
	}
	if _, _, err := net.SplitHostPort(soul.Target); err != nil {
		return configError("target", "target must be in host:port format")
	}

	// SSRF protection - validate target address
	if err := ValidateAddress(soul.Target); err != nil {
		return configError("target", fmt.Sprintf("SSRF validation failed: %v", err))
	}
	if soul.GRPC != nil && !soul.GRPC.TLS && hasNonEmptyStringValue(soul.GRPC.Metadata) {
		return configError("grpc.metadata", "metadata credentials require TLS")
	}

	// Security warning for disabled TLS verification
	if soul.GRPC != nil && soul.GRPC.InsecureSkipVerify {
		slog.Warn("SECURITY WARNING: gRPC check has InsecureSkipVerify enabled. TLS certificate verification is disabled. This should only be used for testing, never in production.",
			"soul", soul.Name,
			"soul_id", soul.ID)
	}

	return nil
}

// Judge performs the gRPC health check
// Implements grpc.health.v1.Health/Check protocol
func (c *gRPCChecker) Judge(ctx context.Context, soul *core.Soul) (*core.Judgment, error) {
	cfg := soul.GRPC
	if cfg == nil {
		cfg = &core.GRPCConfig{}
	}
	if !cfg.TLS && hasNonEmptyStringValue(cfg.Metadata) {
		return failJudgment(soul, fmt.Errorf("gRPC metadata credentials require TLS")), nil
	}

	timeout := soul.Timeout.Duration
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	start := time.Now()
	grpcHost, _, splitErr := net.SplitHostPort(soul.Target)
	if splitErr != nil {
		return failJudgment(soul, fmt.Errorf("invalid gRPC target: %w", splitErr)), nil
	}
	// Own the sockets even while HTTP/2 stream cleanup is still unwinding.
	connectionCtx, closeConnections := context.WithCancel(ctx)
	defer closeConnections()

	// Use HTTP/2 transport (handles both h2 and h2c). G402
	// suppress: cfg.InsecureSkipVerify is gated by K7's
	// applySecurityGate at the engine level.
	grpcTLSConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: cfg.InsecureSkipVerify, // #nosec G402 -- see K7 gate
		ServerName:         grpcHost,
	}
	// SSRF: intercept the HTTP/2 transport's dial with pinned-IP protection.
	// WrapDialerContext resolves the hostname, validates all IPs, and dials
	// the literal IP so an attacker who changes DNS between Validate() and
	// Judge() cannot redirect the connection to an internal address.
	grpcDialCtx := DefaultValidator.WrapDialerContext((&net.Dialer{Timeout: timeout}).DialContext)
	transport := &http2.Transport{
		TLSClientConfig: grpcTLSConfig,
		AllowHTTP:       true,
		DialTLSContext: func(ctx context.Context, network, addr string, tlsCfg *tls.Config) (net.Conn, error) {
			raw, err := grpcDialCtx(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			context.AfterFunc(connectionCtx, func() { _ = raw.Close() })
			if tlsCfg == nil {
				return raw, nil // h2c (plaintext HTTP/2)
			}
			handshakeConfig := tlsCfg.Clone()
			if handshakeConfig.ServerName == "" {
				host, _, splitErr := net.SplitHostPort(addr)
				if splitErr != nil {
					raw.Close()
					return nil, splitErr
				}
				handshakeConfig.ServerName = host
			}
			tlsConn := tls.Client(raw, handshakeConfig)
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				raw.Close()
				return nil, err
			}
			return tlsConn, nil
		},
	}
	defer transport.CloseIdleConnections()

	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}

	// Build gRPC health check URL
	scheme := "https"
	if !cfg.TLS {
		scheme = "http"
	}
	path := "/grpc.health.v1.Health/Check"
	if cfg.Service != "" {
		path = fmt.Sprintf("/grpc.health.v1.Health/Check?service=%s", cfg.Service)
	}
	url := fmt.Sprintf("%s://%s%s", scheme, soul.Target, path)

	// Build request body (protobuf HealthCheckRequest)
	body := buildGRPCHealthCheckRequest(cfg.Service)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return failJudgment(soul, fmt.Errorf("failed to create request: %w", err)), nil
	}

	req.Header.Set("Content-Type", "application/grpc")
	req.Header.Set("TE", "trailers")

	resp, err := client.Do(req)

	if err != nil {
		return failJudgment(soul, fmt.Errorf("gRPC request failed: %w", err)), nil
	}
	defer resp.Body.Close()

	// Read response body (limited)
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxReadSize+1))
	duration := time.Since(start)

	// Trailers carry the final RPC status; headers also support trailers-only replies.
	grpcStatus := resp.Header.Get("Grpc-Status") // "0" = OK
	if trailerStatus := resp.Trailer.Get("Grpc-Status"); trailerStatus != "" {
		grpcStatus = trailerStatus
	}

	judgment := &core.Judgment{
		ID:         core.GenerateID(),
		SoulID:     soul.ID,
		Timestamp:  time.Now().UTC(),
		Duration:   duration,
		StatusCode: resp.StatusCode,
		Details: &core.JudgmentDetails{
			ServiceStatus: "UNKNOWN",
		},
	}

	// Determine status based on gRPC response
	if grpcStatus != "0" && grpcStatus != "" {
		judgment.Status = core.SoulDead
		judgment.Message = fmt.Sprintf("gRPC health check failed (grpc-status=%s) in %s",
			grpcStatus, duration.Round(time.Millisecond))
		judgment.Details.ServiceStatus = "NOT_SERVING"
	} else if resp.StatusCode != http.StatusOK {
		judgment.Status = core.SoulDead
		judgment.Message = fmt.Sprintf("gRPC health check failed (HTTP %d) in %s",
			resp.StatusCode, duration.Round(time.Millisecond))
		judgment.Details.ServiceStatus = "NOT_SERVING"
	} else if readErr != nil {
		judgment.Status = core.SoulDead
		judgment.Message = fmt.Sprintf("failed to read gRPC health response: %v", readErr)
	} else if grpcStatus == "" {
		judgment.Status = core.SoulDead
		judgment.Message = "gRPC health check failed: missing grpc-status"
	} else {
		serviceStatus, err := parseGRPCHealthCheckResponse(responseBody)
		if err != nil {
			judgment.Status = core.SoulDead
			judgment.Message = fmt.Sprintf("invalid gRPC health response: %v", err)
		} else {
			judgment.Details.ServiceStatus = serviceStatus.String()
			if serviceStatus == healthpb.HealthCheckResponse_SERVING {
				judgment.Status = core.SoulAlive
				judgment.Message = fmt.Sprintf("gRPC health check OK in %s", duration.Round(time.Millisecond))
			} else {
				judgment.Status = core.SoulDead
				judgment.Message = fmt.Sprintf("gRPC service health is %s in %s", serviceStatus, duration.Round(time.Millisecond))
			}
		}
	}

	// Performance budget check
	if cfg.Feather.Duration > 0 && duration > cfg.Feather.Duration {
		if judgment.Status == core.SoulAlive {
			judgment.Status = core.SoulDegraded
		}
		judgment.Message = fmt.Sprintf("gRPC health check in %s (exceeds feather %s)",
			duration.Round(time.Millisecond), cfg.Feather.Duration)
	}

	return judgment, nil
}

// parseGRPCHealthCheckResponse decodes a single uncompressed unary response.
func parseGRPCHealthCheckResponse(body []byte) (healthpb.HealthCheckResponse_ServingStatus, error) {
	if len(body) > maxReadSize {
		return healthpb.HealthCheckResponse_UNKNOWN, fmt.Errorf("response exceeds %d bytes", maxReadSize)
	}
	if len(body) < 5 {
		return healthpb.HealthCheckResponse_UNKNOWN, fmt.Errorf("missing message frame")
	}
	if body[0] != 0 {
		return healthpb.HealthCheckResponse_UNKNOWN, fmt.Errorf("unsupported compression flag %d", body[0])
	}
	if uint64(binary.BigEndian.Uint32(body[1:5])) != uint64(len(body)-5) {
		return healthpb.HealthCheckResponse_UNKNOWN, fmt.Errorf("message length does not match unary response")
	}
	var response healthpb.HealthCheckResponse
	if err := proto.Unmarshal(body[5:], &response); err != nil {
		return healthpb.HealthCheckResponse_UNKNOWN, fmt.Errorf("invalid health protobuf: %w", err)
	}
	return response.Status, nil
}

// buildGRPCHealthCheckRequest builds a gRPC Health Check protobuf message
func buildGRPCHealthCheckRequest(serviceName string) []byte {
	// gRPC message format: 1 byte compressed flag + 4 bytes length + protobuf data
	// HealthCheckRequest: message { string service = 1; }

	// Encode service name as protobuf field 1 (wire type 2 = length-delimited)
	var msg []byte
	if serviceName != "" {
		// Field tag: (1 << 3) | 2 = 10 = 0x0A
		msg = append(msg, 0x0A)
		msg = binary.AppendUvarint(msg, uint64(len(serviceName)))
		msg = append(msg, []byte(serviceName)...)
	}

	// Add gRPC framing
	framed := make([]byte, 5+len(msg))
	framed[0] = 0 // Not compressed
	binary.BigEndian.PutUint32(framed[1:], uint32(len(msg)))
	copy(framed[5:], msg)

	return framed
}
