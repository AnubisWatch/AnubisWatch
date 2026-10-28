package api

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/coder/websocket"
)

// revocableAuth is an authenticator whose sessions can end: Logout drops one
// token, ChangePassword drops them all (as LocalAuthenticator does).
type revocableAuth struct {
	mockAuthenticator
	mu     sync.Mutex
	tokens map[string]*User
}

func newRevocableAuth() *revocableAuth {
	return &revocableAuth{tokens: map[string]*User{
		"token-a": {ID: "user-1", Email: "a@example.com", Role: "admin", Workspace: "default"},
		"token-b": {ID: "user-1", Email: "a@example.com", Role: "admin", Workspace: "default"},
		"token-c": {ID: "user-2", Email: "c@example.com", Role: "viewer", Workspace: "default"},
	}}
}

func (a *revocableAuth) Authenticate(token string) (*User, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if u, ok := a.tokens[token]; ok {
		c := *u
		return &c, nil
	}
	return nil, errors.New("invalid token")
}

func (a *revocableAuth) Logout(token string) error {
	a.revoke(token)
	return nil
}

func (a *revocableAuth) ChangePassword(token, _, _ string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.tokens[token]; !ok {
		return errors.New("invalid token")
	}
	for t, u := range a.tokens {
		if u.ID == "user-1" {
			delete(a.tokens, t)
		}
	}
	return nil
}

func (a *revocableAuth) revoke(token string) {
	a.mu.Lock()
	delete(a.tokens, token)
	a.mu.Unlock()
}

type liveStreamHarness struct {
	t      *testing.T
	server *RESTServer
	http   *httptest.Server
	auth   *revocableAuth
}

func newLiveStreamHarness(t *testing.T, revalidate time.Duration) *liveStreamHarness {
	t.Helper()
	auth := newRevocableAuth()
	server := NewRESTServer(core.ServerConfig{}, core.AuthConfig{Enabled: core.BoolPtr(true)}, newMockStorage(), nil, nil, auth, nil, nil, nil, nil, nil, newTestLogger())
	server.ws.revalidateInterval = revalidate
	server.sessionRevalidateInterval = revalidate
	httpServer := httptest.NewServer(server.router)
	t.Cleanup(func() {
		httpServer.CloseClientConnections()
		httpServer.Close()
		_ = server.Stop(context.Background())
	})
	return &liveStreamHarness{t: t, server: server, http: httpServer, auth: auth}
}

// openWS connects with token and consumes the welcome message.
func (h *liveStreamHarness) openWS(token string) *websocket.Conn {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(h.http.URL, "http")+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}},
	})
	if err != nil {
		h.t.Fatalf("dial with %s: %v", token, err)
	}
	h.t.Cleanup(func() { _ = conn.CloseNow() })
	if _, msg, err := conn.Read(ctx); err != nil || !strings.Contains(string(msg), `"connected"`) {
		h.t.Fatalf("welcome message: %q %v", msg, err)
	}
	return conn
}

// openSSE starts an event stream with token and consumes the first event.
func (h *liveStreamHarness) openSSE(token string) *bufio.Reader {
	h.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, h.http.URL+"/api/v1/events", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := h.http.Client().Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { resp.Body.Close() })
	r := bufio.NewReader(resp.Body)
	if line, err := r.ReadString('\n'); err != nil || !strings.Contains(line, `"connected"`) {
		h.t.Fatalf("SSE first event: %q %v", line, err)
	}
	return r
}

func (h *liveStreamHarness) call(method, path, token, body string) int {
	h.t.Helper()
	req, _ := http.NewRequest(method, h.http.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := h.http.Client().Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// wsClosed reports whether the server ends conn within the deadline.
func wsClosed(conn *websocket.Conn, within time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), within)
	defer cancel()
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			return ctx.Err() == nil
		}
	}
}

