package probe

import (
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func tlsURLTargetSoul(target string) *core.Soul {
	return &core.Soul{
		ID:      "tls-url-regression",
		Name:    "TLS URL regression",
		Type:    core.CheckTLS,
		Target:  target,
		Enabled: true,
		TLS:     &core.TLSConfig{},
	}
}

// TLS checks operate on a TCP endpoint. A URL path is valid input metadata but
// must not be retained as part of the port passed to the dialer.
func TestTLSTargetURLDropsHTTPComponentsBeforeDial(t *testing.T) {
	publishValidatorForTest(t, permissiveSSRFValidator())
	checker := NewTLSChecker()

	tests := []struct {
		name   string
		target string
		want   string
	}{
		{name: "raw host port control", target: "127.0.0.1:8443", want: "127.0.0.1:8443"},
		{name: "pathless URL control", target: "https://127.0.0.1:8443", want: "127.0.0.1:8443"},
		{name: "URL path", target: "https://127.0.0.1:8443/health", want: "127.0.0.1:8443"},
		{name: "URL path with default port", target: "https://127.0.0.1/health?ready=true#probe", want: "127.0.0.1:443"},
		{name: "IPv6 URL path", target: "https://[::1]:8443/health", want: "[::1]:8443"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			soul := tlsURLTargetSoul(tc.target)
			if err := soul.Validate(); err != nil {
				t.Fatalf("core validation failed for %q: %v", tc.target, err)
			}
			if err := checker.Validate(soul); err != nil {
				t.Fatalf("checker validation failed for %q: %v", tc.target, err)
			}
			if soul.Target != tc.want {
				t.Fatalf("normalized target = %q, want %q", soul.Target, tc.want)
			}
		})
	}
}
