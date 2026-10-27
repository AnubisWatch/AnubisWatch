package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAuditRequestPreservesProvenance(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelError}))
	audit := &AuditLogger{logger: logger, backend: &structuredAuditBackend{logger: logger}}
	request := httptest.NewRequest("PUT", "http://monitor.invalid/api/v1/config?password=must-not-log", nil)
	request.RemoteAddr = "198.51.100.12:43210"
	request.Header.Set("User-Agent", "audit-proof-client")
	audit.LogRequest(request, "actor", 403, time.Millisecond, "shared-request-id")
	var row map[string]any
	if err := json.Unmarshal(output.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"user_id": "actor", "request_id": "shared-request-id", "ip_address": "198.51.100.12", "user_agent": "audit-proof-client"} {
		if row[key] != want {
			t.Errorf("EXPECTED: %s=%q; ACTUAL: %v; PROBLEM CONFIRMED", key, want, row[key])
		}
	}
	if bytes.Contains(output.Bytes(), []byte("must-not-log")) {
		t.Fatal("query password leaked")
	}
}
