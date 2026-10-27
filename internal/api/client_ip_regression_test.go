package api

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// The client address used to trust the leftmost X-Forwarded-For entry, which
// the client itself writes: a proxy appends to the header, it does not
// replace it. Behind a trusted proxy every request could claim a fresh
// address and escape every per-IP limit.
func TestResolveClientIPWalksForwardedForRightToLeft(t *testing.T) {
	for _, tc := range []struct {
		name    string
		trusted []string
		remote  string
		xff     []string
		want    string
	}{
		{name: "no trusted proxies ignores XFF", remote: "203.0.113.7:1", xff: []string{"1.2.3.4"}, want: "203.0.113.7"},
		{name: "untrusted peer ignores XFF", trusted: []string{"10.0.0.1"}, remote: "203.0.113.7:1", xff: []string{"1.2.3.4"}, want: "203.0.113.7"},
		{name: "single trusted hop", trusted: []string{"10.0.0.1"}, remote: "10.0.0.1:1", xff: []string{"203.0.113.9"}, want: "203.0.113.9"},
		{name: "client-prepended entries are ignored", trusted: []string{"10.0.0.1"}, remote: "10.0.0.1:1", xff: []string{"198.51.100.1, 198.51.100.2, 203.0.113.9"}, want: "203.0.113.9"},
		{name: "chain of trusted proxies", trusted: []string{"10.0.0.0/8"}, remote: "10.0.0.1:1", xff: []string{"6.6.6.6, 203.0.113.9, 10.1.1.1, 10.2.2.2"}, want: "203.0.113.9"},
		{name: "multiple header lines", trusted: []string{"10.0.0.1", "10.0.0.2"}, remote: "10.0.0.1:1", xff: []string{"6.6.6.6", "203.0.113.9, 10.0.0.2"}, want: "203.0.113.9"},
		{name: "malformed hop stops the walk", trusted: []string{"10.0.0.1", "10.0.0.2"}, remote: "10.0.0.1:1", xff: []string{"203.0.113.9, garbage, 10.0.0.2"}, want: "10.0.0.2"},
		{name: "all hops trusted", trusted: []string{"10.0.0.0/8"}, remote: "10.0.0.1:1", xff: []string{"10.3.3.3, 10.2.2.2"}, want: "10.3.3.3"},
		{name: "trusted peer without XFF", trusted: []string{"10.0.0.1"}, remote: "10.0.0.1:1", want: "10.0.0.1"},
		{name: "IPv6 peer and CIDR", trusted: []string{"2001:db8::/32"}, remote: "[2001:db8::1]:1", xff: []string{"2001:db8:ffff::9, 2001:db8::2"}, want: "2001:db8:ffff::9"},
		{name: "invalid trusted entries are ignored", trusted: []string{"not-an-ip", " "}, remote: "10.0.0.1:1", xff: []string{"1.2.3.4"}, want: "10.0.0.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tc.remote
			for _, v := range tc.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := resolveClientIP(r, newTrustedProxyMatcher(tc.trusted)); got != tc.want {
				t.Fatalf("resolveClientIP = %q, want %q", got, tc.want)
			}
			// REST and WebSocket must agree on who the client is.
			rest := &RESTServer{trustedProxies: tc.trusted}
			ws := &WebSocketServer{trustedProxies: tc.trusted}
			if rest.realIP(r) != tc.want || ws.realIP(r) != tc.want {
				t.Fatalf("REST %q / WebSocket %q disagree with %q", rest.realIP(r), ws.realIP(r), tc.want)
			}
		})
	}
}

func TestRequestIsHTTPSTrustsForwardedProtoOnlyFromProxies(t *testing.T) {
	trusted := newTrustedProxyMatcher([]string{"10.0.0.1"})
	for _, tc := range []struct {
		name, remote, proto string
		tls                 bool
		want                bool
	}{
		{name: "TLS", remote: "198.51.100.1:1", tls: true, want: true},
		{name: "plain HTTP", remote: "198.51.100.1:1", want: false},
		{name: "forged proto from client", remote: "198.51.100.1:1", proto: "https", want: false},
		{name: "proto from trusted proxy", remote: "10.0.0.1:1", proto: "https", want: true},
		{name: "proxy says http", remote: "10.0.0.1:1", proto: "http", want: false},
		{name: "proxy list, first hop https", remote: "10.0.0.1:1", proto: "HTTPS, http", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tc.remote
			if tc.proto != "" {
				r.Header.Set("X-Forwarded-Proto", tc.proto)
			}
			if tc.tls {
				r.TLS = &tls.ConnectionState{}
			}
			if got := requestIsHTTPS(r, trusted); got != tc.want {
				t.Fatalf("requestIsHTTPS = %v, want %v", got, tc.want)
			}
		})
	}
}

