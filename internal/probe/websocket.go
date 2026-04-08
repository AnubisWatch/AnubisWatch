package probe

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// WebSocketChecker implements WebSocket health checks
type WebSocketChecker struct{}

// NewWebSocketChecker creates a new WebSocket checker
func NewWebSocketChecker() *WebSocketChecker {
	return &WebSocketChecker{}
}

// Type returns the protocol identifier
func (c *WebSocketChecker) Type() core.CheckType {
	return core.CheckWebSocket
}

// Validate checks configuration
func (c *WebSocketChecker) Validate(soul *core.Soul) error {
	if soul.Target == "" {
		return configError("target", "target URL is required")
	}
	u, err := url.Parse(soul.Target)
	if err != nil {
		return configError("target", "invalid URL: "+err.Error())
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return configError("target", "URL must use ws:// or wss:// scheme")
	}

	// Security warning for disabled TLS verification
	if soul.WebSocket != nil && soul.WebSocket.InsecureSkipVerify {
		slog.Warn("SECURITY WARNING: WebSocket check has InsecureSkipVerify enabled. TLS certificate verification is disabled. This should only be used for testing, never in production.",
			"soul", soul.Name,
			"soul_id", soul.ID)
	}

	return nil
}

// Judge performs the WebSocket check
func (c *WebSocketChecker) Judge(ctx context.Context, soul *core.Soul) (*core.Judgment, error) {
	cfg := soul.WebSocket
	if cfg == nil {
		cfg = &core.WebSocketConfig{}
	}

	timeout := soul.Timeout.Duration
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	// Parse URL
	u, err := url.Parse(soul.Target)
	if err != nil {
		return failJudgment(soul, fmt.Errorf("invalid URL: %w", err)), nil
	}

	// Determine host and port
	host := u.Host
	if !strings.Contains(host, ":") {
		if u.Scheme == "wss" {
			host += ":443"
		} else {
			host += ":80"
		}
	}

	// Connect
	start := time.Now()
	var conn net.Conn

	if u.Scheme == "wss" {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: cfg.InsecureSkipVerify, // Default: false (secure)
			ServerName:         u.Hostname(),
		}
		conn, err = tls.DialWithDialer(&net.Dialer{Timeout: timeout}, "tcp", host, tlsConfig)
	} else {
		conn, err = net.DialTimeout("tcp", host, timeout)
	}

	if err != nil {
		return failJudgment(soul, fmt.Errorf("connection failed: %w", err)), nil
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(timeout))

	// Generate WebSocket key
	wsKey := generateWebSocketKey()

	// Build upgrade request
	req := &http.Request{
		Method: "GET",
		URL:    u,
		Header: make(http.Header),
		Host:   u.Host,
	}

	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-WebSocket-Key", wsKey)
	req.Header.Set("Sec-WebSocket-Version", "13")

	// Add custom headers
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}

	// Add subprotocols
	if len(cfg.Subprotocols) > 0 {
		req.Header.Set("Sec-WebSocket-Protocol", strings.Join(cfg.Subprotocols, ", "))
	}

	// Send request
	if err := req.Write(conn); err != nil {
		return failJudgment(soul, fmt.Errorf("failed to send upgrade request: %w", err)), nil
	}

	// Read response
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, req)
	if err != nil {
		return failJudgment(soul, fmt.Errorf("failed to read upgrade response: %w", err)), nil
	}
	defer resp.Body.Close()

	duration := time.Since(start)

	judgment := &core.Judgment{
		ID:         core.GenerateID(),
		SoulID:     soul.ID,
		Timestamp:  time.Now().UTC(),
		Duration:   duration,
		StatusCode: resp.StatusCode,
		Details:    &core.JudgmentDetails{},
	}

	// Check response
	if resp.StatusCode != http.StatusSwitchingProtocols {
		judgment.Status = core.SoulDead
		judgment.Message = fmt.Sprintf("WebSocket upgrade failed: %s", resp.Status)
		return judgment, nil
	}

	if resp.Header.Get("Upgrade") != "websocket" {
		judgment.Status = core.SoulDead
		judgment.Message = "Server did not accept WebSocket upgrade"
		return judgment, nil
	}

	// Verify Sec-WebSocket-Accept
	expectedAccept := calculateWebSocketAccept(wsKey)
	if resp.Header.Get("Sec-WebSocket-Accept") != expectedAccept {
		judgment.Status = core.SoulDead
		judgment.Message = "Invalid Sec-WebSocket-Accept header"
		return judgment, nil
	}

	// Send message if configured
	if cfg.Send != "" {
		frame := buildWebSocketTextFrame(cfg.Send)
		if _, err := conn.Write(frame); err != nil {
			return failJudgment(soul, fmt.Errorf("failed to send WebSocket message: %w", err)), nil
		}

		// Read response (limited to maxMessageSize)
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		responseBuf := make([]byte, maxMessageSize)
		n, err := conn.Read(responseBuf)
		if err != nil {
			return failJudgment(soul, fmt.Errorf("failed to read WebSocket response: %w", err)), nil
		}

		// Parse frame (simplified)
		if n > 2 {
			// Check for text frame (opcode 1) or binary (opcode 2)
			opcode := responseBuf[0] & 0x0F
			if opcode == 1 { // Text frame
				// Decode payload length
				payloadLen := int(responseBuf[1] & 0x7F)
				var payloadStart int
				if payloadLen < 126 {
					payloadStart = 2
				} else if payloadLen == 126 {
					payloadLen = int(responseBuf[2])<<8 | int(responseBuf[3])
					payloadStart = 4
				}

				if payloadStart+payloadLen <= n {
					payload := string(responseBuf[payloadStart : payloadStart+payloadLen])

					// Check expected content
					if cfg.ExpectContains != "" {
						if !strings.Contains(payload, cfg.ExpectContains) {
							judgment.Status = core.SoulDead
							judgment.Message = fmt.Sprintf("WebSocket response does not contain: %s", cfg.ExpectContains)
							return judgment, nil
						}
					}
				}
			}
		}
	}

	// Ping check if requested
	if cfg.PingCheck {
		pingFrame := []byte{0x89, 0x00} // Ping frame with no payload
		if _, err := conn.Write(pingFrame); err != nil {
			return failJudgment(soul, fmt.Errorf("failed to send ping: %w", err)), nil
		}

		// Wait for pong
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		pongBuf := make([]byte, 10)
		n, err := conn.Read(pongBuf)
		if err != nil || n < 2 {
			return failJudgment(soul, fmt.Errorf("ping/pong failed: %w", err)), nil
		}

		// Check for pong frame (opcode 0x0A)
		if (pongBuf[0] & 0x0F) != 0x0A {
			judgment.Status = core.SoulDegraded
			judgment.Message = "Did not receive pong response"
			return judgment, nil
		}
	}

	judgment.Status = core.SoulAlive
	judgment.Message = fmt.Sprintf("WebSocket connected in %s", duration.Round(time.Millisecond))

	// Check performance budget
	if cfg.Feather.Duration > 0 && duration > cfg.Feather.Duration {
		judgment.Status = core.SoulDegraded
		judgment.Message = fmt.Sprintf("WebSocket connected in %s (exceeds feather %s)",
			duration.Round(time.Millisecond), cfg.Feather.Duration)
	}

	return judgment, nil
}

