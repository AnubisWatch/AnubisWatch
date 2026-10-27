package core

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func retryTestSoul() Soul {
	s := Soul{Name: "api", Type: CheckTCP, Target: "example.com:443", Weight: Duration{Duration: 30 * time.Second}}
	s.ApplyDefaults()
	return s
}

func TestSoulValidateRetries(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*Soul)
		wantField string
	}{
		{"defaults", func(s *Soul) {}, ""},
		{"max retries", func(s *Soul) { s.Retries = MaxSoulRetries; s.RetryInterval = Duration{Duration: 5 * time.Second} }, ""},
		{"negative retries", func(s *Soul) { s.Retries = -1 }, "retries"},
		{"too many retries", func(s *Soul) { s.Retries = MaxSoulRetries + 1 }, "retries"},
		{"negative interval", func(s *Soul) { s.RetryInterval = Duration{Duration: -time.Second} }, "retry_interval"},
		{"interval below minimum", func(s *Soul) { s.RetryInterval = Duration{Duration: time.Millisecond} }, "retry_interval"},
		{"interval not below check interval", func(s *Soul) { s.RetryInterval = Duration{Duration: 30 * time.Second} }, "retry_interval"},
		{"interval below default check interval", func(s *Soul) {
			s.Weight = Duration{}
			s.RetryInterval = Duration{Duration: 59 * time.Second}
		}, ""},
		{"flap threshold too low", func(s *Soul) { s.FlapDetection = &FlapDetectionConfig{Enabled: true, Threshold: 1} }, "flap_detection.threshold"},
		{"flap window too short", func(s *Soul) {
			s.FlapDetection = &FlapDetectionConfig{Enabled: true, Window: Duration{Duration: time.Second}}
		}, "flap_detection.window"},
		{"flap valid", func(s *Soul) {
			s.FlapDetection = &FlapDetectionConfig{Enabled: true, Threshold: 4, Window: Duration{Duration: 5 * time.Minute}}
		}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := retryTestSoul()
			tt.mutate(&s)
			err := s.Validate()
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			ce, ok := err.(*ConfigError)
			if !ok || ce.Field != tt.wantField {
				t.Fatalf("error = %v, want field %q", err, tt.wantField)
			}
		})
	}
}

func TestEffectiveRetrySettings(t *testing.T) {
	s := Soul{Type: CheckHTTP}
	if s.EffectiveRetries() != 0 || s.EffectiveRetryInterval() != DefaultRetryInterval {
		t.Fatalf("defaults: retries=%d interval=%s", s.EffectiveRetries(), s.EffectiveRetryInterval())
	}
	s.Retries = 99
	if s.EffectiveRetries() != MaxSoulRetries {
		t.Fatalf("clamp: %d", s.EffectiveRetries())
	}
	s.Type = CheckHeartbeat
	if s.EffectiveRetries() != 0 {
		t.Fatal("heartbeat souls must not retry")
	}
}

func TestResolveFlapSettings(t *testing.T) {
	got := ResolveFlapSettings(nil, FlapDetectionConfig{})
	if got.Enabled || got.Threshold != DefaultFlapThreshold || got.Window != DefaultFlapWindow {
		t.Fatalf("defaults = %+v", got)
	}
	global := FlapDetectionConfig{Enabled: true, Threshold: 8, Window: Duration{Duration: time.Hour}}
	got = ResolveFlapSettings(nil, global)
	if !got.Enabled || got.Threshold != 8 || got.Window != time.Hour {
		t.Fatalf("global = %+v", got)
	}
	got = ResolveFlapSettings(&FlapDetectionConfig{Enabled: false}, global)
	if got.Enabled || got.Threshold != 8 {
		t.Fatalf("soul disable = %+v", got)
	}
	got = ResolveFlapSettings(&FlapDetectionConfig{Enabled: true, Threshold: 3}, FlapDetectionConfig{})
	if !got.Enabled || got.Threshold != 3 || got.Window != DefaultFlapWindow || got.StopBelow() != 2 {
		t.Fatalf("soul enable = %+v stopBelow=%d", got, got.StopBelow())
	}
}

func TestSoulRetryFieldsSerialization(t *testing.T) {
	plain, err := json.Marshal(Soul{Name: "a"})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"retries", "retry_interval", "flap_detection"} {
		if strings.Contains(string(plain), key) {
			t.Fatalf("unset %s should be omitted: %s", key, plain)
		}
	}

	plainYAML, err := yaml.Marshal(Soul{Name: "a"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plainYAML), "retr") || strings.Contains(string(plainYAML), "flap") {
		t.Fatalf("unset retry/flap fields should be omitted from YAML: %s", plainYAML)
	}

	var fromJSON Soul
	if err := json.Unmarshal([]byte(`{"retries":2,"retry_interval":"3s","flap_detection":{"enabled":true,"threshold":4,"window":"15m"}}`), &fromJSON); err != nil {
		t.Fatal(err)
	}
	var fromYAML Soul
	if err := yaml.Unmarshal([]byte("retries: 2\nretry_interval: 3s\nflap_detection:\n  enabled: true\n  threshold: 4\n  window: 15m\n"), &fromYAML); err != nil {
		t.Fatal(err)
	}
	for name, s := range map[string]Soul{"json": fromJSON, "yaml": fromYAML} {
		if s.Retries != 2 || s.RetryInterval.Duration != 3*time.Second || s.FlapDetection == nil ||
			!s.FlapDetection.Enabled || s.FlapDetection.Threshold != 4 || s.FlapDetection.Window.Duration != 15*time.Minute {
			t.Fatalf("%s decode = %+v %+v", name, s, s.FlapDetection)
		}
	}
}

func TestProbeConfigValidatesFlapDetection(t *testing.T) {
	c := ProbeConfig{FlapDetection: FlapDetectionConfig{Enabled: true, Threshold: 500}}
	err := c.validate()
	if ce, ok := err.(*ConfigError); !ok || ce.Field != "probe.flap_detection.threshold" {
		t.Fatalf("err = %v", err)
	}
}
