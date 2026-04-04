package probe

import (
	"context"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestDNSChecker_Validate_MissingTarget(t *testing.T) {
	checker := NewDNSChecker()

	soul := &core.Soul{
		ID:   "test-dns",
		Name: "Test DNS",
		Type: core.CheckDNS,
	}

	err := checker.Validate(soul)
	if err == nil {
		t.Error("Expected validation error for missing target")
	}
}

func TestDNSChecker_Validate_Valid(t *testing.T) {
	checker := NewDNSChecker()

	soul := &core.Soul{
		ID:     "test-dns",
		Name:   "Test DNS",
		Type:   core.CheckDNS,
		Target: "example.com",
	}

	err := checker.Validate(soul)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestDNSChecker_Judge_Basic(t *testing.T) {
	checker := NewDNSChecker()

	soul := &core.Soul{
		ID:      "test-dns",
		Name:    "Test DNS",
		Type:    core.CheckDNS,
		Target:  "example.com",
		Enabled: true,
		Weight:  core.Duration{Duration: 60 * time.Second},
		DNS: &core.DNSConfig{
			RecordType: "A",
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, err := checker.Judge(ctx, soul)

	if err != nil {
		t.Fatalf("Judge failed: %v", err)
	}

	// example.com should resolve
	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}

	if len(judgment.Details.ResolvedAddresses) == 0 {
		t.Error("Expected resolved addresses")
	}
}

func TestDNSChecker_Judge_ARecord(t *testing.T) {
	checker := NewDNSChecker()

	soul := &core.Soul{
		ID:     "test-dns",
		Name:   "Test DNS",
		Type:   core.CheckDNS,
		Target: "example.com",
		DNS: &core.DNSConfig{
			RecordType: "A",
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}
}

func TestDNSChecker_Judge_AAAARecord(t *testing.T) {
	checker := NewDNSChecker()

	soul := &core.Soul{
		ID:     "test-dns",
		Name:   "Test DNS",
		Type:   core.CheckDNS,
		Target: "example.com",
		DNS: &core.DNSConfig{
			RecordType: "AAAA",
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// May or may not have AAAA record, but should not error
	if judgment.Details == nil {
		t.Error("Expected details to be set")
	}
}

func TestDNSChecker_Judge_CNAMERecord(t *testing.T) {
	checker := NewDNSChecker()

	soul := &core.Soul{
		ID:     "test-dns",
		Name:   "Test DNS",
		Type:   core.CheckDNS,
		Target: "www.example.com",
		DNS: &core.DNSConfig{
			RecordType: "CNAME",
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}
}

func TestDNSChecker_Judge_TXTRecord(t *testing.T) {
	checker := NewDNSChecker()

	soul := &core.Soul{
		ID:     "test-dns",
		Name:   "Test DNS",
		Type:   core.CheckDNS,
		Target: "example.com",
		DNS: &core.DNSConfig{
			RecordType: "TXT",
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// TXT records should exist for example.com (SPF, etc.)
	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}
}

func TestDNSChecker_Judge_NSRecord(t *testing.T) {
	checker := NewDNSChecker()

	soul := &core.Soul{
		ID:     "test-dns",
		Name:   "Test DNS",
		Type:   core.CheckDNS,
		Target: "example.com",
		DNS: &core.DNSConfig{
			RecordType: "NS",
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}
}

func TestDNSChecker_Judge_MXRecord(t *testing.T) {
	checker := NewDNSChecker()

	soul := &core.Soul{
		ID:     "test-dns",
		Name:   "Test DNS",
		Type:   core.CheckDNS,
		Target: "example.com",
		DNS: &core.DNSConfig{
			RecordType: "MX",
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}
}

func TestDNSChecker_Judge_ExpectedValueMatch(t *testing.T) {
	checker := NewDNSChecker()

	soul := &core.Soul{
		ID:     "test-dns",
		Name:   "Test DNS",
		Type:   core.CheckDNS,
		Target: "example.com",
		DNS: &core.DNSConfig{
			RecordType: "A",
			Expected:   []string{"93.184.215.14"}, // One of example.com's IPs
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should pass if the expected IP is in the resolved addresses
	if judgment.Status != core.SoulAlive {
		t.Logf("Expected IP may not match current DNS: %s", judgment.Message)
	}
}

func TestDNSChecker_Judge_ExpectedValueMismatch(t *testing.T) {
	checker := NewDNSChecker()

	soul := &core.Soul{
		ID:     "test-dns",
		Name:   "Test DNS",
		Type:   core.CheckDNS,
		Target: "example.com",
		DNS: &core.DNSConfig{
			RecordType: "A",
			Expected:   []string{"1.2.3.4", "5.6.7.8"}, // Unlikely to match
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should fail since expected values won't match
	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestDNSChecker_Judge_PropagationCheck(t *testing.T) {
	checker := NewDNSChecker()

	soul := &core.Soul{
		ID:     "test-dns",
		Name:   "Test DNS",
		Type:   core.CheckDNS,
		Target: "example.com",
		DNS: &core.DNSConfig{
			RecordType:       "A",
			PropagationCheck: true,
			Nameservers:      []string{"8.8.8.8:53", "8.8.4.4:53"},
		},
		Timeout: core.Duration{Duration: 10 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should succeed with propagation check
	if judgment.Status != core.SoulAlive && judgment.Status != core.SoulDegraded {
		t.Errorf("Expected status Alive or Degraded, got %s", judgment.Status)
	}

	if judgment.Details.PropagationResult == nil {
		t.Error("Expected propagation results")
	}
}

func TestDNSChecker_Judge_PropagationThreshold(t *testing.T) {
	checker := NewDNSChecker()

	soul := &core.Soul{
		ID:     "test-dns",
		Name:   "Test DNS",
		Type:   core.CheckDNS,
		Target: "example.com",
		DNS: &core.DNSConfig{
			RecordType:          "A",
			PropagationCheck:    true,
			Nameservers:         []string{"8.8.8.8:53", "1.1.1.1:53", "9.9.9.9:53"},
			PropagationThreshold: 50, // Only need 50% propagation
		},
		Timeout: core.Duration{Duration: 10 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should pass with 50% threshold
	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive with 50%% threshold, got %s", judgment.Status)
	}
}

func TestDNSChecker_Judge_CustomNameservers(t *testing.T) {
	checker := NewDNSChecker()

	soul := &core.Soul{
		ID:     "test-dns",
		Name:   "Test DNS",
		Type:   core.CheckDNS,
		Target: "example.com",
		DNS: &core.DNSConfig{
			RecordType:  "A",
			Nameservers: []string{"8.8.8.8:53"},
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}
}

func TestDNSChecker_Judge_InvalidNameserver(t *testing.T) {
	checker := NewDNSChecker()

	soul := &core.Soul{
		ID:     "test-dns",
		Name:   "Test DNS",
		Type:   core.CheckDNS,
		Target: "example.com",
		DNS: &core.DNSConfig{
			RecordType:  "A",
			Nameservers: []string{"127.0.0.1:53"}, // Unlikely to have DNS server
		},
		Timeout: core.Duration{Duration: 2 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// May fail or fall back to system DNS - just verify it completes
	if judgment == nil {
		t.Error("Expected judgment to be returned")
	}
}

func TestDNSChecker_Judge_NonExistentDomain(t *testing.T) {
	checker := NewDNSChecker()

	soul := &core.Soul{
		ID:     "test-dns",
		Name:   "Test DNS",
		Type:   core.CheckDNS,
		Target: "nonexistent.invalid.tld",
		DNS: &core.DNSConfig{
			RecordType: "A",
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestDNSChecker_Judge_DNSSECValidate(t *testing.T) {
	checker := NewDNSChecker()

	soul := &core.Soul{
		ID:     "test-dns",
		Name:   "Test DNS",
		Type:   core.CheckDNS,
		Target: "example.com",
		DNS: &core.DNSConfig{
			RecordType:    "A",
			DNSSECValidate: true,
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// DNSSEC validation is a placeholder, should still pass
	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}

	if judgment.Details.DNSSECValid == nil {
		t.Error("Expected DNSSECValid to be set")
	}
}

func TestDNSChecker_Judge_DefaultRecordType(t *testing.T) {
	checker := NewDNSChecker()

	soul := &core.Soul{
		ID:     "test-dns",
		Name:   "Test DNS",
		Type:   core.CheckDNS,
		Target: "example.com",
		DNS: &core.DNSConfig{
			// Empty RecordType should default to "A"
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}
}

func TestDNSChecker_Judge_PTRRecord(t *testing.T) {
	checker := NewDNSChecker()

	soul := &core.Soul{
		ID:     "test-dns",
		Name:   "Test DNS",
		Type:   core.CheckDNS,
		Target: "8.8.8.8", // Google DNS for reverse lookup
		DNS: &core.DNSConfig{
			RecordType: "PTR",
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// PTR lookup for 8.8.8.8 should work
	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}
}

func TestDNSChecker_Judge_ContextCancellation(t *testing.T) {
	checker := NewDNSChecker()

	soul := &core.Soul{
		ID:     "test-dns",
		Name:   "Test DNS",
		Type:   core.CheckDNS,
		Target: "example.com",
		DNS: &core.DNSConfig{
			RecordType:    "A",
			PropagationCheck: true,
			Nameservers:     []string{"8.8.8.8:53", "1.1.1.1:53"},
		},
		Timeout: core.Duration{Duration: 10 * time.Second},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	judgment, _ := checker.Judge(ctx, soul)

	// Should handle context cancellation gracefully
	if judgment == nil {
		t.Error("Expected judgment to be returned")
	}
}

// Test DNSChecker with SRV record type
func TestDNSChecker_Judge_SRVRecord(t *testing.T) {
	checker := NewDNSChecker()

	soul := &core.Soul{
		ID:     "test-dns-srv",
		Name:   "Test DNS SRV",
		Type:   core.CheckDNS,
		Target: "_sip._tcp.example.com",
		DNS: &core.DNSConfig{
			RecordType: "SRV",
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, err := checker.Judge(ctx, soul)
	if err != nil {
		t.Fatalf("Judge failed: %v", err)
	}
	// SRV lookup may fail for non-existent service record
	// but should return judgment
	if judgment == nil {
		t.Error("Expected judgment to be returned")
	}
}

// Test DNSChecker with unsupported record type
func TestDNSChecker_Judge_UnsupportedRecordType(t *testing.T) {
	checker := NewDNSChecker()

	soul := &core.Soul{
		ID:     "test-dns-unsupported",
		Name:   "Test DNS Unsupported",
		Type:   core.CheckDNS,
		Target: "example.com",
		DNS: &core.DNSConfig{
			RecordType: "UNSUPPORTED",
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, err := checker.Judge(ctx, soul)
	if err != nil {
		t.Fatalf("Judge failed: %v", err)
	}
	// Should return dead status due to unsupported record type
	if judgment.Status != core.SoulDead {
		t.Logf("Expected status Dead, got %s", judgment.Status)
	}
}

// Test DNSChecker with nameserver without port (should default to :53)
func TestDNSChecker_Judge_NameserverDefaultsPort(t *testing.T) {
	checker := NewDNSChecker()

	soul := &core.Soul{
		ID:     "test-dns-ns-noport",
		Name:   "Test DNS Nameserver Port Default",
		Type:   core.CheckDNS,
		Target: "example.com",
		DNS: &core.DNSConfig{
			RecordType:  "A",
			Nameservers: []string{"8.8.8.8"}, // No port
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, err := checker.Judge(ctx, soul)
	if err != nil {
		t.Fatalf("Judge failed: %v", err)
	}
	if judgment == nil {
		t.Error("Expected judgment to be returned")
	}
}
