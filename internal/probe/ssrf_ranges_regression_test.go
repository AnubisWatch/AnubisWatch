package probe

import (
	"context"
	"net"
	"strings"
	"testing"
)

// The block list missed several non-public ranges: CGNAT, benchmarking,
// IETF/documentation space, and IPv6 encodings that carry an IPv4 address
// (NAT64, 6to4, IPv4-compatible). The encodings let 64:ff9b::a9fe:a9fe reach
// 169.254.169.254 through a NAT64 gateway even though the IPv4 form was
// hard-blocked.
func TestSSRFValidatorBlocksReservedAndEmbeddedRanges(t *testing.T) {
	tests := []struct {
		ip string
		// blockedStrict is the result under the default policy, and
		// blockedPrivate the result with allow_private_targets enabled.
		blockedStrict  bool
		blockedPrivate bool
	}{
		// Non-public IPv4 ranges: blocked by default, allowed by opt-in.
		{"100.64.0.1", true, false},
		{"100.127.255.254", true, false},
		{"198.18.0.1", true, false},
		{"198.19.255.255", true, false},
		{"192.0.0.1", true, false},
		{"192.0.2.10", true, false},
		{"198.51.100.10", true, false},
		{"203.0.113.10", true, false},
		{"2001:db8::1", true, false},
		// Alibaba metadata sits inside CGNAT space and stays hard-blocked.
		{"100.100.100.200", true, true},
		// Multicast and broadcast.
		{"224.0.0.1", true, true},
		{"255.255.255.255", true, true},
		{"ff02::1", true, true},
		// AWS IMDS over IPv6 is metadata: never reachable.
		{"fd00:ec2::254", true, true},
		// NAT64 well-known prefix: judged by the embedded IPv4 address.
		{"64:ff9b::7f00:1", true, false},       // 127.0.0.1
		{"64:ff9b::a00:1", true, false},        // 10.0.0.1
		{"64:ff9b::a9fe:a9fe", true, true},     // 169.254.169.254
		{"64:ff9b::808:808", false, false},     // 8.8.8.8
		{"64:ff9b:1::7f00:1", true, true},      // local-use NAT64, ambiguous
		{"2002:7f00:1::1", true, false},        // 6to4 of 127.0.0.1
		{"2002:a9fe:a9fe::1", true, true},      // 6to4 of 169.254.169.254
		{"2002:808:808::1", false, false},      // 6to4 of 8.8.8.8
		{"::7f00:1", true, false},              // IPv4-compatible 127.0.0.1
		{"::a9fe:a9fe", true, true},            // IPv4-compatible 169.254.169.254
		{"::808:808", false, false},            // IPv4-compatible 8.8.8.8
		{"2001:0:4136:e378::1", true, true},    // Teredo
		{"::ffff:100.64.0.1", true, false},     // IPv4-mapped CGNAT
		{"::1", true, false},                   // loopback, not 0.0.0.1
		{"8.8.8.8", false, false},              // control
		{"2606:4700:4700::1111", false, false}, // control
	}
	strict := strictSSRFValidator()
	permissive := permissiveSSRFValidator()
	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("bad test IP %q", tt.ip)
			}
			if got := strict.isBlockedIP(ip); got != tt.blockedStrict {
				t.Errorf("default policy: blocked = %v, want %v", got, tt.blockedStrict)
			}
			if got := permissive.isBlockedIP(ip); got != tt.blockedPrivate {
				t.Errorf("allow_private_targets: blocked = %v, want %v", got, tt.blockedPrivate)
			}
		})
	}
}

// An explicit allowed_networks entry still opens a private range, including
// through an embedded IPv4 encoding, but never a hard-blocked one.
func TestSSRFAllowedNetworksWithEmbeddedAddresses(t *testing.T) {
	tests := []struct {
		allowed string
		ip      string
		blocked bool
	}{
		{"10.20.0.0/16", "10.20.1.1", false},
		{"10.20.0.0/16", "64:ff9b::a14:101", false}, // NAT64 of 10.20.1.1
		{"10.20.0.0/16", "10.21.1.1", true},
		{"100.64.0.0/10", "100.64.1.1", false},
		{"64:ff9b::/96", "64:ff9b::a14:101", false},
		{"64:ff9b::/96", "64:ff9b::a9fe:a9fe", true}, // metadata stays hard-blocked
		{"fd00::/8", "fd00:ec2::254", true},
		{"fd00::/8", "fd00::1", false},
	}
	for _, tt := range tests {
		t.Run(tt.allowed+"/"+tt.ip, func(t *testing.T) {
			_, network, err := net.ParseCIDR(tt.allowed)
			if err != nil {
				t.Fatal(err)
			}
			v := strictSSRFValidator()
			v.AllowedNetworks = []*net.IPNet{network}
			if got := v.isBlockedIP(net.ParseIP(tt.ip)); got != tt.blocked {
				t.Fatalf("blocked = %v, want %v", got, tt.blocked)
			}
		})
	}
}

func TestSSRFPrivateHintForEmbeddedAddresses(t *testing.T) {
	if privateHint(net.ParseIP("64:ff9b::7f00:1")) == "" {
		t.Error("NAT64 loopback should carry the private-target hint")
	}
	if privateHint(net.ParseIP("64:ff9b::a9fe:a9fe")) != "" {
		t.Error("NAT64 metadata is hard-blocked and must not suggest a setting")
	}
	if privateHint(net.ParseIP("fd00:ec2::254")) != "" {
		t.Error("AWS IPv6 metadata is hard-blocked and must not suggest a setting")
	}
}

// The dial wrapper validated a decimal/hex spelling of an IP and then passed
// the original spelling to net.Dial, which treats it as a host name to
// resolve rather than the validated address.
func TestWrapDialerContextDialsCanonicalIP(t *testing.T) {
	v := permissiveSSRFValidator()
	var dialed string
	dial := v.WrapDialerContext(func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialed = addr
		return nil, net.ErrClosed
	})
	_, _ = dial(context.Background(), "tcp", "2130706433:25")
	if dialed != "127.0.0.1:25" {
		t.Fatalf("dialed %q, want %q", dialed, "127.0.0.1:25")
	}

	strict := strictSSRFValidator()
	_, err := strict.WrapDialerContext(func(ctx context.Context, network, addr string) (net.Conn, error) {
		t.Fatalf("blocked address %q was dialed", addr)
		return nil, nil
	})(context.Background(), "tcp", "[64:ff9b::a9fe:a9fe]:80")
	if err == nil || !strings.Contains(err.Error(), "SSRF") {
		t.Fatalf("expected SSRF refusal for NAT64 metadata, got %v", err)
	}
}

// SafeDialContext reads the published policy on every dial, so a client
// built before the policy changed still follows the new policy.
func TestSafeDialContextFollowsPublishedPolicy(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	dial := SafeDialContext(nil)

	publishValidatorForTest(t, permissiveSSRFValidator())
	conn, err := dial(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("permissive policy refused loopback: %v", err)
	}
	conn.Close()

	publishValidatorForTest(t, strictSSRFValidator())
	if _, err := dial(context.Background(), "tcp", ln.Addr().String()); err == nil {
		t.Fatal("strict policy published after the dialer was built still allowed loopback")
	}
}
