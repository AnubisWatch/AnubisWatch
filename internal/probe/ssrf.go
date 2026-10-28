package probe

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
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

	// hardBlockedNetworks are never reachable through probes or alert
	// channels, even when private monitoring is enabled. They cover local
	// host, metadata/link-local, unspecified, multicast, broadcast, and
	// reserved address space.
	hardBlockedNetworks = mustParseCIDRs([]string{
		"0.0.0.0/8",
		"169.254.0.0/16",
		"100.100.100.200/32", // Alibaba Cloud metadata (inside CGNAT space)
		"224.0.0.0/4",
		"240.0.0.0/4",
		"255.255.255.255/32",
		"::/128",
		"fe80::/10",
		"ff00::/8",
		"fd00:ec2::254/128", // AWS IMDS over IPv6 (inside fc00::/7)
		// Local-use NAT64 (RFC 8215) embeds IPv4 at an operator-chosen
		// prefix length, so the embedded address cannot be judged reliably.
		"64:ff9b:1::/48",
		// Teredo obfuscates the embedded client IPv4 address; the
		// protocol is obsolete and has no legitimate monitoring use.
		"2001::/32",
	})

	// privateNetworks are not public Internet destinations. They may be
	// reached only when explicitly enabled (or when a narrower
	// AllowedNetworks entry permits the address).
	privateNetworks = mustParseCIDRs([]string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"100.64.0.0/10",   // carrier-grade NAT shared space (RFC 6598)
		"192.0.0.0/24",    // IETF protocol assignments (RFC 6890)
		"192.0.2.0/24",    // TEST-NET-1 (RFC 5737)
		"198.18.0.0/15",   // benchmarking (RFC 2544)
		"198.51.100.0/24", // TEST-NET-2 (RFC 5737)
		"203.0.113.0/24",  // TEST-NET-3 (RFC 5737)
		"fc00::/7",
		"::1/128",
		"2001:db8::/32", // IPv6 documentation (RFC 3849)
	})

	// IPv6 ranges that carry an IPv4 address in a fixed position. A packet to
	// one of these is delivered to the embedded IPv4 host by a translator or
	// relay, so the embedded address is judged in place of the IPv6 one.
	nat64Network     = mustParseCIDR("64:ff9b::/96") // RFC 6052 well-known prefix
	sixToFourNetwork = mustParseCIDR("2002::/16")    // RFC 3056 6to4
	ipv4CompatNet    = mustParseCIDR("::/96")        // deprecated IPv4-compatible
)

func mustParseCIDR(cidr string) *net.IPNet {
	return mustParseCIDRs([]string{cidr})[0]
}

func mustParseCIDRs(cidrs []string) []*net.IPNet {
	networks := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			panic("invalid built-in SSRF network " + cidr)
		}
		networks = append(networks, network)
	}
	return networks
}

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

	// Only allow web-scheme protocols to prevent internal service probing
	switch u.Scheme {
	case "http", "https", "ws", "wss":
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

	// Check if it's an IP address (including decimal/hex/octal notation)
	if ip := v.parseIP(host); ip != nil {
		if v.isBlockedIP(ip) {
			return fmt.Errorf("target IP %q is blocked%s", ip, privateHint(ip))
		}
		return nil
	}

	// It's a hostname - resolve it and check all IPs
	addrs, err := net.DefaultResolver.LookupHost(context.Background(), host)
	if err != nil {
		// If we can't resolve, we can't validate - block it
		return fmt.Errorf("cannot resolve hostname %q: %w", host, err)
	}

	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip != nil && v.isBlockedIP(ip) {
			return fmt.Errorf("target hostname %q resolves to blocked IP %q%s", host, addr, privateHint(ip))
		}
	}

	return nil
}

