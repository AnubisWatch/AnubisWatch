package probe

import (
	"context"
	"net"
	"strings"
	"testing"
)

// The probe checkers in this package dial local test servers through the
// process-wide SSRF validator, so the package runs under a policy that allows
// private targets (published by the init functions in the other test files).
//
// Unit tests that assert the strict policy must therefore state that policy
// explicitly instead of inferring it from the ambient environment. An
// environment change made by one test leaks into every test that runs after
// it, and whether it leaks decides the outcome: the same suite passes in one
// order and fails in another. The helpers below give every test a local,
// unpublished validator, so the assertions depend on the test and not on the
// order it runs in.

// strictSSRFValidator returns a validator that denies private targets,
// independent of the ambient ANUBIS_SSRF_ALLOW_PRIVATE value.
func strictSSRFValidator() *SSRFValidator {
	v := NewSSRFValidator()
	v.AllowPrivate = false
	return v
}

// permissiveSSRFValidator returns a validator that allows private targets.
func permissiveSSRFValidator() *SSRFValidator {
	v := NewSSRFValidator()
	v.AllowPrivate = true
	return v
}

// publishValidatorForTest publishes v as the process-wide validator and
// restores the previous one when the test ends. The validator is replaced
// rather than mutated: the published pointer is read concurrently by the
// probe checkers, and a validator that is published once must stay valid for
// the whole run.
func publishValidatorForTest(t *testing.T, v *SSRFValidator) {
	t.Helper()
	prev := DefaultValidator.Load()
	t.Cleanup(func() { DefaultValidator.Store(prev) })
	DefaultValidator.Store(v)
}

func TestNewSSRFValidator(t *testing.T) {
	v := NewSSRFValidator()
	if v == nil {
		t.Fatal("NewSSRFValidator returned nil")
	}
}

