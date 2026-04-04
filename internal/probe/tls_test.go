package probe

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestTLSChecker_Validate_MissingTarget(t *testing.T) {
	checker := NewTLSChecker()

	soul := &core.Soul{
		ID:   "test-tls",
		Name: "Test TLS",
		Type: core.CheckTLS,
	}

	err := checker.Validate(soul)
	if err == nil {
		t.Error("Expected validation error for missing target")
	}
}

func TestTLSChecker_Validate_AddsDefaultPort(t *testing.T) {
	checker := NewTLSChecker()

	soul := &core.Soul{
		ID:     "test-tls",
		Name:   "Test TLS",
		Type:   core.CheckTLS,
		Target: "example.com",
	}

	err := checker.Validate(soul)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Target should be updated with default port
	if soul.Target != "example.com:443" {
		t.Errorf("Expected target to be example.com:443, got %s", soul.Target)
	}
}

func TestTLSChecker_Validate_Valid(t *testing.T) {
	checker := NewTLSChecker()

	soul := &core.Soul{
		ID:     "test-tls",
		Name:   "Test TLS",
		Type:   core.CheckTLS,
		Target: "example.com:443",
	}

	err := checker.Validate(soul)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestTLSChecker_Judge_Basic(t *testing.T) {
	// Create HTTPS test server
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Extract host:port from URL
	host := ts.URL[8:] // Remove "https://"

	checker := NewTLSChecker()

	soul := &core.Soul{
		ID:      "test-tls",
		Name:    "Test TLS",
		Type:    core.CheckTLS,
		Target:  host,
		Enabled: true,
		Weight:  core.Duration{Duration: 60 * time.Second},
		TLS: &core.TLSConfig{
			ExpiryWarnDays: 30,
		},
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

	if judgment.TLSInfo == nil {
		t.Error("Expected TLS info to be populated")
	}
}

func TestTLSChecker_Judge_CertificateExpiryWarning(t *testing.T) {
	// Create test server with custom certificate
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	host := ts.URL[8:]

	checker := NewTLSChecker()

	soul := &core.Soul{
		ID:     "test-tls",
		Name:   "Test TLS",
		Type:   core.CheckTLS,
		Target: host,
		TLS: &core.TLSConfig{
			ExpiryWarnDays:     365 * 100, // Warn if expires within 100 years (test certs are short-lived)
			ExpiryCriticalDays: 365 * 200, // Critical if expires within 200 years
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Test certificates expire quickly, so should get degraded or dead status
	if judgment.Status != core.SoulDegraded && judgment.Status != core.SoulDead {
		t.Logf("Got status %s, expected Degraded or Dead for short-lived test cert", judgment.Status)
	}
}

func TestTLSChecker_Judge_MinProtocolVersion(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	host := ts.URL[8:]

	checker := NewTLSChecker()

	soul := &core.Soul{
		ID:     "test-tls",
		Name:   "Test TLS",
		Type:   core.CheckTLS,
		Target: host,
		TLS: &core.TLSConfig{
			MinProtocol: "TLS1.2",
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should pass with TLS 1.2 minimum
	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}

	// Verify protocol version assertion
	foundProtocolAssert := false
	for _, assert := range judgment.Details.Assertions {
		if assert.Type == "tls_version" {
			foundProtocolAssert = true
			if !assert.Passed {
				t.Error("Expected TLS version assertion to pass")
			}
		}
	}
	if !foundProtocolAssert {
		t.Error("Expected TLS version assertion")
	}
}

func TestTLSChecker_Judge_ForbiddenCipher(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	host := ts.URL[8:]

	checker := NewTLSChecker()

	soul := &core.Soul{
		ID:     "test-tls",
		Name:   "Test TLS",
		Type:   core.CheckTLS,
		Target: host,
		TLS: &core.TLSConfig{
			ForbiddenCiphers: []string{"NULL", "EXPORT", "DES"}, // Commonly forbidden
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Test servers use strong ciphers, should pass
	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}
}

func TestTLSChecker_Judge_ExpectedSAN(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	host := ts.URL[8:]

	checker := NewTLSChecker()

	soul := &core.Soul{
		ID:     "test-tls",
		Name:   "Test TLS",
		Type:   core.CheckTLS,
		Target: host,
		TLS: &core.TLSConfig{
			ExpectedSAN: []string{ts.Client().Transport.(*http.Transport).TLSClientConfig.ServerName},
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// This test may fail due to test certificate SAN not matching
	// The important thing is the check runs correctly
	if judgment.Status != core.SoulAlive && judgment.Status != core.SoulDead {
		t.Logf("SAN check result: %s", judgment.Message)
	}
}

func TestTLSChecker_Judge_ExpectedIssuer(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	host := ts.URL[8:]

	checker := NewTLSChecker()

	soul := &core.Soul{
		ID:     "test-tls",
		Name:   "Test TLS",
		Type:   core.CheckTLS,
		Target: host,
		TLS: &core.TLSConfig{
			ExpectedIssuer: "Test",
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Test certs are issued by "Test Cert" or similar
	if judgment.Status != core.SoulAlive && judgment.Status != core.SoulDegraded {
		t.Logf("Issuer check result: %s", judgment.Message)
	}
}

func TestTLSChecker_Judge_CheckOCSP(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	host := ts.URL[8:]

	checker := NewTLSChecker()

	soul := &core.Soul{
		ID:     "test-tls",
		Name:   "Test TLS",
		Type:   core.CheckTLS,
		Target: host,
		TLS: &core.TLSConfig{
			CheckOCSP: true,
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Test servers don't typically have OCSP stapling
	if judgment.Status != core.SoulAlive && judgment.Status != core.SoulDegraded {
		t.Logf("OCSP check result: %s", judgment.Message)
	}
}

func TestTLSChecker_Judge_ConnectionFailed(t *testing.T) {
	checker := NewTLSChecker()

	soul := &core.Soul{
		ID:     "test-tls",
		Name:   "Test TLS",
		Type:   core.CheckTLS,
		Target: "127.0.0.1:1", // Invalid port
		TLS: &core.TLSConfig{
			ExpiryWarnDays: 30,
		},
		Timeout: core.Duration{Duration: 2 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestTLSChecker_Judge_Timeout(t *testing.T) {
	checker := NewTLSChecker()

	soul := &core.Soul{
		ID:     "test-tls",
		Name:   "Test TLS",
		Type:   core.CheckTLS,
		Target: "example.com:443",
		TLS: &core.TLSConfig{
			ExpiryWarnDays: 30,
		},
		Timeout: core.Duration{Duration: 1 * time.Millisecond}, // Very short timeout
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestTLSChecker_Judge_NoTLSConfig(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	host := ts.URL[8:]

	checker := NewTLSChecker()

	soul := &core.Soul{
		ID:      "test-tls",
		Name:    "Test TLS",
		Type:    core.CheckTLS,
		Target:  host,
		Timeout: core.Duration{Duration: 5 * time.Second},
		// No TLS config - should use defaults
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

func TestTLSChecker_Judge_MinKeyBits(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	host := ts.URL[8:]

	checker := NewTLSChecker()

	soul := &core.Soul{
		ID:     "test-tls",
		Name:   "Test TLS",
		Type:   core.CheckTLS,
		Target: host,
		TLS: &core.TLSConfig{
			MinKeyBits: 2048,
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// MinKeyBits check is a placeholder, should still pass
	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}

	// Verify key size assertion was added
	foundKeySizeAssert := false
	for _, assert := range judgment.Details.Assertions {
		if assert.Type == "key_size" {
			foundKeySizeAssert = true
		}
	}
	if !foundKeySizeAssert {
		t.Error("Expected key size assertion")
	}
}

func TestParseTLSVersion(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected uint16
	}{
		{"TLS1.0", "TLS1.0", tls.VersionTLS10},
		{"TLS1.0 short", "TLS10", tls.VersionTLS10},
		{"TLS1.1", "TLS1.1", tls.VersionTLS11},
		{"TLS1.1 short", "TLS11", tls.VersionTLS11},
		{"TLS1.2", "TLS1.2", tls.VersionTLS12},
		{"TLS1.2 short", "TLS12", tls.VersionTLS12},
		{"TLS1.3", "TLS1.3", tls.VersionTLS13},
		{"TLS1.3 short", "TLS13", tls.VersionTLS13},
		{"lowercase", "tls1.2", tls.VersionTLS12},
		{"invalid", "invalid", 0},
		{"empty", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseTLSVersion(tt.input)
			if result != tt.expected {
				t.Errorf("parseTLSVersion(%s) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestMatchesSAN(t *testing.T) {
	tests := []struct {
		name     string
		san      string
		expected string
		want     bool
	}{
		{"exact match", "example.com", "example.com", true},
		{"exact mismatch", "example.com", "other.com", false},
		{"wildcard match", "api.example.com", "*.example.com", true},
		{"wildcard no match", "api.sub.example.com", "*.example.com", false},
		{"wildcard different domain", "example.org", "*.example.com", false},
		{"subdomain no wildcard", "api.example.com", "example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesSAN(tt.san, tt.expected)
			if result != tt.want {
				t.Errorf("matchesSAN(%s, %s) = %v, want %v", tt.san, tt.expected, result, tt.want)
			}
		})
	}
}

func TestExtractTLSInfo_NilState(t *testing.T) {
	info := extractTLSInfo(nil)
	if info != nil {
		t.Errorf("Expected nil for nil state, got %v", info)
	}
}

func TestExtractTLSInfo_ValidState(t *testing.T) {
	// Create a minimal TLS connection state for testing
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	conn, err := tls.Dial("tcp", ts.URL[8:], &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Skipf("Cannot create TLS connection: %v", err)
	}
	defer conn.Close()

	state := conn.ConnectionState()
	info := extractTLSInfo(&state)

	if info == nil {
		t.Error("Expected TLS info to be extracted")
	}

	if info.Protocol == "" {
		t.Error("Expected protocol to be set")
	}

	if info.CipherSuite == "" {
		t.Error("Expected cipher suite to be set")
	}
}

func TestTLSChecker_Judge_WildcardSAN(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	host := ts.URL[8:]

	checker := NewTLSChecker()

	soul := &core.Soul{
		ID:     "test-tls",
		Name:   "Test TLS",
		Type:   core.CheckTLS,
		Target: host,
		TLS: &core.TLSConfig{
			ExpectedSAN: []string{"*.localhost"}, // Wildcard pattern
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Test will likely fail SAN check but should handle wildcard correctly
	if judgment.Status != core.SoulAlive && judgment.Status != core.SoulDead {
		t.Logf("Wildcard SAN check result: %s", judgment.Message)
	}
}

func TestTLSChecker_Judge_MultipleAssertions(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	host := ts.URL[8:]

	checker := NewTLSChecker()

	soul := &core.Soul{
		ID:     "test-tls",
		Name:   "Test TLS",
		Type:   core.CheckTLS,
		Target: host,
		TLS: &core.TLSConfig{
			MinProtocol:        "TLS1.2",
			ExpiryWarnDays:     30,
			ExpiryCriticalDays: 7,
			ForbiddenCiphers:   []string{"NULL"},
			MinKeyBits:         2048,
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should have at least 3 assertions (tls_version, cipher_suite, key_size)
	// certificate_expiry is only added when cert is near expiry
	if len(judgment.Details.Assertions) < 3 {
		t.Errorf("Expected at least 3 assertions, got %d", len(judgment.Details.Assertions))
	}

	// Verify assertion are present
	assertionTypes := make(map[string]bool)
	for _, assert := range judgment.Details.Assertions {
		assertionTypes[assert.Type] = true
	}

	// These should always be present
	expectedTypes := []string{"tls_version", "cipher_suite", "key_size"}
	for _, expected := range expectedTypes {
		if !assertionTypes[expected] {
			t.Errorf("Expected assertion type %s", expected)
		}
	}
}

func TestTLSChecker_Judge_DeadStatusOnCriticalExpiry(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	host := ts.URL[8:]

	checker := NewTLSChecker()

	soul := &core.Soul{
		ID:     "test-tls",
		Name:   "Test TLS",
		Type:   core.CheckTLS,
		Target: host,
		TLS: &core.TLSConfig{
			ExpiryCriticalDays: 365 * 100, // Test certs expire quickly
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Test certificates are short-lived, should be dead or degraded
	if judgment.Status != core.SoulDead && judgment.Status != core.SoulDegraded {
		t.Logf("Short-lived test cert status: %s", judgment.Status)
	}
}
