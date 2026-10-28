package probe

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func websocketUpgradeValueServer(t *testing.T, upgradeValue string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		req, readErr := http.ReadRequest(bufio.NewReader(conn))
		if readErr != nil {
			return
		}
		accept := calculateWebSocketAccept(req.Header.Get("Sec-WebSocket-Key"))
		fmt.Fprintf(conn, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: %s\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", upgradeValue, accept)
		time.Sleep(200 * time.Millisecond)
	}()
	return "ws://" + listener.Addr().String()
}

// RFC 6455 §4.2.2: the Upgrade header value "websocket" is an ASCII
// case-insensitive value, so "WebSocket" is a valid accepted handshake.
func TestWebSocketUpgradeHeaderIsCaseInsensitive(t *testing.T) {
	publishValidatorForTest(t, permissiveSSRFValidator())
	for _, tc := range []struct{ name, value string }{
		{"lowercase control", "websocket"},
		{"mixed case", "WebSocket"},
		{"uppercase", "WEBSOCKET"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j, _ := NewWebSocketChecker().Judge(context.Background(), &core.Soul{
				ID: "ws-case", Type: core.CheckWebSocket, Target: websocketUpgradeValueServer(t, tc.value),
				Timeout: core.Duration{Duration: 3 * time.Second},
			})
			if j.Status != core.SoulAlive {
				t.Fatalf("Upgrade: %s rejected: status=%s msg=%q", tc.value, j.Status, j.Message)
			}
		})
	}
	t.Run("wrong protocol still rejected", func(t *testing.T) {
		j, _ := NewWebSocketChecker().Judge(context.Background(), &core.Soul{
			ID: "ws-case", Type: core.CheckWebSocket, Target: websocketUpgradeValueServer(t, "h2c"),
			Timeout: core.Duration{Duration: 3 * time.Second},
		})
		if j.Status != core.SoulDead || !strings.Contains(j.Message, "did not accept") {
			t.Fatalf("status=%s msg=%q", j.Status, j.Message)
		}
	})
}
