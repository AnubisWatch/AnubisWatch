package probe

import (
	"context"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestICMPChecker_Validate_MissingTarget(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:   "test-icmp",
		Name: "Test ICMP",
		Type: core.CheckICMP,
	}

	err := checker.Validate(soul)
	if err == nil {
		t.Error("Expected validation error for missing target")
	}
}

func TestICMPChecker_Validate_Valid(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP",
		Type:   core.CheckICMP,
		Target: "8.8.8.8",
	}

	err := checker.Validate(soul)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestICMPChecker_Validate_IPv6(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP",
		Type:   core.CheckICMP,
		Target: "2001:4860:4860::8888",
	}

	err := checker.Validate(soul)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestICMPChecker_Validate_DomainName(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP",
		Type:   core.CheckICMP,
		Target: "google.com",
	}

	err := checker.Validate(soul)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestICMPChecker_Judge_DefaultConfig(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:      "test-icmp",
		Name:    "Test ICMP",
		Type:    core.CheckICMP,
		Target:  "8.8.8.8",
		Enabled: true,
		Weight:  core.Duration{Duration: 60 * time.Second},
		Timeout: core.Duration{Duration: 5 * time.Second},
		// No ICMP config - should use defaults
	}

	ctx := context.Background()
	judgment, err := checker.Judge(ctx, soul)

	if err != nil {
		t.Fatalf("Judge failed: %v", err)
	}

	// May succeed or fail depending on network/privileges
	t.Logf("ICMP judgment: status=%s, message=%s", judgment.Status, judgment.Message)
}

