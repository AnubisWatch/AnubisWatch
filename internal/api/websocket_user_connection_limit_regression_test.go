package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// The server declares maxConnsPerUser (default 5) next to maxConnsPerIP, but
// only the per-IP limit was ever checked. One account could open any number
// of sockets from distinct IPs (or up to the per-IP cap from each), each with
// its own send buffer, read pump and write pump.
func TestWebSocketEnforcesPerUserConnectionLimit(t *testing.T) {
	s := NewWebSocketServer(newTestLogger(), &mockAuthenticator{}, nil)
	s.Start()
	defer s.Stop()
	s.maxConnsPerUser = 3
	s.maxConnsPerIP = 100 // keep the per-IP limit out of the way
	s.connRateLimit = 1000

	srv := httptest.NewServer(http.HandlerFunc(s.HandleConnection))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	dial := func(token string) (*websocket.Conn, int, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		conn, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
			HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}},
		})
		code := 0
		if resp != nil {
			code = resp.StatusCode
		}
		return conn, code, err
	}

	var open []*websocket.Conn
	defer func() {
		for _, c := range open {
			_ = c.CloseNow()
		}
	}()

	// The first three connections of user-1 are accepted.
	for i := 0; i < 3; i++ {
		conn, code, err := dial("valid-token")
		if err != nil {
			t.Fatalf("connection %d of user-1 rejected: code=%d err=%v", i+1, code, err)
		}
		open = append(open, conn)
	}

	// The fourth is refused with 429.
	if conn, code, err := dial("valid-token"); err == nil {
		_ = conn.CloseNow()
		t.Errorf("4th connection of user-1 accepted (limit 3)")
	} else if code != http.StatusTooManyRequests {
		t.Errorf("4th connection of user-1: status %d, want 429 (err=%v)", code, err)
	}

	// Control: a different user is not affected by user-1's connections.
	conn, code, err := dial("viewer-token")
	if err != nil {
		t.Fatalf("control: user-2 rejected: code=%d err=%v", code, err)
	}
	open = append(open, conn)

	// Closing one of user-1's connections frees exactly one slot.
	_ = open[0].CloseNow()
	deadline := time.Now().Add(3 * time.Second)
	for {
		c, _, err := dial("valid-token")
		if err == nil {
			open = append(open, c)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("slot of a closed connection was never released: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if c, _, err := dial("valid-token"); err == nil {
		_ = c.CloseNow()
		t.Errorf("user-1 exceeded the limit after reconnecting")
	}
}
