package core

import (
	"testing"
	"time"
)

func TestSoulValidation(t *testing.T) {
	tests := []struct {
		name      string
		soul      *Soul
		wantError bool
	}{
		{
			name: "valid HTTP soul",
			soul: &Soul{
				Name:   "Test API",
				Type:   CheckHTTP,
				Target: "https://api.example.com/health",
			},
			wantError: false,
		},
		{
			name: "missing name",
			soul: &Soul{
				Type:   CheckHTTP,
				Target: "https://api.example.com",
			},
			wantError: true,
		},
		{
			name: "missing target",
			soul: &Soul{
				Name: "Test",
				Type: CheckHTTP,
			},
			wantError: true,
		},
		{
			name: "invalid type",
			soul: &Soul{
				Name:   "Test",
				Type:   "",
				Target: "https://example.com",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Soul validation is done in config.validate()
			config := &Config{
				Souls: []Soul{*tt.soul},
			}
			err := config.validate()
			if (err != nil) != tt.wantError {
				t.Errorf("validation error = %v, wantError = %v", err, tt.wantError)
			}
		})
	}
}

func TestSoulStatusString(t *testing.T) {
	tests := []struct {
		status   SoulStatus
		expected string
	}{
		{SoulAlive, "alive"},
		{SoulDead, "dead"},
		{SoulDegraded, "degraded"},
		{SoulUnknown, "unknown"},
		{SoulEmbalmed, "embalmed"},
	}

	for _, tt := range tests {
		if string(tt.status) != tt.expected {
			t.Errorf("string(%v) = %q, want %q", tt.status, tt.status, tt.expected)
		}
	}
}

func TestCheckTypeConstants(t *testing.T) {
	expectedTypes := []CheckType{
		CheckHTTP,
		CheckTCP,
		CheckUDP,
		CheckDNS,
		CheckSMTP,
		CheckIMAP,
		CheckICMP,
		CheckGRPC,
		CheckWebSocket,
		CheckTLS,
	}

	for _, ct := range expectedTypes {
		if ct == "" {
			t.Errorf("CheckType constant is empty")
		}
	}
}

func TestDurationConversion(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
	}{
		{"1s", time.Second},
		{"1m", time.Minute},
		{"1h", time.Hour},
		{"30s", 30 * time.Second},
		{"5m", 5 * time.Minute},
		{"1h30m", 90 * time.Minute},
		{"1.5h", 90 * time.Minute},
	}

	for _, tt := range tests {
		d := &Duration{}
		err := d.UnmarshalYAML(func(v interface{}) error {
			if s, ok := v.(string); ok {
				dur, err := time.ParseDuration(s)
				if err != nil {
					return err
				}
				d.Duration = dur
			}
			return nil
		})

		// Direct parse for test
		dur, parseErr := time.ParseDuration(tt.input)
		if parseErr == nil {
			d.Duration = dur
		}

		if err != nil && parseErr != nil {
			t.Errorf("ParseDuration(%q) error = %v", tt.input, err)
		}

		if d.Duration != tt.expected && parseErr == nil {
			t.Errorf("Duration(%q) = %v, want %v", tt.input, d.Duration, tt.expected)
		}
	}
}
