package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestHandleListSouls_RedactsSecretFields(t *testing.T) {
	storage := newMockStorage()
	storage.SaveSoul(context.Background(), &core.Soul{
		ID:          "soul-secret",
		Name:        "Secret Soul",
		Type:        core.CheckHTTP,
		Target:      "https://example.com",
		WorkspaceID: "default",
		HTTP: &core.HTTPConfig{
			Method:      "POST",
			ValidStatus: []int{200},
			Headers:     map[string]string{"X-Test": "keep-me"},
			Body:        "keep-body",
		},
		GRPC: &core.GRPCConfig{Metadata: map[string]string{"trace": "keep-meta"}},
	})
	server := &RESTServer{store: storage, logger: newTestLogger()}
	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:   httptest.NewRequest(http.MethodGet, "/api/v1/souls", nil),
		Response:  rec,
		Workspace: "default",
		User:      &User{Role: "viewer", Workspace: "default"},
	}

	if err := server.handleListSouls(ctx); err != nil {
		t.Fatalf("handleListSouls failed: %v", err)
	}

	body := rec.Body.String()
	for _, secret := range []string{"keep-me", "keep-body", "keep-meta"} {
		if strings.Contains(body, secret) {
			t.Fatalf("response leaked secret %q: %s", secret, body)
		}
	}
	if !strings.Contains(body, redactedSecretValue) || !strings.Contains(body, "has_secrets") {
		t.Fatalf("expected redaction markers in response: %s", body)
	}
}

func TestHandleUpdateSoul_PreservesSecretsWhenPayloadIsRedacted(t *testing.T) {
	storage := newMockStorage()
	storage.SaveSoul(context.Background(), &core.Soul{
		ID:          "soul-secret",
		Name:        "Original",
		Type:        core.CheckHTTP,
		Target:      "https://example.com",
		WorkspaceID: "default",
		CreatedAt:   time.Now().Add(-time.Hour),
		HTTP: &core.HTTPConfig{
			Method:      "POST",
			ValidStatus: []int{200},
			Headers:     map[string]string{"X-Test": "keep-me"},
			Body:        "keep-body",
		},
	})
	server := &RESTServer{store: storage, logger: newTestLogger()}
	payload := core.Soul{
		Name:   "Updated Soul",
		Type:   core.CheckHTTP,
		Target: "https://example.com",
		Weight: core.Duration{Duration: 60 * time.Second},
		HTTP: &core.HTTPConfig{
			Method:      "POST",
			ValidStatus: []int{200},
			Headers:     map[string]string{"X-Test": redactedSecretValue},
			Body:        redactedSecretValue,
		},
	}
	body, _ := json.Marshal(payload)
	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:   httptest.NewRequest(http.MethodPut, "/api/v1/souls/soul-secret", bytes.NewBuffer(body)),
		Response:  rec,
		Params:    map[string]string{"id": "soul-secret"},
		Workspace: "default",
		User:      &User{Role: "admin", Workspace: "default"},
	}

	if err := server.handleUpdateSoul(ctx); err != nil {
		t.Fatalf("handleUpdateSoul failed: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	stored := storage.souls["soul-secret"]
	if stored.HTTP.Headers["X-Test"] != "keep-me" {
		t.Fatalf("header changed: %#v", stored.HTTP.Headers)
	}
	if stored.HTTP.Body != "keep-body" {
		t.Fatalf("body changed: %q", stored.HTTP.Body)
	}
}

func TestHandleUpdateSoul_DoesNotRebindSecretsToChangedTarget(t *testing.T) {
	storage := newMockStorage()
	storage.SaveSoul(context.Background(), &core.Soul{
		ID: "soul-secret", Name: "Original", Type: core.CheckHTTP,
		Target: "https://old.example.com", WorkspaceID: "default",
		HTTP: &core.HTTPConfig{Method: "POST", ValidStatus: []int{200},
			Headers: map[string]string{"Authorization": "Bearer old-secret"}, Body: "old-body"},
	})
	server := &RESTServer{store: storage, logger: newTestLogger()}
	payload := core.Soul{
		Name: "Updated", Type: core.CheckHTTP, Target: "https://new.example.com",
		HTTP: &core.HTTPConfig{Method: "POST", ValidStatus: []int{200},
			Headers: map[string]string{"Authorization": redactedSecretValue}, Body: redactedSecretValue},
	}
	body, _ := json.Marshal(payload)
	ctx := &Context{
		Request:  httptest.NewRequest(http.MethodPut, "/api/v1/souls/soul-secret", bytes.NewReader(body)),
		Response: httptest.NewRecorder(), Params: map[string]string{"id": "soul-secret"},
		Workspace: "default", User: &User{Role: "admin", Workspace: "default"},
	}
	if err := server.handleUpdateSoul(ctx); err != nil {
		t.Fatalf("handleUpdateSoul failed: %v", err)
	}
	stored := storage.souls["soul-secret"]
	if stored.HTTP.Headers["Authorization"] == "Bearer old-secret" || stored.HTTP.Body == "old-body" {
		t.Fatalf("old endpoint secrets rebound to new target: %#v", stored.HTTP)
	}
}

func TestRedactedSoulDTO_RedactsTargetAndSendPayloads(t *testing.T) {
	soul := &core.Soul{
		ID: "soul-secret", Type: core.CheckWebSocket,
		Target:    "wss://user:password@example.com/socket?token=query-secret&safe=yes",
		TCP:       &core.TCPConfig{Send: "tcp-secret", BannerMatch: "ready"},
		UDP:       &core.UDPConfig{SendHex: "deadbeef", ExpectContains: "ok"},
		WebSocket: &core.WebSocketConfig{Send: "websocket-secret", Headers: map[string]string{"Authorization": "Bearer secret"}},
	}
	encoded, err := json.Marshal(redactedSoulDTOFromCore(soul))
	if err != nil {
		t.Fatalf("marshal redacted soul: %v", err)
	}
	text := string(encoded)
	for _, secret := range []string{"password", "query-secret", "tcp-secret", "deadbeef", "websocket-secret", "Bearer secret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("response leaked %q: %s", secret, text)
		}
	}
	for _, marker := range []string{`"target":true`, `"tcp":true`, `"udp":true`, `"websocket":true`} {
		if !strings.Contains(text, marker) {
			t.Fatalf("missing secret marker %s: %s", marker, text)
		}
	}
}