// Promoted from the audit proof: one real client behind a trusted proxy
// rotating the leftmost X-Forwarded-For entry used to get all 50 login
// attempts through the 10/minute credential limit.
func TestLoginRateLimitCannotBeEvadedWithForgedForwardedFor(t *testing.T) {
	cfg := core.ServerConfig{Host: "127.0.0.1", Port: 8080, TrustedProxies: []string{"10.0.0.1"}}
	s := NewRESTServer(cfg, core.AuthConfig{Enabled: core.BoolPtr(true)}, newMockStorage(), &mockProbeEngine{}, &mockAlertManager{}, &mockAuthenticator{}, &mockClusterManager{}, nil, nil, nil, nil, newTestLogger())
	defer s.Stop(context.Background())

	attempt := func(forwarded string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"email":"a@b.c","password":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "10.0.0.1:5555"
		req.Header.Set("X-Forwarded-For", forwarded)
		rec := httptest.NewRecorder()
		s.router.ServeHTTP(rec, req)
		return rec.Code
	}

	limited := 0
	for i := 0; i < 50; i++ {
		// The proxy appends the real client (203.0.113.9); the client controls
		// everything to its left.
		if attempt(fmt.Sprintf("198.51.100.%d, 203.0.113.9", i)) == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited != 50-rateLimitAuth {
		t.Fatalf("%d of 50 attempts rate limited, want %d", limited, 50-rateLimitAuth)
	}
	// Control: another real client behind the same proxy has its own budget.
	if code := attempt("203.0.113.10"); code == http.StatusTooManyRequests {
		t.Fatal("a different client behind the proxy was rate limited")
	}
}

// Promoted from the audit proof: the dashboard SPA, its login route, status
// pages and the 404 fallback were served outside the middleware chain, with
// no CSP, X-Frame-Options or nosniff.
func TestFallbackResponsesCarrySecurityHeaders(t *testing.T) {
	dash := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>dash</html>")) })
	sp := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>status</html>")) })
	s := NewRESTServer(core.ServerConfig{Host: "127.0.0.1", Port: 8080}, core.AuthConfig{Enabled: core.BoolPtr(true)}, newMockStorage(), &mockProbeEngine{}, &mockAlertManager{}, &mockAuthenticator{}, &mockClusterManager{}, nil, dash, sp, nil, newTestLogger())
	defer s.Stop(context.Background())

	for _, tc := range []struct {
		path, frame string
		code        int
	}{
		{"/", "DENY", http.StatusOK},
		{"/login", "DENY", http.StatusOK},
		{"/souls/abc", "DENY", http.StatusOK},
		{"/assets/index.js", "DENY", http.StatusOK},
		{"/status/public-page", "SAMEORIGIN", http.StatusOK},
		{"/api/v1/no-such-route", "DENY", http.StatusNotFound},
		{"/api/v1/souls", "DENY", http.StatusUnauthorized},
	} {
		t.Run(tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			h := rec.Header()
			if rec.Code != tc.code {
				t.Fatalf("status %d, want %d", rec.Code, tc.code)
			}
			if got := h.Get("X-Frame-Options"); got != tc.frame {
				t.Errorf("X-Frame-Options = %q, want %q", got, tc.frame)
			}
			if got := h.Get("Content-Security-Policy"); got != defaultContentSecurityPolicy {
				t.Errorf("Content-Security-Policy = %q", got)
			}
			if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Referrer-Policy") == "" {
				t.Errorf("missing nosniff/Referrer-Policy: %v", h)
			}
		})
	}
}

// The CSP must keep the embedded dashboard working: scripts only from this
// origin and never inline, and the shipped index.html must not need more.
func TestDefaultCSPFitsEmbeddedDashboard(t *testing.T) {
	directives := map[string]string{}
	for _, d := range strings.Split(defaultContentSecurityPolicy, ";") {
		fields := strings.Fields(d)
		if len(fields) > 0 {
			directives[fields[0]] = strings.Join(fields[1:], " ")
		}
	}
	if directives["script-src"] != "'self'" {
		t.Errorf("script-src = %q, want 'self' only", directives["script-src"])
	}
	if directives["object-src"] != "'none'" || directives["base-uri"] != "'self'" {
		t.Errorf("object-src/base-uri = %q/%q", directives["object-src"], directives["base-uri"])
	}

	s := NewRESTServer(core.ServerConfig{}, core.AuthConfig{Enabled: core.BoolPtr(true)}, newMockStorage(), nil, nil, &mockAuthenticator{}, nil, nil,
		http.FileServer(http.Dir("../dashboard/dist")), nil, nil, newTestLogger())
	defer s.Stop(context.Background())
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	html := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(html, `id="root"`) {
		t.Skipf("embedded dashboard not built (status %d)", rec.Code)
	}
	if rec.Header().Get("Content-Security-Policy") != defaultContentSecurityPolicy {
		t.Fatalf("dashboard index served without the CSP")
	}
	lower := strings.ToLower(html)
	for i := 0; ; {
		j := strings.Index(lower[i:], "<script")
		if j < 0 {
			break
		}
		tag := lower[i+j : i+j+strings.Index(lower[i+j:], ">")]
		if !strings.Contains(tag, " src=") {
			t.Errorf("index.html has an inline script, which script-src 'self' blocks: %s", tag)
		}
		i += j + len(tag)
	}
	for _, attr := range []string{" onload=", " onerror=", " onclick=", "javascript:"} {
		if strings.Contains(lower, attr) {
			t.Errorf("index.html uses %q, which the CSP blocks", attr)
		}
	}
}
