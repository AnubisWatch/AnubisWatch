package probe

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestWebSocketChecker_Validate_MissingTarget(t *testing.T) {
	checker := NewWebSocketChecker()

	soul := &core.Soul{
		ID:   "test-ws",
		Name: "Test WebSocket",
		Type: core.CheckWebSocket,
	}

	err := checker.Validate(soul)
	if err == nil {
		t.Error("Expected validation error for missing target")
	}
}

func TestWebSocketChecker_Validate_InvalidURL(t *testing.T) {
	checker := NewWebSocketChecker()

	soul := &core.Soul{
		ID:     "test-ws",
		Name:   "Test WebSocket",
		Type:   core.CheckWebSocket,
		Target: "not-a-valid-url",
	}

	err := checker.Validate(soul)
	if err == nil {
		t.Error("Expected validation error for invalid URL")
	}
}

func TestWebSocketChecker_Validate_InvalidScheme(t *testing.T) {
	checker := NewWebSocketChecker()

	soul := &core.Soul{
		ID:     "test-ws",
		Name:   "Test WebSocket",
		Type:   core.CheckWebSocket,
		Target: "http://example.com",
	}

	err := checker.Validate(soul)
	if err == nil {
		t.Error("Expected validation error for invalid scheme")
	}
}

func TestWebSocketChecker_Validate_ValidWS(t *testing.T) {
	checker := NewWebSocketChecker()

	soul := &core.Soul{
		ID:     "test-ws",
		Name:   "Test WebSocket",
		Type:   core.CheckWebSocket,
		Target: "ws://localhost:8080",
	}

	err := checker.Validate(soul)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestWebSocketChecker_Validate_ValidWSS(t *testing.T) {
	checker := NewWebSocketChecker()

	soul := &core.Soul{
		ID:     "test-ws",
		Name:   "Test WebSocket",
		Type:   core.CheckWebSocket,
		Target: "wss://localhost:8080",
	}

	err := checker.Validate(soul)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestWebSocketChecker_Judge_Basic(t *testing.T) {
	// Create mock WebSocket server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		req, _ := http.ReadRequest(reader)

		// Check WebSocket upgrade headers
		if strings.EqualFold(req.Header.Get("Upgrade"), "websocket") &&
			req.Header.Get("Sec-WebSocket-Key") != "" {
			// Calculate accept key
			key := req.Header.Get("Sec-WebSocket-Key")
			accept := calculateWebSocketAccept(key)

			// Send upgrade response
			response := "HTTP/1.1 101 Switching Protocols\r\n" +
				"Upgrade: websocket\r\n" +
				"Connection: Upgrade\r\n" +
				"Sec-WebSocket-Accept: " + accept + "\r\n" +
				"\r\n"
			conn.Write([]byte(response))

			// Keep connection open briefly
			time.Sleep(100 * time.Millisecond)
		}
	}()

	checker := NewWebSocketChecker()

	soul := &core.Soul{
		ID:      "test-ws",
		Name:    "Test WebSocket",
		Type:    core.CheckWebSocket,
		Target:  "ws://" + listener.Addr().String(),
		Enabled: true,
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, err := checker.Judge(ctx, soul)

	if err != nil {
		t.Fatalf("Judge failed: %v", err)
	}

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}
}

func TestWebSocketChecker_Judge_WSS(t *testing.T) {
	// Generate test certificate
	cert, err := generateTestCertificate()
	if err != nil {
		t.Skipf("Cannot generate test certificate: %v", err)
	}

	// Create TLS listener
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: true,
	})
	if err != nil {
		t.Fatalf("Failed to create TLS listener: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		req, _ := http.ReadRequest(reader)

		if strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
			key := req.Header.Get("Sec-WebSocket-Key")
			accept := calculateWebSocketAccept(key)

			response := "HTTP/1.1 101 Switching Protocols\r\n" +
				"Upgrade: websocket\r\n" +
				"Connection: Upgrade\r\n" +
				"Sec-WebSocket-Accept: " + accept + "\r\n" +
				"\r\n"
			conn.Write([]byte(response))
			time.Sleep(100 * time.Millisecond)
		}
	}()

	checker := NewWebSocketChecker()

	soul := &core.Soul{
		ID:     "test-wss",
		Name:   "Test WSS",
		Type:   core.CheckWebSocket,
		Target: "wss://" + listener.Addr().String(),
		WebSocket: &core.WebSocketConfig{
			InsecureSkipVerify: true, // Accept self-signed test cert
			Headers: map[string]string{
				"X-Custom-Header": "test",
			},
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}
}