func TestRedactedJudgment_DropsHTTPResponseMaterialWithoutMutation(t *testing.T) {
	original := &core.Judgment{ID: "j1", Details: &core.JudgmentDetails{
		ResponseHeaders: map[string]string{"Set-Cookie": "session=secret", "X-Request-ID": "req-1"},
		ResponseBody:    `{"token":"secret"}`,
		Assertions:      []core.AssertionResult{{Type: "status_code", Passed: true}},
	}}
	redacted := redactedJudgmentFromCore(original)
	if redacted.Details.ResponseBody != "" || redacted.Details.ResponseHeaders != nil {
		t.Fatalf("response material not removed: %#v", redacted.Details)
	}
	if original.Details.ResponseBody == "" || original.Details.ResponseHeaders == nil {
		t.Fatal("redaction mutated stored judgment")
	}
	if len(redacted.Details.Assertions) != 1 {
		t.Fatal("non-sensitive judgment details were removed")
	}
}

func TestHandleListChannels_RedactsSecretConfigForViewer(t *testing.T) {
	alert := &mockAlertManager{channels: map[string]*core.AlertChannel{
		"channel-1": {
			ID:          "channel-1",
			Name:        "Ops",
			Type:        core.ChannelSlack,
			Enabled:     true,
			WorkspaceID: "default",
			Config: map[string]interface{}{
				"url":    "keep-url",
				"nested": map[string]interface{}{"password": "keep-pass"},
			},
		},
	}}
	server := &RESTServer{alert: alert, logger: newTestLogger()}
	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:   httptest.NewRequest(http.MethodGet, "/api/v1/channels", nil),
		Response:  rec,
		Workspace: "default",
		User:      &User{Role: "viewer", Workspace: "default"},
	}

	if err := server.handleListChannels(ctx); err != nil {
		t.Fatalf("handleListChannels failed: %v", err)
	}

	body := rec.Body.String()
	for _, secret := range []string{"keep-url", "keep-pass"} {
		if strings.Contains(body, secret) {
			t.Fatalf("channel response leaked secret %q: %s", secret, body)
		}
	}
	if !strings.Contains(body, redactedSecretValue) || !strings.Contains(body, "has_secrets") {
		t.Fatalf("expected redaction markers in channel response: %s", body)
	}
}

