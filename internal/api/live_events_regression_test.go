package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Regression: only judgments ever reached the WebSocket. Incidents, alerts
// and soul writes had broadcast helpers that nothing called, so the
// dashboard's live feed and incident inbox never updated on their own.

func newLiveEventServer(t *testing.T, store Storage) (*RESTServer, map[string]*WSClient) {
	t.Helper()
	server := newTestServerWithStorage(store)
	t.Cleanup(server.ws.Stop)
	clients := map[string]*WSClient{}
	for _, ws := range []string{"ws1", "ws2"} {
		client := &WSClient{
			ID:        ws + "-client",
			Workspace: ws,
			Rooms:     make(map[string]bool),
			send:      make(chan []byte, 16),
			server:    server.ws,
		}
		server.ws.mu.Lock()
		server.ws.clients[client.ID] = client
		server.ws.mu.Unlock()
		// The dashboard subscribes to exactly these events (useWebSocket.tsx).
		server.ws.SubscribeClient(client.ID, []string{"judgment", "alert", "incident", "soul", "stats", "status"})
		clients[ws] = client
	}
	return server, clients
}

// decodeOne asserts the client got exactly one message and decodes it.
func decodeOne(t *testing.T, client *WSClient) (string, map[string]any, string) {
	t.Helper()
	messages := drainWSMessages(client.send)
	if len(messages) != 1 {
		t.Fatalf("%s: expected exactly one message, got %d: %v", client.ID, len(messages), messages)
	}
	var msg struct {
		Type    string         `json:"type"`
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal([]byte(messages[0]), &msg); err != nil {
		t.Fatalf("decode %s: %v", messages[0], err)
	}
	return msg.Type, msg.Payload, messages[0]
}

func expectSilent(t *testing.T, client *WSClient) {
	t.Helper()
	if messages := drainWSMessages(client.send); len(messages) != 0 {
		t.Fatalf("%s must not receive another workspace's event, got %v", client.ID, messages)
	}
}

func TestLiveEvents_IncidentReachesOnlyItsWorkspace(t *testing.T) {
	server, clients := newLiveEventServer(t, newMockStorage())

	acked := time.Now().UTC()
	server.OnIncidentCallback()(&core.Incident{
		ID:          "inc-1",
		SoulID:      "soul-1",
		SoulName:    "API",
		WorkspaceID: "ws1",
		Status:      core.IncidentAcked,
		Severity:    core.SeverityCritical,
		AckedAt:     &acked,
		Events:      []core.AlertEvent{{ID: "evt-1", Message: "boom"}},
	})

	msgType, payload, raw := decodeOne(t, clients["ws1"])
	if msgType != "incident" {
		t.Fatalf("type = %q, want incident", msgType)
	}
	for key, want := range map[string]any{"id": "inc-1", "status": "acknowledged", "severity": "critical", "soul_name": "API", "soul_id": "soul-1"} {
		if payload[key] != want {
			t.Errorf("payload[%q] = %#v, want %#v (%s)", key, payload[key], want, raw)
		}
	}
	if _, ok := payload["events"]; ok {
		t.Errorf("incident payload should be a summary without the event history: %s", raw)
	}
	expectSilent(t, clients["ws2"])
}

func TestLiveEvents_AlertIsRedactedAndScoped(t *testing.T) {
	server, clients := newLiveEventServer(t, newMockStorage())

	server.OnAlertCallback()(&core.AlertEvent{
		ID:          "alert-1",
		SoulID:      "soul-1",
		SoulName:    "API",
		WorkspaceID: "ws2",
		Severity:    core.SeverityWarning,
		Message:     "latency high",
		Judgment: &core.Judgment{
			ID:      "j-1",
			Details: &core.JudgmentDetails{ResponseBody: "session=SECRET-BODY", ResponseHeaders: map[string]string{"Set-Cookie": "SECRET-HEADER"}},
		},
	})

	msgType, payload, raw := decodeOne(t, clients["ws2"])
	if msgType != "alert" || payload["message"] != "latency high" || payload["severity"] != "warning" || payload["soul_name"] != "API" {
		t.Fatalf("unexpected alert message: %s", raw)
	}
	if strings.Contains(raw, "SECRET-") {
		t.Fatalf("alert payload leaked raw response material: %s", raw)
	}
	expectSilent(t, clients["ws1"])
}

func TestLiveEvents_SoulChangeIsRedactedAndScoped(t *testing.T) {
	store := newMockStorage()
	server, clients := newLiveEventServer(t, store)
	soul := &core.Soul{
		ID:          "soul-1",
		Name:        "Checkout",
		WorkspaceID: "ws1",
		Type:        core.CheckHTTP,
		Target:      "https://example.com",
		HTTP:        &core.HTTPConfig{Headers: map[string]string{"Authorization": "Bearer SECRET-TOKEN"}},
	}
	store.souls[soul.ID] = soul
	store.judgments["j-1"] = &core.Judgment{ID: "j-1", SoulID: soul.ID, Status: core.SoulDead, Timestamp: time.Now().Add(-time.Minute)}

	cases := []struct {
		action     string
		wantStatus string
	}{
		{"created", "unhealthy"},
		{"updated", "unhealthy"},
		{"deleted", "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			server.OnSoulChangeCallback()(soul, tc.action)
			msgType, payload, raw := decodeOne(t, clients["ws1"])
			if msgType != "soul_update" {
				t.Fatalf("type = %q, want soul_update", msgType)
			}
			if payload["id"] != "soul-1" || payload["name"] != "Checkout" || payload["action"] != tc.action || payload["status"] != tc.wantStatus {
				t.Fatalf("unexpected soul_update payload: %s", raw)
			}
			if strings.Contains(raw, "SECRET-TOKEN") {
				t.Fatalf("soul_update leaked a credential: %s", raw)
			}
			expectSilent(t, clients["ws2"])
		})
	}
}

