package api

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/coder/websocket"
)

// Regression coverage for the responseRecorder wrapper installed by
// loggingMiddleware (internal/api/rest.go). The recorder replaces the
// ResponseWriter seen by every handler, so it must keep the optional
// capabilities of the wrapped writer that streaming/upgrade endpoints rely on:
//
//   - http.Hijacker  — required by coder/websocket Accept for GET /ws
//   - http.Flusher   — required by the SSE handler for GET /api/v1/events
//
// Without them the real-time dashboard silently breaks: the WebSocket
// handshake fails ("response does not implement http.Hijacker") and the SSE
// endpoint panics on the unchecked w.(http.Flusher) assertion.

// The optional interfaces are asserted at runtime (not with compile-time
// `var _ http.Hijacker = ...` declarations) so that a regression is reported
// as a readable EXPECTED/ACTUAL failure instead of a package build break.

// hijackableRecorder is an underlying writer that supports both Hijack and
// Flush, standing in for the net/http server writer.
type hijackableRecorder struct {
	*httptest.ResponseRecorder
	conn       net.Conn
	brw        *bufio.ReadWriter
	flushes    int
	hijackCall int
}

func (h *hijackableRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.hijackCall++
	return h.conn, h.brw, nil
}

func (h *hijackableRecorder) Flush() {
	h.flushes++
	h.ResponseRecorder.Flush()
}

// newStreamingRegressionServer builds a REST server whose router carries the
// full middleware chain (request ID, logging/audit recorder, security
// headers, CORS, recovery, rate limiting).
func newStreamingRegressionServer(t *testing.T) *RESTServer {
	t.Helper()
	server := NewRESTServer(
		core.ServerConfig{Host: "127.0.0.1", Port: 8080},
		core.AuthConfig{Enabled: core.BoolPtr(true)},
		newMockStorage(), &mockProbeEngine{}, &mockAlertManager{}, &mockAuthenticator{},
		&mockClusterManager{}, nil, nil, nil, nil, newTestLogger(),
	)
	t.Cleanup(func() {
		if err := server.Stop(context.Background()); err != nil {
			t.Logf("REST server stop: %v", err)
		}
	})
	return server
}

// TestRESTWebSocketUpgradeSurvivesResponseRecorder is the end-to-end proof for
// DEFECT 1: the WebSocket handshake must complete with 101 through the
// logging middleware's responseRecorder wrapper.
func TestRESTWebSocketUpgradeSurvivesResponseRecorder(t *testing.T) {
	server := newStreamingRegressionServer(t)
	srv := httptest.NewServer(server.router)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	conn, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer valid-token"}},
	})
	// On success coder/websocket hands the response body to the connection
	// and sets resp.Body to nil, so only a failed handshake has a body to close.
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("WebSocket upgrade through loggingMiddleware failed: "+
			"EXPECTED 101 Switching Protocols handshake, ACTUAL error=%v (http status %d)", err, status)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "") })

	// Positive control: the upgraded connection carries live traffic, so the
	// handshake was not merely a status code but a working stream.
	readCtx, readCancel := context.WithTimeout(ctx, 10*time.Second)
	defer readCancel()
	_, msg, err := conn.Read(readCtx)
	if err != nil {
		t.Fatalf("WebSocket welcome message read failed: EXPECTED a frame from the upgraded connection, ACTUAL error=%v", err)
	}
	if !strings.Contains(string(msg), "connected") {
		t.Fatalf("WebSocket welcome payload mismatch: EXPECTED a \"connected\" frame, ACTUAL %q", string(msg))
	}
}