func TestHandleUpdateChannel_PreservesSecretsWhenPayloadIsRedacted(t *testing.T) {
	alert := &mockAlertManager{channels: map[string]*core.AlertChannel{
		"channel-1": {
			ID:          "channel-1",
			Name:        "Ops",
			Type:        core.ChannelSlack,
			Enabled:     true,
			WorkspaceID: "default",
			CreatedAt:   time.Now().Add(-time.Hour),
			Config: map[string]interface{}{
				"url":    "keep-url",
				"nested": map[string]interface{}{"password": "keep-pass"},
			},
		},
	}}
	server := &RESTServer{alert: alert, store: newMockStorage(), logger: newTestLogger()}
	payload := core.AlertChannel{
		Name:    "Ops Updated",
		Type:    core.ChannelSlack,
		Enabled: true,
		Config: map[string]interface{}{
			"url":    redactedSecretValue,
			"nested": map[string]interface{}{"password": redactedSecretValue},
		},
	}
	body, _ := json.Marshal(payload)
	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:   httptest.NewRequest(http.MethodPut, "/api/v1/channels/channel-1", bytes.NewBuffer(body)),
		Response:  rec,
		Params:    map[string]string{"id": "channel-1"},
		Workspace: "default",
		User:      &User{Role: "admin", Workspace: "default"},
	}

	if err := server.handleUpdateChannel(ctx); err != nil {
		t.Fatalf("handleUpdateChannel failed: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	stored := alert.channels["channel-1"]
	if stored.Config["url"] != "keep-url" {
		t.Fatalf("url changed: %#v", stored.Config)
	}
	nested, _ := stored.Config["nested"].(map[string]interface{})
	if nested["password"] != "keep-pass" {
		t.Fatalf("nested password changed: %#v", stored.Config)
	}
}

func TestHandleUpdateChannel_RejectsDestinationChangeWithRedactedSecrets(t *testing.T) {
	alert := &mockAlertManager{channels: map[string]*core.AlertChannel{
		"channel-1": {
			ID:          "channel-1",
			Name:        "Ops",
			Type:        core.ChannelSlack,
			Enabled:     true,
			WorkspaceID: "default",
			CreatedAt:   time.Now().Add(-time.Hour),
			Config: map[string]interface{}{
				"webhook_url": "https://safe.example.com/hook",
				"headers":     map[string]interface{}{"Authorization": "original-token"},
			},
		},
	}}
	server := &RESTServer{alert: alert, store: newMockStorage(), logger: newTestLogger()}
	payload := core.AlertChannel{
		Name:    "Ops Updated",
		Type:    core.ChannelSlack,
		Enabled: true,
		Config: map[string]interface{}{
			"webhook_url": "https://attacker.example.com/steal",
			"headers":     map[string]interface{}{"Authorization": "[REDACTED]"},
		},
	}
	body, _ := json.Marshal(payload)
	rec := httptest.NewRecorder()
	ctx := &Context{
		Request:   httptest.NewRequest(http.MethodPut, "/api/v1/channels/channel-1", bytes.NewBuffer(body)),
		Response:  rec,
		Params:    map[string]string{"id": "channel-1"},
		Workspace: "default",
		User:      &User{Role: "admin", Workspace: "default"},
	}

	_ = server.handleUpdateChannel(ctx)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for destination change with redacted secrets, got %d: %s", rec.Code, rec.Body.String())
	}

	// The original channel must remain unmodified.
	stored := alert.channels["channel-1"]
	if stored.Config["webhook_url"] != "https://safe.example.com/hook" {
		t.Fatalf("channel was modified despite rejection: %#v", stored.Config)
	}
}

func TestMCPHandleListSouls_RedactsSecretFields(t *testing.T) {
	store := newMockStorage()
	store.souls["soul-1"] = &core.Soul{
		ID:          "soul-1",
		Name:        "Test Soul",
		Type:        core.CheckHTTP,
		WorkspaceID: "default",
		HTTP: &core.HTTPConfig{
			Method:      "GET",
			ValidStatus: []int{200},
			Headers:     map[string]string{"X-Test": "keep-me"},
			Body:        "keep-body",
		},
	}
	server := NewMCPServer(store, &mockProbeEngine{}, &mockAlertManager{}, newTestLogger())
	ctx := core.ContextWithWorkspaceID(core.ContextWithUserRole(context.Background(), string(core.RoleViewer)), "default")

	result, err := server.handleListSouls(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("handleListSouls failed: %v", err)
	}
	body := mcpTextResult(result)
	if strings.Contains(body, "keep-me") || strings.Contains(body, "keep-body") {
		t.Fatalf("mcp list leaked secrets: %s", body)
	}
	if !strings.Contains(body, redactedSecretValue) || !strings.Contains(body, "has_secrets") {
		t.Fatalf("expected redaction markers in mcp list: %s", body)
	}
}

func TestMCPHandleGetSoul_RedactsSecretFields(t *testing.T) {
	store := newMockStorage()
	store.souls["soul-1"] = &core.Soul{
		ID:          "soul-1",
		Name:        "Test Soul",
		Type:        core.CheckHTTP,
		WorkspaceID: "default",
		HTTP: &core.HTTPConfig{
			Method:      "GET",
			ValidStatus: []int{200},
			Headers:     map[string]string{"X-Test": "keep-me"},
			Body:        "keep-body",
		},
	}
	server := NewMCPServer(store, &mockProbeEngine{}, &mockAlertManager{}, newTestLogger())
	ctx := core.ContextWithWorkspaceID(core.ContextWithUserRole(context.Background(), string(core.RoleViewer)), "default")

	result, err := server.handleGetSoul(ctx, json.RawMessage(`{"soul_id":"soul-1"}`))
	if err != nil {
		t.Fatalf("handleGetSoul failed: %v", err)
	}
	body := mcpTextResult(result)
	if strings.Contains(body, "keep-me") || strings.Contains(body, "keep-body") {
		t.Fatalf("mcp get leaked secrets: %s", body)
	}
	if !strings.Contains(body, redactedSecretValue) || !strings.Contains(body, "has_secrets") {
		t.Fatalf("expected redaction markers in mcp get: %s", body)
	}
}
