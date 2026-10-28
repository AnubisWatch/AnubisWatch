package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalWritesRejectInvalidSelectedConfig(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		directory     bool
	}{
		{"malformed.json", "{ invalid json", false},
		{"malformed.yaml", "storage: [", false},
		{"invalid.json", `{"environment":"production","server":{"tls":{"enabled":false}}}`, false},
		{"directory.json", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, tc.name)
			if tc.directory {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte(tc.content), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("ANUBIS_CONFIG", path)
			t.Setenv("ANUBIS_DATA_DIR", filepath.Join(root, "fallback"))
			t.Setenv("ANUBIS_ENCRYPTION_KEY", "")
			oldArgs := os.Args
			os.Args = []string{"anubis", "admin", "reset-password"}
			t.Cleanup(func() { os.Args = oldArgs })

			for attempt := 0; attempt < 2; attempt++ {
				err := adminResetPassword([]string{"--email", "admin@example.test", "--password-stdin", "--force"}, strings.NewReader("Recovered-Passw0rd!\n"))
				if err == nil || !strings.Contains(err.Error(), path) {
					t.Fatalf("reset must identify invalid selected config: %v", err)
				}
				store, err := openLocalStorage()
				if store != nil {
					_ = store.Close()
				}
				if err == nil || !strings.Contains(err.Error(), path) {
					t.Fatalf("local storage must identify invalid selected config: %v", err)
				}
			}
			if _, err := os.Stat(filepath.Join(root, "fallback")); !os.IsNotExist(err) {
				t.Fatalf("invalid config must not create fallback data directory: %v", err)
			}
		})
	}
}

func TestLocalConfigArgumentOverridesInvalidEnvironment(t *testing.T) {
	root := t.TempDir()
	bad := filepath.Join(root, "bad.json")
	good := filepath.Join(root, "good.json")
	if err := os.WriteFile(bad, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(good, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANUBIS_CONFIG", bad)
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	for _, flag := range [][]string{{"--config", good}, {"-c", good}, {"--config=" + good}} {
		os.Args = append([]string{"anubis", "backup", "create"}, flag...)
		if cfg, err := loadLocalCLIConfig(); err != nil || cfg == nil {
			t.Fatalf("valid argument must override malformed env config: cfg=%v err=%v", cfg != nil, err)
		}
	}
	// Client connection defaults retain their best-effort behavior; local
	// writers use the error-returning loader above.
	os.Args = []string{"anubis", "status"}
	if loadCLIConfig() != nil {
		t.Fatal("invalid client defaults should remain unavailable")
	}
}
