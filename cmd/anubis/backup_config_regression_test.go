package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBackupDeleteRejectsInvalidSelectedConfig(t *testing.T) {
	if os.Getenv("ANUBIS_BACKUP_CONFIG_HELPER") == "1" {
		t.Setenv("ANUBIS_CONFIG", os.Getenv("ANUBIS_BACKUP_CONFIG_PATH"))
		t.Setenv("ANUBIS_DATA_DIR", os.Getenv("ANUBIS_BACKUP_CONFIG_DATA"))
		os.Args = []string{"anubis", "backup", "delete", "protected.json.gz"}
		backupDelete()
		return
	}
	for _, mode := range []string{"valid", "malformed", "missing", "directory"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			data := filepath.Join(root, "data")
			backups := filepath.Join(data, "backups")
			if err := os.MkdirAll(backups, 0700); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(backups, "protected.json.gz")
			before := []byte("backup that must survive a config failure")
			if err := os.WriteFile(file, before, 0600); err != nil {
				t.Fatal(err)
			}
			cfg := filepath.Join(root, "config.json")
			switch mode {
			case "valid", "malformed":
				content := "{}"
				if mode == "malformed" {
					content = "{ invalid"
				}
				if err := os.WriteFile(cfg, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(cfg, 0700); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestBackupDeleteRejectsInvalidSelectedConfig$")
			cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1", "ANUBIS_BACKUP_CONFIG_HELPER=1", "ANUBIS_BACKUP_CONFIG_PATH="+cfg, "ANUBIS_BACKUP_CONFIG_DATA="+data)
			output, err := cmd.CombinedOutput()
			if mode == "valid" {
				if err != nil {
					t.Fatalf("valid control failed: %v\n%s", err, output)
				}
				if _, err := os.Stat(file); !os.IsNotExist(err) {
					t.Fatalf("valid delete control retained backup: %v", err)
				}
				return
			}
			if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 || !bytes.Contains(output, []byte(cfg)) {
				t.Fatalf("invalid config should exit 1 and identify config: %v\n%s", err, output)
			}
			after, err := os.ReadFile(file)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("failed delete must preserve backup: read=%v unchanged=%v", err, bytes.Equal(before, after))
			}
		})
	}
}

func TestLocalBackupManagerRejectsInvalidConfigBeforeInit(t *testing.T) {
	for _, content := range []string{"{", "{}"} {
		t.Run(content, func(t *testing.T) {
			root := t.TempDir()
			cfg := filepath.Join(root, "config.json")
			if err := os.WriteFile(cfg, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("ANUBIS_CONFIG", cfg)
			t.Setenv("ANUBIS_DATA_DIR", filepath.Join(root, "target"))
			mgr, err := openLocalBackupManager()
			if content == "{}" {
				if err != nil || mgr == nil {
					t.Fatalf("valid manager control failed: %v", err)
				}
				return
			}
			if err == nil || mgr != nil {
				t.Fatalf("invalid config must not initialize manager: %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, "target")); !os.IsNotExist(err) {
				t.Fatalf("invalid config must not create backup directory: %v", err)
			}
		})
	}
}