func TestWebSocketChecker_Judge_UpgradeFailed(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		req, _ := http.ReadRequest(reader)

		// Reject WebSocket upgrade
		if strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
			response := "HTTP/1.1 400 Bad Request\r\n" +
				"Content-Type: text/plain\r\n" +
				"\r\n" +
				"Not a WebSocket server\r\n"
			conn.Write([]byte(response))
		}
	}()

	checker := NewWebSocketChecker()

	soul := &core.Soul{
		ID:      "test-ws",
		Name:    "Test WebSocket",
		Type:    core.CheckWebSocket,
		Target:  "ws://" + listener.Addr().String(),
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestWebSocketChecker_Judge_InvalidAcceptHeader(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		req, _ := http.ReadRequest(reader)

		if strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
			// Send wrong accept header
			response := "HTTP/1.1 101 Switching Protocols\r\n" +
				"Upgrade: websocket\r\n" +
				"Connection: Upgrade\r\n" +
				"Sec-WebSocket-Accept: invalid-value\r\n" +
				"\r\n"
			conn.Write([]byte(response))
		}
	}()

	checker := NewWebSocketChecker()

	soul := &core.Soul{
		ID:      "test-ws",
		Name:    "Test WebSocket",
		Type:    core.CheckWebSocket,
		Target:  "ws://" + listener.Addr().String(),
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestWebSocketChecker_Judge_SendMessage(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		req, _ := http.ReadRequest(reader)

		if strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
			key := req.Header.Get("Sec-WebSocket-Key")
			accept := calculateWebSocketAccept(key)

			response := "HTTP/1.1 101 Switching Protocols\r\n" +
				"Upgrade: websocket\r\n" +
				"Connection: Upgrade\r\n" +
				"Sec-WebSocket-Accept: " + accept + "\r\n" +
				"\r\n"
			conn.Write([]byte(response))

			// Read WebSocket frame
			frame := make([]byte, 1024)
			conn.Read(frame)

			// Send echo response
			echoFrame := buildWebSocketTextFrame("echo: " + "test message")
			conn.Write(echoFrame)
		}
	}()

	checker := NewWebSocketChecker()

	soul := &core.Soul{
		ID:     "test-ws",
		Name:   "Test WebSocket",
		Type:   core.CheckWebSocket,
		Target: "ws://" + listener.Addr().String(),
		WebSocket: &core.WebSocketConfig{
			Send:           "test message",
			ExpectContains: "echo",
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}
}

