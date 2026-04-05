package probe

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"golang.org/x/net/http2"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// gRPCChecker implements gRPC health checks
type gRPCChecker struct{}

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
	return nil
}

// Judge performs the gRPC health check
// Implements grpc.health.v1.Health/Check protocol
func (c *gRPCChecker) Judge(ctx context.Context, soul *core.Soul) (*core.Judgment, error) {
	cfg := soul.GRPC
	if cfg == nil {
		cfg = &core.GRPCConfig{}
	}

	timeout := soul.Timeout.Duration
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	start := time.Now()

	// Use HTTP/2 transport (handles both h2 and h2c)
	transport := &http2.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.InsecureSkipVerify, // Default: false (secure)
			ServerName:         soul.Target,
		},
		AllowHTTP: true, // Allow h2c (HTTP/2 over cleartext)
	}

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
	duration := time.Since(start)

	if err != nil {
		return failJudgment(soul, fmt.Errorf("gRPC request failed: %w", err)), nil
	}
	defer resp.Body.Close()

	// Read response body (limited)
	_, err = io.ReadAll(io.LimitReader(resp.Body, maxReadSize))
	if err != nil {
		// Non-fatal, we got a response - ignore read error
	}

	// Check gRPC status from headers
	grpcStatus := resp.Header.Get("Grpc-Status") // "0" = OK

	judgment := &core.Judgment{
		ID:         core.GenerateID(),
		SoulID:     soul.ID,
		Timestamp:  time.Now().UTC(),
		Duration:   duration,
		StatusCode: resp.StatusCode,
		Details: &core.JudgmentDetails{
			ServiceStatus: "SERVING",
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
	} else {
		judgment.Status = core.SoulAlive
		judgment.Message = fmt.Sprintf("gRPC health check OK in %s", duration.Round(time.Millisecond))
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

// buildHTTP2SettingsFrame builds an HTTP/2 SETTINGS frame
func buildHTTP2SettingsFrame() []byte {
	// Frame header: 3 bytes length + 1 byte type + 1 byte flags + 4 bytes stream ID
	// Empty SETTINGS frame
	frame := make([]byte, 9)
	frame[3] = 0x04 // SETTINGS type
	frame[4] = 0x00 // No flags
	frame[5] = 0x00 // Stream ID = 0
	frame[6] = 0x00
	frame[7] = 0x00
	frame[8] = 0x00
	return frame
}

// buildHTTP2HeadersFrame builds an HTTP/2 HEADERS frame
func buildHTTP2HeadersFrame(host, port string, contentLength int) []byte {
	// Simplified HPACK-encoded headers
	// :method: POST
	// :scheme: http
	// :authority: host:port
	// :path: /grpc.health.v1.Health/Check
	// content-type: application/grpc
	// te: trailers

	// This is a simplified implementation - real HPACK is complex
	// For production, use proper HPACK encoding
	_ = host
	_ = port
	_ = contentLength

	// Return placeholder frame
	frame := make([]byte, 9)
	frame[3] = 0x01 // HEADERS type
	frame[4] = 0x04 // END_HEADERS flag
	frame[5] = 0x00 // Stream ID = 1
	frame[6] = 0x00
	frame[7] = 0x00
	frame[8] = 0x01

	return frame
}

// buildHTTP2DataFrame builds an HTTP/2 DATA frame
func buildHTTP2DataFrame(data []byte, endStream bool) []byte {
	length := len(data)
	frame := make([]byte, 9+length)

	// Length (3 bytes, big-endian)
	frame[0] = byte(length >> 16)
	frame[1] = byte(length >> 8)
	frame[2] = byte(length)

	frame[3] = 0x00 // DATA type
	if endStream {
		frame[4] = 0x01 // END_STREAM flag
	}
	frame[5] = 0x00 // Stream ID = 1
	frame[6] = 0x00
	frame[7] = 0x00
	frame[8] = 0x01

	copy(frame[9:], data)
	return frame
}

// buildGRPCHealthCheckRequest builds a gRPC Health Check protobuf message
func buildGRPCHealthCheckRequest(serviceName string) []byte {
	// gRPC message format: 1 byte compressed flag + 4 bytes length + protobuf data
	// HealthCheckRequest: message { string service = 1; }

	// Encode service name as protobuf field 1 (wire type 2 = length-delimited)
	var msg []byte
	if serviceName != "" {
		// Field tag: (1 << 3) | 2 = 10 = 0x0A
		// Length: len(serviceName)
		msg = append(msg, 0x0A)
		msg = append(msg, byte(len(serviceName)))
		msg = append(msg, []byte(serviceName)...)
	}

	// Add gRPC framing
	framed := make([]byte, 5+len(msg))
	framed[0] = 0 // Not compressed
	binary.BigEndian.PutUint32(framed[1:], uint32(len(msg)))
	copy(framed[5:], msg)

	return framed
}
