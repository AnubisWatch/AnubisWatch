package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalWritesRejectExplicitMissingConfig(t *testing.T) {
	for _, selection := range []string{"environment", "long-argument", "short-argument", "equals-argument", "argument-overrides-env", "missing-parent"} {
		t.Run(selection, func(t *testing.T) {
			root := t.TempDir()
			missing := filepath.Join(root, "missing.json")
			data := filepath.Join(root, "fallback")
			t.Setenv("ANUBIS_DATA_DIR", data)
			t.Setenv("ANUBIS_CONFIG", "")
			oldArgs := os.Args
			os.Args = []string{"anubis", "backup", "create"}
			t.Cleanup(func() { os.Args = oldArgs })
			switch selection {
			case "environment":
				t.Setenv("ANUBIS_CONFIG", missing)
			case "long-argument":
				os.Args = append(os.Args, "--config", missing)
			case "short-argument":
				os.Args = append(os.Args, "-c", missing)
			case "equals-argument":
				os.Args = append(os.Args, "--config="+missing)
			case "argument-overrides-env":
				valid := filepath.Join(root, "valid.json")
				if err := os.WriteFile(valid, []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("ANUBIS_CONFIG", valid)
				os.Args = append(os.Args, "--config", missing)
			case "missing-parent":
				missing = filepath.Join(root, "absent-parent", "config.json")
				t.Setenv("ANUBIS_CONFIG", missing)
			}
			for attempt := 0; attempt < 2; attempt++ {
				if cfg, err := loadLocalCLIConfig(); cfg != nil || !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), missing) {
					t.Fatalf("selected absent config must identify missing path: cfg=%v err=%v", cfg != nil, err)
				}
				store, err := openLocalStorage()
				if store != nil {
					_ = store.Close()
				}
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("local storage must reject absent config: %v", err)
				}
				err = adminResetPassword([]string{"--email", "admin@example.test", "--password-stdin", "--force"}, strings.NewReader("Recovered-Passw0rd!\n"))
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("local reset must reject absent config: %v", err)
				}
			}
			if _, err := os.Stat(data); !os.IsNotExist(err) {
				t.Fatalf("selected missing config must not create fallback target: %v", err)
			}
		})
	}
}

func TestLocalStorageSupportsImplicitMissingConfig(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("ANUBIS_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	t.Setenv("ANUBIS_DATA_DIR", filepath.Join(root, "data"))
	t.Setenv("ANUBIS_ENCRYPTION_KEY", "")
	oldArgs := os.Args
	os.Args = []string{"anubis", "backup", "create"}
	t.Cleanup(func() { os.Args = oldArgs })
	if path := findConfig(); path != "./anubis.json" {
		t.Fatalf("implicit missing-config control discovered external config: %s", path)
	}
	if cfg, err := loadLocalCLIConfig(); cfg != nil || err != nil {
		t.Fatalf("unselected absent config must remain optional: cfg=%v err=%v", cfg != nil, err)
	}
	store, err := openLocalStorage()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}
