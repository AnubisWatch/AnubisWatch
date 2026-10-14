package statuspage

import (
	"net/http/httptest"
	"testing"
)

func auditStatusHost(t *testing.T, host, want string) {
	t.Helper()
	got := normalizeHost(host)
	t.Logf("host=%q EXPECTED: normalized=%q ACTUAL: normalized=%q", host, want, got)
	if got != want {
		t.Fatal("regression mismatch")
	}
	h := NewHandler(&domainOnlyRepository{domain: want})
	if !h.CanServeHost(host) {
		t.Fatal("host not recognized")
	}
	r := httptest.NewRequest("GET", "http://local/?format=json", nil)
	r.Host = host
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("expected status page 200 got %d", w.Code)
	}
}
func TestAuditStatusIPv6Host(t *testing.T) {
	t.Run("dns-port-control", func(t *testing.T) { auditStatusHost(t, "STATUS.example.com:443", "status.example.com") })
	t.Run("bracketed-no-port", func(t *testing.T) { auditStatusHost(t, "[::1]", "::1") })
	t.Run("ipv6-port", func(t *testing.T) { auditStatusHost(t, "[::1]:8443", "::1") })
	t.Run("bare-ipv6", func(t *testing.T) { auditStatusHost(t, "::1", "::1") })
	t.Run("ipv6-case", func(t *testing.T) { auditStatusHost(t, "[2001:DB8::1]", "2001:db8::1") })
	t.Run("ipv4-control", func(t *testing.T) { auditStatusHost(t, "127.0.0.1:8443", "127.0.0.1") })
	t.Run("dns-no-port-control", func(t *testing.T) { auditStatusHost(t, " STATUS.example.com ", "status.example.com") })
}
