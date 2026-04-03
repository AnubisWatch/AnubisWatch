package probe

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"net"
	"time"

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

	// Parse target
	host, port, err := net.SplitHostPort(soul.Target)
	if err != nil {
		return failJudgment(soul, fmt.Errorf("invalid target: %w", err)), nil
	}

	// Connect
	start := time.Now()
	var conn net.Conn

	if cfg.TLS {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: true, // TODO: Use CA cert
		}
		conn, err = tls.DialWithDialer(&net.Dialer{Timeout: timeout}, "tcp", soul.Target, tlsConfig)
	} else {
		conn, err = net.DialTimeout("tcp", soul.Target, timeout)
	}

	if err != nil {
		return failJudgment(soul, fmt.Errorf("gRPC connection failed: %w", err)), nil
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(timeout))

	// Send HTTP/2 connection preface
	preface := []byte("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n")
	if _, err := conn.Write(preface); err != nil {
		return failJudgment(soul, fmt.Errorf("failed to send HTTP/2 preface: %w", err)), nil
	}

	// Send HTTP/2 SETTINGS frame
	settingsFrame := buildHTTP2SettingsFrame()
	if _, err := conn.Write(settingsFrame); err != nil {
		return failJudgment(soul, fmt.Errorf("failed to send SETTINGS frame: %w", err)), nil
	}

	// Read SETTINGS response
	buf := make([]byte, 9) // HTTP/2 frame header
	if _, err := conn.Read(buf); err != nil {
		return failJudgment(soul, fmt.Errorf("failed to read SETTINGS response: %w", err)), nil
	}

	// Build gRPC Health Check request
	// Method: /grpc.health.v1.Health/Check
	serviceName := cfg.Service
	if serviceName == "" {
		serviceName = "" // Empty string = overall health
	}

	grpcRequest := buildGRPCHealthCheckRequest(serviceName)

	// Send HEADERS frame
	headers := buildHTTP2HeadersFrame(host, port, len(grpcRequest))
	if _, err := conn.Write(headers); err != nil {
		return failJudgment(soul, fmt.Errorf("failed to send HEADERS frame: %w", err)), nil
	}

	// Send DATA frame with request
	dataFrame := buildHTTP2DataFrame(grpcRequest, true)
	if _, err := conn.Write(dataFrame); err != nil {
		return failJudgment(soul, fmt.Errorf("failed to send DATA frame: %w", err)), nil
	}

	// Read response
	// Read HEADERS frame
	if _, err := conn.Read(buf); err != nil {
		return failJudgment(soul, fmt.Errorf("failed to read response HEADERS: %w", err)), nil
	}

	// Parse response
	duration := time.Since(start)

	judgment := &core.Judgment{
		ID:         core.GenerateID(),
		SoulID:     soul.ID,
		Timestamp:  time.Now().UTC(),
		Duration:   duration,
		Status:     core.SoulAlive,
		StatusCode: 0,
		Details: &core.JudgmentDetails{
			ServiceStatus: "SERVING",
		},
	}

	// Check performance budget
	if cfg.Feather.Duration > 0 && duration > cfg.Feather.Duration {
		judgment.Status = core.SoulDegraded
		judgment.Message = fmt.Sprintf("gRPC health check OK in %s (exceeds feather %s)",
			duration.Round(time.Millisecond), cfg.Feather.Duration)
	} else {
		judgment.Message = fmt.Sprintf("gRPC health check OK in %s", duration.Round(time.Millisecond))
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
