package api

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// validateJSONMiddleware enforces the body policy for write requests that carry a
// body: Content-Type must be application/json, the body must be valid JSON, and
// bodyless writes (logout, force-check, acknowledge) stay exempt. These tests pin
// that the policy also applies when the body length is unknown.
//
// It did not: hasBody was `ContentLength > 0 || TransferEncoding != nil`, and
// net/http reports an unknown length as -1 (HTTP/2 requests may omit
// content-length, since HTTP/2 has no chunked encoding), so the whole policy was
// skipped for those requests.

const bodyPolicyProbePath = "/probe-body-policy"

func bodyPolicyServer(t *testing.T) *RESTServer {
	t.Helper()

	s := NewRESTServer(
		core.ServerConfig{Host: "localhost", Port: 8080},
		core.AuthConfig{Enabled: core.BoolPtr(true)},
		newMockStorage(), &mockProbeEngine{}, &mockAlertManager{}, &mockAuthenticator{},
		&mockClusterManager{}, nil, nil, nil, nil, newTestLogger(),
	)
	s.router.Handle(http.MethodPost, bodyPolicyProbePath, func(ctx *Context) error {
		return ctx.JSON(http.StatusOK, map[string]any{"reached_handler": true})
	})
	return s
}

// unknownLengthPost sends a POST whose body length the transport cannot know, so
// net/http reports ContentLength == -1 to the server. A *bytes.Reader would be
// recognised and sent with content-length.
func unknownLengthPost(t *testing.T, url, contentType, body string) *http.Request {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, url, io.NopCloser(io.LimitReader(strings.NewReader(body), int64(len(body)))))
	if err != nil {
		t.Fatalf("could not build request: %v", err)
	}
	req.Header.Set("Content-Type", contentType)
	return req
}

func http2Server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(bodyPolicyServer(t).router)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func postStatus(t *testing.T, srv *httptest.Server, req *http.Request) int {
	t.Helper()

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func TestBodyPolicyAppliesToUnknownLengthBody(t *testing.T) {
	srv := http2Server(t)

	for _, tc := range []struct {
		name        string
		contentType string
		body        string
		want        int
	}{
		{"non-JSON content type", "text/plain", `{"ok":true}`, http.StatusBadRequest},
		{"invalid JSON with a JSON content type", "application/json", `not json`, http.StatusBadRequest},
		{"conforming body", "application/json", `{"ok":true}`, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := postStatus(t, srv, unknownLengthPost(t, srv.URL+bodyPolicyProbePath, tc.contentType, tc.body))
			if got != tc.want {
				t.Errorf("unknown-length POST with %s got %d, want %d", tc.contentType, got, tc.want)
			}
		})
	}
}

// TestBodyPolicyKeepsDeclaredLengthBehaviour is the neighbouring control: the
// declared-length path (HTTP/1.1) must keep working exactly as before.
func TestBodyPolicyKeepsDeclaredLengthBehaviour(t *testing.T) {
	srv := httptest.NewServer(bodyPolicyServer(t).router)
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+bodyPolicyProbePath, "text/plain", bytes.NewReader([]byte(`{"ok":true}`)))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("declared-length POST with a non-JSON content type got %d, want 400", resp.StatusCode)
	}
}

// TestBodyPolicyLeavesBodylessWritesAlone pins the documented exception: a
// bodyless write must not be required to send a Content-Type header.
func TestBodyPolicyLeavesBodylessWritesAlone(t *testing.T) {
	srv := httptest.NewServer(bodyPolicyServer(t).router)
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodPost, srv.URL+bodyPolicyProbePath, nil)
	if err != nil {
		t.Fatalf("could not build request: %v", err)
	}
	if got := postStatus(t, srv, req); got != http.StatusOK {
		t.Errorf("bodyless POST got %d, want 200: a bodyless write must not require a Content-Type", got)
	}
}

// TestBodyPolicyLeavesUnknownLengthBodylessWritesAlone covers the exception
// over HTTP/2: a write whose length is unknown (-1) but whose body turns out
// empty carries no body, so it must not need a Content-Type either. Deciding
// from the declared length alone rejected it with 400, breaking bodyless
// logout, force-check and acknowledge for HTTP/2 clients that omit
// content-length.
func TestBodyPolicyLeavesUnknownLengthBodylessWritesAlone(t *testing.T) {
	srv := http2Server(t)

	req := unknownLengthPost(t, srv.URL+bodyPolicyProbePath, "", "")
	req.Header.Del("Content-Type")
	if got := postStatus(t, srv, req); got != http.StatusOK {
		t.Errorf("unknown-length empty POST without Content-Type got %d, want 200", got)
	}

	// The same request carrying bytes is still held to the policy.
	req = unknownLengthPost(t, srv.URL+bodyPolicyProbePath, "", `{"ok":true}`)
	req.Header.Del("Content-Type")
	if got := postStatus(t, srv, req); got != http.StatusBadRequest {
		t.Errorf("unknown-length POST with a body and no Content-Type got %d, want 400", got)
	}
}