// generateWebSocketKey generates a random WebSocket key per RFC 6455
func generateWebSocketKey() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to deterministic (should never happen in practice)
		for i := range b {
			b[i] = byte(i * 7)
		}
	}
	return base64.StdEncoding.EncodeToString(b)
}

// calculateWebSocketAccept calculates the Sec-WebSocket-Accept header value
func calculateWebSocketAccept(key string) string {
	// Append magic string and SHA1 hash
	magic := key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	hash := sha1.Sum([]byte(magic))
	return base64.StdEncoding.EncodeToString(hash[:])
}

// buildWebSocketTextFrame builds a WebSocket text frame
func buildWebSocketTextFrame(payload string) []byte {
	payloadBytes := []byte(payload)
	length := len(payloadBytes)

	var frame []byte
	frame = append(frame, 0x81) // FIN=1, opcode=1 (text)

	if length < 126 {
		frame = append(frame, byte(length))
	} else if length < 65536 {
		frame = append(frame, 126)
		frame = append(frame, byte(length>>8))
		frame = append(frame, byte(length))
	} else {
		frame = append(frame, 127)
		for i := 7; i >= 0; i-- {
			frame = append(frame, byte(length>>(i*8)))
		}
	}

	frame = append(frame, payloadBytes...)
	return frame
}
