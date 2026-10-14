package alert

import (
	"context"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// A dispatcher whose credential lives in the request URL — a Slack/Discord webhook
// URL is the secret itself, a Telegram bot token sits in the path — used to wrap
// client.Do's error verbatim. Go's url.Error prints the whole URL, and the alert
// manager logs the error it gets back ("Failed to send alert", manager.go), so the
// credential reached the log stream. These tests pin that the error still names the
// endpoint but never the secret.
//
// Tokens are generated per run so no credential-shaped literal lives here.

func secretLeakEvent() *core.AlertEvent {
	return &core.AlertEvent{
		ID: "redaction-event", SoulID: "soul-1", SoulName: "redaction soul",
		Status: core.SoulDead, Severity: core.SeverityCritical,
	}
}

func secretLeakChannel(rawURL string) *core.AlertChannel {
	return &core.AlertChannel{
		ID: "redaction-channel", Name: "redaction webhook", Type: core.ChannelWebHook,
		Enabled: true, Config: map[string]any{"url": rawURL},
	}
}

// TestDispatcherErrorRedactsTheCredentialInTheURL covers both places a secret hides
// in a URL: the query string and the path.
func TestDispatcherErrorRedactsTheCredentialInTheURL(t *testing.T) {
	token := "tok-" + core.GenerateID() + "-" + core.GenerateID()

	for _, tc := range []struct{ name, rawURL string }{
		{"secret in the query", "http://127.0.0.1:1/hook?token=" + token},
		{"secret in the path", "http://127.0.0.1:1/hook-" + token},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Port 1 is closed, so the send fails at the transport and the error
			// carries the URL — the same shape a real outage or bad host produces.
			err := (&WebHookDispatcher{logger: newTestLogger()}).Send(
				context.Background(), secretLeakEvent(), secretLeakChannel(tc.rawURL))
			if err == nil {
				t.Fatal("control: sending to a closed port must fail, so the failure path is exercised")
			}
			if strings.Contains(err.Error(), token) {
				t.Errorf("the error still carries the credential from the URL: %v", err)
			}
			if !strings.Contains(err.Error(), "127.0.0.1") {
				t.Errorf("the error no longer identifies the endpoint, so it is useless for diagnosis: %v", err)
			}
		})
	}
}