// wsAlive reports whether conn still answers a ping message.
func wsAlive(conn *websocket.Conn) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"ping"}`)); err != nil {
		return false
	}
	_, msg, err := conn.Read(ctx)
	return err == nil && strings.Contains(string(msg), "pong")
}

// sseClosed reports whether the stream ends within the deadline.
func sseClosed(r *bufio.Reader, within time.Duration) bool {
	done := make(chan error, 1)
	go func() {
		for {
			if _, err := r.ReadString('\n'); err != nil {
				done <- err
				return
			}
		}
	}()
	select {
	case err := <-done:
		return errors.Is(err, io.EOF) || err != nil
	case <-time.After(within):
		return false
	}
}

// Before the fix WebSocket and SSE streams authenticated only at their
// handshake: a logged-out, password-changed or otherwise revoked session kept
// receiving live events for as long as the connection stayed open.
func TestLogoutClosesThatSessionsLiveStreams(t *testing.T) {
	h := newLiveStreamHarness(t, time.Hour) // isolate explicit revocation from revalidation
	wsA := h.openWS("token-a")
	sseA := h.openSSE("token-a")
	wsB := h.openWS("token-b") // same user, another session

	if code := h.call(http.MethodPost, "/api/v1/auth/logout", "token-a", "{}"); code != http.StatusOK {
		t.Fatalf("logout: %d", code)
	}
	if !wsClosed(wsA, 2*time.Second) {
		t.Error("WebSocket of the logged-out session is still open")
	}
	if !sseClosed(sseA, 2*time.Second) {
		t.Error("SSE stream of the logged-out session is still open")
	}
	if !wsAlive(wsB) {
		t.Error("logout of one session closed another session's WebSocket")
	}
}

func TestPasswordChangeClosesTheUsersLiveStreams(t *testing.T) {
	h := newLiveStreamHarness(t, time.Hour)
	wsA := h.openWS("token-a")
	sseB := h.openSSE("token-b")
	wsOther := h.openWS("token-c") // another user

	body := `{"current_password":"OldPassword123!","new_password":"NewPassword123!"}`
	if code := h.call(http.MethodPut, "/api/v1/auth/change-password", "token-a", body); code != http.StatusOK {
		t.Fatalf("change-password: %d", code)
	}
	if !wsClosed(wsA, 2*time.Second) {
		t.Error("WebSocket survived the password change")
	}
	if !sseClosed(sseB, 2*time.Second) {
		t.Error("SSE stream of another session of the same user survived the password change")
	}
	if !wsAlive(wsOther) {
		t.Error("password change closed another user's WebSocket")
	}
}

func TestRevalidationClosesStreamsOfRevokedSessions(t *testing.T) {
	h := newLiveStreamHarness(t, 50*time.Millisecond)
	wsA := h.openWS("token-a")
	sseA := h.openSSE("token-a")
	wsC := h.openWS("token-c")

	// Revoked behind the API's back (expiry, another node, an admin action).
	h.auth.revoke("token-a")

	if !wsClosed(wsA, 2*time.Second) {
		t.Error("WebSocket of a revoked session was not closed by revalidation")
	}
	if !sseClosed(sseA, 2*time.Second) {
		t.Error("SSE stream of a revoked session was not closed by revalidation")
	}
	if !wsAlive(wsC) {
		t.Error("revalidation closed a still-valid session")
	}

	h.server.ws.mu.RLock()
	clients := len(h.server.ws.clients)
	h.server.ws.mu.RUnlock()
	if clients != 1 {
		t.Errorf("%d WebSocket clients registered, want 1", clients)
	}
}

func TestLiveSessionRegistry(t *testing.T) {
	var nilRegistry *liveSessionRegistry
	nilRegistry.add("t", "u", func() {})()
	if nilRegistry.revokeToken("t") != 0 || nilRegistry.revokeUser("u") != 0 {
		t.Fatal("nil registry revoked something")
	}

	r := newLiveSessionRegistry()
	closed := map[string]int{}
	var mu sync.Mutex
	closer := func(name string) func() {
		return func() { mu.Lock(); closed[name]++; mu.Unlock() }
	}
	r.add("t1", "u1", closer("a"))
	r.add("t2", "u1", closer("b"))
	unregister := r.add("t3", "u2", closer("c"))
	unregister()

	for _, tc := range []struct {
		name string
		got  int
		want int
	}{
		{"empty token", r.revokeToken(""), 0},
		{"unknown token", r.revokeToken("nope"), 0},
		{"unregistered stream", r.revokeUser("u2"), 0},
		{"token", r.revokeToken("t1"), 1},
		{"token again", r.revokeToken("t1"), 0},
		{"user", r.revokeUser("u1"), 1},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: revoked %d, want %d", tc.name, tc.got, tc.want)
		}
	}
	if closed["a"] != 1 || closed["b"] != 1 || closed["c"] != 0 {
		t.Errorf("close calls = %v", closed)
	}
}