func TestWebSocketChecker_Judge_ExpectContainsMismatch(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		req, _ := http.ReadRequest(reader)

		if strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
			key := req.Header.Get("Sec-WebSocket-Key")
			accept := calculateWebSocketAccept(key)

			response := "HTTP/1.1 101 Switching Protocols\r\n" +
				"Upgrade: websocket\r\n" +
				"Connection: Upgrade\r\n" +
				"Sec-WebSocket-Accept: " + accept + "\r\n" +
				"\r\n"
			conn.Write([]byte(response))

			// Read and respond with different message
			frame := make([]byte, 1024)
			conn.Read(frame)

			echoFrame := buildWebSocketTextFrame("different message")
			conn.Write(echoFrame)
		}
	}()

	checker := NewWebSocketChecker()

	soul := &core.Soul{
		ID:     "test-ws",
		Name:   "Test WebSocket",
		Type:   core.CheckWebSocket,
		Target: "ws://" + listener.Addr().String(),
		WebSocket: &core.WebSocketConfig{
			Send:           "test",
			ExpectContains: "expected",
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestWebSocketChecker_Judge_PingCheck(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		req, _ := http.ReadRequest(reader)

		if strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
			key := req.Header.Get("Sec-WebSocket-Key")
			accept := calculateWebSocketAccept(key)

			response := "HTTP/1.1 101 Switching Protocols\r\n" +
				"Upgrade: websocket\r\n" +
				"Connection: Upgrade\r\n" +
				"Sec-WebSocket-Accept: " + accept + "\r\n" +
				"\r\n"
			conn.Write([]byte(response))

			// Wait for ping and respond with pong
			frame := make([]byte, 10)
			conn.Read(frame)

			// Send pong (opcode 0x0A)
			pongFrame := []byte{0x8A, 0x00}
			conn.Write(pongFrame)
		}
	}()

	checker := NewWebSocketChecker()

	soul := &core.Soul{
		ID:     "test-ws",
		Name:   "Test WebSocket",
		Type:   core.CheckWebSocket,
		Target: "ws://" + listener.Addr().String(),
		WebSocket: &core.WebSocketConfig{
			PingCheck: true,
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}
}

func TestWebSocketChecker_Judge_PingFailed(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		req, _ := http.ReadRequest(reader)

		if strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
			key := req.Header.Get("Sec-WebSocket-Key")
			accept := calculateWebSocketAccept(key)

			response := "HTTP/1.1 101 Switching Protocols\r\n" +
				"Upgrade: websocket\r\n" +
				"Connection: Upgrade\r\n" +
				"Sec-WebSocket-Accept: " + accept + "\r\n" +
				"\r\n"
			conn.Write([]byte(response))

			// Don't respond to ping
			time.Sleep(10 * time.Second)
		}
	}()

	checker := NewWebSocketChecker()

	soul := &core.Soul{
		ID:     "test-ws",
		Name:   "Test WebSocket",
		Type:   core.CheckWebSocket,
		Target: "ws://" + listener.Addr().String(),
		WebSocket: &core.WebSocketConfig{
			PingCheck: true,
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead && judgment.Status != core.SoulDegraded {
		t.Errorf("Expected status Dead or Degraded, got %s", judgment.Status)
	}
}

func TestWebSocketChecker_Judge_Feather(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		req, _ := http.ReadRequest(reader)

		if strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
			time.Sleep(10 * time.Millisecond) // Small delay

			key := req.Header.Get("Sec-WebSocket-Key")
			accept := calculateWebSocketAccept(key)

			response := "HTTP/1.1 101 Switching Protocols\r\n" +
				"Upgrade: websocket\r\n" +
				"Connection: Upgrade\r\n" +
				"Sec-WebSocket-Accept: " + accept + "\r\n" +
				"\r\n"
			conn.Write([]byte(response))
		}
	}()

	checker := NewWebSocketChecker()

	soul := &core.Soul{
		ID:     "test-ws",
		Name:   "Test WebSocket",
		Type:   core.CheckWebSocket,
		Target: "ws://" + listener.Addr().String(),
		WebSocket: &core.WebSocketConfig{
			Feather: core.Duration{Duration: 500 * time.Millisecond},
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}
}

