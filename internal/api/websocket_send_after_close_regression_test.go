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

// removeClient closes client.send (under sendOnce) from whichever goroutine
// drops the client: a broadcast that found its buffer full, or Stop(). The
// read pump is a different goroutine and may still be inside handleMessage,
// which answers with a bare `c.send <- ...`. A send on a closed channel
// panics, and nothing on the read pump recovers, so one slow-consumer
// eviction raced with an incoming ping crashes the whole process.
func TestHandleMessageAfterClientRemovalDoesNotPanic(t *testing.T) {
	s := NewWebSocketServer(newTestLogger(), &mockAuthenticator{}, nil)

	// A bare accept hands the test a real server-side connection without
	// HandleConnection's own welcome send, so the only traffic on a client's
	// send channel is the call under test.
	accepted := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		accepted <- conn
		<-r.Context().Done()
	}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	newClient := func(t *testing.T, id string) *WSClient {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		peer, _, err := websocket.Dial(ctx, wsURL, nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		t.Cleanup(func() { _ = peer.CloseNow() })
		var conn *websocket.Conn
		select {
		case conn = <-accepted:
		case <-ctx.Done():
			t.Fatal("server never accepted the connection")
		}
		clientCtx, clientCancel := context.WithCancel(context.Background())
		c := &WSClient{
			ID:        id,
			Conn:      conn,
			Workspace: "default",
			UserID:    "user-" + id,
			Rooms:     make(map[string]bool),
			send:      make(chan []byte, 8),
			server:    s,
			ctx:       clientCtx,
			cancel:    clientCancel,
		}
		s.mu.Lock()
		s.clients[c.ID] = c
		s.mu.Unlock()
		return c
	}

	// Control: on a live client the pong is queued for the writer.
	live := newClient(t, "live")
	live.handleMessage([]byte(`{"type":"ping"}`))
	select {
	case msg := <-live.send:
		if !strings.Contains(string(msg), `"pong"`) {
			t.Fatalf("control: queued %s, want a pong", msg)
		}
	default:
		t.Fatal("control: no pong queued for a live client")
	}

	// The client is evicted (send closed by removeClient) while its read pump
	// is still handling a message of every kind.
	for i, payload := range []string{
		`{"type":"ping"}`,
		`{"type":"subscribe","events":["judgment"]}`,
		`{"type":"unsubscribe","events":["judgment"]}`,
		`{"type":"join_workspace"}`,
		`{"type":"nope"}`,
		`not json`,
	} {
		c := newClient(t, "evicted-"+string(rune('a'+i)))
		s.removeClient(c.ID)
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("handleMessage(%s) after removal panicked: %v", payload, r)
				}
			}()
			c.handleMessage([]byte(payload))
		}()
	}
}