func TestSSRFValidator_ValidateTarget_Empty(t *testing.T) {
	v := strictSSRFValidator()
	err := v.ValidateTarget("")
	if err == nil {
		t.Fatal("Expected error for empty target")
	}
	if !strings.Contains(err.Error(), "target URL is empty") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestSSRFValidator_ValidateTarget_InvalidURL(t *testing.T) {
	v := strictSSRFValidator()
	err := v.ValidateTarget("://invalid")
	if err == nil {
		t.Fatal("Expected error for invalid URL")
	}
}

func TestSSRFValidator_ValidateTarget_BlockedScheme(t *testing.T) {
	v := strictSSRFValidator()
	err := v.ValidateTarget("file:///etc/passwd")
	if err == nil {
		t.Fatal("Expected error for file scheme")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestSSRFValidator_ValidateTarget_AllowedSchemes(t *testing.T) {
	v := permissiveSSRFValidator()

	schemes := []string{"http", "https", "ws", "wss"}
	for _, scheme := range schemes {
		err := v.ValidateTarget(scheme + "://example.com")
		if err != nil {
			t.Errorf("Scheme %q should be allowed, got: %v", scheme, err)
		}
	}
}

func TestSSRFValidator_ValidateTarget_BlockedSchemes(t *testing.T) {
	v := strictSSRFValidator()

	blocked := []string{"grpc", "tcp", "udp", "file", "ftp", "dict"}
	for _, scheme := range blocked {
		err := v.ValidateTarget(scheme + "://example.com")
		if err == nil {
			t.Errorf("Scheme %q should be blocked, got nil", scheme)
			continue
		}
		if !strings.Contains(err.Error(), "not allowed") {
			t.Errorf("Scheme %q error should contain 'not allowed', got: %v", scheme, err)
		}
	}
}

func TestSSRFValidator_ValidateTarget_BlockedHost(t *testing.T) {
	v := strictSSRFValidator()
	err := v.ValidateTarget("http://169.254.169.254/latest/meta-data/")
	if err == nil {
		t.Fatal("Expected error for blocked host")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestSSRFValidator_ValidateTarget_BlockedPrivateIP(t *testing.T) {
	v := strictSSRFValidator()
	err := v.ValidateTarget("http://10.0.0.1/admin")
	if err == nil {
		t.Fatal("Expected error for private IP")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestSSRFValidator_ValidateTarget_AllowedPublicIP(t *testing.T) {
	// Allow private so only the blocked list matters
	v := permissiveSSRFValidator()
	// Public IP should be allowed
	err := v.ValidateTarget("http://8.8.8.8/resolve")
	if err != nil {
		t.Errorf("Public IP should be allowed: %v", err)
	}
}

func TestSSRFValidator_IsBlockedIP_Loopback(t *testing.T) {
	// Loopback is a private range: it is denied by the strict policy, and
	// allowed only when the caller opts into private targets.
	v := strictSSRFValidator()
	ip := net.ParseIP("127.0.0.1")
	if !v.isBlockedIP(ip) {
		t.Error("127.0.0.1 should be blocked")
	}
}

func TestSSRFValidator_IsBlockedIP_Private(t *testing.T) {
	v := strictSSRFValidator()
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
	v := permissiveSSRFValidator()
	for _, ipStr := range []string{"10.0.0.1", "172.16.0.1", "192.168.1.1", "fc00::1", "127.0.0.1", "::1"} {
		if v.isBlockedIP(net.ParseIP(ipStr)) {
			t.Errorf("%s should not be blocked when AllowPrivate is true", ipStr)
		}
	}
}

func TestSSRFValidator_HardBlockedNetworksCannotBeOverridden(t *testing.T) {
	_, metadataAllowed, _ := net.ParseCIDR("169.254.0.0/16")
	v := permissiveSSRFValidator()
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
	// The allowlist has to work under the strict policy, otherwise the test
	// would pass even without the allowed network.
	v := strictSSRFValidator()
	v.AllowedNetworks = append(v.AllowedNetworks, cidr)
	ip := net.ParseIP("10.10.1.1")
	if v.isBlockedIP(ip) {
		t.Error("IP in allowed network should not be blocked")
	}
}

func TestSSRFValidator_IsBlockedIP_EnvVar(t *testing.T) {
	// t.Setenv restores the previous value when the test ends, so the
	// package-wide policy is unaffected by this test.
	t.Setenv("ANUBIS_SSRF_ALLOW_PRIVATE", "1")
	v := NewSSRFValidator()
	if !v.AllowPrivate {
		t.Fatal("Expected AllowPrivate to be true with ANUBIS_SSRF_ALLOW_PRIVATE=1")
	}
	ip := net.ParseIP("192.168.1.1")
	if v.isBlockedIP(ip) {
		t.Error("Private IP should not be blocked via env var")
	}
}

// The env var is the only switch for the private-target policy: any value
// other than "1" keeps private targets blocked.
func TestSSRFValidator_IsBlockedIP_EnvVarDisabled(t *testing.T) {
	t.Setenv("ANUBIS_SSRF_ALLOW_PRIVATE", "0")
	v := NewSSRFValidator()
	if v.AllowPrivate {
		t.Fatal("Expected AllowPrivate to be false with ANUBIS_SSRF_ALLOW_PRIVATE=0")
	}
	if !v.isBlockedIP(net.ParseIP("192.168.1.1")) {
		t.Error("Private IP should be blocked when the env var is not 1")
	}
}

func TestSSRFValidator_ValidateTarget_PublicHostname(t *testing.T) {
	v := permissiveSSRFValidator()
	// google.com should resolve and be allowed
	err := v.ValidateTarget("http://google.com")
	if err != nil {
		// May fail in some environments, just log
		t.Logf("Public hostname check (may vary by env): %v", err)
	}
}

func TestSSRFValidator_ValidateAddress_Empty(t *testing.T) {
	v := strictSSRFValidator()
	err := v.ValidateAddress("")
	if err == nil {
		t.Fatal("Expected error for empty address")
	}
}

func TestSSRFValidator_ValidateAddress_BlockedIP(t *testing.T) {
	v := strictSSRFValidator()
	err := v.ValidateAddress("10.0.0.1:8080")
	if err == nil {
		t.Fatal("Expected error for blocked IP address")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestSSRFValidator_ValidateAddress_PublicIP(t *testing.T) {
	v := permissiveSSRFValidator()
	err := v.ValidateAddress("8.8.8.8:53")
	if err != nil {
		t.Errorf("Public IP address should be allowed: %v", err)
	}
}

func TestResetDefaultForTest(t *testing.T) {
	// The reset replaces the published validator, so the previous one is
	// restored afterwards: the package-wide policy has to survive this test.
	prev := DefaultValidator.Load()
	t.Cleanup(func() { DefaultValidator.Store(prev) })
	t.Setenv("ANUBIS_SSRF_ALLOW_PRIVATE", "1")

	ResetDefaultForTest()

	// After reset, DefaultValidator should reflect the env var
	if !DefaultValidator.Load().AllowPrivate {
		t.Error("Expected AllowPrivate to be true after reset with env var set")
	}
}

func TestWrapDialer_BlockedIP(t *testing.T) {
	// The policy is stated by the validator, not by the environment.
	v := strictSSRFValidator()
	v.AllowedNetworks = nil // Remove any allowed networks
	var dialed bool
	dial := func(network, addr string) (net.Conn, error) {
		dialed = true
		return nil, nil
	}

	wrapped := v.WrapDialer(dial)
	_, err := wrapped("tcp", "10.0.0.1:8080")
	if err == nil {
		t.Fatal("Expected error for blocked IP")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("Unexpected error: %v", err)
	}
	if dialed {
		t.Error("Dial should not have been called for blocked IP")
	}
}

func TestWrapDialer_AllowedIP(t *testing.T) {
	v := permissiveSSRFValidator()
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
	v := strictSSRFValidator()
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
		t.Fatal("Expected error for blocked IP")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("Unexpected error: %v", err)
	}
	if dialed {
		t.Error("Dial should not have been called for blocked IP")
	}
}

func TestWrapDialerContext_AllowedIP(t *testing.T) {
	v := permissiveSSRFValidator()
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
	v := permissiveSSRFValidator()
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
	v := permissiveSSRFValidator()
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
	// A public address is accepted under any policy, so this only depends on
	// the published validator being readable.
	err := ValidateTarget("http://8.8.8.8/dns-query")
	if err != nil {
		t.Errorf("ValidateTarget should allow public IP: %v", err)
	}
}

func TestValidateAddress_Convenience(t *testing.T) {
	// A public address is accepted under any policy, so this only depends on
	// the published validator being readable.
	err := ValidateAddress("8.8.8.8:53")
	if err != nil {
		t.Errorf("ValidateAddress should allow public IP: %v", err)
	}
}

func TestWrapDialer_HostnameBlocksResolution(t *testing.T) {
	// The strict policy is stated by the validator: resolution of localhost
	// must be blocked.
	v := strictSSRFValidator()
	dialed := false
	dial := func(network, addr string) (net.Conn, error) {
		dialed = true
		return nil, nil
	}

	wrapped := v.WrapDialer(dial)
	_, err := wrapped("tcp", "localhost:8080")
	if err == nil {
		t.Fatal("Expected error for hostname resolving to blocked IP")
	}
	if dialed {
		t.Error("Dial should not have been called")
	}
}

func TestWrapDialerContext_HostnameBlocksResolution(t *testing.T) {
	v := strictSSRFValidator()
	dialed := false
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialed = true
		return nil, nil
	}

	wrapped := v.WrapDialerContext(dial)
	ctx := context.Background()
	_, err := wrapped(ctx, "tcp", "localhost:8080")
	if err == nil {
		t.Fatal("Expected error for hostname resolving to blocked IP")
	}
	if dialed {
		t.Error("Dial should not have been called")
	}
}

func TestWrapDialer_AllowPrivateHostname(t *testing.T) {
	v := permissiveSSRFValidator()
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
	v := permissiveSSRFValidator()
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
	v := permissiveSSRFValidator()
	// Address with invalid port format - SplitHostPort fails, falls back to full string as host
	err := v.ValidateAddress("10.0.0.1")
	if err != nil {
		t.Errorf("Expected no error for address without port: %v", err)
	}
}

func TestSSRFValidator_ValidateTarget_NoHostname(t *testing.T) {
	v := strictSSRFValidator()
	err := v.ValidateTarget("http:///path")
	if err == nil {
		t.Fatal("Expected error for URL with no hostname")
	}
}

func TestSSRFValidator_ValidateTarget_BlockedCustomHost(t *testing.T) {
	v := strictSSRFValidator()
	v.BlockedHosts = append(v.BlockedHosts, "evil.internal")
	err := v.ValidateTarget("http://evil.internal/admin")
	if err == nil {
		t.Fatal("Expected error for custom blocked host")
	}
}

func TestSSRFValidator_IsBlockedIP_Multicast(t *testing.T) {
	// Multicast is hard-blocked: it stays blocked even when private targets
	// are allowed.
	v := permissiveSSRFValidator()
	ip := net.ParseIP("224.0.0.1")
	if !v.isBlockedIP(ip) {
		t.Error("Multicast IP should be blocked")
	}
}

func TestSSRFValidator_IsBlockedIP_Reserved(t *testing.T) {
	// Reserved space is hard-blocked: it stays blocked even when private
	// targets are allowed.
	v := permissiveSSRFValidator()
	ip := net.ParseIP("240.0.0.1")
	if !v.isBlockedIP(ip) {
		t.Error("Reserved IP should be blocked")
	}
}

func TestSSRFValidator_IsBlockedIP_Broadcast(t *testing.T) {
	// The broadcast address is hard-blocked: it stays blocked even when
	// private targets are allowed.
	v := permissiveSSRFValidator()
	ip := net.ParseIP("255.255.255.255")
	if !v.isBlockedIP(ip) {
		t.Error("Broadcast IP should be blocked")
	}
}

func TestSSRFValidator_IsBlockedIP_LinkLocal(t *testing.T) {
	// Link-local (metadata) space is hard-blocked: it stays blocked even when
	// private targets are allowed.
	v := permissiveSSRFValidator()
	ip := net.ParseIP("169.254.1.1")
	if !v.isBlockedIP(ip) {
		t.Error("Link-local IP should be blocked")
	}
}

func TestSSRFValidator_ValidateTarget_UnresolvableHostname(t *testing.T) {
	v := strictSSRFValidator()
	// Use a hostname that won't resolve
	err := v.ValidateTarget("http://this-host-definitely-does-not-exist-12345.invalid")
	if err == nil {
		t.Fatal("Expected error for unresolvable hostname")
	}
	if !strings.Contains(err.Error(), "cannot resolve") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestWrapDialer_UnresolvableHostname(t *testing.T) {
	v := permissiveSSRFValidator()
	dialed := false
	dial := func(network, addr string) (net.Conn, error) {
		dialed = true
		return nil, nil
	}

	wrapped := v.WrapDialer(dial)
	_, err := wrapped("tcp", "this-host-definitely-does-not-exist-12345.invalid:80")
	if err == nil {
		t.Fatal("Expected error for unresolvable hostname")
	}
	if dialed {
		t.Error("Dial should not have been called")
	}
}
