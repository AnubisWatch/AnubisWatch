package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newRateBoundaryServer(t *testing.T) *RESTServer {
	t.Helper()
	s := &RESTServer{router: &Router{routes: make(map[string]map[string]Handler)}, logger: newTestLogger(), rateLimitStopCh: make(chan struct{})}
	s.router.Use(s.rateLimitMiddleware)
	for _, p := range []string{"/ordinary", "/another", "/api/v1/auth/me", "/api/v1/auth/methods", "/api/v1/auth/login", "/api/v1/auth/reset-password"} {
		s.router.Handle(http.MethodGet, p, sharedQuotaRoute)
		s.router.Handle(http.MethodPost, p, sharedQuotaRoute)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	return s
}
func rateBoundaryRequest(s *RESTServer, method, path, remote string) int {
	r := httptest.NewRequest(method, path, nil)
	r.RemoteAddr = remote
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, r)
	return w.Code
}
func TestRateLimitSeparatesCredentialBudgetFromOrdinaryTraffic(t *testing.T) {
	s := newRateBoundaryServer(t)
	for i := 0; i < 25; i++ {
		if got := rateBoundaryRequest(s, "GET", "/ordinary", "198.51.100.10:1"); got != 200 {
			t.Fatalf("control ordinary request: %d", got)
		}
	}
	for _, p := range []string{"/api/v1/auth/me", "/api/v1/auth/methods", "/api/v1/auth/login"} {
		method := "GET"
		if p == "/api/v1/auth/login" {
			method = "POST"
		}
		if got := rateBoundaryRequest(s, method, p, "198.51.100.10:1"); got != 200 {
			t.Errorf("EXPECTED: ordinary traffic does not consume credential budget for %s; ACTUAL: %d; PROBLEM CONFIRMED", p, got)
		}
	}
}
func TestRateLimitNormalizesClientPorts(t *testing.T) {
	s := newRateBoundaryServer(t)
	for i := 0; i < 100; i++ {
		if got := rateBoundaryRequest(s, "GET", "/ordinary", "198.51.100.10:1"); got != 200 {
			t.Fatalf("control: request %d status %d", i, got)
		}
	}
	if got := rateBoundaryRequest(s, "GET", "/another", "198.51.100.10:2"); got != 429 {
		t.Errorf("EXPECTED: one IP quota across connection ports and routes; ACTUAL: %d; PROBLEM CONFIRMED", got)
	}
	if got := rateBoundaryRequest(s, "GET", "/ordinary", "198.51.100.11:2"); got != 200 {
		t.Errorf("unaffected other IP: %d", got)
	}
}
func TestRateLimitCredentialBudgetSharedAcrossCredentialRoutes(t *testing.T) {
	s := newRateBoundaryServer(t)
	for i := 0; i < 10; i++ {
		if got := rateBoundaryRequest(s, "POST", "/api/v1/auth/login", "198.51.100.10:1"); got != 200 {
			t.Fatalf("control credential request %d: %d", i, got)
		}
	}
	if got := rateBoundaryRequest(s, "POST", "/api/v1/auth/reset-password", "198.51.100.10:1"); got != 429 {
		t.Errorf("credential route bypassed shared quota: %d", got)
	}
	if got := rateBoundaryRequest(s, "GET", "/api/v1/auth/me", "198.51.100.10:1"); got != 200 {
		t.Errorf("EXPECTED: read-only session lookup uses ordinary quota; ACTUAL: %d; PROBLEM CONFIRMED", got)
	}
}

func TestRateLimitTrustedProxyAndIPv6Identity(t *testing.T) {
	for _, tc := range []struct {
		name, remote, forwarded, want string
		trusted                       []string
	}{
		{name: "untrusted header", remote: "198.51.100.10:1234", forwarded: "203.0.113.1", want: "198.51.100.10"},
		{name: "IPv6 source", remote: "[2001:db8::1]:1234", want: "2001:db8::1"},
		{name: "trusted forwarding", remote: "192.0.2.1:1234", forwarded: " 203.0.113.1 , 192.0.2.1", trusted: []string{"192.0.2.1"}, want: "203.0.113.1"},
		{name: "invalid forwarding", remote: "192.0.2.1:1234", forwarded: "invalid", trusted: []string{"192.0.2.1"}, want: "192.0.2.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &RESTServer{logger: newTestLogger(), trustedProxies: tc.trusted}
			s.rateLimitMiddleware(sharedQuotaRoute)
			t.Cleanup(func() { _ = s.Stop(context.Background()) })
			req := httptest.NewRequest("GET", "/ordinary", nil)
			req.RemoteAddr = tc.remote
			req.Header.Set("X-Forwarded-For", tc.forwarded)
			if got := s.realIP(req); got != tc.want {
				t.Fatalf("realIP=%q want %q", got, tc.want)
			}
		})
	}
}