// TestRESTEventsSSEStreamsThroughResponseRecorder is the end-to-end proof for
// DEFECT 2: GET /api/v1/events must stream the initial event and keep the
// connection open while the handler loops (no panic on the Flusher
// assertion), closing only when the client cancels.
func TestRESTEventsSSEStreamsThroughResponseRecorder(t *testing.T) {
	server := newStreamingRegressionServer(t)
	srv := httptest.NewServer(server.router)
	t.Cleanup(srv.Close)

	reqCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, srv.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatalf("build SSE request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer valid-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("SSE request failed: EXPECTED an established event stream, ACTUAL error=%v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("SSE status mismatch: EXPECTED 200, ACTUAL %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("SSE content type mismatch: EXPECTED text/event-stream, ACTUAL %q", ct)
	}

	reader := bufio.NewReader(resp.Body)

	// The initial event must be flushed to the client while the handler is
	// still running. A handler that died on the Flusher assertion would only
	// release the buffered bytes when the request completed.
	firstEvent := make(chan string, 1)
	readErr := make(chan error, 1)
	go func() {
		line, err := readSSEDataLine(reader)
		firstEvent <- line
		readErr <- err
	}()

	var payload string
	select {
	case payload = <-firstEvent:
	case <-time.After(10 * time.Second):
		t.Fatalf("SSE initial event never arrived: EXPECTED a flushed \"data:\" frame within 10s, ACTUAL timeout")
	}
	if err := <-readErr; err != nil {
		t.Fatalf("SSE initial event read failed: EXPECTED a flushed \"data:\" frame, ACTUAL error=%v", err)
	}
	if !strings.Contains(payload, `"type":"connected"`) {
		t.Fatalf("SSE initial event payload mismatch: EXPECTED a connected event, ACTUAL %q", payload)
	}

	// The handler loops until the request context is done. If the Flusher
	// assertion had panicked, the recovery middleware would have ended the
	// response early and the stream would report EOF straight away.
	type streamResult struct {
		data string
		err  error
	}
	next := make(chan streamResult, 1)
	go func() {
		line, err := readSSEDataLine(reader)
		next <- streamResult{data: line, err: err}
	}()

	select {
	case got := <-next:
		t.Fatalf("SSE stream ended while the handler should still be running: "+
			"EXPECTED an open stream (handler looping), ACTUAL stream closed (data=%q err=%v)", got.data, got.err)
	case <-time.After(500 * time.Millisecond):
		// Control passed: the stream is still open.
	}

	// Cancelling the request must terminate the handler and close the stream.
	cancel()
	select {
	case got := <-next:
		if got.err == nil {
			t.Fatalf("SSE stream close mismatch: EXPECTED the stream to end after cancel, ACTUAL another data frame %q", got.data)
		}
		if !errors.Is(got.err, io.EOF) && !strings.Contains(got.err.Error(), "context canceled") {
			t.Fatalf("SSE stream close error mismatch: EXPECTED EOF or context cancellation, ACTUAL %v", got.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("SSE stream did not close after cancel: EXPECTED EOF within 10s, ACTUAL timeout")
	}
}

// readSSEDataLine reads the next "data:" line from an SSE stream.
func readSSEDataLine(r *bufio.Reader) (string, error) {
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return line, err
		}
		if strings.HasPrefix(line, "data:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "data:")), nil
		}
	}
}

// TestRESTJSONRouteStillWorksThroughResponseRecorder is the nearby passing
// control: the logging/audit middleware keeps serving ordinary JSON routes
// with the recorded status code.
func TestRESTJSONRouteStillWorksThroughResponseRecorder(t *testing.T) {
	server := newStreamingRegressionServer(t)
	srv := httptest.NewServer(server.router)
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/souls", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer valid-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("JSON route request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("JSON route status mismatch: EXPECTED 200, ACTUAL %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("JSON route body read failed: %v", err)
	}
	if !strings.Contains(string(body), `"pagination"`) {
		t.Fatalf("JSON route body mismatch: EXPECTED a paginated envelope, ACTUAL %q", string(body))
	}
}

// TestResponseRecorderDelegatesHijackAndFlush covers the recorder contract
// directly: the recorder must expose http.Hijacker and http.Flusher, both
// must reach the wrapped writer, and neither may panic when the wrapped
// writer cannot support them.
func TestResponseRecorderDelegatesHijackAndFlush(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })

	underlying := &hijackableRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		conn:             client,
		brw:              bufio.NewReadWriter(bufio.NewReader(client), bufio.NewWriter(client)),
	}
	recorder := &responseRecorder{ResponseWriter: underlying, statusCode: http.StatusOK}

	hijacker, ok := any(recorder).(http.Hijacker)
	if !ok {
		t.Fatalf("recorder hijack capability missing: " +
			"EXPECTED *responseRecorder to implement http.Hijacker, ACTUAL it does not (websocket upgrade cannot hijack the connection)")
	}
	conn, brw, err := hijacker.Hijack()
	if err != nil {
		t.Fatalf("Hijack delegation failed: EXPECTED the underlying writer's connection, ACTUAL error=%v", err)
	}
	if conn != underlying.conn || brw != underlying.brw {
		t.Fatalf("Hijack delegation mismatch: EXPECTED the underlying writer's conn/brw, ACTUAL different values")
	}
	if underlying.hijackCall != 1 {
		t.Fatalf("Hijack delegation count mismatch: EXPECTED 1 call on the underlying writer, ACTUAL %d", underlying.hijackCall)
	}

	flusher, ok := any(recorder).(http.Flusher)
	if !ok {
		t.Fatalf("recorder flush capability missing: " +
			"EXPECTED *responseRecorder to implement http.Flusher, ACTUAL it does not (SSE endpoint would panic on w.(http.Flusher))")
	}
	flusher.Flush()
	if underlying.flushes != 1 {
		t.Fatalf("Flush delegation count mismatch: EXPECTED 1 call on the underlying writer, ACTUAL %d", underlying.flushes)
	}
	if recorder.statusCode != http.StatusOK {
		t.Fatalf("Flush status mismatch: EXPECTED 200, ACTUAL %d", recorder.statusCode)
	}

	// A writer without hijack/flush support (e.g. httptest.ResponseRecorder)
	// must produce an error, never a panic.
	plain := &responseRecorder{ResponseWriter: httptest.NewRecorder(), statusCode: http.StatusOK}
	if plainHijacker, ok := any(plain).(http.Hijacker); ok {
		if _, _, err := plainHijacker.Hijack(); err == nil {
			t.Fatalf("Hijack on an unsupported writer: EXPECTED an error, ACTUAL nil")
		}
	}
	if plainFlusher, ok := any(plain).(http.Flusher); ok {
		plainFlusher.Flush() // must not panic
	}
}