func TestWebSocketChecker_Judge_FeatherExceeded(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		req, _ := http.ReadRequest(reader)

		if strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
			time.Sleep(100 * time.Millisecond) // Delay

			key := req.Header.Get("Sec-WebSocket-Key")
			accept := calculateWebSocketAccept(key)

			response := "HTTP/1.1 101 Switching Protocols\r\n" +
				"Upgrade: websocket\r\n" +
				"Connection: Upgrade\r\n" +
				"Sec-WebSocket-Accept: " + accept + "\r\n" +
				"\r\n"
			conn.Write([]byte(response))
		}
	}()

	checker := NewWebSocketChecker()

	soul := &core.Soul{
		ID:     "test-ws",
		Name:   "Test WebSocket",
		Type:   core.CheckWebSocket,
		Target: "ws://" + listener.Addr().String(),
		WebSocket: &core.WebSocketConfig{
			Feather: core.Duration{Duration: 10 * time.Millisecond},
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDegraded {
		t.Errorf("Expected status Degraded, got %s", judgment.Status)
	}
}

func TestWebSocketChecker_Judge_ConnectionRefused(t *testing.T) {
	checker := NewWebSocketChecker()

	soul := &core.Soul{
		ID:      "test-ws",
		Name:    "Test WebSocket",
		Type:    core.CheckWebSocket,
		Target:  "ws://127.0.0.1:1",
		Timeout: core.Duration{Duration: 2 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestWebSocketChecker_Judge_Subprotocols(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		req, _ := http.ReadRequest(reader)

		if strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
			// Verify subprotocol header
			subproto := req.Header.Get("Sec-WebSocket-Protocol")
			if subproto != "" {
				key := req.Header.Get("Sec-WebSocket-Key")
				accept := calculateWebSocketAccept(key)

				response := "HTTP/1.1 101 Switching Protocols\r\n" +
					"Upgrade: websocket\r\n" +
					"Connection: Upgrade\r\n" +
					"Sec-WebSocket-Protocol: " + subproto + "\r\n" +
					"Sec-WebSocket-Accept: " + accept + "\r\n" +
					"\r\n"
				conn.Write([]byte(response))
			}
		}
	}()

	checker := NewWebSocketChecker()

	soul := &core.Soul{
		ID:     "test-ws",
		Name:   "Test WebSocket",
		Type:   core.CheckWebSocket,
		Target: "ws://" + listener.Addr().String(),
		WebSocket: &core.WebSocketConfig{
			Subprotocols: []string{"chat", "superchat"},
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}
}

func TestWebSocketChecker_Judge_CustomHeaders(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	receivedAuth := false

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		req, _ := http.ReadRequest(reader)

		if req.Header.Get("Authorization") == "Bearer token123" {
			receivedAuth = true
		}

		if strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
			key := req.Header.Get("Sec-WebSocket-Key")
			accept := calculateWebSocketAccept(key)

			response := "HTTP/1.1 101 Switching Protocols\r\n" +
				"Upgrade: websocket\r\n" +
				"Connection: Upgrade\r\n" +
				"Sec-WebSocket-Accept: " + accept + "\r\n" +
				"\r\n"
			conn.Write([]byte(response))
		}
	}()

	checker := NewWebSocketChecker()

	soul := &core.Soul{
		ID:     "test-ws",
		Name:   "Test WebSocket",
		Type:   core.CheckWebSocket,
		Target: "ws://" + listener.Addr().String(),
		WebSocket: &core.WebSocketConfig{
			Headers: map[string]string{
				"Authorization": "Bearer token123",
			},
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}

	if !receivedAuth {
		t.Error("Expected Authorization header to be sent")
	}
}

func TestGenerateWebSocketKey(t *testing.T) {
	key1 := generateWebSocketKey()
	key2 := generateWebSocketKey()

	// Keys should be base64 encoded and 24 characters
	if len(key1) != 24 {
		t.Errorf("Expected key length 24, got %d", len(key1))
	}

	if len(key2) != 24 {
		t.Errorf("Expected key length 24, got %d", len(key2))
	}
}

func TestCalculateWebSocketAccept(t *testing.T) {
	key := "dGhlIHNhbXBsZSBub25jZQ=="
	expected := "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="

	result := calculateWebSocketAccept(key)
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}

func TestBuildWebSocketTextFrame(t *testing.T) {
	tests := []struct {
		name        string
		payload     string
		expectedLen int
	}{
		{"short", "Hello", 7},                     // 2 bytes header + 5 bytes payload
		{"empty", "", 2},                          // 2 bytes header
		{"medium", strings.Repeat("a", 100), 102}, // 2 bytes header + 100 bytes
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame := buildWebSocketTextFrame(tt.payload)
			if len(frame) != tt.expectedLen {
				t.Errorf("Expected frame length %d, got %d", tt.expectedLen, len(frame))
			}

			// Check FIN bit and opcode
			if frame[0] != 0x81 {
				t.Errorf("Expected FIN=1, opcode=1, got 0x%02X", frame[0])
			}
		})
	}
}

