package probe

import (
	"context"
	"net"
	"os"
	"strings"
	"testing"
)

func TestNewSSRFValidator(t *testing.T) {
	v := NewSSRFValidator()
	if v == nil {
		t.Fatal("NewSSRFValidator returned nil")
	}
}

func TestSSRFValidator_ValidateTarget_Empty(t *testing.T) {
	v := NewSSRFValidator()
	err := v.ValidateTarget("")
	if err == nil {
		t.Error("Expected error for empty target")
	}
	if !strings.Contains(err.Error(), "target URL is empty") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestSSRFValidator_ValidateTarget_InvalidURL(t *testing.T) {
	v := NewSSRFValidator()
	err := v.ValidateTarget("://invalid")
	if err == nil {
		t.Error("Expected error for invalid URL")
	}
}

func TestSSRFValidator_ValidateTarget_BlockedScheme(t *testing.T) {
	v := NewSSRFValidator()
	err := v.ValidateTarget("file:///etc/passwd")
	if err == nil {
		t.Error("Expected error for file scheme")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestSSRFValidator_ValidateTarget_AllowedSchemes(t *testing.T) {
	v := NewSSRFValidator()
	// Allow private for this test
	os.Setenv("ANUBIS_SSRF_ALLOW_PRIVATE", "1")
	defer os.Unsetenv("ANUBIS_SSRF_ALLOW_PRIVATE")
	v = NewSSRFValidator()

	schemes := []string{"http", "https", "ws", "wss"}
	for _, scheme := range schemes {
		err := v.ValidateTarget(scheme + "://example.com")
		if err != nil {
			t.Errorf("Scheme %q should be allowed, got: %v", scheme, err)
		}
	}
}

func TestSSRFValidator_ValidateTarget_BlockedSchemes(t *testing.T) {
	v := NewSSRFValidator()

	blocked := []string{"grpc", "tcp", "udp", "file", "ftp", "dict"}
	for _, scheme := range blocked {
		err := v.ValidateTarget(scheme + "://example.com")
		if err == nil {
			t.Errorf("Scheme %q should be blocked, got nil", scheme)
		}
		if !strings.Contains(err.Error(), "not allowed") {
			t.Errorf("Scheme %q error should contain 'not allowed', got: %v", scheme, err)
		}
	}
}

func TestSSRFValidator_ValidateTarget_BlockedHost(t *testing.T) {
	v := NewSSRFValidator()
	err := v.ValidateTarget("http://169.254.169.254/latest/meta-data/")
	if err == nil {
		t.Error("Expected error for blocked host")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestSSRFValidator_ValidateTarget_BlockedPrivateIP(t *testing.T) {
	v := NewSSRFValidator()
	err := v.ValidateTarget("http://10.0.0.1/admin")
	if err == nil {
		t.Error("Expected error for private IP")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestSSRFValidator_ValidateTarget_AllowedPublicIP(t *testing.T) {
	v := NewSSRFValidator()
	// Allow private so only the blocked list matters
	v.AllowPrivate = true
	// Public IP should be allowed
	err := v.ValidateTarget("http://8.8.8.8/resolve")
	if err != nil {
		t.Errorf("Public IP should be allowed: %v", err)
	}
}

func TestSSRFValidator_IsBlockedIP_Loopback(t *testing.T) {
	v := NewSSRFValidator()
	ip := net.ParseIP("127.0.0.1")
	if !v.isBlockedIP(ip) {
		t.Error("127.0.0.1 should be blocked")
	}
}

func TestSSRFValidator_IsBlockedIP_Private(t *testing.T) {
	v := NewSSRFValidator()
	privateIPs := []string{
		"10.0.0.1",
		"172.16.0.1",
		"172.31.255.255",
		"192.168.1.1",
	}
	for _, ipStr := range privateIPs {
		ip := net.ParseIP(ipStr)
		if !v.isBlockedIP(ip) {
			t.Errorf("Private IP %s should be blocked", ipStr)
		}
	}
}

func TestSSRFValidator_IsBlockedIP_AllowPrivate(t *testing.T) {
	v := NewSSRFValidator()
	v.AllowPrivate = true
	for _, ipStr := range []string{"10.0.0.1", "172.16.0.1", "192.168.1.1", "fc00::1"} {
		if v.isBlockedIP(net.ParseIP(ipStr)) {
			t.Errorf("%s should not be blocked when AllowPrivate is true", ipStr)
		}
	}
}

func TestSSRFValidator_HardBlockedNetworksCannotBeOverridden(t *testing.T) {
	_, metadataAllowed, _ := net.ParseCIDR("169.254.0.0/16")
	v := NewSSRFValidator()
	v.AllowPrivate = true
	v.AllowedNetworks = []*net.IPNet{metadataAllowed}

	for _, ipStr := range []string{
		"0.0.0.0", "169.254.169.254", "100.100.100.200", "224.0.0.1",
		"240.0.0.1", "255.255.255.255", "::", "fe80::1", "ff02::1",
	} {
		if !v.isBlockedIP(net.ParseIP(ipStr)) {
			t.Errorf("hard-blocked IP %s was allowed", ipStr)
		}
	}
}

func TestSSRFValidator_IsBlockedIP_AllowedNetworks(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.10.0.0/16")
	v := NewSSRFValidator()
	v.AllowedNetworks = append(v.AllowedNetworks, cidr)
	ip := net.ParseIP("10.10.1.1")
	if v.isBlockedIP(ip) {
		t.Error("IP in allowed network should not be blocked")
	}
}

func TestSSRFValidator_IsBlockedIP_EnvVar(t *testing.T) {
	os.Setenv("ANUBIS_SSRF_ALLOW_PRIVATE", "1")
	defer os.Unsetenv("ANUBIS_SSRF_ALLOW_PRIVATE")
	v := NewSSRFValidator()
	ip := net.ParseIP("192.168.1.1")
	if v.isBlockedIP(ip) {
		t.Error("Private IP should not be blocked via env var")
	}
}

func TestSSRFValidator_ValidateTarget_PublicHostname(t *testing.T) {
	v := NewSSRFValidator()
	v.AllowPrivate = true
	// google.com should resolve and be allowed
	err := v.ValidateTarget("http://google.com")
	if err != nil {
		// May fail in some environments, just log
		t.Logf("Public hostname check (may vary by env): %v", err)
	}
}

func TestSSRFValidator_ValidateAddress_Empty(t *testing.T) {
	v := NewSSRFValidator()
	err := v.ValidateAddress("")
	if err == nil {
		t.Error("Expected error for empty address")
	}
}

func TestSSRFValidator_ValidateAddress_BlockedIP(t *testing.T) {
	v := NewSSRFValidator()
	err := v.ValidateAddress("10.0.0.1:8080")
	if err == nil {
		t.Error("Expected error for blocked IP address")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestSSRFValidator_ValidateAddress_PublicIP(t *testing.T) {
	v := NewSSRFValidator()
	v.AllowPrivate = true
	err := v.ValidateAddress("8.8.8.8:53")
	if err != nil {
		t.Errorf("Public IP address should be allowed: %v", err)
	}
}

func TestResetDefaultForTest(t *testing.T) {
	os.Setenv("ANUBIS_SSRF_ALLOW_PRIVATE", "1")
	defer os.Unsetenv("ANUBIS_SSRF_ALLOW_PRIVATE")

	ResetDefaultForTest()

	// After reset, DefaultValidator should reflect the env var
	if !DefaultValidator.AllowPrivate {
		t.Error("Expected AllowPrivate to be true after reset with env var set")
	}

	// Don't restore the old validator - all tests need AllowPrivate=true
}

func TestWrapDialer_BlockedIP(t *testing.T) {
	// Temporarily unset env var so isBlockedIP uses the struct field only
	os.Unsetenv("ANUBIS_SSRF_ALLOW_PRIVATE")
	defer os.Setenv("ANUBIS_SSRF_ALLOW_PRIVATE", "1")

	v := NewSSRFValidator()
	v.AllowPrivate = false
	v.AllowedNetworks = nil // Remove any allowed networks
	var dialed bool
	dial := func(network, addr string) (net.Conn, error) {
		dialed = true
		return nil, nil
	}

	wrapped := v.WrapDialer(dial)
	_, err := wrapped("tcp", "10.0.0.1:8080")
	if err == nil {
		t.Error("Expected error for blocked IP")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("Unexpected error: %v", err)
	}
	if dialed {
		t.Error("Dial should not have been called for blocked IP")
	}
}

func TestWrapDialer_AllowedIP(t *testing.T) {
	v := NewSSRFValidator()
	v.AllowPrivate = true
	dialed := false
	dial := func(network, addr string) (net.Conn, error) {
		dialed = true
		return nil, nil
	}

	wrapped := v.WrapDialer(dial)
	_, err := wrapped("tcp", "10.0.0.1:8080")
	if err != nil {
		t.Errorf("Expected no error for allowed IP: %v", err)
	}
	if !dialed {
		t.Error("Dial should have been called for allowed IP")
	}
}

func TestWrapDialerContext_BlockedIP(t *testing.T) {
	os.Unsetenv("ANUBIS_SSRF_ALLOW_PRIVATE")
	defer os.Setenv("ANUBIS_SSRF_ALLOW_PRIVATE", "1")

	v := NewSSRFValidator()
	v.AllowPrivate = false
	v.AllowedNetworks = nil
	var dialed bool
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialed = true
		return nil, nil
	}

	wrapped := v.WrapDialerContext(dial)
	ctx := context.Background()
	_, err := wrapped(ctx, "tcp", "10.0.0.1:8080")
	if err == nil {
		t.Error("Expected error for blocked IP")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("Unexpected error: %v", err)
	}
	if dialed {
		t.Error("Dial should not have been called for blocked IP")
	}
}

func TestWrapDialerContext_AllowedIP(t *testing.T) {
	v := NewSSRFValidator()
	v.AllowPrivate = true
	dialed := false
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialed = true
		return nil, nil
	}

	wrapped := v.WrapDialerContext(dial)
	ctx := context.Background()
	_, err := wrapped(ctx, "tcp", "10.0.0.1:8080")
	if err != nil {
		t.Errorf("Expected no error for allowed IP: %v", err)
	}
	if !dialed {
		t.Error("Dial should have been called for allowed IP")
	}
}

func TestWrapDialer_SplitHostPortError(t *testing.T) {
	v := NewSSRFValidator()
	v.AllowPrivate = true
	dialed := false
	dial := func(network, addr string) (net.Conn, error) {
		dialed = true
		return nil, nil
	}

	wrapped := v.WrapDialer(dial)
	// Pass just an IP without port - should still work
	_, err := wrapped("tcp", "10.0.0.1")
	if err != nil {
		t.Errorf("Expected no error: %v", err)
	}
	if !dialed {
		t.Error("Dial should have been called")
	}
}

func TestWrapDialerContext_SplitHostPortError(t *testing.T) {
	v := NewSSRFValidator()
	v.AllowPrivate = true
	dialed := false
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialed = true
		return nil, nil
	}

	wrapped := v.WrapDialerContext(dial)
	ctx := context.Background()
	_, err := wrapped(ctx, "tcp", "10.0.0.1")
	if err != nil {
		t.Errorf("Expected no error: %v", err)
	}
	if !dialed {
		t.Error("Dial should have been called")
	}
}

func TestValidateTarget_Convenience(t *testing.T) {
	// The DefaultValidator is already permissive via init() in other test files
	err := ValidateTarget("http://8.8.8.8/dns-query")
	if err != nil {
		t.Errorf("ValidateTarget should allow public IP: %v", err)
	}
}

func TestValidateAddress_Convenience(t *testing.T) {
	// The DefaultValidator is already permissive via init() in other test files
	err := ValidateAddress("8.8.8.8:53")
	if err != nil {
		t.Errorf("ValidateAddress should allow public IP: %v", err)
	}
}

func TestWrapDialer_HostnameBlocksResolution(t *testing.T) {
	os.Unsetenv("ANUBIS_SSRF_ALLOW_PRIVATE")
	defer os.Setenv("ANUBIS_SSRF_ALLOW_PRIVATE", "1")

	v := NewSSRFValidator()
	// Don't set AllowPrivate - resolution of localhost should be blocked
	dialed := false
	dial := func(network, addr string) (net.Conn, error) {
		dialed = true
		return nil, nil
	}

	wrapped := v.WrapDialer(dial)
	_, err := wrapped("tcp", "localhost:8080")
	if err == nil {
		t.Error("Expected error for hostname resolving to blocked IP")
	}
	if dialed {
		t.Error("Dial should not have been called")
	}
}

func TestWrapDialerContext_HostnameBlocksResolution(t *testing.T) {
	os.Unsetenv("ANUBIS_SSRF_ALLOW_PRIVATE")
	defer os.Setenv("ANUBIS_SSRF_ALLOW_PRIVATE", "1")

	v := NewSSRFValidator()
	dialed := false
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialed = true
		return nil, nil
	}

	wrapped := v.WrapDialerContext(dial)
	ctx := context.Background()
	_, err := wrapped(ctx, "tcp", "localhost:8080")
	if err == nil {
		t.Error("Expected error for hostname resolving to blocked IP")
	}
	if dialed {
		t.Error("Dial should not have been called")
	}
}

func TestWrapDialer_AllowPrivateHostname(t *testing.T) {
	v := NewSSRFValidator()
	v.AllowPrivate = true
	var dialedAddr string
	dial := func(network, addr string) (net.Conn, error) {
		dialedAddr = addr
		return nil, nil
	}

	if _, err := v.WrapDialer(dial)("tcp", "localhost:8080"); err != nil {
		t.Fatalf("AllowPrivate should allow loopback hostname: %v", err)
	}
	host, _, err := net.SplitHostPort(dialedAddr)
	if err != nil || net.ParseIP(host) == nil {
		t.Fatalf("dial was not pinned to a literal IP: %q", dialedAddr)
	}
}

func TestWrapDialerContext_AllowPrivateHostname(t *testing.T) {
	v := NewSSRFValidator()
	v.AllowPrivate = true
	var dialedAddr string
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialedAddr = addr
		return nil, nil
	}

	if _, err := v.WrapDialerContext(dial)(context.Background(), "tcp", "localhost:8080"); err != nil {
		t.Fatalf("AllowPrivate should allow loopback hostname: %v", err)
	}
	host, _, err := net.SplitHostPort(dialedAddr)
	if err != nil || net.ParseIP(host) == nil {
		t.Fatalf("context dial was not pinned to a literal IP: %q", dialedAddr)
	}
}

func TestSSRFValidator_ValidateAddress_InvalidPort(t *testing.T) {
	v := NewSSRFValidator()
	v.AllowPrivate = true
	// Address with invalid port format - SplitHostPort fails, falls back to full string as host
	err := v.ValidateAddress("10.0.0.1")
	if err != nil {
		t.Errorf("Expected no error for address without port: %v", err)
	}
}

func TestSSRFValidator_ValidateTarget_NoHostname(t *testing.T) {
	v := NewSSRFValidator()
	err := v.ValidateTarget("http:///path")
	if err == nil {
		t.Error("Expected error for URL with no hostname")
	}
}

func TestSSRFValidator_ValidateTarget_BlockedCustomHost(t *testing.T) {
	v := NewSSRFValidator()
	v.BlockedHosts = append(v.BlockedHosts, "evil.internal")
	err := v.ValidateTarget("http://evil.internal/admin")
	if err == nil {
		t.Error("Expected error for custom blocked host")
	}
}

func TestSSRFValidator_IsBlockedIP_Multicast(t *testing.T) {
	os.Unsetenv("ANUBIS_SSRF_ALLOW_PRIVATE")
	defer os.Setenv("ANUBIS_SSRF_ALLOW_PRIVATE", "1")
	v := NewSSRFValidator()
	ip := net.ParseIP("224.0.0.1")
	if !v.isBlockedIP(ip) {
		t.Error("Multicast IP should be blocked")
	}
}

func TestSSRFValidator_IsBlockedIP_Reserved(t *testing.T) {
	os.Unsetenv("ANUBIS_SSRF_ALLOW_PRIVATE")
	defer os.Setenv("ANUBIS_SSRF_ALLOW_PRIVATE", "1")
	v := NewSSRFValidator()
	ip := net.ParseIP("240.0.0.1")
	if !v.isBlockedIP(ip) {
		t.Error("Reserved IP should be blocked")
	}
}

func TestSSRFValidator_IsBlockedIP_Broadcast(t *testing.T) {
	os.Unsetenv("ANUBIS_SSRF_ALLOW_PRIVATE")
	defer os.Setenv("ANUBIS_SSRF_ALLOW_PRIVATE", "1")
	v := NewSSRFValidator()
	ip := net.ParseIP("255.255.255.255")
	if !v.isBlockedIP(ip) {
		t.Error("Broadcast IP should be blocked")
	}
}

func TestSSRFValidator_IsBlockedIP_LinkLocal(t *testing.T) {
	os.Unsetenv("ANUBIS_SSRF_ALLOW_PRIVATE")
	defer os.Setenv("ANUBIS_SSRF_ALLOW_PRIVATE", "1")
	v := NewSSRFValidator()
	ip := net.ParseIP("169.254.1.1")
	if !v.isBlockedIP(ip) {
		t.Error("Link-local IP should be blocked")
	}
}

func TestSSRFValidator_ValidateTarget_UnresolvableHostname(t *testing.T) {
	v := NewSSRFValidator()
	v.AllowPrivate = true
	// Use a hostname that won't resolve
	err := v.ValidateTarget("http://this-host-definitely-does-not-exist-12345.invalid")
	if err == nil {
		t.Error("Expected error for unresolvable hostname")
	}
	if !strings.Contains(err.Error(), "cannot resolve") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestWrapDialer_UnresolvableHostname(t *testing.T) {
	v := NewSSRFValidator()
	v.AllowPrivate = true
	dialed := false
	dial := func(network, addr string) (net.Conn, error) {
		dialed = true
		return nil, nil
	}

	wrapped := v.WrapDialer(dial)
	_, err := wrapped("tcp", "this-host-definitely-does-not-exist-12345.invalid:80")
	if err == nil {
		t.Error("Expected error for unresolvable hostname")
	}
	if dialed {
		t.Error("Dial should not have been called")
	}
}
