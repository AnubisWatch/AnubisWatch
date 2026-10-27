package core

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// The configuration reference documents ${VAR:+alternate}: the alternate is
// selected when VAR exists, including when its value is empty.
func TestLoadConfigExpandsAlternateEnvironmentSyntax(t *testing.T) {
	t.Setenv("ANUBIS_HOST", "")
	t.Setenv("ANUBIS_R6_PRESENT", "configured")
	t.Setenv("ANUBIS_R6_EMPTY", "")
	t.Setenv("ANUBIS_R6_PLAIN", "127.0.0.4")

	const unsetName = "ANUBIS_R6_UNSET"
	previous, previouslySet := os.LookupEnv(unsetName)
	if err := os.Unsetenv(unsetName); err != nil {
		t.Fatalf("unset %s: %v", unsetName, err)
	}
	t.Cleanup(func() {
		if previouslySet {
			_ = os.Setenv(unsetName, previous)
		} else {
			_ = os.Unsetenv(unsetName)
		}
	})

	tests := []struct {
		name       string
		expression string
		wantHost   string
	}{
		{name: "present non-empty selects alternate", expression: "${ANUBIS_R6_PRESENT:+127.0.0.1}", wantHost: "127.0.0.1"},
		{name: "present empty selects alternate", expression: "${ANUBIS_R6_EMPTY:+127.0.0.2}", wantHost: "127.0.0.2"},
		{name: "absent expands empty then defaults", expression: "${ANUBIS_R6_UNSET:+127.0.0.3}", wantHost: "0.0.0.0"},
		{name: "plain syntax control", expression: "${ANUBIS_R6_PLAIN}", wantHost: "127.0.0.4"},
		{name: "default syntax control", expression: "${ANUBIS_R6_EMPTY:-127.0.0.5}", wantHost: "127.0.0.5"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			configPath := filepath.Join(t.TempDir(), "anubis.yaml")
			body := fmt.Sprintf("server:\n  host: %q\n  port: 8080\nstorage:\n  path: %q\n", tc.expression, t.TempDir())
			if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}

			cfg, err := LoadConfig(configPath)
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}
			if cfg.Server.Host != tc.wantHost {
				t.Fatalf("host = %q, want %q", cfg.Server.Host, tc.wantHost)
			}
		})
	}
}