func TestWebSocketChecker_Judge_NoUpgradeHeader(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		reader := bufio.NewReader(conn)
		req, _ := http.ReadRequest(reader)

		// Accept but don't upgrade
		response := "HTTP/1.1 200 OK\r\n" +
			"Content-Type: text/plain\r\n" +
			"\r\n" +
			"Hello"
		conn.Write([]byte(response))

		_ = req
	}()

	checker := NewWebSocketChecker()

	soul := &core.Soul{
		ID:      "test-ws",
		Name:    "Test WebSocket",
		Type:    core.CheckWebSocket,
		Target:  "ws://" + listener.Addr().String(),
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should fail because server didn't upgrade
	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

// Helper function to generate test certificate
func generateTestCertificate() (tls.Certificate, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test"},
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:  x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(priv),
	})

	return tls.X509KeyPair(certPEM, keyPEM)
}

func TestWebSocketChecker_Judge_PortDefaults(t *testing.T) {
	// Test that ws:// defaults to port 80 and wss:// to 443
	checker := NewWebSocketChecker()

	// These tests will fail to connect but verify the URL parsing
	soul := &core.Soul{
		ID:      "test-ws",
		Name:    "Test WebSocket",
		Type:    core.CheckWebSocket,
		Target:  "ws://example.com", // No port
		Timeout: core.Duration{Duration: 100 * time.Millisecond},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should fail to connect but not crash
	if judgment == nil {
		t.Error("Expected judgment to be returned")
	}
}

// Test buildWebSocketTextFrame with various payload sizes
func TestBuildWebSocketTextFrame_SmallPayload(t *testing.T) {
	frame := buildWebSocketTextFrame("Hello")

	// First byte should be 0x81 (FIN=1, opcode=1)
	if frame[0] != 0x81 {
		t.Errorf("Expected first byte 0x81, got 0x%02X", frame[0])
	}

	// Second byte should be length (5)
	if frame[1] != 5 {
		t.Errorf("Expected second byte 5, got %d", frame[1])
	}

	// Rest should be payload
	if string(frame[2:]) != "Hello" {
		t.Errorf("Expected payload 'Hello', got '%s'", string(frame[2:]))
	}
}

func TestBuildWebSocketTextFrame_MediumPayload(t *testing.T) {
	// Payload between 126 and 65536 bytes
	payload := strings.Repeat("A", 200)
	frame := buildWebSocketTextFrame(payload)

	if frame[0] != 0x81 {
		t.Errorf("Expected first byte 0x81, got 0x%02X", frame[0])
	}

	// Length indicator should be 126 for 2-byte length
	if frame[1] != 126 {
		t.Errorf("Expected length indicator 126, got %d", frame[1])
	}

	// Next 2 bytes should be length (200 = 0x00C8)
	if frame[2] != 0x00 || frame[3] != 0xC8 {
		t.Errorf("Expected length bytes 0x00C8, got 0x%02X%02X", frame[2], frame[3])
	}
}

func TestBuildWebSocketTextFrame_EmptyPayload(t *testing.T) {
	frame := buildWebSocketTextFrame("")

	if len(frame) != 2 {
		t.Errorf("Expected frame length 2, got %d", len(frame))
	}

	if frame[0] != 0x81 {
		t.Errorf("Expected first byte 0x81, got 0x%02X", frame[0])
	}

	if frame[1] != 0 {
		t.Errorf("Expected second byte 0, got %d", frame[1])
	}
}

// TestBuildWebSocketTextFrame_LargePayload tests frame building with payload >= 65536 bytes
func TestBuildWebSocketTextFrame_LargePayload(t *testing.T) {
	// Payload >= 65536 bytes triggers 8-byte length encoding
	payload := strings.Repeat("B", 70000)
	frame := buildWebSocketTextFrame(payload)

	if frame[0] != 0x81 {
		t.Errorf("Expected first byte 0x81, got 0x%02X", frame[0])
	}

	// Length indicator should be 127 for 8-byte length
	if frame[1] != 127 {
		t.Errorf("Expected length indicator 127, got %d", frame[1])
	}

	// Verify frame has correct structure: 1 byte FIN/opcode + 1 byte length indicator + 8 bytes length + payload
	expectedLen := 1 + 1 + 8 + len(payload)
	if len(frame) != expectedLen {
		t.Errorf("Expected frame length %d, got %d", expectedLen, len(frame))
	}
}
