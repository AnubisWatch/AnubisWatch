package main

import (
	"net"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/auth"
)

// The reset-password API generates a token nothing delivers; operators need
// a way to recover the admin account from the data directory.
func TestAdminResetPassword(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", dir)
	prev := testDataDir
	testDataDir = dir
	t.Cleanup(func() { testDataDir = prev })

	// Nothing listens here, so the "server is running" guard does not trip.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	ln.Close()
	t.Setenv("ANUBIS_HOST", "127.0.0.1")
	t.Setenv("ANUBIS_PORT", port)

	if err := adminResetPassword([]string{"--email", "admin@anubis.watch", "--password-stdin"}, strings.NewReader("Recovered-Passw0rd!\n")); err != nil {
		t.Fatal(err)
	}
	a, err := auth.NewLocalAuthenticator(filepath.Join(dir, "sessions.json"), "admin@anubis.watch", "Config-Passw0rd!")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Shutdown()
	if _, _, err := a.Login("admin@anubis.watch", "Recovered-Passw0rd!"); err != nil {
		t.Fatalf("login with reset password: %v", err)
	}
}

func TestAdminResetPasswordRefusesWhileServerRuns(t *testing.T) {
	t.Setenv("ANUBIS_DATA_DIR", t.TempDir())
	withFakeHealthServer(t)
	err := adminResetPassword([]string{"--email", "admin@anubis.watch", "--password-stdin"}, strings.NewReader("Recovered-Passw0rd!"))
	if err == nil || !strings.Contains(err.Error(), "is running") {
		t.Fatalf("err = %v, want refusal while the server runs", err)
	}
}
