package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMainRejectsMissingConfigValueBeforeDelete(t *testing.T) {
	if os.Getenv("ANUBIS_REQUIRED_CONFIG_HELPER") == "1" {
		var flags []string
		if err := json.Unmarshal([]byte(os.Getenv("ANUBIS_REQUIRED_CONFIG_FLAGS")), &flags); err != nil {
			t.Fatal(err)
		}
		t.Setenv("ANUBIS_CONFIG", os.Getenv("ANUBIS_REQUIRED_CONFIG_PATH"))
		t.Setenv("ANUBIS_DATA_DIR", os.Getenv("ANUBIS_REQUIRED_CONFIG_DATA"))
		os.Args = append([]string{"anubis", "backup", "delete", "protected.json.gz"}, flags...)
		main()
		return
	}
	for _, tc := range []struct {
		name  string
		flags []string
		valid bool
	}{
		{"omitted-control", nil, true},
		{"valid-control", []string{"--config=CONFIG"}, true},
		{"empty-equals", []string{"--config="}, false},
		{"missing-long", []string{"--config"}, false},
		{"missing-short", []string{"-c"}, false},
		{"empty-value", []string{"--config", ""}, false},
		{"next-flag", []string{"--config", "--force"}, false},
		{"trailing-invalid", []string{"--config=CONFIG", "--config="}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			cfg := filepath.Join(root, "config.json")
			if err := os.WriteFile(cfg, []byte("{}"), 0600); err != nil {
				t.Fatal(err)
			}
			data := filepath.Join(root, "data")
			backup := filepath.Join(data, "backups", "protected.json.gz")
			if err := os.MkdirAll(filepath.Dir(backup), 0700); err != nil {
				t.Fatal(err)
			}
			before := []byte("protected backup")
			if err := os.WriteFile(backup, before, 0600); err != nil {
				t.Fatal(err)
			}
			flags := append([]string(nil), tc.flags...)
			for i := range flags {
				flags[i] = strings.ReplaceAll(flags[i], "CONFIG", cfg)
			}
			encoded, err := json.Marshal(flags)
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestMainRejectsMissingConfigValueBeforeDelete$")
			cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1", "ANUBIS_REQUIRED_CONFIG_HELPER=1", "ANUBIS_REQUIRED_CONFIG_FLAGS="+string(encoded), "ANUBIS_REQUIRED_CONFIG_PATH="+cfg, "ANUBIS_REQUIRED_CONFIG_DATA="+data)
			out, err := cmd.CombinedOutput()
			if tc.valid {
				if err != nil {
					t.Fatalf("valid delete control: %v\n%s", err, out)
				}
				if _, err := os.Stat(backup); !os.IsNotExist(err) {
					t.Fatalf("valid control did not delete backup: %v", err)
				}
				return
			}
			if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 || !bytes.Contains(out, []byte("requires a config file path")) {
				t.Fatalf("invalid selector must exit 1 before dispatch: %v\n%s", err, out)
			}
			after, err := os.ReadFile(backup)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("invalid selector changed backup: read=%v unchanged=%v", err, bytes.Equal(before, after))
			}
		})
	}
}

func TestConfigSelectorPreservesValidPaths(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--config", "path with spaces.json"}, "path with spaces.json"},
		{[]string{"--config=-leading-dash.json"}, "-leading-dash.json"},
		{[]string{"-c", "first.json", "--config=second.json"}, "first.json"},
		{nil, ""},
	} {
		got, err := parseConfigPathFromArgs(tc.args)
		if err != nil || got != tc.want {
			t.Fatalf("parse %q: got=%q err=%v want=%q", tc.args, got, err, tc.want)
		}
	}
}

func TestLocalConfigLoaderRejectsMissingConfigValue(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.json")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANUBIS_CONFIG", path)
	t.Setenv("ANUBIS_DATA_DIR", filepath.Join(root, "target"))
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	for _, flags := range [][]string{{"--config="}, {"--config"}, {"-c"}, {"--config", "--force"}} {
		os.Args = append([]string{"anubis", "backup", "create"}, flags...)
		if cfg, err := loadLocalCLIConfig(); cfg != nil || err == nil || !strings.Contains(err.Error(), "requires a config file path") {
			t.Fatalf("malformed selector must not fall back to valid env config: cfg=%v err=%v", cfg != nil, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "target")); !os.IsNotExist(err) {
		t.Fatalf("malformed selector created target: %v", err)
	}
}

func TestAdminResetRejectsMissingEmailValue(t *testing.T) {
	for _, flags := range [][]string{{"--email"}, {"-e"}, {"--email="}, {"--email", ""}, {"--email", "--force"}, {"--email=admin@example.test", "--email="}} {
		t.Run(strings.Join(flags, "_"), func(t *testing.T) {
			root := t.TempDir()
			cfg := filepath.Join(root, "config.json")
			if err := os.WriteFile(cfg, []byte(`{"auth":{"type":"local","local":{"admin_email":"admin@example.test","admin_password":"Config-Passw0rd!"}}}`), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("ANUBIS_CONFIG", cfg)
			t.Setenv("ANUBIS_DATA_DIR", root)
			sessions := filepath.Join(root, "sessions.json")
			before := []byte(`{"tokens":{},"users":{}}`)
			if err := os.WriteFile(sessions, before, 0600); err != nil {
				t.Fatal(err)
			}
			input := "Recovered-Passw0rd!\n"
			reader := strings.NewReader(input)
			err := adminResetPassword(append([]string{"--force", "--password-stdin"}, flags...), reader)
			if err == nil || !strings.Contains(err.Error(), "requires an email address") || reader.Len() != len(input) {
				t.Fatalf("missing email must reject before password read: err=%v bytes_read=%d", err, len(input)-reader.Len())
			}
			after, err := os.ReadFile(sessions)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("missing email changed auth state: read=%v unchanged=%v", err, bytes.Equal(before, after))
			}
		})
	}
}
