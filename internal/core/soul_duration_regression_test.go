package core

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The dashboard used to send weight/timeout as bare numbers of seconds, which
// decode as nanoseconds. Such souls must be rejected instead of producing a
// 30ns check loop.
func TestSoulValidateRejectsSubMinimumDurations(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"seconds as number weight", `{"weight":30,"timeout":"10s"}`, "interval must be at least"},
		{"seconds as number timeout", `{"weight":"30s","timeout":10}`, "timeout must be at least"},
		{"sub-second interval string", `{"weight":"500ms"}`, "interval must be at least"},
		{"duration strings", `{"weight":"30s","timeout":"10s"}`, ""},
		{"unset durations use defaults", `{}`, ""},
		{"nanosecond numbers above minimum", `{"weight":30000000000,"timeout":10000000000}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			soul := Soul{Name: "s", Type: CheckTCP, Target: "example.com:443", TCP: &TCPConfig{}}
			if err := json.Unmarshal([]byte(tt.body), &soul); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			err := soul.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestSoulDurationRoundTripsAsString(t *testing.T) {
	soul := Soul{Weight: Duration{30 * time.Second}, Timeout: Duration{10 * time.Second}}
	b, err := json.Marshal(soul)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"weight":"30s"`) || !strings.Contains(string(b), `"timeout":"10s"`) {
		t.Fatalf("unexpected encoding: %s", b)
	}
}
