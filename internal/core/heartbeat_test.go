package core

import (
	"strings"
	"testing"
	"time"
)

func TestHeartbeatSoulValidation(t *testing.T) {
	base := func() Soul {
		s := Soul{Name: "nightly backup", Type: CheckHeartbeat, Weight: Duration{time.Hour}}
		s.ApplyDefaults()
		return s
	}

	s := base()
	if s.Heartbeat == nil || !ValidHeartbeatToken(s.Heartbeat.Token) {
		t.Fatalf("ApplyDefaults did not generate a valid token: %+v", s.Heartbeat)
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("heartbeat soul without target must validate: %v", err)
	}
	other := base()
	if other.Heartbeat.Token == s.Heartbeat.Token {
		t.Fatal("two generated tokens are equal")
	}

	tests := []struct {
		name   string
		mutate func(*Soul)
		field  string
	}{
		{"missing config", func(s *Soul) { s.Heartbeat = nil }, "heartbeat"},
		{"short token", func(s *Soul) { s.Heartbeat.Token = "abc" }, "heartbeat.token"},
		{"bad token chars", func(s *Soul) { s.Heartbeat.Token = strings.Repeat("a", 40) + "/x" }, "heartbeat.token"},
		{"negative grace", func(s *Soul) { s.Heartbeat.Grace = Duration{-time.Second} }, "heartbeat.grace"},
		{"tiny grace", func(s *Soul) { s.Heartbeat.Grace = Duration{time.Millisecond} }, "heartbeat.grace"},
		{"huge grace", func(s *Soul) { s.Heartbeat.Grace = Duration{8 * 24 * time.Hour} }, "heartbeat.grace"},
		{"huge interval", func(s *Soul) { s.Weight = Duration{32 * 24 * time.Hour} }, "weight"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := base()
			tt.mutate(&s)
			err := s.Validate()
			ce, ok := err.(*ConfigError)
			if !ok || ce.Field != tt.field {
				t.Fatalf("Validate() = %v, want ConfigError on %q", err, tt.field)
			}
		})
	}

	// Other types still require a target.
	httpSoul := Soul{Name: "x", Type: CheckHTTP}
	httpSoul.ApplyDefaults()
	if err := httpSoul.Validate(); err == nil {
		t.Fatal("http soul without target must not validate")
	}
	// Config-file souls are validated without ApplyDefaults: a supplied
	// token is accepted, a missing one is refused.
	cfgSoul := Soul{Name: "cfg", Type: CheckHeartbeat, Heartbeat: &HeartbeatConfig{Token: strings.Repeat("ab", 32)}}
	if err := cfgSoul.validate(0); err != nil {
		t.Fatalf("config heartbeat soul with token: %v", err)
	}
	cfgSoul.Heartbeat.Token = ""
	if err := cfgSoul.validate(0); err == nil {
		t.Fatal("config heartbeat soul without token must not validate")
	}
}

func TestHeartbeatDefaults(t *testing.T) {
	s := Soul{Type: CheckHeartbeat}
	if got := s.HeartbeatInterval(); got != DefaultHeartbeatInterval {
		t.Fatalf("interval = %s", got)
	}
	s.Weight = Duration{10 * time.Minute}
	if got := s.HeartbeatGrace(); got != 5*time.Minute {
		t.Fatalf("default grace = %s, want half the interval", got)
	}
	s.Heartbeat = &HeartbeatConfig{Grace: Duration{time.Minute}}
	if got := s.HeartbeatGrace(); got != time.Minute {
		t.Fatalf("grace = %s", got)
	}
}

func TestHeartbeatStateApply(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var st HeartbeatState
	st.Apply(HeartbeatStart, t0, "", "1.2.3.4", "n1")
	if !st.LastPingAt.IsZero() || st.Pings != 0 {
		t.Fatal("start must not count as a ping")
	}
	st.Apply(HeartbeatSuccess, t0.Add(90*time.Second), "ignored", "1.2.3.4", "n1")
	if st.LastDuration != 90*time.Second || st.LastEvent != HeartbeatSuccess || st.Pings != 1 || st.LastMessage != "" {
		t.Fatalf("after success: %+v", st)
	}
	// A second success without a new start measures nothing.
	st.Apply(HeartbeatSuccess, t0.Add(3*time.Minute), "", "", "n2")
	if st.LastDuration != 0 {
		t.Fatalf("duration without start = %s", st.LastDuration)
	}
	st.Apply(HeartbeatFail, t0.Add(4*time.Minute), strings.Repeat("x", 4000), "", "n2")
	if st.LastEvent != HeartbeatFail || len(st.LastMessage) != MaxHeartbeatMessage {
		t.Fatalf("after fail: event=%s len=%d", st.LastEvent, len(st.LastMessage))
	}
	if HeartbeatTokenHash("a") == HeartbeatTokenHash("b") || len(HeartbeatTokenHash("a")) != 64 {
		t.Fatal("token hash")
	}
	if HeartbeatStateKey("", "s1") != "default/heartbeats/s1" {
		t.Fatal("state key")
	}
}