// parseIP parses an IP address string, supporting decimal (2130706433),
// hex (0x7F000001), and octal (0177) notations in addition to standard
// dotted-decimal. Returns nil if the input cannot be parsed.
func (v *SSRFValidator) parseIP(host string) net.IP {
	// Try standard parsing first
	if ip := net.ParseIP(host); ip != nil {
		return ip
	}
	// Try decimal: 2130706433 -> 127.0.0.1
	if parsed, err := strconv.ParseUint(host, 10, 32); err == nil {
		return net.IPv4(byte(parsed>>24&0xff), byte(parsed>>16&0xff), byte(parsed>>8&0xff), byte(parsed&0xff))
	}
	// Try hex: 0x7F000001 -> 127.0.0.1
	if len(host) >= 3 && (host[:2] == "0x" || host[:2] == "0X") {
		if parsed, err := strconv.ParseUint(host[2:], 16, 32); err == nil {
			return net.IPv4(byte(parsed>>24&0xff), byte(parsed>>16&0xff), byte(parsed>>8&0xff), byte(parsed&0xff))
		}
	}
	// Try pure octal (no dots): 0177 -> 127.0.0.1
	if parsed, err := strconv.ParseUint(host, 8, 32); err == nil && !strings.Contains(host, ".") {
		return net.IPv4(byte(parsed>>24&0xff), byte(parsed>>16&0xff), byte(parsed>>8&0xff), byte(parsed&0xff))
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

// isBlockedIP checks if an IP address is blocked.
// The AllowPrivate field is set once at construction time via NewSSRFValidator
// (which reads ANUBIS_SSRF_ALLOW_PRIVATE) or explicitly by the caller.
// We do NOT re-read the env var here — it never changes at runtime and
// os.Getenv has non-trivial overhead on every probe check hot path.
//
// An IPv6 address that embeds an IPv4 address (NAT64, 6to4, IPv4-compatible)
// is judged by its embedded address, so 64:ff9b::7f00:1 is treated exactly
// like 127.0.0.1. An AllowedNetworks entry that names the IPv6 address itself
// still allows it, but never past the hard-deny ranges.
func (v *SSRFValidator) isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	// Normalize IPv4-mapped IPv6 addresses before CIDR checks so ::ffff:127.0.0.1
	// cannot bypass IPv4 hard-deny ranges.
	if ipv4 := ip.To4(); ipv4 != nil {
		ip = ipv4
	}
	embedded := embeddedIPv4(ip)
	if isHardBlockedIP(ip) || (embedded != nil && isHardBlockedIP(embedded)) {
		return true
	}
	for _, network := range v.AllowedNetworks {
		if network.Contains(ip) {
			return false
		}
	}
	if embedded != nil {
		return v.isBlockedIP(embedded)
	}
	for _, network := range privateNetworks {
		if network.Contains(ip) {
			return !v.AllowPrivate
		}
	}
	return false
}

// isHardBlockedIP reports whether ip is in a range no policy can allow.
func isHardBlockedIP(ip net.IP) bool {
	for _, network := range hardBlockedNetworks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// embeddedIPv4 returns the IPv4 address carried by a NAT64, 6to4, or
// IPv4-compatible IPv6 address, or nil when ip carries none. ip must already
// be normalized with To4, so IPv4 and IPv4-mapped addresses return nil.
func embeddedIPv4(ip net.IP) net.IP {
	if len(ip) != net.IPv6len {
		return nil
	}
	switch {
	case nat64Network.Contains(ip):
		return net.IPv4(ip[12], ip[13], ip[14], ip[15]).To4()
	case sixToFourNetwork.Contains(ip):
		return net.IPv4(ip[2], ip[3], ip[4], ip[5]).To4()
	case ipv4CompatNet.Contains(ip):
		// :: and ::1 are IPv6 addresses in their own right (unspecified and
		// loopback), not IPv4-compatible encodings of 0.0.0.0/0.0.0.1.
		if ip.Equal(net.IPv6unspecified) || ip.Equal(net.IPv6loopback) {
			return nil
		}
		return net.IPv4(ip[12], ip[13], ip[14], ip[15]).To4()
	}
	return nil
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

	// Check if it's an IP, including alternate numeric representations.
	ip := v.parseIP(host)
	if ip != nil {
		if v.isBlockedIP(ip) {
			return fmt.Errorf("target IP %q is blocked%s", ip, privateHint(ip))
		}
		return nil
	}

	// Resolve hostname
	addrs, err := net.DefaultResolver.LookupHost(context.Background(), host)
	if err != nil {
		return fmt.Errorf("cannot resolve hostname %q: %w", host, err)
	}

	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip != nil && v.isBlockedIP(ip) {
			return fmt.Errorf("target hostname %q resolves to blocked IP %q%s", host, addr, privateHint(ip))
		}
	}

	return nil
}

// DefaultValidator is the default SSRF validator instance.
//
// It is published atomically. Probe checks and alert dispatchers read it on
// every operation, while ConfigureDefaultValidator replaces it: a plain
// variable made that replacement a data race, and a reader could even
// observe a validator that was still being constructed. The validator itself
// is immutable once published, so a reader that loads the pointer always
// sees a complete and consistent policy.
var DefaultValidator = newDefaultValidatorRef()

// newDefaultValidatorRef returns the holder for the process-wide validator,
// already populated with the default policy. It runs during package variable
// initialization, so the validator exists before any init function or check
// can read it.
func newDefaultValidatorRef() *atomic.Pointer[SSRFValidator] {
	ref := &atomic.Pointer[SSRFValidator]{}
	ref.Store(NewSSRFValidator())
	return ref
}

// ValidateTarget is a convenience function using the default validator
func ValidateTarget(target string) error {
	return DefaultValidator.Load().ValidateTarget(target)
}

// ValidateAddress is a convenience function using the default validator
func ValidateAddress(address string) error {
	return DefaultValidator.Load().ValidateAddress(address)
}

// privateHint tells the operator how to allow a target that was blocked only
// because it is in a private range. Hard-blocked ranges get no hint.
func privateHint(ip net.IP) string {
	if ipv4 := ip.To4(); ipv4 != nil {
		ip = ipv4
	}
	embedded := embeddedIPv4(ip)
	if isHardBlockedIP(ip) || (embedded != nil && isHardBlockedIP(embedded)) {
		return ""
	}
	if embedded != nil {
		ip = embedded
	}
	for _, network := range privateNetworks {
		if network.Contains(ip) {
			return " (private address; set security.allow_private_targets: true or list it in security.allowed_networks to monitor internal services)"
		}
	}
	return ""
}

// ConfigureDefaultValidator applies the operator's SSRF policy to the
// process-wide validator used by probes and alert dispatchers. It must run
// before probes start. allowPrivate is OR-ed with ANUBIS_SSRF_ALLOW_PRIVATE.
func ConfigureDefaultValidator(allowPrivate bool, allowedCIDRs []string) error {
	v := NewSSRFValidator()
	v.AllowPrivate = v.AllowPrivate || allowPrivate
	for _, cidr := range allowedCIDRs {
		_, network, err := net.ParseCIDR(strings.TrimSpace(cidr))
		if err != nil {
			return fmt.Errorf("security.allowed_networks: invalid CIDR %q: %w", cidr, err)
		}
		v.AllowedNetworks = append(v.AllowedNetworks, network)
	}
	DefaultValidator.Store(v)
	return nil
}

// ResetDefaultForTest reinitializes DefaultValidator with the current env vars.
// Test-only: call after setting ANUBIS_SSRF_ALLOW_PRIVATE=1.
func ResetDefaultForTest() {
	DefaultValidator.Store(NewSSRFValidator())
}

// bindConnDeadline bounds connection I/O by both the configured timeout and
// caller cancellation. SetDeadline alone does not react to cancellation when
// the context has no deadline, so context.AfterFunc forces blocked I/O awake.
func bindConnDeadline(ctx context.Context, conn net.Conn, timeout time.Duration) (func(), error) {
	if err := ctx.Err(); err != nil {
		return func() {}, err
	}
	deadline := time.Now().Add(timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return func() {}, err
	}
	stop := context.AfterFunc(ctx, func() {
		_ = conn.SetDeadline(time.Now())
	})
	return func() { stop() }, nil
}

// DialContextFunc is a function that dials a network address.
type DialContextFunc = func(network, addr string) (net.Conn, error)

// WrapDialer wraps a dial function with SSRF-aware DNS rebinding protection.
// It re-resolves the hostname and validates the IP against the blocklist
// immediately before each connection attempt, preventing TOCTOU DNS rebinding attacks.
func (v *SSRFValidator) WrapDialer(dial func(network, addr string) (net.Conn, error)) func(network, addr string) (net.Conn, error) {
	return func(network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
		}

		// If it's a literal IP (including decimal/hex), check directly
		if ip := v.parseIP(host); ip != nil {
			if v.isBlockedIP(ip) {
				return nil, fmt.Errorf("SSRF: target IP %q is blocked%s", ip, privateHint(ip))
			}
			return dial(network, addr)
		}

		// Re-resolve hostname, validate every answer, and pin the connection to
		// the first validated literal IP. Passing the hostname back to dial would
		// trigger a second, attacker-controlled DNS lookup.
		addrs, err := net.DefaultResolver.LookupHost(context.Background(), host)
		if err != nil {
			return nil, fmt.Errorf("SSRF: cannot resolve hostname %q: %w", host, err)
		}
		var pinned net.IP
		for _, resolved := range addrs {
			ip := v.parseIP(resolved)
			if ip == nil {
				return nil, fmt.Errorf("SSRF: hostname %q resolved to unparseable address %q", host, resolved)
			}
			if v.isBlockedIP(ip) {
				return nil, fmt.Errorf("SSRF: hostname %q resolves to blocked IP %q%s", host, resolved, privateHint(ip))
			}
			if pinned == nil {
				pinned = ip
			}
		}
		if pinned == nil {
			return nil, fmt.Errorf("SSRF: hostname %q resolved to no addresses", host)
		}
		pinnedAddr := pinned.String()
		if _, port, splitErr := net.SplitHostPort(addr); splitErr == nil {
			pinnedAddr = net.JoinHostPort(pinned.String(), port)
		}
		return dial(network, pinnedAddr)
	}
}

