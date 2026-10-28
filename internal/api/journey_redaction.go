package api

import (
	"fmt"
	"slices"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// RedactJourney returns an independently owned view safe for read-only clients.
// Arbitrary variables, request headers and payloads may all hold credentials.
func RedactJourney(j *core.JourneyConfig) *core.JourneyConfig {
	if j == nil {
		return nil
	}
	out := *j
	out.Variables = maskJourneyValues(j.Variables)
	out.Steps = slices.Clone(j.Steps)
	for i := range out.Steps {
		step := &out.Steps[i]
		if _, secret := redactTarget(step.Target); secret {
			step.Target = redactedSecretValue
		}
		if step.HTTP != nil {
			h := *step.HTTP
			h.Headers = maskJourneyValues(h.Headers)
			h.ResponseHeaders = maskJourneyValues(h.ResponseHeaders)
			h.Body = redactSecretValue(h.Body)
			step.HTTP = &h
		}
		if step.TCP != nil {
			v := *step.TCP
			v.Send = redactSecretValue(v.Send)
			step.TCP = &v
		}
		if step.UDP != nil {
			v := *step.UDP
			v.SendHex = redactSecretValue(v.SendHex)
			step.UDP = &v
		}
	}
	return &out
}
func maskJourneyValues(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	out := make(map[string]string, len(values))
	for k, v := range values {
		out[k] = redactSecretValue(v)
	}
	return out
}

// MergeJourneySecrets preserves masks only when the step destinations are unchanged.
// A caller changing a destination must supply fresh credentials explicitly.
func MergeJourneySecrets(old, incoming *core.JourneyConfig) error {
	if old == nil || incoming == nil {
		return nil
	}
	changed := len(old.Steps) != len(incoming.Steps)
	// Variables can choose destinations even when the raw target is unchanged.
	// Require fresh secrets whenever a variable is edited rather than a mask.
	for key, value := range incoming.Variables {
		if value != redactedSecretValue && value != "***" && value != old.Variables[key] {
			changed = true
		}
	}
	for i := range incoming.Steps {
		if i >= len(old.Steps) {
			continue
		}
		a, b := &old.Steps[i], &incoming.Steps[i]
		if a.Name == b.Name && a.Type == b.Type && b.Target == redactedSecretValue {
			b.Target = a.Target
		}
		if a.Name != b.Name || a.Type != b.Type || a.Target != b.Target || (a.HTTP != nil && b.HTTP != nil && (a.HTTP.Method != b.HTTP.Method || a.HTTP.FollowRedirects != b.HTTP.FollowRedirects || a.HTTP.MaxRedirects != b.HTTP.MaxRedirects)) {
			changed = true
		}
	}
	if changed {
		if journeyHasMasks(incoming) {
			return fmt.Errorf("journey destination changed: secrets must be re-entered")
		}
		return nil
	}
	incoming.Variables = mergeSecretStringMap(old.Variables, incoming.Variables)
	for i := range incoming.Steps {
		a, b := &old.Steps[i], &incoming.Steps[i]
		if a.HTTP != nil && b.HTTP != nil {
			b.HTTP.Headers = mergeSecretStringMap(a.HTTP.Headers, b.HTTP.Headers)
			b.HTTP.ResponseHeaders = mergeSecretStringMap(a.HTTP.ResponseHeaders, b.HTTP.ResponseHeaders)
			b.HTTP.Body = preserveSecretString(a.HTTP.Body, b.HTTP.Body)
		}
		if a.TCP != nil && b.TCP != nil {
			b.TCP.Send = preserveSecretString(a.TCP.Send, b.TCP.Send)
		}
		if a.UDP != nil && b.UDP != nil {
			b.UDP.SendHex = preserveSecretString(a.UDP.SendHex, b.UDP.SendHex)
		}
	}
	return nil
}
func journeyHasMasks(j *core.JourneyConfig) bool {
	mask := func(v string) bool { return v == redactedSecretValue || v == "***" }
	for _, v := range j.Variables {
		if mask(v) {
			return true
		}
	}
	for _, s := range j.Steps {
		if mask(s.Target) {
			return true
		}
		if s.HTTP != nil {
			if mask(s.HTTP.Body) {
				return true
			}
			for _, v := range s.HTTP.Headers {
				if mask(v) {
					return true
				}
			}
			for _, v := range s.HTTP.ResponseHeaders {
				if mask(v) {
					return true
				}
			}
		}
		if s.TCP != nil && mask(s.TCP.Send) {
			return true
		}
		if s.UDP != nil && mask(s.UDP.SendHex) {
			return true
		}
	}
	return false
}

// RedactJourneyRun masks captured tokens without mutating persisted runs.
func RedactJourneyRun(run *core.JourneyRun) *core.JourneyRun {
	if run == nil {
		return nil
	}
	out := *run
	out.Variables = maskJourneyValues(run.Variables)
	out.Steps = slices.Clone(run.Steps)
	for i := range out.Steps {
		out.Steps[i].Extracted = maskJourneyValues(out.Steps[i].Extracted)
	}
	return &out
}
