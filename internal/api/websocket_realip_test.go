package api

import (
	"net/http"
	"testing"
)

// TestWebSocketRealIP verifies the WebSocket client-IP derivation trusts
// X-Forwarded-For only from configured proxies, so it cannot be spoofed to
// bypass per-IP connection/rate limits.
func TestWebSocketRealIP(t *testing.T) {
	newReq := func(remoteAddr, xff string) *http.Request {
		r := &http.Request{RemoteAddr: remoteAddr, Header: http.Header{}}
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		return r
	}

	cases := []struct {
		name    string
		proxies []string
		remote  string
		xff     string
		want    string
	}{
		{"no proxies ignores XFF", nil, "203.0.113.7:5555", "1.2.3.4", "203.0.113.7"},
		{"untrusted remote ignores XFF", []string{"10.0.0.1"}, "203.0.113.7:5555", "1.2.3.4", "203.0.113.7"},
		{"trusted proxy honors XFF", []string{"10.0.0.1"}, "10.0.0.1:5555", "1.2.3.4", "1.2.3.4"},
		{"trusted proxy, no XFF falls back", []string{"10.0.0.1"}, "10.0.0.1:5555", "", "10.0.0.1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &WebSocketServer{trustedProxies: tc.proxies}
			if got := s.realIP(newReq(tc.remote, tc.xff)); got != tc.want {
				t.Fatalf("realIP(remote=%q xff=%q proxies=%v) = %q, want %q",
					tc.remote, tc.xff, tc.proxies, got, tc.want)
			}
		})
	}
}
