package probe

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

var (
	// blockedHosts contains hostnames that should never be accessed
	blockedHosts = []string{
		// AWS metadata
		"169.254.169.254",
		"169.254.170.2",
		"169.254.170.254",
		// GCP metadata
		"metadata.google.internal",
		"metadata.google.internal.",
		"169.254.169.254",
		// Azure metadata
		"169.254.169.254",
		"169.254.169.250",
		"169.254.169.251",
		// DigitalOcean
		"169.254.169.254",
		// Alibaba Cloud
		"100.100.100.200",
		// Oracle Cloud
		"169.254.169.254",
		// OpenStack
		"169.254.169.254",
	}

	// blockedNetworks contains CIDR ranges that should never be accessed
	blockedNetworks = func() []*net.IPNet {
		networks := []*net.IPNet{}
		cidrs := []string{
			// Private IPv4 ranges
			"10.0.0.0/8",
			"172.16.0.0/12",
			"192.168.0.0/16",
			"127.0.0.0/8",
			"0.0.0.0/8",
			"169.254.0.0/16", // Link-local
			"224.0.0.0/4",    // Multicast
			"240.0.0.0/4",    // Reserved
			"255.255.255.255/32",
			// Private IPv6 ranges
			"::1/128",
			"fe80::/10",
			"fc00::/7",
			"ff00::/8",
		}
		for _, cidr := range cidrs {
			_, ipNet, err := net.ParseCIDR(cidr)
			if err == nil {
				networks = append(networks, ipNet)
			}
		}
		return networks
	}()
)

// SSRFValidator provides SSRF protection for probe targets
type SSRFValidator struct {
	// AllowPrivate allows private IP ranges (for internal monitoring)
	AllowPrivate bool
	// AllowedNetworks contains additional allowed CIDR ranges
	AllowedNetworks []*net.IPNet
	// BlockedHosts contains additional blocked hostnames/IPs
	BlockedHosts []string
}

// NewSSRFValidator creates a new SSRF validator with default settings
func NewSSRFValidator() *SSRFValidator {
	return &SSRFValidator{
		AllowPrivate: os.Getenv("ANUBIS_SSRF_ALLOW_PRIVATE") == "1",
	}
}

// ValidateTarget validates a target URL to prevent SSRF attacks
func (v *SSRFValidator) ValidateTarget(target string) error {
	if target == "" {
		return fmt.Errorf("target URL is empty")
	}

	// Parse the URL
	u, err := url.Parse(target)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	// Only allow specific schemes
	switch u.Scheme {
	case "http", "https", "ws", "wss", "grpc", "tcp", "udp":
		// Allowed
	default:
		return fmt.Errorf("URL scheme %q is not allowed", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL has no hostname")
	}

	// Check if host is in blocked list
	if v.isBlockedHost(host) {
		return fmt.Errorf("target host %q is blocked", host)
	}

	// Check if it's an IP address
	ip := net.ParseIP(host)
	if ip != nil {
		if v.isBlockedIP(ip) {
			return fmt.Errorf("target IP %q is blocked", ip)
		}
		return nil
	}

	// It's a hostname - resolve it and check all IPs
	addrs, err := net.LookupHost(host)
	if err != nil {
		// If we can't resolve, we can't validate - block it
		return fmt.Errorf("cannot resolve hostname %q: %w", host, err)
	}

	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip != nil && v.isBlockedIP(ip) {
			return fmt.Errorf("target hostname %q resolves to blocked IP %q", host, addr)
		}
	}

	return nil
}

// isBlockedHost checks if a hostname is in the blocked list
func (v *SSRFValidator) isBlockedHost(host string) bool {
	host = strings.ToLower(host)

	// Check default blocked hosts
	for _, blocked := range blockedHosts {
		if host == strings.ToLower(blocked) {
			return true
		}
	}

	// Check custom blocked hosts
	for _, blocked := range v.BlockedHosts {
		if host == strings.ToLower(blocked) {
			return true
		}
	}

	return false
}

// isBlockedIP checks if an IP address is blocked
func (v *SSRFValidator) isBlockedIP(ip net.IP) bool {
	// Check if explicitly allowed first (or via environment variable for tests)
	if v.AllowPrivate || os.Getenv("ANUBIS_SSRF_ALLOW_PRIVATE") == "1" {
		return false
	}

	// Check allowed networks
	for _, network := range v.AllowedNetworks {
		if network.Contains(ip) {
			return false
		}
	}

	// Check blocked networks
	for _, network := range blockedNetworks {
		if network.Contains(ip) {
			return true
		}
	}

	return false
}

// ValidateAddress validates a raw host:port address (for TCP/UDP probes)
func (v *SSRFValidator) ValidateAddress(address string) error {
	if address == "" {
		return fmt.Errorf("address is empty")
	}

	// Split host and port
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		// Try without port
		host = address
	}

	_ = port // Port validation can be added here if needed

	// Check blocked hosts
	if v.isBlockedHost(host) {
		return fmt.Errorf("target address %q is blocked", host)
	}

	// Check if it's an IP
	ip := net.ParseIP(host)
	if ip != nil {
		if v.isBlockedIP(ip) {
			return fmt.Errorf("target IP %q is blocked", ip)
		}
		return nil
	}

	// Resolve hostname
	addrs, err := net.LookupHost(host)
	if err != nil {
		return fmt.Errorf("cannot resolve hostname %q: %w", host, err)
	}

	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip != nil && v.isBlockedIP(ip) {
			return fmt.Errorf("target hostname %q resolves to blocked IP %q", host, addr)
		}
	}

	return nil
}

// DefaultValidator is the default SSRF validator instance
var DefaultValidator = NewSSRFValidator()

// ValidateTarget is a convenience function using the default validator
func ValidateTarget(target string) error {
	return DefaultValidator.ValidateTarget(target)
}

// ValidateAddress is a convenience function using the default validator
func ValidateAddress(address string) error {
	return DefaultValidator.ValidateAddress(address)
}

// ResetDefaultForTest reinitializes DefaultValidator with the current env vars.
// Test-only: call after setting ANUBIS_SSRF_ALLOW_PRIVATE=1.
func ResetDefaultForTest() {
	DefaultValidator = NewSSRFValidator()
}
