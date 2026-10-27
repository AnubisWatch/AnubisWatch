package api

import (
	"bufio"
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/coder/websocket"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestRequestTraceAndAuditActualStatus(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); provider.Shutdown(context.Background()) })
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	server := NewRESTServer(core.ServerConfig{}, core.AuthConfig{}, newMockStorage(), nil, nil, nil, nil, nil, nil, nil, nil, logger)
	defer server.Stop(context.Background())
	for _, status := range []int{201, 401, 403, 500} {
		request := httptest.NewRequest("POST", "/api/v1/souls?token=private-token", nil)
		rec := httptest.NewRecorder()
		ctx := &Context{Request: request, Response: rec, User: &User{ID: "audit-user"}, RequestID: "audit-request"}
		if err := server.loggingMiddleware(func(c *Context) error { return c.Error(status, "test") })(ctx); err != nil {
			t.Fatal(err)
		}
		spans := exporter.GetSpans()
		span := spans[len(spans)-1]
		actual := 0
		for _, a := range span.Attributes {
			if string(a.Key) == "http.status_code" {
				actual = int(a.Value.AsInt64())
			}
		}
		if actual != status {
			t.Errorf("EXPECTED trace=%d; ACTUAL=%d", status, actual)
		}
	}
	if !strings.Contains(logs.String(), "security_audit") || !strings.Contains(logs.String(), "audit-user") {
		t.Error("EXPECTED security_audit with user identity; ACTUAL missing")
	}
	if strings.Contains(logs.String(), "private-token") {
		t.Error("audit leaked query credential")
	}
}

func TestAuditSurvivesOperationalLogLevel(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelError}))
	server := NewRESTServer(core.ServerConfig{}, core.AuthConfig{}, newMockStorage(), nil, nil, nil, nil, nil, nil, nil, nil, logger)
	defer server.Stop(context.Background())
	request := httptest.NewRequest("POST", "/api/v1/souls?password=secret-value", nil)
	ctx := &Context{Request: request, Response: httptest.NewRecorder(), User: &User{ID: "actor"}}
	server.loggingMiddleware(func(c *Context) error { return c.Error(403, "forbidden") })(ctx)
	if !strings.Contains(logs.String(), "security_audit") || !strings.Contains(logs.String(), "actor") {
		t.Fatal("audit suppressed by operational log level")
	}
	if strings.Contains(logs.String(), "secret-value") {
		t.Fatal("audit leaked credential")
	}
}

func TestResponseRecorderPreservesStreaming(t *testing.T) {
	underlying := httptest.NewRecorder()
	wrapper := &responseRecorder{ResponseWriter: underlying, statusCode: 200}
	var writer http.ResponseWriter = wrapper
	flusher, ok := writer.(http.Flusher)
	if !ok {
		t.Fatal("streaming trait missing")
	}
	flusher.Flush()
	if !underlying.Flushed || !wrapper.written {
		t.Fatal("Flush did not propagate")
	}
	if wrapper.Unwrap() != underlying {
		t.Fatal("ResponseController chain broken")
	}
}

func TestLoggedRoutesPreserveSSEAndWebSocket(t *testing.T) {
	server := NewRESTServer(core.ServerConfig{}, core.AuthConfig{Enabled: core.BoolPtr(true)}, newMockStorage(), nil, nil, &mockAuthenticator{}, nil, nil, nil, nil, nil, newTestLogger())
	defer server.Stop(context.Background())
	httpServer := httptest.NewServer(server.router)
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, "GET", httpServer.URL+"/api/v1/events", nil)
	request.Header.Set("Authorization", "Bearer valid-token")
	response, err := httpServer.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(response.Body).ReadString('\n')
	response.Body.Close()
	if err != nil || !strings.Contains(line, `"type":"connected"`) {
		t.Fatalf("SSE did not flush initial event: %q %v", line, err)
	}
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(httpServer.URL, "http")+"/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer valid-token"}}})
	if err != nil {
		t.Fatalf("wrapped websocket upgrade failed: %v", err)
	}
	conn.CloseNow()
}
