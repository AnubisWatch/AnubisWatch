package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func sharedQuotaRoute(ctx *Context) error {
	return ctx.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func sharedQuotaRequest(router *Router, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = "198.51.100.10:43210"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	return response
}

func TestRateLimitIsSharedAcrossRoutes(t *testing.T) {
	server := &RESTServer{
		router:          &Router{routes: make(map[string]map[string]Handler)},
		logger:          newTestLogger(),
		rateLimitStopCh: make(chan struct{}),
	}
	server.router.Use(server.rateLimitMiddleware)
	server.router.Handle(http.MethodGet, "/limit-a", sharedQuotaRoute)
	server.router.Handle(http.MethodGet, "/limit-b", sharedQuotaRoute)
	t.Cleanup(func() { _ = server.Stop(context.Background()) })

	for i := 0; i < 100; i++ {
		if got := sharedQuotaRequest(server.router, "/limit-a").Code; got != http.StatusOK {
			t.Fatalf("setup request %d returned %d, want 200", i+1, got)
		}
	}
	if got := sharedQuotaRequest(server.router, "/limit-a").Code; got != http.StatusTooManyRequests {
		t.Fatalf("control: request 101 on the same route returned %d, want 429", got)
	}
	if got := sharedQuotaRequest(server.router, "/limit-b").Code; got != http.StatusTooManyRequests {
		t.Fatalf("FAIL: request 102 from the same IP used a fresh per-route quota: got %d, want 429", got)
	}
	t.Log("PASS: the IP quota is shared across routes")
}
