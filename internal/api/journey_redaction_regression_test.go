package api

import (
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"testing"
)

func TestJourneyRedactionPreservesSecretsSafely(t *testing.T) {
	old := &core.JourneyConfig{Variables: map[string]string{"token": "hidden"}, Steps: []core.JourneyStep{{Name: "login", Type: core.CheckHTTP, Target: "https://example.com", HTTP: &core.HTTPConfig{Method: "POST", Headers: map[string]string{"Authorization": "bearer secret"}, Body: "credential"}}}}
	incoming := RedactJourney(old)
	if incoming.Variables["token"] != "[REDACTED]" || incoming.Steps[0].HTTP.Body != "[REDACTED]" {
		t.Fatal("not masked")
	}
	if old.Variables["token"] != "hidden" || old.Steps[0].HTTP.Body != "credential" {
		t.Fatal("redaction mutated stored journey")
	}
	if err := MergeJourneySecrets(old, incoming); err != nil {
		t.Fatal(err)
	}
	if incoming.Variables["token"] != "hidden" || incoming.Steps[0].HTTP.Headers["Authorization"] != "bearer secret" {
		t.Fatal("roundtrip lost secrets")
	}
	changed := RedactJourney(old)
	changed.Steps[0].Target = "https://attacker.invalid"
	if err := MergeJourneySecrets(old, changed); err == nil {
		t.Fatal("secret reused for changed destination")
	}
	changed = RedactJourney(old)
	changed.Steps = append(changed.Steps, core.JourneyStep{Name: "exfiltrate", Target: "https://attacker.invalid"})
	if err := MergeJourneySecrets(old, changed); err == nil {
		t.Fatal("variables reused for new destination")
	}
	variableTarget := *old
	variableTarget.Steps = append([]core.JourneyStep(nil), old.Steps...)
	variableTarget.Steps[0].Target = "https://{{host}}"
	variableTarget.Variables = map[string]string{"host": "trusted.example"}
	changed = RedactJourney(&variableTarget)
	changed.Variables["host"] = "attacker.example"
	if err := MergeJourneySecrets(&variableTarget, changed); err == nil {
		t.Error("variable change reused masked credentials for another destination")
	}
	changed = RedactJourney(old)
	changed.Steps[0].HTTP.FollowRedirects = true
	if err := MergeJourneySecrets(old, changed); err == nil {
		t.Error("redirect policy change reused masked credentials")
	}
	run := &core.JourneyRun{Variables: map[string]string{"token": "captured"}, Steps: []core.JourneyStepResult{{Extracted: map[string]string{"token": "captured"}}}}
	view := RedactJourneyRun(run)
	if view.Variables["token"] != "[REDACTED]" || view.Steps[0].Extracted["token"] != "[REDACTED]" || run.Variables["token"] != "captured" {
		t.Fatal("run values not safely masked")
	}
}
