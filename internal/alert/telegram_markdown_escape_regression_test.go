package alert

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type captureRoundTripper struct{ body []byte }

func (c *captureRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	c.body, _ = io.ReadAll(req.Body)
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}")), Header: make(http.Header), Request: req}, nil
}

func sendTelegramCaptured(t *testing.T, soulName, message string, details map[string]string) (text, parseMode string) {
	t.Helper()
	client := getHTTPClient()
	prev := client.Transport
	rt := &captureRoundTripper{}
	client.Transport = rt
	t.Cleanup(func() { client.Transport = prev })

	err := (&TelegramDispatcher{logger: newTestLogger()}).Send(context.Background(), &core.AlertEvent{
		SoulName: soulName, Status: core.SoulDead, Severity: core.SeverityCritical,
		Message: message, Details: details, Timestamp: time.Unix(0, 0).UTC(),
	}, &core.AlertChannel{Type: core.ChannelTelegram, Config: map[string]any{"bot_token": "123:abc", "chat_id": "1"}})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	var payload map[string]string
	if err := json.Unmarshal(rt.body, &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	return payload["text"], payload["parse_mode"]
}

// The message is sent with parse_mode=Markdown, where an unbalanced `_`, `*`,
// backtick or `[` in user-controlled text makes Telegram reject the whole
// message ("can't parse entities") — the alert is silently lost. Soul names
// like "billing_api" are ordinary, so the dispatcher must escape them.
func TestTelegramEscapesMarkdownInUserText(t *testing.T) {
	text, mode := sendTelegramCaptured(t, "plain", "ok", nil)
	if mode != "Markdown" || !strings.Contains(text, "*Soul:* plain") {
		t.Fatalf("control: mode=%q text=%q", mode, text)
	}

	text, _ = sendTelegramCaptured(t, "billing_api", "disk *full* at [node_1] `x`", map[string]string{"k_ey": "v_al"})
	for _, raw := range []string{"billing_api", "*full*", "[node_1]", "`x`", "k_ey", "v_al"} {
		if strings.Contains(text, raw) {
			t.Errorf("unescaped Markdown text %q reached Telegram: %q", raw, text)
		}
	}
	for _, escaped := range []string{`billing\_api`, `\*full\*`, `\[node\_1]`, "\\`x\\`", `k\_ey`, `v\_al`} {
		if !strings.Contains(text, escaped) {
			t.Errorf("missing escaped text %q in %q", escaped, text)
		}
	}
	// The dispatcher's own formatting must stay live.
	if !strings.Contains(text, "*Soul:*") || !strings.Contains(text, "_AnubisWatch") {
		t.Errorf("scaffolding formatting was escaped: %q", text)
	}
}
