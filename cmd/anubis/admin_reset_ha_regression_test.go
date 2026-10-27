package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

func TestAdminResetPasswordRefusesReplicatedAuthority(t *testing.T) {
	cases := []struct {
		name string
		set  func(map[string]any, map[string]any)
	}{
		{"enabled", func(n, r map[string]any) { n["enabled"] = true }},
		{"single-node-consensus", func(n, r map[string]any) { n["single_node"] = true }},
		{"bootstrap", func(n, r map[string]any) { r["bootstrap"] = true }},
		{"static-peers", func(n, r map[string]any) {
			r["peers"] = []map[string]any{{"id": "other", "address": "127.0.0.1:17947", "role": "voter"}}
		}},
		{"join", func(n, r map[string]any) { r["join"] = []string{"127.0.0.1:17947"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			raft := map[string]any{"node_id": "test-node", "bind_addr": "127.0.0.1:17946", "advertise_addr": "127.0.0.1:17946"}
			necro := map[string]any{"enabled": false, "node_name": "test-node", "bind_addr": "127.0.0.1:17946", "cluster_secret": "fixture-cluster-secret-0123456789abcdef", "raft": raft}
			tc.set(necro, raft)
			cfg := map[string]any{
				"server":     map[string]any{"host": "127.0.0.1", "port": 18443, "grpc_port": 19090},
				"storage":    map[string]any{"path": dir},
				"auth":       map[string]any{"type": "local", "local": map[string]any{"admin_email": "admin@example.test", "admin_password": "Config-Passw0rd!"}},
				"necropolis": necro,
			}
			data, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "config.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := core.LoadConfig(path); err != nil {
				t.Fatalf("invalid control configuration: %v", err)
			}
			t.Setenv("ANUBIS_CONFIG", path)
			t.Setenv("ANUBIS_DATA_DIR", dir)
			sessions := filepath.Join(dir, "sessions.json")
			before := []byte(`{"tokens":{},"users":{}}`)
			if err := os.WriteFile(sessions, before, 0600); err != nil {
				t.Fatal(err)
			}
			for _, force := range []bool{false, true} {
				args := []string{"--email", "admin@example.test", "--password-stdin"}
				if force {
					args = append(args, "--force")
				}
				err := adminResetPassword(args, strings.NewReader("Recovered-Passw0rd!\n"))
				if err == nil || !strings.Contains(err.Error(), "replicated state") {
					t.Fatalf("force=%v: got %v, want explicit shared-authority refusal", force, err)
				}
				after, err := os.ReadFile(sessions)
				if err != nil || string(after) != string(before) {
					t.Fatalf("force=%v: local authentication state changed: %v", force, err)
				}
			}
		})
	}
}

func TestAdminResetPasswordChecksPersistedAuthority(t *testing.T) {
	for _, key := range []string{"system/auth/state", "raft/stable/hard_state", "default/souls/control"} {
		t.Run(strings.ReplaceAll(key, "/", "-"), func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("ANUBIS_CONFIG", "")
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
			t.Chdir(dir)
			t.Setenv("ANUBIS_DATA_DIR", dir)
			t.Setenv("ANUBIS_ENCRYPTION_KEY", "")
			store, err := storage.NewEngine(core.StorageConfig{Path: dir}, errorLogger())
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Put(key, []byte("{}")); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "sessions.json")
			before := []byte(`{"tokens":{},"users":{}}`)
			if err := os.WriteFile(path, before, 0600); err != nil {
				t.Fatal(err)
			}
			err = adminResetPassword([]string{"--email", "admin@example.test", "--password-stdin", "--force"}, strings.NewReader("Recovered-Passw0rd!\n"))
			after, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if strings.HasPrefix(key, "default/") {
				if err != nil || string(after) == string(before) {
					t.Fatalf("standalone stored-data control: err=%v, state changed=%v", err, string(after) != string(before))
				}
			} else if err == nil || !strings.Contains(err.Error(), "replicated state") || string(after) != string(before) {
				t.Fatalf("shared authority reset: err=%v, state changed=%v", err, string(after) != string(before))
			}
		})
	}
}

func TestAdminResetPasswordRefusesLockedOrUnreadableStore(t *testing.T) {
	for _, locked := range []bool{false, true} {
		t.Run(fmt.Sprint(locked), func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("ANUBIS_CONFIG", "")
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
			t.Chdir(dir)
			t.Setenv("ANUBIS_DATA_DIR", dir)
			t.Setenv("ANUBIS_ENCRYPTION_KEY", "")
			if locked {
				store, err := storage.NewEngine(core.StorageConfig{Path: dir}, errorLogger())
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { store.Close() })
			} else if err := os.Mkdir(filepath.Join(dir, "wal.log"), 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "sessions.json")
			before := []byte(`{"tokens":{},"users":{}}`)
			if err := os.WriteFile(path, before, 0600); err != nil {
				t.Fatal(err)
			}
			err := adminResetPassword([]string{"--email", "admin@example.test", "--password-stdin", "--force"}, strings.NewReader("Recovered-Passw0rd!\n"))
			after, readErr := os.ReadFile(path)
			if err == nil || !strings.Contains(err.Error(), "cannot inspect authentication authority") || readErr != nil || string(after) != string(before) {
				t.Fatalf("uncertain authority reset: err=%v, read=%v, state changed=%v", err, readErr, string(after) != string(before))
			}
		})
	}
}
