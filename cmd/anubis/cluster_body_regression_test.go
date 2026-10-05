package main

import (
	"net/http"
	"strings"
	"testing"
)

type audit50Body struct {
	*strings.Reader
	closes int
}

func (b *audit50Body) Close() error { b.closes++; return nil }

type audit50RoundTrip func(*http.Request) (*http.Response, error)

func (f audit50RoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestShowClusterClosesResponseBodies(t *testing.T) {
	old := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = old })
	t.Setenv("ANUBIS_API_TOKEN", "fixture-token")
	t.Setenv("ANUBIS_DATA_DIR", t.TempDir())
	for _, status := range []int{200, 500, 404, 204, 503} {
		for i := 0; i < 3; i++ {
			body := &audit50Body{Reader: strings.NewReader(`{"state":"Leader"}`)}
			http.DefaultClient = &http.Client{Transport: audit50RoundTrip(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Body: body, Header: make(http.Header), Request: r}, nil
			})}
			showCluster()
			if body.closes != 1 {
				t.Fatalf("status=%d repeat=%d EXPECTED: closes=1 ACTUAL: closes=%d", status, i, body.closes)
			}
		}
	}
	body := &audit50Body{Reader: strings.NewReader("invalid json")}
	http.DefaultClient = &http.Client{Transport: audit50RoundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: body, Header: make(http.Header), Request: r}, nil
	})}
	showCluster()
	if body.closes != 1 {
		t.Fatalf("decode failure left body open: %d", body.closes)
	}
}
