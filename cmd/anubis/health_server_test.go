package main

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

// withFakeHealthServer points client commands at a server whose /health is OK,
// so in-process `anubis health` tests do not exit as unhealthy.
func withFakeHealthServer(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_, _ = w.Write([]byte(`{"status":"healthy"}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	host, port, _ := net.SplitHostPort(u.Host)
	t.Setenv("ANUBIS_HOST", host)
	t.Setenv("ANUBIS_PORT", port)
}

func captureSelfHealth(t *testing.T) string {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	selfHealth()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

// `anubis health` is the docker-compose healthcheck but used to report
// healthy whenever the data directory existed, even with the server down.
func TestSelfHealthReflectsServer(t *testing.T) {
	withFakeHealthServer(t)
	if out := captureSelfHealth(t); !strings.Contains(out, `"status": "healthy"`) || !strings.Contains(out, `"server": "healthy"`) {
		t.Fatalf("expected healthy with server up:\n%s", out)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	ln.Close() // nothing listens on this port now
	t.Setenv("ANUBIS_HOST", "127.0.0.1")
	t.Setenv("ANUBIS_PORT", port)
	if out := captureSelfHealth(t); !strings.Contains(out, `"status": "unhealthy"`) {
		t.Fatalf("expected unhealthy with server down:\n%s", out)
	}
}
