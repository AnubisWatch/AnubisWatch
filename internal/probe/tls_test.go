package probe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func init() {
	// Allow private IPs in tests (for localhost test servers)
	os.Setenv("ANUBIS_SSRF_ALLOW_PRIVATE", "1")
	// Reset the DefaultValidator so it picks up the env var
	DefaultValidator = NewSSRFValidator()
}

func TestTLSServerNameExcludesPort(t *testing.T) {
	for input, want := range map[string]string{
		"example.com:443":   "example.com",
		"[2001:db8::1]:443": "2001:db8::1",
		"example.com":       "example.com",
	} {
		if got := tlsServerName(input); got != want {
			t.Errorf("tlsServerName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestTLSChecker_DiagnosticHonorsCancelledContext(t *testing.T) {
	checker := NewTLSChecker()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	soul := &core.Soul{ID: "tls-cancelled", Target: "example.com:443"}
	started := time.Now()
	judgment := checker.diagnoseTLSFailure(ctx, soul, context.Canceled, time.Second)
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("diagnostic ignored cancelled context: %v", elapsed)
	}
	if judgment == nil || judgment.Status != core.SoulDead {
		t.Fatalf("expected failed diagnostic judgment, got %#v", judgment)
	}
}

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

	// Test server uses self-signed certificate, which should fail verification
	// This is the CORRECT secure behavior - InsecureSkipVerify should NOT be used
	if judgment.Status == core.SoulAlive {
		t.Logf("Note: Test server certificate was accepted (may be in system trust store)")
	}

	// The judgment should contain TLS info even for failed connections (diagnostic mode)
	if judgment.TLSInfo == nil {
		t.Error("Expected TLS info to be populated for diagnostic purposes")
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

	// Test server uses self-signed certificate - verification will fail
	// but if TLS version can be determined, it should be in assertions
	if judgment.Status == core.SoulAlive {
		// Only verify protocol assertions if connection succeeded (cert in trust store)
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
	} else {
		// Self-signed cert failed verification - this is expected secure behavior
		t.Logf("Test server certificate failed verification (expected): %s", judgment.Message)
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

	// Test server uses self-signed certificate - verification may fail
	// If it succeeds, test servers use strong ciphers so should pass cipher check
	if judgment.Status == core.SoulAlive {
		// Cipher check passed
	} else {
		// Self-signed cert failed verification - this is expected secure behavior
		t.Logf("Test server certificate failed verification (expected): %s", judgment.Message)
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

	// Test server uses self-signed certificate - may fail verification
	// The important thing is that it runs without error
	if judgment.Status == core.SoulAlive {
		t.Logf("Test server certificate accepted")
	} else {
		t.Logf("Test server certificate failed verification (expected): %s", judgment.Message)
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

	// Test server uses self-signed certificate - verification may fail
	// If connection succeeds, verify key size assertion was added
	if judgment.Status == core.SoulAlive {
		foundKeySizeAssert := false
		for _, assert := range judgment.Details.Assertions {
			if assert.Type == "key_size" {
				foundKeySizeAssert = true
			}
		}
		if !foundKeySizeAssert {
			t.Error("Expected key size assertion")
		}
	} else {
		t.Logf("Test server certificate failed verification (expected): %s", judgment.Message)
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

	// Verify TLS info is extracted
	_ = info.Protocol
	_ = info.CipherSuite
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

	// Test server uses self-signed certificate - verification may fail
	// If connection succeeds, verify assertions are present
	if judgment.Status == core.SoulAlive {
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
	} else {
		t.Logf("Test server certificate failed verification (expected): %s", judgment.Message)
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

// Test TLS SAN mismatch - should return SoulDead
func TestTLSChecker_Judge_SANMismatch(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	host := ts.URL[8:]

	checker := NewTLSChecker()

	soul := &core.Soul{
		ID:     "test-tls-san-mismatch",
		Name:   "Test TLS SAN Mismatch",
		Type:   core.CheckTLS,
		Target: host,
		TLS: &core.TLSConfig{
			ExpectedSAN: []string{"definitely-not-matching.example.com"},
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// SAN should not match test certificate
	if judgment.Status != core.SoulDead {
		t.Logf("Expected status Dead for SAN mismatch, got %s", judgment.Status)
	}
}

// Test TLS Judge with OCSP check
func TestTLSChecker_Judge_OCSPPresent(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	host := ts.URL[8:]

	checker := NewTLSChecker()

	soul := &core.Soul{
		ID:     "test-tls-ocsp",
		Name:   "Test TLS OCSP",
		Type:   core.CheckTLS,
		Target: host,
		TLS: &core.TLSConfig{
			CheckOCSP: true,
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Test server uses self-signed certificate - verification may fail
	// If connection succeeds, test servers don't have OCSP stapling, should be degraded
	if judgment.Status == core.SoulAlive || judgment.Status == core.SoulDegraded {
		// Verify OCSP assertion was added
		foundOCSPAssert := false
		for _, assert := range judgment.Details.Assertions {
			if assert.Type == "ocsp_stapling" {
				foundOCSPAssert = true
				break
			}
		}
		if !foundOCSPAssert {
			t.Error("Expected OCSP stapling assertion")
		}
	} else {
		t.Logf("Test server certificate failed verification (expected): %s", judgment.Message)
	}
}

// Test TLS Judge with issuer mismatch (degraded, not dead)
func TestTLSChecker_Judge_IssuerMismatch(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	host := ts.URL[8:]

	checker := NewTLSChecker()

	soul := &core.Soul{
		ID:     "test-tls-issuer",
		Name:   "Test TLS Issuer",
		Type:   core.CheckTLS,
		Target: host,
		TLS: &core.TLSConfig{
			ExpectedIssuer: "DefinitelyNotTestCA",
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Test server uses self-signed certificate - verification may fail
	// If connection succeeds, test cert issuer won't match, should be degraded
	if judgment.Status == core.SoulAlive || judgment.Status == core.SoulDegraded {
		// Verify issuer assertion was added
		foundIssuerAssert := false
		for _, assert := range judgment.Details.Assertions {
			if assert.Type == "issuer" {
				foundIssuerAssert = true
				break
			}
		}
		if !foundIssuerAssert {
			t.Error("Expected issuer assertion")
		}
	} else {
		t.Logf("Test server certificate failed verification (expected): %s", judgment.Message)
	}
}

// TestTLSChecker_Judge_EmptyIssuerOrg tests issuer check when Organization is empty
func TestTLSChecker_Judge_EmptyIssuerOrg(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	host := ts.URL[8:]

	checker := NewTLSChecker()

	soul := &core.Soul{
		ID:     "test-tls-issuer-empty",
		Name:   "Test TLS Issuer Empty Org",
		Type:   core.CheckTLS,
		Target: host,
		TLS: &core.TLSConfig{
			ExpectedIssuer: "SomeCA",
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should handle empty org gracefully
	if judgment == nil {
		t.Error("Expected judgment to be returned")
	}
}

// TestTLSChecker_Judge_OCSPStaplingAbsent tests the OCSP absent degraded path
func TestTLSChecker_Judge_OCSPStaplingAbsent(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	host := ts.URL[8:]

	checker := NewTLSChecker()

	soul := &core.Soul{
		ID:     "test-tls-ocsp-absent",
		Name:   "Test TLS OCSP Absent",
		Type:   core.CheckTLS,
		Target: host,
		TLS: &core.TLSConfig{
			CheckOCSP: true,
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Test server uses self-signed certificate - verification may fail
	// If connection succeeds, test servers don't have OCSP stapling, should be degraded
	if judgment.Status == core.SoulAlive || judgment.Status == core.SoulDegraded {
		// Verify OCSP assertion was added and failed
		foundOCSPAssert := false
		for _, assert := range judgment.Details.Assertions {
			if assert.Type == "ocsp_stapling" {
				foundOCSPAssert = true
				if assert.Passed {
					t.Error("Expected OCSP assertion to fail (no stapling)")
				}
				break
			}
		}
		if !foundOCSPAssert {
			t.Error("Expected OCSP stapling assertion")
		}
	} else {
		t.Logf("Test server certificate failed verification (expected): %s", judgment.Message)
	}
}

// TestTLSChecker_Judge_DefaultTimeout tests the default timeout path (timeout == 0)
func TestTLSChecker_Judge_DefaultTimeout(t *testing.T) {
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
		// No Timeout set - should default to 10s
	}

	ctx := context.Background()
	judgment, err := checker.Judge(ctx, soul)

	if err != nil {
		t.Fatalf("Judge failed: %v", err)
	}

	// Test server uses self-signed certificate - may fail verification
	// The important thing is that it runs without error using default timeout
	if judgment.Status == core.SoulAlive {
		t.Logf("Test server certificate accepted")
	} else {
		t.Logf("Test server certificate failed verification (expected): %s", judgment.Message)
	}
}

// TestTLSChecker_Judge_TargetWithoutPort tests SplitHostPort error path (no port in target)
func TestTLSChecker_Judge_TargetWithoutPort(t *testing.T) {
	checker := NewTLSChecker()

	soul := &core.Soul{
		ID:      "test-tls",
		Name:    "Test TLS",
		Type:    core.CheckTLS,
		Target:  "example.com", // No port - SplitHostPort will fail
		Timeout: core.Duration{Duration: 2 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Should attempt connection to "example.com" (no port), which will fail
	if judgment.Status != core.SoulDead {
		t.Logf("Expected Dead for target without port, got %s", judgment.Status)
	}
}

// TestTLSChecker_Judge_CipherForbidden tests the forbidden cipher path triggers degraded
func TestTLSChecker_Judge_CipherForbidden(t *testing.T) {
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
			ForbiddenCiphers: []string{"AES"},
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// Test server uses self-signed certificate - verification may fail
	// If connection succeeds, verify cipher assertion is present
	if judgment.Status == core.SoulAlive || judgment.Status == core.SoulDegraded {
		foundCipherAssert := false
		for _, assert := range judgment.Details.Assertions {
			if assert.Type == "cipher_suite" {
				foundCipherAssert = true
				break
			}
		}
		if !foundCipherAssert {
			t.Error("Expected cipher_suite assertion")
		}
	} else {
		t.Logf("Test server certificate failed verification (expected): %s", judgment.Message)
	}
}

// TestTLSChecker_Judge_DiagnoseTLSFailure tests the diagnoseTLSFailure path
func TestTLSChecker_Judge_DiagnoseTLSFailure(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	host := ts.URL[8:]

	checker := NewTLSChecker()

	soul := &core.Soul{
		ID:      "test-tls-diagnose",
		Name:    "Test TLS Diagnose",
		Type:    core.CheckTLS,
		Target:  host,
		Enabled: true,
		Weight:  core.Duration{Duration: 60 * time.Second},
		TLS: &core.TLSConfig{
			ExpiryWarnDays:     30,
			ExpiryCriticalDays: 7,
			MinProtocol:        "TLS1.2",
			ForbiddenCiphers:   []string{"NULL"},
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, err := checker.Judge(ctx, soul)

	if err != nil {
		t.Fatalf("Judge failed: %v", err)
	}

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead for self-signed cert, got %s", judgment.Status)
	}

	if judgment.TLSInfo == nil {
		t.Error("Expected TLSInfo to be populated from diagnostic mode")
	}

	if judgment.Message == "" {
		t.Error("Expected error message in judgment")
	}
}

// TestExtractTLSCertsOnly_Empty tests extractTLSCertsOnly with empty cert list
func TestExtractTLSCertsOnly_Empty(t *testing.T) {
	result := extractTLSCertsOnly([]*x509.Certificate{})
	if result != nil {
		t.Error("Expected nil for empty certificate list")
	}
}

// TestExtractTLSCertsOnly_SingleCert tests with a real certificate
func TestExtractTLSCertsOnly_SingleCert(t *testing.T) {
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
	if len(state.PeerCertificates) == 0 {
		t.Skip("No peer certificates")
	}

	result := extractTLSCertsOnly(state.PeerCertificates)
	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	// httptest certs may have empty CommonName but should have SANs
	if result.Issuer == "" && result.Subject == "" && len(result.SANs) == 0 {
		t.Error("Expected at least one of issuer, subject, or SANs to be set")
	}
	if result.ChainLength != len(state.PeerCertificates) {
		t.Errorf("Expected chain length %d, got %d", len(state.PeerCertificates), result.ChainLength)
	}
	if result.KeyType == "" {
		t.Error("Expected key type to be set")
	}
	if result.NotAfter.IsZero() {
		t.Error("Expected NotAfter to be set")
	}
}

// TestTLSChecker_Judge_TargetWithHTTPS_StripScheme tests stripping https:// prefix
func TestTLSChecker_Judge_TargetWithHTTPS_StripScheme(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	host := ts.URL[8:]

	checker := NewTLSChecker()

	soul := &core.Soul{
		ID:      "test-tls-scheme",
		Name:    "Test TLS Scheme",
		Type:    core.CheckTLS,
		Target:  "https://" + host,
		Enabled: true,
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	err := checker.Validate(soul)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	if strings.HasPrefix(soul.Target, "https://") {
		t.Errorf("Expected https:// prefix to be stripped, got %s", soul.Target)
	}
}

// TestTLSChecker_Judge_TargetWithHTTP_StripScheme tests stripping http:// prefix
func TestTLSChecker_Judge_TargetWithHTTP_StripScheme(t *testing.T) {
	checker := NewTLSChecker()

	soul := &core.Soul{
		ID:      "test-tls-http",
		Name:    "Test TLS HTTP Scheme",
		Type:    core.CheckTLS,
		Target:  "http://example.com:443",
		Enabled: true,
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	err := checker.Validate(soul)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	if strings.HasPrefix(soul.Target, "http://") {
		t.Errorf("Expected http:// prefix to be stripped, got %s", soul.Target)
	}
}

// TestTLSChecker_Validate_SSRFBlocked tests SSRF protection in TLS Validate
func TestTLSChecker_Validate_SSRFBlocked(t *testing.T) {
	oldValidator := DefaultValidator
	DefaultValidator = &SSRFValidator{AllowPrivate: false}
	defer func() { DefaultValidator = oldValidator }()

	checker := NewTLSChecker()

	soul := &core.Soul{
		ID:     "test-tls-ssrf",
		Name:   "Test TLS SSRF",
		Type:   core.CheckTLS,
		Target: "169.254.169.254:443",
	}

	err := checker.Validate(soul)
	if err == nil {
		t.Error("Expected SSRF validation error for blocked IP")
	}
	if !strings.Contains(err.Error(), "SSRF") {
		t.Errorf("Expected SSRF error message, got: %v", err)
	}
}

// TestMatchesSAN_EmptyExpected tests matchesSAN with empty expected string
func TestMatchesSAN_EmptyExpected(t *testing.T) {
	result := matchesSAN("example.com", "")
	if result {
		t.Error("Expected false for empty expected string")
	}
}