func TestLiveEvents_BroadcastSoulUpdateRedactsCredentials(t *testing.T) {
	server, clients := newLiveEventServer(t, newMockStorage())
	server.ws.BroadcastSoulUpdate(&core.Soul{
		ID:          "soul-2",
		Name:        "Mail",
		WorkspaceID: "ws2",
		HTTP:        &core.HTTPConfig{Headers: map[string]string{"Authorization": "SECRET-TOKEN"}},
	})
	_, payload, raw := decodeOne(t, clients["ws2"])
	if payload["name"] != "Mail" || strings.Contains(raw, "SECRET-TOKEN") {
		t.Fatalf("BroadcastSoulUpdate must send the redacted soul view: %s", raw)
	}
	expectSilent(t, clients["ws1"])
}

func TestLiveEvents_JudgmentCarriesSoulNameAndIsRedacted(t *testing.T) {
	store := newMockStorage()
	store.souls["soul-1"] = &core.Soul{ID: "soul-1", Name: "Checkout", WorkspaceID: "ws1"}
	server, clients := newLiveEventServer(t, store)

	server.OnJudgmentCallback()(&core.Judgment{
		ID:          "j-1",
		SoulID:      "soul-1",
		WorkspaceID: "ws1",
		Status:      core.SoulAlive,
		Details:     &core.JudgmentDetails{ResponseBody: "SECRET-BODY"},
	})

	msgType, payload, raw := decodeOne(t, clients["ws1"])
	if msgType != "judgment" || payload["soul_name"] != "Checkout" || payload["status"] != "alive" {
		t.Fatalf("unexpected judgment message: %s", raw)
	}
	if strings.Contains(raw, "SECRET-BODY") {
		t.Fatalf("judgment broadcast leaked the response body: %s", raw)
	}
	expectSilent(t, clients["ws2"])
}

func TestLiveEvents_StatsWithoutWorkspaceStayInDefault(t *testing.T) {
	server, clients := newLiveEventServer(t, newMockStorage())
	server.ws.BroadcastStats("", map[string]int{"total": 1})
	time.Sleep(20 * time.Millisecond) // the old code also pushed through the async global loop
	expectSilent(t, clients["ws1"])
	expectSilent(t, clients["ws2"])
}
