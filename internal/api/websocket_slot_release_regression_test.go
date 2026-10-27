package api

// Regression test: a WebSocket handshake that is rejected after admission
// (bad token, failed upgrade) must release its reserved per-IP connection
// slot. HandleConnection reserves the slot before validating the token; the
// release is owned by removeClient, which only registered clients reach.
// Leaking the slot let unauthenticated attempts permanently lock an IP out
// of WebSocket connections with 429s (maxConnsPerIP phantom connections).

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func wsAdmissionRequest(t *testing.T, s *WebSocketServer, ip, token string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", "/api/v1/ws", nil)
	r.RemoteAddr = ip + ":54321"
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Connection", "Upgrade")
	r.Header.Set("Sec-WebSocket-Version", "13")
	r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	s.HandleConnection(w, r)
	return w
}

func TestWebSocketRejectedHandshakeReleasesConnectionSlot(t *testing.T) {
	logger := newTestLogger()
	s := NewWebSocketServer(logger, &mockAuthenticator{}, nil)
	// Make the per-IP concurrent limit the binding constraint; the connect
	// rate stays far above the attempt count so only a slot leak can cause
	// the final lockout.
	s.maxConnsPerIP = 2
	s.connRateLimit = 100

	const victimIP = "10.1.1.1"

	// Control: an unrelated IP behaves normally from the start.
	if w := wsAdmissionRequest(t, s, "10.9.9.9", "no-such-token"); w.Code != http.StatusUnauthorized {
		t.Fatalf("control request expected 401, got %d", w.Code)
	}

	// maxConnsPerIP rejected handshakes from one IP. Each consumed no
	// connection, so none may still be counted afterwards.
	for i := 0; i < 2; i++ {
		if w := wsAdmissionRequest(t, s, victimIP, "no-such-token"); w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d expected 401, got %d", i+1, w.Code)
		}
	}

	s.mu.RLock()
	limiter := s.ipLimits[victimIP]
	conns := 0
	if limiter != nil {
		conns = limiter.connections
	}
	s.mu.RUnlock()
	if conns != 0 {
		t.Fatalf("%d phantom concurrent connections held after rejected handshakes (expected 0)", conns)
	}

	// The IP must still be admitted: the next attempt gets the normal auth
	// rejection, not a rate-limit lockout.
	if w := wsAdmissionRequest(t, s, victimIP, "no-such-token"); w.Code == http.StatusTooManyRequests {
		t.Fatalf("IP locked out with 429 despite zero live connections: %s", w.Body.String())
	}

	// Control: an unrelated IP still sees the normal path.
	if w := wsAdmissionRequest(t, s, "10.8.8.8", "no-such-token"); w.Code != http.StatusUnauthorized {
		t.Fatalf("second control expected 401, got %d", w.Code)
	}
}
