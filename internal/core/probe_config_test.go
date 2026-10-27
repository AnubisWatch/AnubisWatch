package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestProbeConfigMaxConcurrentChecks(t *testing.T) {
	tests := []struct {
		name          string
		format        string // yaml | json
		block         string // probe section, empty = omitted
		wantErr       bool
		wantEffective int
	}{
		{name: "omitted uses default", format: "yaml", wantEffective: DefaultMaxConcurrentChecks},
		{name: "zero uses default", format: "yaml", block: "probe:\n  max_concurrent_checks: 0\n", wantEffective: DefaultMaxConcurrentChecks},
		{name: "yaml explicit", format: "yaml", block: "probe:\n  max_concurrent_checks: 250\n", wantEffective: 250},
		{name: "json explicit", format: "json", block: `"probe": {"max_concurrent_checks": 8},`, wantEffective: 8},
		{name: "upper bound accepted", format: "yaml", block: fmt.Sprintf("probe:\n  max_concurrent_checks: %d\n", MaxMaxConcurrentChecks), wantEffective: MaxMaxConcurrentChecks},
		{name: "negative rejected", format: "yaml", block: "probe:\n  max_concurrent_checks: -1\n", wantErr: true},
		{name: "above max rejected", format: "json", block: fmt.Sprintf(`"probe": {"max_concurrent_checks": %d},`, MaxMaxConcurrentChecks+1), wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			var path, body string
			if tc.format == "json" {
				path = filepath.Join(dir, "anubis.json")
				body = fmt.Sprintf(`{%s "server": {"port": 8080}, "storage": {"path": %q}}`, tc.block, dir)
			} else {
				path = filepath.Join(dir, "anubis.yaml")
				body = fmt.Sprintf("server:\n  port: 8080\nstorage:\n  path: %q\n%s", dir, tc.block)
			}
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}

			cfg, err := LoadConfig(path)
			if tc.wantErr {
				var cfgErr *ConfigError
				if !errors.As(err, &cfgErr) || cfgErr.Field != "probe.max_concurrent_checks" {
					t.Fatalf("LoadConfig error = %v, want ConfigError on probe.max_concurrent_checks", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}
			if got := cfg.Probe.EffectiveMaxConcurrentChecks(); got != tc.wantEffective {
				t.Fatalf("EffectiveMaxConcurrentChecks = %d, want %d", got, tc.wantEffective)
			}
		})
	}
}
