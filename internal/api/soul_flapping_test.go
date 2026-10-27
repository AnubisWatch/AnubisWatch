package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// The soul payloads (REST monitor view, WS soul_update, MCP) carry the retry
// settings and a flapping flag taken from the latest judgment; status keeps
// its usual value.
func TestSoulPayloadsExposeRetriesAndFlapping(t *testing.T) {
	store := newMockStorage()
	soul := &core.Soul{
		ID: "s1", WorkspaceID: "default", Name: "api", Type: core.CheckTCP, Target: "example.com:443",
		Retries: 2, RetryInterval: core.Duration{Duration: 3 * time.Second},
		FlapDetection: &core.FlapDetectionConfig{Enabled: true, Threshold: 4},
	}
	store.souls[soul.ID] = soul
	now := time.Now()
	store.judgments["old"] = &core.Judgment{ID: "old", SoulID: "s1", Status: core.SoulAlive, Timestamp: now.Add(-2 * time.Minute)}
	store.judgments["new"] = &core.Judgment{ID: "new", SoulID: "s1", Status: core.SoulDead, Timestamp: now.Add(-time.Minute),
		Details: &core.JudgmentDetails{Flapping: true, Attempts: 3}}

	rest := &RESTServer{store: store, logger: newTestLogger()}
	view := rest.soulWithLatestJudgmentAt(soul, nil, now)
	if !view.Flapping || view.Status != "unhealthy" {
		t.Fatalf("REST view flapping=%v status=%s, want true/unhealthy", view.Flapping, view.Status)
	}
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"flapping":true`, `"retries":2`, `"retry_interval":"3s"`, `"flap_detection":{"enabled":true,"threshold":4}`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("REST payload missing %s: %s", want, raw)
		}
	}

	mcp := &MCPServer{store: store}
	got, err := mcp.handleGetSoul(context.Background(), json.RawMessage(`{"soul_id":"s1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if dto, ok := got.(mcpSoulDTO); !ok || !dto.Flapping || dto.Retries != 2 {
		t.Fatalf("MCP get_soul = %#v", got)
	}

	// Once the latest judgment is no longer flagged, the flag clears.
	store.judgments["newest"] = &core.Judgment{ID: "newest", SoulID: "s1", Status: core.SoulAlive, Timestamp: now.Add(-time.Second),
		Details: &core.JudgmentDetails{FlapChange: core.FlapStopped}}
	if rest.soulWithLatestJudgmentAt(soul, nil, now).Flapping {
		t.Fatal("flag should clear after a stopped judgment")
	}
}
