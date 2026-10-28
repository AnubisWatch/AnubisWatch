package main

import (
	"strings"
	"testing"
)

// `anubis config show` prints a config file with credential values masked. A
// YAML plain scalar may contain spaces (a passphrase), and the mask stopped at
// the first one, printing the rest of the secret next to the mask. Block
// scalars (`key: |`) kept their whole body visible for the same reason.
func TestRedactConfigSecretsMasksWholeValue(t *testing.T) {
	// Control: single-token values, quoted values and comments keep working.
	control := "auth:\n  admin_password: oneword\n  token: \"quoted value with spaces\"\n  bot_token: 'single quoted'\nlogging:\n  level: debug # trailing comment\n"
	out := redactConfigSecrets(control)
	for _, secret := range []string{"oneword", "quoted value with spaces", "single quoted"} {
		if strings.Contains(out, secret) {
			t.Fatalf("control: secret %q visible:\n%s", secret, out)
		}
	}
	if !strings.Contains(out, "level: debug # trailing comment") {
		t.Fatalf("control: non-secret line changed:\n%s", out)
	}

	cases := []struct {
		name, in string
		secrets  []string
		keep     []string
	}{
		{
			name:    "plain scalar with spaces",
			in:      "auth:\n  admin_password: correct horse battery staple\nnext: kept\n",
			secrets: []string{"horse", "battery", "staple"},
			keep:    []string{"next: kept"},
		},
		{
			name:    "plain scalar followed by comment",
			in:      "storage:\n  password: first second # rotate monthly\n",
			secrets: []string{"first", "second"},
			keep:    []string{"# rotate monthly"},
		},
		{
			name:    "block scalar",
			in:      "tls:\n  secret: |\n    line-one-of-secret\n    line-two-of-secret\n  other: kept\n",
			secrets: []string{"line-one-of-secret", "line-two-of-secret"},
			keep:    []string{"other: kept"},
		},
		{
			name:    "folded block scalar",
			in:      "tls:\n  api_key: >-\n    folded part one\n    folded part two\nafter: kept\n",
			secrets: []string{"folded part one", "folded part two"},
			keep:    []string{"after: kept"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := redactConfigSecrets(tc.in)
			for _, secret := range tc.secrets {
				if strings.Contains(out, secret) {
					t.Errorf("secret fragment %q still visible:\n%s", secret, out)
				}
			}
			for _, keep := range tc.keep {
				if !strings.Contains(out, keep) {
					t.Errorf("non-secret %q was lost:\n%s", keep, out)
				}
			}
		})
	}
}
