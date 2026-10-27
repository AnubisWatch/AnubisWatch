package alert

// Regression test: buildEmailBody renders user-controlled SoulName, Message
// and Details keys/values into the HTML alert email. They must be HTML-
// escaped, matching the statuspage handler's html.EscapeString convention,
// so a soul name or check message cannot inject markup into operator emails.

import (
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestEmailBodyHTMLEscapesUntrustedFields(t *testing.T) {
	d := &EmailDispatcher{logger: newTestLogger()}
	payload := "<img src=x onerror=alert(1)>"
	event := &core.AlertEvent{
		ID:        "regression",
		SoulID:    "soul",
		SoulName:  payload,
		Status:    core.SoulDead,
		Severity:  core.SeverityCritical,
		Message:   payload,
		Timestamp: time.Now().UTC(),
		Details:   map[string]string{"<b>key": "<script>alert(2)</script>"},
	}

	body := d.buildEmailBody(event)

	// Control: the benign template scaffolding must still be present.
	for _, want := range []string{"<!DOCTYPE html>", "<strong>Soul:</strong>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("control failed: body missing %q", want)
		}
	}

	for _, field := range []string{payload, "<script>alert(2)</script>"} {
		if strings.Contains(body, field) {
			t.Fatalf("untrusted HTML reached the email body unescaped: %q", field)
		}
	}
	if !strings.Contains(body, "&lt;img src=x") {
		t.Fatalf("soul name not HTML-escaped in email body")
	}
}
