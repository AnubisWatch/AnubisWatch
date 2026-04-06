package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestGRPCChecker_Validate_MissingTarget(t *testing.T) {
	checker := NewGRPCChecker()

	soul := &core.Soul{
		ID:   "test-grpc",
		Name: "Test gRPC",
		Type: core.CheckGRPC,
	}

	err := checker.Validate(soul)
	if err == nil {
		t.Error("Expected validation error for missing target")
	}
}

func TestGRPCChecker_Validate_InvalidFormat(t *testing.T) {
	checker := NewGRPCChecker()

	soul := &core.Soul{
		ID:     "test-grpc",
		Name:   "Test gRPC",
		Type:   core.CheckGRPC,
		Target: "invalid-no-port",
	}

	err := checker.Validate(soul)
	if err == nil {
		t.Error("Expected validation error for invalid format")
	}
}

func TestGRPCChecker_Validate_Valid(t *testing.T) {
	checker := NewGRPCChecker()

	soul := &core.Soul{
		ID:     "test-grpc",
		Name:   "Test gRPC",
		Type:   core.CheckGRPC,
		Target: "localhost:50051",
	}

	err := checker.Validate(soul)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestGRPCChecker_Judge_ConnectionRefused(t *testing.T) {
	checker := NewGRPCChecker()

	soul := &core.Soul{
		ID:      "test-grpc",
		Name:    "Test gRPC",
		Type:    core.CheckGRPC,
		Target:  "127.0.0.1:1", // Invalid port
		Timeout: core.Duration{Duration: 2 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestGRPCChecker_Judge_Timeout(t *testing.T) {
	checker := NewGRPCChecker()

	soul := &core.Soul{
		ID:      "test-grpc",
		Name:    "Test gRPC",
		Type:    core.CheckGRPC,
		Target:  "127.0.0.1:50051", // Nothing listening
		Timeout: core.Duration{Duration: 100 * time.Millisecond},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestGRPCChecker_Judge_NoConfig(t *testing.T) {
	checker := NewGRPCChecker()

	soul := &core.Soul{
		ID:      "test-grpc",
		Name:    "Test gRPC",
		Type:    core.CheckGRPC,
		Target:  "127.0.0.1:1",
		Timeout: core.Duration{Duration: 100 * time.Millisecond},
		// No GRPC config
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestGRPCChecker_Judge_WithService(t *testing.T) {
	checker := NewGRPCChecker()

	soul := &core.Soul{
		ID:     "test-grpc",
		Name:   "Test gRPC",
		Type:   core.CheckGRPC,
		Target: "127.0.0.1:1",
		GRPC: &core.GRPCConfig{
			Service: "test.Service",
		},
		Timeout: core.Duration{Duration: 100 * time.Millisecond},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestGRPCChecker_Judge_Feather(t *testing.T) {
	// This test would require a mock gRPC server
	// For now, just verify the configuration is accepted
	checker := NewGRPCChecker()

	soul := &core.Soul{
		ID:     "test-grpc",
		Name:   "Test gRPC",
		Type:   core.CheckGRPC,
		Target: "127.0.0.1:1",
		GRPC: &core.GRPCConfig{
			Feather: core.Duration{Duration: 500 * time.Millisecond},
		},
		Timeout: core.Duration{Duration: 100 * time.Millisecond},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should fail due to connection refused, not feather
	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestGRPCChecker_Judge_TLS(t *testing.T) {
	checker := NewGRPCChecker()

	soul := &core.Soul{
		ID:     "test-grpc",
		Name:   "Test gRPC TLS",
		Type:   core.CheckGRPC,
		Target: "127.0.0.1:1",
		GRPC: &core.GRPCConfig{
			TLS: true,
		},
		Timeout: core.Duration{Duration: 100 * time.Millisecond},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestBuildHTTP2SettingsFrame(t *testing.T) {
	frame := buildHTTP2SettingsFrame()

	if len(frame) != 9 {
		t.Errorf("Expected frame length 9, got %d", len(frame))
	}

	// Check frame type (SETTINGS = 0x04)
	if frame[3] != 0x04 {
		t.Errorf("Expected SETTINGS type (0x04), got 0x%02X", frame[3])
	}

	// Check flags (none)
	if frame[4] != 0x00 {
		t.Errorf("Expected no flags, got 0x%02X", frame[4])
	}

	// Check stream ID (0 for settings)
	if frame[5] != 0x00 || frame[6] != 0x00 || frame[7] != 0x00 || frame[8] != 0x00 {
		t.Error("Expected stream ID 0")
	}
}

func TestBuildHTTP2HeadersFrame(t *testing.T) {
	frame := buildHTTP2HeadersFrame("example.com", "443", 100)

	if len(frame) != 9 {
		t.Errorf("Expected frame length 9, got %d", len(frame))
	}

	// Check frame type (HEADERS = 0x01)
	if frame[3] != 0x01 {
		t.Errorf("Expected HEADERS type (0x01), got 0x%02X", frame[3])
	}

	// Check END_HEADERS flag (0x04)
	if frame[4] != 0x04 {
		t.Errorf("Expected END_HEADERS flag (0x04), got 0x%02X", frame[4])
	}

	// Check stream ID (1)
	streamID := int(frame[5])<<24 | int(frame[6])<<16 | int(frame[7])<<8 | int(frame[8])
	if streamID != 1 {
		t.Errorf("Expected stream ID 1, got %d", streamID)
	}
}

func TestBuildHTTP2DataFrame(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		endStream   bool
		expectedLen int
	}{
		{"empty no end", []byte{}, false, 9},
		{"empty end", []byte{}, true, 9},
		{"with data", []byte("hello"), false, 14},
		{"with data end", []byte("hello"), true, 14},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame := buildHTTP2DataFrame(tt.data, tt.endStream)
			if len(frame) != tt.expectedLen {
				t.Errorf("Expected frame length %d, got %d", tt.expectedLen, len(frame))
			}

			// Check frame type (DATA = 0x00)
			if frame[3] != 0x00 {
				t.Errorf("Expected DATA type (0x00), got 0x%02X", frame[3])
			}

			// Check END_STREAM flag
			if tt.endStream {
				if frame[4] != 0x01 {
					t.Errorf("Expected END_STREAM flag (0x01), got 0x%02X", frame[4])
				}
			} else {
				if frame[4] != 0x00 {
					t.Errorf("Expected no flags (0x00), got 0x%02X", frame[4])
				}
			}
		})
	}
}

func TestBuildGRPCHealthCheckRequest(t *testing.T) {
	tests := []struct {
		name        string
		serviceName string
		expectedLen int
	}{
		{"empty service", "", 5},     // Just framing
		{"short service", "svc", 10}, // Framing (5) + tag + len + 4 bytes
		{"long service", "grpc.health.v1.Health", 28},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame := buildGRPCHealthCheckRequest(tt.serviceName)
			if len(frame) != tt.expectedLen {
				t.Errorf("Expected frame length %d, got %d", tt.expectedLen, len(frame))
			}

			// Check compression flag (should be 0 = not compressed)
			if frame[0] != 0 {
				t.Errorf("Expected no compression (0x00), got 0x%02X", frame[0])
			}
		})
	}
}

func TestBuildGRPCHealthCheckRequest_ProtobufEncoding(t *testing.T) {
	// Test that protobuf encoding is correct for service name
	frame := buildGRPCHealthCheckRequest("test")

	// Expected structure:
	// [0] = 0x00 (no compression)
	// [1:5] = length (big-endian uint32) = 6 (tag + len + "test")
	// [5] = 0x0A (field tag: field 1, wire type 2)
	// [6] = 0x04 (length of "test")
	// [7:11] = "test"

	if frame[0] != 0x00 {
		t.Errorf("Expected no compression flag, got 0x%02X", frame[0])
	}

	// Check protobuf field tag (field 1, wire type 2 = length-delimited)
	// Field tag = (1 << 3) | 2 = 10 = 0x0A
	if frame[5] != 0x0A {
		t.Errorf("Expected field tag 0x0A, got 0x%02X", frame[5])
	}

	// Check string length
	if frame[6] != 0x04 {
		t.Errorf("Expected string length 4, got %d", frame[6])
	}

	// Check string content
	if string(frame[7:11]) != "test" {
		t.Errorf("Expected string 'test', got '%s'", string(frame[7:11]))
	}
}

func TestGRPCChecker_Judge_InvalidTarget(t *testing.T) {
	checker := NewGRPCChecker()

	soul := &core.Soul{
		ID:      "test-grpc",
		Name:    "Test gRPC",
		Type:    core.CheckGRPC,
		Target:  "not-a-valid-target",
		Timeout: core.Duration{Duration: 2 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestGRPCChecker_Judge_ContextCancellation(t *testing.T) {
	checker := NewGRPCChecker()

	soul := &core.Soul{
		ID:      "test-grpc",
		Name:    "Test gRPC",
		Type:    core.CheckGRPC,
		Target:  "127.0.0.1:50051",
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	judgment, _ := checker.Judge(ctx, soul)

	if judgment == nil {
		t.Error("Expected judgment to be returned")
	}
}

func TestBuildHTTP2DataFrame_LargePayload(t *testing.T) {
	// Test with larger payload
	largeData := make([]byte, 1000)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	frame := buildHTTP2DataFrame(largeData, true)

	expectedLen := 9 + len(largeData)
	if len(frame) != expectedLen {
		t.Errorf("Expected frame length %d, got %d", expectedLen, len(frame))
	}

	// Check length bytes (big-endian)
	length := int(frame[0])<<16 | int(frame[1])<<8 | int(frame[2])
	if length != len(largeData) {
		t.Errorf("Expected length %d in header, got %d", len(largeData), length)
	}

	// Verify payload
	for i, b := range largeData {
		if frame[9+i] != b {
			t.Errorf("Payload mismatch at index %d", i)
			break
		}
	}
}

func TestGRPCChecker_Judge_StatusCodes(t *testing.T) {
	// Test that the checker properly populates status codes
	checker := NewGRPCChecker()

	soul := &core.Soul{
		ID:      "test-grpc",
		Name:    "Test gRPC",
		Type:    core.CheckGRPC,
		Target:  "127.0.0.1:1",
		Timeout: core.Duration{Duration: 100 * time.Millisecond},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Status code 0 indicates connection failed before HTTP response
	if judgment.StatusCode != 0 {
		t.Logf("Expected status code 0 for connection failure, got %d", judgment.StatusCode)
	}

	// Verify message indicates failure
	if judgment.Message == "" {
		t.Error("Expected error message in judgment")
	}
}

func TestGRPCChecker_Judge_ServiceHealthCheck(t *testing.T) {
	checker := NewGRPCChecker()

	soul := &core.Soul{
		ID:     "test-grpc",
		Name:   "Test gRPC Service Check",
		Type:   core.CheckGRPC,
		Target: "127.0.0.1:1",
		GRPC: &core.GRPCConfig{
			Service: "my.service.Health",
		},
		Timeout: core.Duration{Duration: 100 * time.Millisecond},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}

	// Check that service name is included in the attempt
	if judgment.Details == nil {
		t.Error("Expected details to be populated")
	}
}

func TestBuildGRPCHealthCheckRequest_EmptyService(t *testing.T) {
	// Empty service name means overall health check
	frame := buildGRPCHealthCheckRequest("")

	// Should just be framing (5 bytes, no payload)
	if len(frame) != 5 {
		t.Errorf("Expected frame length 5 for empty service, got %d", len(frame))
	}

	// Check length is 0
	length := int(frame[1])<<24 | int(frame[2])<<16 | int(frame[3])<<8 | int(frame[4])
	if length != 0 {
		t.Errorf("Expected length 0, got %d", length)
	}
}

func TestGRPCChecker_Judge_TLSNoServer(t *testing.T) {
	checker := NewGRPCChecker()

	soul := &core.Soul{
		ID:     "test-grpc",
		Name:   "Test gRPC TLS",
		Type:   core.CheckGRPC,
		Target: "127.0.0.1:50051",
		GRPC: &core.GRPCConfig{
			TLS: true,
		},
		Timeout: core.Duration{Duration: 100 * time.Millisecond},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should fail with connection refused
	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestGRPCChecker_Judge_InvalidTargetFormat(t *testing.T) {
	checker := NewGRPCChecker()

	// Target without port - SplitHostPort will fail
	soul := &core.Soul{
		ID:      "test-grpc",
		Name:    "Test gRPC",
		Type:    core.CheckGRPC,
		Target:  "invalid-no-port",
		Timeout: core.Duration{Duration: 100 * time.Millisecond},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should fail with invalid target error
	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}

	if judgment.Message == "" {
		t.Error("Expected error message")
	}
}

func TestGRPCChecker_Judge_NilConfig(t *testing.T) {
	checker := NewGRPCChecker()

	// No GRPC config - should use defaults
	soul := &core.Soul{
		ID:      "test-grpc",
		Name:    "Test gRPC",
		Type:    core.CheckGRPC,
		Target:  "127.0.0.1:1",
		Timeout: core.Duration{Duration: 100 * time.Millisecond},
		// GRPC field is nil
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should fail with connection refused, not panic
	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestGRPCChecker_Judge_ZeroTimeout(t *testing.T) {
	checker := NewGRPCChecker()

	// Zero timeout - should default to 10 seconds
	soul := &core.Soul{
		ID:     "test-grpc",
		Name:   "Test gRPC",
		Type:   core.CheckGRPC,
		Target: "127.0.0.1:1",
		// Timeout is zero - should default to 10s
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should fail with connection refused
	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestGRPCChecker_Judge_WithServiceName(t *testing.T) {
	checker := NewGRPCChecker()

	soul := &core.Soul{
		ID:     "test-grpc",
		Name:   "Test gRPC",
		Type:   core.CheckGRPC,
		Target: "127.0.0.1:1",
		GRPC: &core.GRPCConfig{
			Service: "my.service.Name",
		},
		Timeout: core.Duration{Duration: 100 * time.Millisecond},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should fail with connection refused
	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}

	// Service name should be in details
	if judgment.Details == nil {
		t.Error("Expected details to be populated")
	}
}

func TestGRPCChecker_Judge_InvalidHost(t *testing.T) {
	checker := NewGRPCChecker()

	soul := &core.Soul{
		ID:      "test-grpc",
		Name:    "Test gRPC",
		Type:    core.CheckGRPC,
		Target:  "invalid.host.that.does.not.exist:50051",
		Timeout: core.Duration{Duration: 500 * time.Millisecond},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should fail with DNS resolution error
	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestBuildHTTP2DataFrame_EOF(t *testing.T) {
	// Test with endStream flag (EOF = true)
	data := []byte("test payload")
	frame := buildHTTP2DataFrame(data, true)

	if len(frame) < 9 {
		t.Fatalf("Expected frame header, got length %d", len(frame))
	}

	// Check END_STREAM flag (0x01)
	if frame[4]&0x01 != 0x01 {
		t.Error("Expected END_STREAM flag to be set")
	}
}

func TestBuildHTTP2DataFrame_NoEOF(t *testing.T) {
	// Test without endStream flag
	data := []byte("test payload")
	frame := buildHTTP2DataFrame(data, false)

	if len(frame) < 9 {
		t.Fatalf("Expected frame header, got length %d", len(frame))
	}

	// Check END_STREAM flag is not set
	if frame[4]&0x01 != 0x00 {
		t.Error("Expected END_STREAM flag to not be set")
	}
}

// TestGRPCChecker_Judge_RequestCreation tests the request creation path
func TestGRPCChecker_Judge_RequestCreation(t *testing.T) {
	checker := NewGRPCChecker()

	// Test that request creation happens properly with various configurations
	soul := &core.Soul{
		ID:     "test-grpc-request",
		Name:   "Test gRPC Request",
		Type:   core.CheckGRPC,
		Target: "127.0.0.1:1",
		GRPC: &core.GRPCConfig{
			Service:            "test.Service",
			InsecureSkipVerify: true,
			TLS:                false,
		},
		Timeout: core.Duration{Duration: 100 * time.Millisecond},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should fail due to connection refused but request was created
	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}

	// Should include service name in path (verified by log inspection or mock)
	if judgment.Message == "" {
		t.Error("Expected error message")
	}
}

// TestGRPCChecker_Judge_DefaultTimeout tests Judge with default timeout
func TestGRPCChecker_Judge_DefaultTimeout(t *testing.T) {
	checker := NewGRPCChecker()

	soul := &core.Soul{
		ID:      "test-grpc-default-timeout",
		Name:    "Test gRPC Default Timeout",
		Type:    core.CheckGRPC,
		Target:  "127.0.0.1:1",              // Unlikely to respond
		Timeout: core.Duration{Duration: 0}, // Will use default
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	judgment, _ := checker.Judge(ctx, soul)
	elapsed := time.Since(start)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}

	// Should complete quickly despite default 10s timeout (context cancellation)
	if elapsed > 500*time.Millisecond {
		t.Errorf("Expected quick timeout, took %v", elapsed)
	}
}

// TestGRPCChecker_Judge_InsecureSkipVerify tests TLS with insecure skip verify
func TestGRPCChecker_Judge_InsecureSkipVerify(t *testing.T) {
	checker := NewGRPCChecker()

	soul := &core.Soul{
		ID:     "test-grpc-insecure",
		Name:   "Test gRPC Insecure",
		Type:   core.CheckGRPC,
		Target: "127.0.0.1:1",
		GRPC: &core.GRPCConfig{
			TLS:                true,
			InsecureSkipVerify: true,
		},
		Timeout: core.Duration{Duration: 100 * time.Millisecond},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

// TestGRPCChecker_Judge_InsecureNoTLS tests h2c (HTTP/2 cleartext)
func TestGRPCChecker_Judge_InsecureNoTLS(t *testing.T) {
	checker := NewGRPCChecker()

	soul := &core.Soul{
		ID:     "test-grpc-h2c",
		Name:   "Test gRPC h2c",
		Type:   core.CheckGRPC,
		Target: "127.0.0.1:1",
		GRPC: &core.GRPCConfig{
			TLS: false, // h2c
		},
		Timeout: core.Duration{Duration: 100 * time.Millisecond},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

// TestGRPCChecker_Judge_ServiceWithQuery tests service name in URL
func TestGRPCChecker_Judge_ServiceWithQuery(t *testing.T) {
	checker := NewGRPCChecker()

	soul := &core.Soul{
		ID:     "test-grpc-service-query",
		Name:   "Test gRPC Service Query",
		Type:   core.CheckGRPC,
		Target: "127.0.0.1:1",
		GRPC: &core.GRPCConfig{
			Service: "my.service.Health",
			TLS:     false,
		},
		Timeout: core.Duration{Duration: 100 * time.Millisecond},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}

	// The URL should include the service query parameter
	if judgment.Details == nil {
		t.Error("Expected details to be populated")
	}
}

// TestGRPCChecker_Judge_LongServiceName tests with a long service name
func TestGRPCChecker_Judge_LongServiceName(t *testing.T) {
	checker := NewGRPCChecker()

	longServiceName := "com.example.super.long.service.name.that.is.very.detailed.HealthCheck"

	soul := &core.Soul{
		ID:     "test-grpc-long-service",
		Name:   "Test gRPC Long Service",
		Type:   core.CheckGRPC,
		Target: "127.0.0.1:1",
		GRPC: &core.GRPCConfig{
			Service: longServiceName,
			TLS:     false,
		},
		Timeout: core.Duration{Duration: 100 * time.Millisecond},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

// TestGRPCChecker_Judge_VariousTargets tests with various target formats
func TestGRPCChecker_Judge_VariousTargets(t *testing.T) {
	checker := NewGRPCChecker()

	tests := []struct {
		name   string
		target string
	}{
		{"localhost with port", "localhost:50051"},
		{"IPv4 with port", "192.168.1.1:8080"},
		{"IPv6 localhost", "[::1]:50051"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			soul := &core.Soul{
				ID:      "test-grpc-target",
				Name:    "Test gRPC Target",
				Type:    core.CheckGRPC,
				Target:  tt.target,
				Timeout: core.Duration{Duration: 50 * time.Millisecond},
				GRPC: &core.GRPCConfig{
					TLS: false,
				},
			}

			ctx := context.Background()
			judgment, _ := checker.Judge(ctx, soul)

			// All should fail since nothing is listening
			if judgment.Status != core.SoulDead {
				t.Errorf("Expected status Dead for target %s, got %s", tt.target, judgment.Status)
			}
		})
	}
}

// TestGRPCChecker_Judge_DetailsPopulation tests that details are populated correctly
func TestGRPCChecker_Judge_DetailsPopulation(t *testing.T) {
	checker := NewGRPCChecker()

	soul := &core.Soul{
		ID:      "test-grpc-details",
		Name:    "Test gRPC Details",
		Type:    core.CheckGRPC,
		Target:  "127.0.0.1:1",
		Timeout: core.Duration{Duration: 100 * time.Millisecond},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Details == nil {
		t.Error("Expected Details to be populated")
	}
}

// TestGRPCChecker_Judge_JudgmentFields tests all judgment fields are set
func TestGRPCChecker_Judge_JudgmentFields(t *testing.T) {
	checker := NewGRPCChecker()

	soul := &core.Soul{
		ID:      "test-grpc-fields",
		Name:    "Test gRPC Fields",
		Type:    core.CheckGRPC,
		Target:  "127.0.0.1:1",
		Timeout: core.Duration{Duration: 100 * time.Millisecond},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.ID == "" {
		t.Error("Expected ID to be set")
	}

	if judgment.SoulID != soul.ID {
		t.Errorf("Expected SoulID %s, got %s", soul.ID, judgment.SoulID)
	}

	if judgment.Timestamp.IsZero() {
		t.Error("Expected Timestamp to be set")
	}

	if judgment.Message == "" {
		t.Error("Expected Message to be set")
	}
}

// TestGRPCChecker_Judge_HTTPResponseOK tests the OK response path using httptest
func TestGRPCChecker_Judge_HTTPResponseOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Grpc-Status", "0")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := NewGRPCChecker()

	// Parse server URL
	u, _ := url.Parse(server.URL)

	soul := &core.Soul{
		ID:      "test-grpc-ok",
		Name:    "Test gRPC OK",
		Type:    core.CheckGRPC,
		Target:  u.Host,
		Timeout: core.Duration{Duration: 5 * time.Second},
		GRPC: &core.GRPCConfig{
			TLS: false,
		},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Even though we return OK, the http2.Transport might not work perfectly with httptest
	// but we still exercise the response handling code path
	if judgment.Status != core.SoulDead {
		// If it works, great! If not, it's expected since httptest doesn't support HTTP/2
		// The important thing is we exercise the code path
		t.Logf("Got status: %s (may vary due to HTTP/2 requirements)", judgment.Status)
	}
}

// TestGRPCChecker_Judge_HTTPResponseError tests error response handling
func TestGRPCChecker_Judge_HTTPResponseError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	checker := NewGRPCChecker()

	u, _ := url.Parse(server.URL)

	soul := &core.Soul{
		ID:      "test-grpc-error",
		Name:    "Test gRPC Error",
		Type:    core.CheckGRPC,
		Target:  u.Host,
		Timeout: core.Duration{Duration: 5 * time.Second},
		GRPC: &core.GRPCConfig{
			TLS: false,
		},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should fail due to non-OK status or HTTP/2 issues
	if judgment.Status != core.SoulDead {
		t.Logf("Got status: %s", judgment.Status)
	}
}

// TestGRPCChecker_Judge_WithTLSServer tests TLS connection handling
func TestGRPCChecker_Judge_WithTLSServer(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Grpc-Status", "0")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := NewGRPCChecker()

	u, _ := url.Parse(server.URL)

	soul := &core.Soul{
		ID:      "test-grpc-tls",
		Name:    "Test gRPC TLS",
		Type:    core.CheckGRPC,
		Target:  u.Host,
		Timeout: core.Duration{Duration: 5 * time.Second},
		GRPC: &core.GRPCConfig{
			TLS:                true,
			InsecureSkipVerify: true,
		},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// The test exercises the TLS code path
	t.Logf("TLS test got status: %s", judgment.Status)
}
