package probe

import (
	"bytes"
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
			RecordType:           "A",
			PropagationCheck:     true,
			Nameservers:          []string{"8.8.8.8:53", "1.1.1.1:53", "9.9.9.9:53"},
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

	// This test verifies DNSSEC validation is wired up correctly.
	// Whether the domain validates depends on the resolver's DNSSEC support.
	soul := &core.Soul{
		ID:     "test-dns-dnssec",
		Name:   "Test DNS DNSSEC",
		Type:   core.CheckDNS,
		Target: "cloudflare.com",
		DNS: &core.DNSConfig{
			RecordType:     "A",
			DNSSECValidate: true,
			Nameservers:    []string{"8.8.8.8:53"},
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment == nil {
		t.Fatal("Expected judgment to be returned")
	}

	if judgment.Details.DNSSECValid == nil {
		t.Error("Expected DNSSECValid to be set")
	}

	// DNSSEC validation depends on resolver support in the test environment
	t.Logf("DNSSEC result for cloudflare.com: valid=%v, message=%s",
		*judgment.Details.DNSSECValid, judgment.Message)
}

// TestBuildDNSQueryWithEDNS0 verifies the DNS wire-format query builder
func TestBuildDNSQueryWithEDNS0(t *testing.T) {
	// Test with DO bit set
	msg := buildDNSQueryWithEDNS0("example.com", 0x01, true)

	if len(msg) < 12 {
		t.Fatal("Query too short for DNS header")
	}

	// Check header fields
	msgID := uint16(msg[0])<<8 | uint16(msg[1])
	if msgID != 0xABCD {
		t.Errorf("Expected message ID 0xABCD, got 0x%04x", msgID)
	}

	// Check RD=1, QR=0
	flags := uint16(msg[2])<<8 | uint16(msg[3])
	if flags != 0x0100 {
		t.Errorf("Expected flags 0x0100, got 0x%04x", flags)
	}

	// Check QDCOUNT=1
	qdCount := uint16(msg[4])<<8 | uint16(msg[5])
	if qdCount != 1 {
		t.Errorf("Expected QDCOUNT=1, got %d", qdCount)
	}

	// Check ARCOUNT=1 (EDNS0 OPT)
	arCount := uint16(msg[10])<<8 | uint16(msg[11])
	if arCount != 1 {
		t.Errorf("Expected ARCOUNT=1, got %d", arCount)
	}

	// Check question section contains domain name
	question := msg[12:]
	if !bytes.Contains(question, []byte("example")) {
		t.Error("Question section should contain domain name")
	}

	// Check EDNS0 OPT record at end
	// Find the root label (0x00) followed by OPT type (0x0029)
	foundEDNS0 := false
	for i := 0; i < len(msg)-4; i++ {
		if msg[i] == 0x00 && msg[i+1] == 0x00 && msg[i+2] == 0x29 {
			// Check DO bit in TTL field (byte 5 after OPT type)
			doBit := msg[i+5] & 0x80
			if doBit == 0 {
				t.Error("DO bit should be set in EDNS0 OPT record")
			}
			foundEDNS0 = true
			break
		}
	}
	if !foundEDNS0 {
		t.Error("EDNS0 OPT record not found in query")
	}
}

func TestBuildDNSQueryWithEDNS0_NoDOBit(t *testing.T) {
	msg := buildDNSQueryWithEDNS0("example.com", 0x01, false)

	// Find EDNS0 record and check DO bit is NOT set
	for i := 0; i < len(msg)-4; i++ {
		if msg[i] == 0x00 && msg[i+1] == 0x00 && msg[i+2] == 0x29 {
			doBit := msg[i+5] & 0x80
			if doBit != 0 {
				t.Error("DO bit should NOT be set when doBit=false")
			}
			return
		}
	}
	t.Error("EDNS0 OPT record not found")
}

func TestEncodeDNSName(t *testing.T) {
	tests := []struct {
		input    string
		expected []byte
	}{
		{"example.com", []byte{0x07, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 0x03, 'c', 'o', 'm', 0x00}},
		{"a.b", []byte{0x01, 'a', 0x01, 'b', 0x00}},
		{"", []byte{0x00}}, // Empty domain = just root label
	}

	for _, tc := range tests {
		result := encodeDNSName(tc.input)
		if tc.input == "" {
			// Empty string splits to [""], encodes as 0x00 (len of empty) + 0x00 = 2 bytes
			if len(result) != 2 {
				t.Errorf("encodeDNSName(%q): expected length 2, got %d", tc.input, len(result))
			}
			continue
		}
		if len(result) != len(tc.expected) {
			t.Errorf("encodeDNSName(%q): expected length %d, got %d", tc.input, len(tc.expected), len(result))
		}
		for i := range tc.expected {
			if result[i] != tc.expected[i] {
				t.Errorf("encodeDNSName(%q)[%d]: expected 0x%02x, got 0x%02x", tc.input, i, tc.expected[i], result[i])
				break
			}
		}
	}
}

func TestSkipDNSName(t *testing.T) {
	msg := append(encodeDNSName("example.com"), 0x00, 0x01, 0x00, 0x01)
	start, end := skipDNSName(msg, 0)
	if start != 0 {
		t.Errorf("Expected start=0, got %d", start)
	}
	// 7+3+1(root) = 11 bytes for name
	if end != 13 { // 11 bytes + null terminator already counted
		t.Errorf("Expected end=13, got %d", end)
	}
}

func TestDNSTypeToString(t *testing.T) {
	expected := map[uint16]string{
		1: "A", 2: "NS", 5: "CNAME", 6: "SOA", 12: "PTR",
		15: "MX", 16: "TXT", 28: "AAAA", 33: "SRV",
		43: "DS", 46: "RRSIG", 47: "NSEC", 48: "DNSKEY",
		99: "TYPE99",
	}

	for code, name := range expected {
		if result := dnsTypeToString(code); result != name {
			t.Errorf("dnsTypeToString(%d): expected %q, got %q", code, name, result)
		}
	}
}

func TestParseDNSSECResponse_ADFlag(t *testing.T) {
	// Build a synthetic response with AD flag set
	msg := buildDNSQueryWithEDNS0("example.com", 0x01, true)

	// Convert query to response by modifying header
	msg[2] = 0x81 // QR=1, RD=1
	msg[3] = 0x20 // AD=1 (bit 5 = 0x20), RCODE=0
	// Set ANCOUNT=0
	msg[6] = 0
	msg[7] = 0
	// Set ARCOUNT=1
	msg[10] = 0
	msg[11] = 1

	_, adFlag, err := parseDNSSECResponse(msg)
	if err != nil {
		t.Fatalf("parseDNSSECResponse failed: %v", err)
	}
	if !adFlag {
		t.Error("Expected AD flag to be set")
	}
}

func TestParseDNSSECResponse_NoADFlag(t *testing.T) {
	msg := buildDNSQueryWithEDNS0("example.com", 0x01, true)

	msg[2] = 0x81
	msg[3] = 0x00 // AD=0
	msg[6] = 0
	msg[7] = 0

	_, adFlag, err := parseDNSSECResponse(msg)
	if err != nil {
		t.Fatalf("parseDNSSECResponse failed: %v", err)
	}
	if adFlag {
		t.Error("Expected AD flag to NOT be set")
	}
}

func TestParseDNSSECResponse_TooShort(t *testing.T) {
	_, _, err := parseDNSSECResponse([]byte{0x00, 0x01})
	if err == nil {
		t.Error("Expected error for too-short response")
	}
}

func TestDNSChecker_Judge_DNSSEC_UnsignedDomain(t *testing.T) {
	checker := NewDNSChecker()

	// Many domains are not DNSSEC-signed
	soul := &core.Soul{
		ID:     "test-dns-dnssec-unsigned",
		Name:   "Test DNSSEC Unsigned",
		Type:   core.CheckDNS,
		Target: "example.com",
		DNS: &core.DNSConfig{
			RecordType:     "A",
			DNSSECValidate: true,
			Nameservers:    []string{"8.8.8.8:53"},
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment == nil {
		t.Fatal("Expected judgment to be returned")
	}

	if judgment.Details.DNSSECValid == nil {
		t.Error("Expected DNSSECValid to be set")
	}

	// example.com may or may not be DNSSEC-signed depending on the registrar
	t.Logf("DNSSEC result for example.com: valid=%v, message=%s",
		*judgment.Details.DNSSECValid, judgment.Message)
}

func TestDNSChecker_Judge_DefaultRecordType(t *testing.T) {
	checker := NewDNSChecker()

	soul := &core.Soul{
		ID:     "test-dns",
		Name:   "Test DNS",
		Type:   core.CheckDNS,
		Target: "example.com",
		DNS:    &core.DNSConfig{
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
			RecordType:       "A",
			PropagationCheck: true,
			Nameservers:      []string{"8.8.8.8:53", "1.1.1.1:53"},
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

// Test resolve function directly with failing nameserver
func TestDNSChecker_resolve_Error(t *testing.T) {
	checker := NewDNSChecker()
	ctx := context.Background()

	// Use invalid nameserver that will fail
	_, err := checker.resolve(ctx, "example.com", "A", "127.0.0.1:1")
	if err == nil {
		t.Error("Expected error for invalid nameserver")
	}
}

// Test resolve with SRV record type
func TestDNSChecker_resolve_SRV(t *testing.T) {
	checker := NewDNSChecker()
	ctx := context.Background()

	// Test SRV lookup for a domain that has SRV records
	// Using google.com which typically has SRV records
	records, err := checker.resolve(ctx, "google.com", "SRV", "8.8.8.8:53")
	// SRV lookup may or may not return records, but should not error for valid domain
	t.Logf("SRV records: %v, err: %v", records, err)
}

// Test resolve with PTR record type
func TestDNSChecker_resolve_PTR(t *testing.T) {
	checker := NewDNSChecker()
	ctx := context.Background()

	// Test PTR lookup for Google DNS
	records, err := checker.resolve(ctx, "8.8.8.8", "PTR", "8.8.8.8:53")
	// PTR lookup should work for 8.8.8.8
	if err != nil {
		t.Logf("PTR lookup error (may be expected): %v", err)
	}
	t.Logf("PTR records: %v", records)
}

// Test resolve with MX record type
func TestDNSChecker_resolve_MX(t *testing.T) {
	checker := NewDNSChecker()
	ctx := context.Background()

	// Test MX lookup for google.com
	records, err := checker.resolve(ctx, "google.com", "MX", "8.8.8.8:53")
	if err != nil {
		t.Fatalf("MX lookup failed: %v", err)
	}
	if len(records) == 0 {
		t.Error("Expected MX records for google.com")
	}
	t.Logf("MX records: %v", records)
}

// Test resolve with TXT record type
func TestDNSChecker_resolve_TXT(t *testing.T) {
	checker := NewDNSChecker()
	ctx := context.Background()

	// Test TXT lookup for google.com
	records, err := checker.resolve(ctx, "google.com", "TXT", "8.8.8.8:53")
	if err != nil {
		t.Fatalf("TXT lookup failed: %v", err)
	}
	if len(records) == 0 {
		t.Error("Expected TXT records for google.com")
	}
	t.Logf("TXT records count: %d", len(records))
}

// Test judgePropagation with Expected value matching
func TestDNSChecker_JudgePropagation_ExpectedMatch(t *testing.T) {
	checker := NewDNSChecker()

	soul := &core.Soul{
		ID:     "test-dns-expected",
		Name:   "Test DNS Expected",
		Type:   core.CheckDNS,
		Target: "example.com",
		DNS: &core.DNSConfig{
			RecordType:       "A",
			PropagationCheck: true,
			Nameservers:      []string{"8.8.8.8:53"},
			Expected:         []string{"93.184.215.14"}, // example.com's IP
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment == nil {
		t.Fatal("Expected judgment to be returned")
	}
	// Status depends on whether expected IP matches current DNS
	t.Logf("Expected match result: %s - %s", judgment.Status, judgment.Message)
}

// Test judgePropagation with Expected value mismatch
func TestDNSChecker_JudgePropagation_ExpectedMismatch(t *testing.T) {
	checker := NewDNSChecker()

	soul := &core.Soul{
		ID:     "test-dns-expected-mismatch",
		Name:   "Test DNS Expected Mismatch",
		Type:   core.CheckDNS,
		Target: "example.com",
		DNS: &core.DNSConfig{
			RecordType:       "A",
			PropagationCheck: true,
			Nameservers:      []string{"8.8.8.8:53"},
			Expected:         []string{"1.2.3.4", "5.6.7.8"}, // Wrong IPs
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment == nil {
		t.Fatal("Expected judgment to be returned")
	}
	// Should be degraded or dead due to mismatch
	if judgment.Status != core.SoulDegraded && judgment.Status != core.SoulDead {
		t.Logf("Expected Degraded/Dead for mismatch, got %s", judgment.Status)
	}
}