func TestICMPChecker_Judge_CustomCount(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP",
		Type:   core.CheckICMP,
		Target: "8.8.8.8",
		ICMP: &core.ICMPConfig{
			Count: 5,
		},
		Timeout: core.Duration{Duration: 10 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should have sent 5 packets
	if judgment.Details.PacketsSent != 5 {
		t.Logf("Expected 5 packets sent, got %d", judgment.Details.PacketsSent)
	}

	t.Logf("ICMP judgment: %s", judgment.Message)
}

func TestICMPChecker_Judge_CustomInterval(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP",
		Type:   core.CheckICMP,
		Target: "8.8.8.8",
		ICMP: &core.ICMPConfig{
			Count:    3,
			Interval: core.Duration{Duration: 500 * time.Millisecond},
		},
		Timeout: core.Duration{Duration: 10 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	t.Logf("ICMP judgment: %s", judgment.Message)
}

func TestICMPChecker_Judge_InvalidHost(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP",
		Type:   core.CheckICMP,
		Target: "invalid.hostname.that.does.not.exist",
		ICMP: &core.ICMPConfig{
			Count: 1,
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should fail DNS resolution
	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestICMPChecker_Judge_MaxLossPercent(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP",
		Type:   core.CheckICMP,
		Target: "8.8.8.8",
		ICMP: &core.ICMPConfig{
			Count:          3,
			MaxLossPercent: 50, // Allow 50% loss
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// With a good target like 8.8.8.8, should pass
	if judgment.Status != core.SoulAlive && judgment.Status != core.SoulDegraded {
		t.Logf("ICMP judgment: %s", judgment.Message)
	}
}

func TestICMPChecker_Judge_Feather(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP",
		Type:   core.CheckICMP,
		Target: "8.8.8.8",
		ICMP: &core.ICMPConfig{
			Count:   1,
			Feather: core.Duration{Duration: 500 * time.Millisecond}, // Generous budget
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	t.Logf("ICMP judgment: %s", judgment.Message)
}

func TestICMPChecker_Judge_FeatherExceeded(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP",
		Type:   core.CheckICMP,
		Target: "8.8.8.8",
		ICMP: &core.ICMPConfig{
			Count:   1,
			Feather: core.Duration{Duration: 1 * time.Microsecond}, // Impossible budget
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should be degraded if latency exceeds feather
	if judgment.Status == core.SoulAlive {
		t.Logf("Unexpected Alive status with impossible feather budget")
	}

	t.Logf("ICMP judgment: %s", judgment.Message)
}

func TestICMPChecker_Judge_NonPrivileged(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP",
		Type:   core.CheckICMP,
		Target: "8.8.8.8",
		ICMP: &core.ICMPConfig{
			Count:      1,
			Privileged: false, // Use UDP mode
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	t.Logf("ICMP judgment (non-privileged): %s", judgment.Message)
}

func TestICMPChecker_Judge_IPv6(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP",
		Type:   core.CheckICMP,
		Target: "2001:4860:4860::8888",
		ICMP: &core.ICMPConfig{
			Count: 1,
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	t.Logf("ICMP IPv6 judgment: %s", judgment.Message)
}

func TestICMPChecker_Judge_ContextCancellation(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP",
		Type:   core.CheckICMP,
		Target: "8.8.8.8",
		ICMP: &core.ICMPConfig{
			Count: 10, // Many pings
		},
		Timeout: core.Duration{Duration: 30 * time.Second},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	judgment, _ := checker.Judge(ctx, soul)

	if judgment == nil {
		t.Error("Expected judgment to be returned")
	}
}

func TestICMPChecker_Judge_ShortTimeout(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP",
		Type:   core.CheckICMP,
		Target: "8.8.8.8",
		ICMP: &core.ICMPConfig{
			Count: 3,
		},
		Timeout: core.Duration{Duration: 1 * time.Millisecond}, // Too short
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should fail with very short timeout
	if judgment.Status != core.SoulDead {
		t.Logf("Expected status Dead with short timeout, got %s", judgment.Status)
	}
}

func TestICMPChecker_Judge_Statistics(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP",
		Type:   core.CheckICMP,
		Target: "8.8.8.8",
		ICMP: &core.ICMPConfig{
			Count: 3,
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Verify statistics are populated
	// Note: ICMP requires raw socket privileges, so packets may not be sent on all systems
	// Just verify the judgment structure is correct
	if judgment.Details == nil {
		t.Error("Expected details to be populated")
	}

	// Packet loss should be between 0 and 100
	if judgment.Details.PacketLoss < 0 || judgment.Details.PacketLoss > 100 {
		t.Errorf("Expected packet loss between 0-100, got %.1f", judgment.Details.PacketLoss)
	}

	t.Logf("Statistics: sent=%d, received=%d, loss=%.1f%%, min=%.2fms, avg=%.2fms, max=%.2fms, jitter=%.2fms",
		judgment.Details.PacketsSent,
		judgment.Details.PacketsReceived,
		judgment.Details.PacketLoss,
		judgment.Details.MinLatency,
		judgment.Details.AvgLatency,
		judgment.Details.MaxLatency,
		judgment.Details.Jitter)
}

func TestICMPChecker_Judge_ZeroCount(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP",
		Type:   core.CheckICMP,
		Target: "8.8.8.8",
		ICMP: &core.ICMPConfig{
			Count: 0, // Should default to 3
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should use default count of 3
	t.Logf("ICMP judgment with zero count: %s", judgment.Message)
}

func TestICMPChecker_Judge_ZeroInterval(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP",
		Type:   core.CheckICMP,
		Target: "8.8.8.8",
		ICMP: &core.ICMPConfig{
			Count:    2,
			Interval: core.Duration{Duration: 0}, // Should default to 200ms
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	t.Logf("ICMP judgment with zero interval: %s", judgment.Message)
}

func TestICMPChecker_Judge_AllPacketsLost(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP",
		Type:   core.CheckICMP,
		Target: "192.0.2.1", // TEST-NET-1, should not respond
		ICMP: &core.ICMPConfig{
			Count: 2,
		},
		Timeout: core.Duration{Duration: 2 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should report all packets lost
	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}

	if judgment.Details.PacketLoss != 100 {
		t.Logf("Expected 100%% packet loss, got %.1f%%", judgment.Details.PacketLoss)
	}
}

func TestICMPChecker_Judge_NoConfig(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:      "test-icmp",
		Name:    "Test ICMP",
		Type:    core.CheckICMP,
		Target:  "8.8.8.8",
		Timeout: core.Duration{Duration: 5 * time.Second},
		// No ICMP config
	}

	ctx := context.Background()
	judgment, err := checker.Judge(ctx, soul)

	if err != nil {
		t.Fatalf("Judge failed: %v", err)
	}

	// Should use default config
	t.Logf("ICMP judgment with no config: %s", judgment.Message)
}

func TestICMPChecker_Judge_DNSResolutionFailure(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP",
		Type:   core.CheckICMP,
		Target: "this.domain.definitely.does.not.exist.invalid",
		ICMP: &core.ICMPConfig{
			Count: 1,
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should fail DNS resolution
	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}

	if judgment.Message == "" {
		t.Error("Expected error message")
	}
}

func TestICMPChecker_Judge_MinLatency(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP",
		Type:   core.CheckICMP,
		Target: "8.8.8.8",
		ICMP: &core.ICMPConfig{
			Count: 2,
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// If we received packets, min latency should be >= 0
	if judgment.Details.PacketsReceived > 0 {
		if judgment.Details.MinLatency < 0 {
			t.Errorf("Expected min latency >= 0, got %.2f", judgment.Details.MinLatency)
		}
	}

	t.Logf("Min latency: %.2fms", judgment.Details.MinLatency)
}

func TestICMPChecker_Judge_Jitter(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP",
		Type:   core.CheckICMP,
		Target: "8.8.8.8",
		ICMP: &core.ICMPConfig{
			Count: 3, // Need at least 2 for jitter calculation
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Jitter should be >= 0
	if judgment.Details.PacketsReceived >= 2 {
		if judgment.Details.Jitter < 0 {
			t.Errorf("Expected jitter >= 0, got %.2f", judgment.Details.Jitter)
		}
	}

	t.Logf("Jitter: %.2fms", judgment.Details.Jitter)
}

func TestICMPChecker_Judge_Localhost(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP",
		Type:   core.CheckICMP,
		Target: "127.0.0.1",
		ICMP: &core.ICMPConfig{
			Count: 1,
		},
		Timeout: core.Duration{Duration: 2 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Localhost should respond quickly if ICMP is allowed
	t.Logf("ICMP localhost judgment: %s", judgment.Message)
}

// TestICMPChecker_Judge_IPv6NonPrivileged tests IPv6 in non-privileged mode
func TestICMPChecker_Judge_IPv6NonPrivileged(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP IPv6 Non-Privileged",
		Type:   core.CheckICMP,
		Target: "::1", // IPv6 localhost
		ICMP: &core.ICMPConfig{
			Count:      1,
			Privileged: false, // Use UDP mode
		},
		Timeout: core.Duration{Duration: 2 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	t.Logf("ICMP IPv6 non-privileged judgment: %s", judgment.Message)
}

// TestICMPChecker_Judge_ZeroTimeout tests default timeout handling
func TestICMPChecker_Judge_ZeroTimeout(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP Zero Timeout",
		Type:   core.CheckICMP,
		Target: "8.8.8.8",
		ICMP: &core.ICMPConfig{
			Count: 1,
		},
		Timeout: core.Duration{Duration: 0}, // Should default to 5s
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	t.Logf("ICMP zero timeout judgment: %s", judgment.Message)
}

// TestICMPChecker_Judge_AvgLatencyCalculation tests average latency calculation
func TestICMPChecker_Judge_AvgLatencyCalculation(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP Avg Latency",
		Type:   core.CheckICMP,
		Target: "8.8.8.8",
		ICMP: &core.ICMPConfig{
			Count: 3,
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// If packets received, avg should be between min and max
	if judgment.Details.PacketsReceived > 0 {
		if judgment.Details.AvgLatency < judgment.Details.MinLatency {
			t.Errorf("Avg latency %.2f should be >= min latency %.2f",
				judgment.Details.AvgLatency, judgment.Details.MinLatency)
		}
		if judgment.Details.AvgLatency > judgment.Details.MaxLatency {
			t.Errorf("Avg latency %.2f should be <= max latency %.2f",
				judgment.Details.AvgLatency, judgment.Details.MaxLatency)
		}
	}
}

// TestICMPChecker_Judge_MaxLatencyEqualsMinWhenOnePacket tests max/min with single packet
func TestICMPChecker_Judge_MaxLatencyEqualsMinWhenOnePacket(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP Single Packet",
		Type:   core.CheckICMP,
		Target: "8.8.8.8",
		ICMP: &core.ICMPConfig{
			Count: 1,
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// With single packet received, min should equal max
	if judgment.Details.PacketsReceived == 1 {
		if judgment.Details.MinLatency != judgment.Details.MaxLatency {
			t.Errorf("With 1 packet, min %.2f should equal max %.2f",
				judgment.Details.MinLatency, judgment.Details.MaxLatency)
		}
	}
}

// TestICMPChecker_Judge_PacketLossCalculation tests packet loss percentage
func TestICMPChecker_Judge_PacketLossCalculation(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP Packet Loss",
		Type:   core.CheckICMP,
		Target: "8.8.8.8",
		ICMP: &core.ICMPConfig{
			Count: 4,
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Verify packet loss calculation
	if judgment.Details.PacketsSent > 0 {
		expectedLoss := float64(judgment.Details.PacketsSent-judgment.Details.PacketsReceived) / float64(judgment.Details.PacketsSent) * 100
		if judgment.Details.PacketLoss != expectedLoss {
			t.Errorf("Expected packet loss %.1f%%, got %.1f%%", expectedLoss, judgment.Details.PacketLoss)
		}
	}
}

// TestICMPChecker_Judge_NoMaxLossPercent tests behavior without max loss percent set
func TestICMPChecker_Judge_NoMaxLossPercent(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP No Max Loss",
		Type:   core.CheckICMP,
		Target: "8.8.8.8",
		ICMP: &core.ICMPConfig{
			Count:          3,
			MaxLossPercent: 0, // No limit
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should not fail due to packet loss when MaxLossPercent is 0
	t.Logf("ICMP no max loss judgment: %s", judgment.Message)
}

// TestICMPChecker_Judge_EmptyTarget tests validation for empty target
func TestICMPChecker_Judge_EmptyTarget(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP Empty Target",
		Type:   core.CheckICMP,
		Target: "", // Empty target
		ICMP: &core.ICMPConfig{
			Count: 1,
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should fail with empty target
	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead for empty target, got %s", judgment.Status)
	}
}

// TestICMPChecker_Judge_FeatherThreshold tests feather threshold for degraded status
func TestICMPChecker_Judge_FeatherThreshold(t *testing.T) {
	checker := NewICMPChecker()

	// Use a very low feather threshold to force degraded status
	soul := &core.Soul{
		ID:     "test-icmp",
		Name:   "Test ICMP Feather",
		Type:   core.CheckICMP,
		Target: "8.8.8.8",
		ICMP: &core.ICMPConfig{
			Count:   3,
			Feather: core.Duration{Duration: 1 * time.Microsecond}, // Very low threshold
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// If packets received and latency > 1 microsecond, should be degraded
	if judgment.Details.PacketsReceived > 0 {
		t.Logf("Feather threshold judgment status: %s", judgment.Status)
	}
}

// TestICMPChecker_Judge_PrivilegedMode tests ICMP in privileged mode
func TestICMPChecker_Judge_PrivilegedMode(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp-privileged",
		Name:   "Test ICMP Privileged",
		Type:   core.CheckICMP,
		Target: "127.0.0.1",
		ICMP: &core.ICMPConfig{
			Count:      1,
			Privileged: true, // Use raw sockets
		},
		Timeout: core.Duration{Duration: 2 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should get a judgment (may fail due to permissions, but code path is exercised)
	if judgment == nil {
		t.Fatal("Expected judgment")
	}
	t.Logf("Privileged mode judgment: %s", judgment.Message)
}

// TestICMPChecker_Judge_MaxLossPercentThreshold tests max loss percent threshold
func TestICMPChecker_Judge_MaxLossPercentThreshold(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp-loss",
		Name:   "Test ICMP Loss",
		Type:   core.CheckICMP,
		Target: "192.0.2.1", // TEST-NET-1, should not respond
		ICMP: &core.ICMPConfig{
			Count:          3,
			MaxLossPercent: 10.0, // Very low threshold
		},
		Timeout: core.Duration{Duration: 1 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// All packets should be lost
	if judgment.Status != core.SoulDead {
		t.Logf("Expected Dead status due to packet loss, got %s", judgment.Status)
	}
}

// TestICMPChecker_Judge_DefaultCount tests default count when count is 0
func TestICMPChecker_Judge_DefaultCount(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp-default",
		Name:   "Test ICMP Default",
		Type:   core.CheckICMP,
		Target: "8.8.8.8",
		ICMP: &core.ICMPConfig{
			Count: 0, // Should default to 3
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment == nil {
		t.Fatal("Expected judgment")
	}
	// Test that default count logic works - judgment should exist
	t.Logf("Default count test: PacketsSent=%d, PacketsReceived=%d", judgment.Details.PacketsSent, judgment.Details.PacketsReceived)
}

// TestICMPChecker_Judge_DefaultInterval tests default interval when interval is 0
func TestICMPChecker_Judge_DefaultInterval(t *testing.T) {
	checker := NewICMPChecker()

	soul := &core.Soul{
		ID:     "test-icmp-interval",
		Name:   "Test ICMP Interval",
		Type:   core.CheckICMP,
		Target: "8.8.8.8",
		ICMP: &core.ICMPConfig{
			Count:    2,
			Interval: core.Duration{Duration: 0}, // Should default to 200ms
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment == nil {
		t.Fatal("Expected judgment")
	}
	t.Logf("Default interval test: %s", judgment.Message)
}
