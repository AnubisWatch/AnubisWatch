package core

import (
	"strings"
	"testing"
)

func TestDistributionConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     DistributionConfig
		wantErr string // substring; empty means valid
	}{
		{"zero value", DistributionConfig{}, ""},
		{"majority", DistributionConfig{Quorum: QuorumMajority, Redundancy: 3}, ""},
		{"any", DistributionConfig{Quorum: QuorumAny, Redundancy: 3}, ""},
		{"all", DistributionConfig{Quorum: QuorumAll, Redundancy: 3}, ""},
		{"min_down within redundancy", DistributionConfig{Redundancy: 5, MinDown: 3}, ""},
		{"min_down equal to redundancy", DistributionConfig{Redundancy: 3, MinDown: 3}, ""},
		{"min_down 1 without redundancy", DistributionConfig{Redundancy: 1, MinDown: 1}, ""},
		{"unknown quorum", DistributionConfig{Quorum: "most"}, "is not one of majority, any, all"},
		{"quorum is case sensitive", DistributionConfig{Quorum: "Majority"}, "is not one of"},
		{"negative min_down", DistributionConfig{Redundancy: 3, MinDown: -1}, "must not be negative"},
		{"min_down above redundancy", DistributionConfig{Redundancy: 3, MinDown: 4}, "exceeds"},
		{"min_down above default redundancy", DistributionConfig{Redundancy: 1, MinDown: 2}, "exceeds"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.validate()
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("error = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
}

// A config that omits quorum gets the strict majority, and a bad one is
// rejected by the config-level validation, not only by the helper.
func TestConfigValidateAppliesAndChecksQuorum(t *testing.T) {
	newCfg := func() *Config {
		return &Config{Logging: LoggingConfig{Level: "info", Format: "json", Output: "stdout"}}
	}
	cfg := newCfg()
	if err := cfg.validate(); err != nil {
		t.Fatalf("baseline config invalid: %v", err)
	}
	if cfg.Necropolis.Distribution.Quorum != QuorumMajority {
		t.Errorf("default quorum = %q, want %q", cfg.Necropolis.Distribution.Quorum, QuorumMajority)
	}

	cfg = newCfg()
	cfg.Necropolis.Distribution.Quorum = "sometimes"
	if err := cfg.validate(); err == nil {
		t.Error("an unknown quorum passed config validation")
	}
	cfg = newCfg()
	cfg.Necropolis.Distribution.Redundancy = 2
	cfg.Necropolis.Distribution.MinDown = 3
	if err := cfg.validate(); err == nil {
		t.Error("min_down above redundancy passed config validation")
	}
}