// WrapDialerContext wraps net.DialContext with SSRF protection.
func (v *SSRFValidator) WrapDialerContext(dial func(ctx context.Context, network, addr string) (net.Conn, error)) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		hasPort := err == nil
		if !hasPort {
			host = addr
		}

		if ip := v.parseIP(host); ip != nil {
			if v.isBlockedIP(ip) {
				return nil, fmt.Errorf("SSRF: target IP %q is blocked%s", ip, privateHint(ip))
			}
			// Dial the canonical form: a decimal/hex/octal spelling such as
			// "2130706433" is not an IP literal to net.Dial, which would hand
			// it to the resolver instead of connecting to the validated IP.
			if hasPort {
				return dial(ctx, network, net.JoinHostPort(ip.String(), port))
			}
			return dial(ctx, network, ip.String())
		}

		addrs, err := net.DefaultResolver.LookupHost(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("SSRF: cannot resolve hostname %q: %w", host, err)
		}
		// Validate every resolved address and pin the dial to a validated IP.
		// Dialing the literal IP (rather than re-passing the hostname, which
		// would trigger a second, unvalidated resolution) closes the
		// DNS-rebinding TOCTOU window between validation and connection.
		var pinned net.IP
		for _, resolved := range addrs {
			ip := v.parseIP(resolved)
			if ip == nil {
				return nil, fmt.Errorf("SSRF: hostname %q resolved to unparseable address %q", host, resolved)
			}
			if v.isBlockedIP(ip) {
				return nil, fmt.Errorf("SSRF: hostname %q resolves to blocked IP %q%s", host, resolved, privateHint(ip))
			}
			if pinned == nil {
				pinned = ip
			}
		}
		if pinned == nil {
			return nil, fmt.Errorf("SSRF: hostname %q resolved to no addresses", host)
		}

		pinnedAddr := pinned.String()
		if hasPort {
			pinnedAddr = net.JoinHostPort(pinned.String(), port)
		}
		return dial(ctx, network, pinnedAddr)
	}
}

// SafeDialContext returns a dial function that enforces the process-wide SSRF
// policy at connection time. Each dial loads the current DefaultValidator, so
// a long-lived client (alert dispatchers share one) follows a policy that is
// reconfigured after the client was built. Hostnames are resolved once,
// validated, and dialed by the pinned literal IP, which closes the DNS
// rebinding window between validation and connection.
func SafeDialContext(dialer *net.Dialer) func(ctx context.Context, network, addr string) (net.Conn, error) {
	if dialer == nil {
		dialer = &net.Dialer{}
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return DefaultValidator.Load().WrapDialerContext(dialer.DialContext)(ctx, network, addr)
	}
}
