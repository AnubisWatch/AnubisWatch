package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Client commands used to assume port 8443 and /var/lib/anubis while
// `anubis init` writes another port and a per-user data dir, so
// init → serve → judge failed out of the box.
func TestClientDefaultsComeFromConfigFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "anubis.json")
	cfg := `{"server":{"host":"0.0.0.0","port":18555},"storage":{"path":"` + filepath.ToSlash(dir) + `/data"},
	"auth":{"enabled":true,"type":"local","local":{"admin_email":"a@b.c","admin_password":"Str0ng!Passw0rd"}}}`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANUBIS_CONFIG", cfgPath)
	t.Setenv("ANUBIS_HOST", "")
	t.Setenv("ANUBIS_PORT", "")

	if got := getAPIURL(); got != "http://localhost:18555" {
		t.Errorf("getAPIURL() = %s, want http://localhost:18555", got)
	}

	t.Setenv("ANUBIS_PORT", "9000")
	if got := getAPIURL(); got != "http://localhost:9000" {
		t.Errorf("ANUBIS_PORT should override config, got %s", got)
	}

	// ANUBIS_DATA_DIR and testDataDir win; without them the config is used.
	t.Setenv("ANUBIS_DATA_DIR", "")
	prev := testDataDir
	testDataDir = ""
	t.Cleanup(func() { testDataDir = prev })
	if got := getDataDir(); got != filepath.Join(dir, "data") {
		t.Errorf("getDataDir() = %s, want %s", got, filepath.Join(dir, "data"))
	}
}

func TestAPITokenFallsBackToLoginTokenFile(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "sub", "token")
	t.Setenv("ANUBIS_TOKEN_FILE", tokenPath)
	t.Setenv("ANUBIS_API_TOKEN", "")

	if got := getAPIToken(); got != "" {
		t.Fatalf("token without file = %q", got)
	}
	if err := saveToken(tokenPath, "abc123"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("token file mode = %v, want 0600", info.Mode().Perm())
	}
	if got := getAPIToken(); got != "abc123" {
		t.Fatalf("getAPIToken() = %q, want abc123", got)
	}
	t.Setenv("ANUBIS_API_TOKEN", "env-token")
	if got := getAPIToken(); got != "env-token" {
		t.Fatalf("env token should win, got %q", got)
	}
}
