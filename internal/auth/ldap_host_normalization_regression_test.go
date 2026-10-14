package auth

import (
	"net"
	"testing"
)

// TestExtractHostnameFromURLNormalizesIPv6Literals pins that an LDAP URL is
// reduced to a bare host.
//
// ldapLogin hands the result straight to tls.Config.ServerName for StartTLS.
// crypto/tls only takes the IP-verification path (and omits SNI) when the value
// parses as an IP literal, so a bracketed or truncated literal is treated as a
// DNS name that no server certificate can satisfy and every bind fails.
//
// The same normalisation is pinned for status-page hosts in
// internal/statuspage/host_ipv6_regression_test.go.
func TestExtractHostnameFromURLNormalizesIPv6Literals(t *testing.T) {
	cases := []struct {
		name   string
		url    string
		want   string
		wantIP bool
	}{
		// Regression: bracketed IPv6 literals.
		{"ipv6 with port", "ldap://[::1]:389", "::1", true},
		{"ipv6 with port and base dn", "ldap://[2001:db8::1]:389/dc=example,dc=com", "2001:db8::1", true},
		{"ipv6 without port", "ldap://[::1]", "::1", true},
		{"ipv6 ldaps with port", "ldaps://[2001:db8::1]:636", "2001:db8::1", true},
		{"ipv6 without brackets", "ldap://::1", "::1", true},

		// Controls: the previously working forms must be unaffected.
		{"dns with port and base dn", "ldap://ldap.example.com:389/dc=example,dc=com", "ldap.example.com", false},
		{"dns without port", "ldap://ldap.example.com", "ldap.example.com", false},
		{"ldaps without port", "ldaps://ldap.example.com", "ldap.example.com", false},
		{"ipv4", "ldap://10.0.0.1:389", "10.0.0.1", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractHostnameFromURL(tc.url)
			if got != tc.want {
				t.Fatalf("extractHostnameFromURL(%q) = %q, want %q", tc.url, got, tc.want)
			}
			isIP := net.ParseIP(got) != nil
			if tc.wantIP && !isIP {
				t.Fatalf("extracted %q is not an IP literal, so crypto/tls would verify it "+
					"as a DNS name and certificate verification would fail", got)
			}
			if !tc.wantIP && isIP {
				t.Fatalf("extracted %q unexpectedly parses as an IP literal", got)
			}
		})
	}
}
