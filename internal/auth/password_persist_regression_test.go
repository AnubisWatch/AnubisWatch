package auth

import (
	"path/filepath"
	"testing"
)

const (
	persistAdmin   = "admin@anubis.watch"
	configPassword = "Config-Passw0rd!"
	newPassword    = "Changed-Passw0rd!"
)

// A password changed at runtime used to live only in memory: after a restart
// the server went back to the config file's password.
func TestChangedPasswordSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	a, err := NewLocalAuthenticator(path, persistAdmin, configPassword)
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := a.Login(persistAdmin, configPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ChangePassword(token, configPassword, newPassword); err != nil {
		t.Fatal(err)
	}
	a.Shutdown()

	b, err := NewLocalAuthenticator(path, persistAdmin, configPassword)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Shutdown()
	if _, _, err := b.Login(persistAdmin, configPassword); err == nil {
		t.Fatal("config password still works after the password was changed")
	}
	if _, _, err := b.Login(persistAdmin, newPassword); err != nil {
		t.Fatalf("changed password rejected after restart: %v", err)
	}
}

// A persisted hash is bound to its email: renaming the admin in config starts
// from the configured password.
func TestPersistedPasswordIgnoredForDifferentAdmin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	if err := ResetLocalAdminPassword(path, persistAdmin, newPassword); err != nil {
		t.Fatal(err)
	}
	b, err := NewLocalAuthenticator(path, "other@anubis.watch", configPassword)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Shutdown()
	if _, _, err := b.Login("other@anubis.watch", configPassword); err != nil {
		t.Fatalf("configured password for a new admin email rejected: %v", err)
	}
}

func TestResetLocalAdminPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	a, err := NewLocalAuthenticator(path, persistAdmin, configPassword)
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := a.Login(persistAdmin, configPassword)
	if err != nil {
		t.Fatal(err)
	}
	a.Shutdown()

	if err := ResetLocalAdminPassword(path, persistAdmin, "short"); err == nil {
		t.Fatal("weak password accepted")
	}
	if err := ResetLocalAdminPassword(path, persistAdmin, newPassword); err != nil {
		t.Fatal(err)
	}

	b, err := NewLocalAuthenticator(path, persistAdmin, configPassword)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Shutdown()
	if _, err := b.Authenticate(token); err == nil {
		t.Fatal("session from before the reset is still valid")
	}
	if _, _, err := b.Login(persistAdmin, newPassword); err != nil {
		t.Fatalf("reset password rejected: %v", err)
	}
	// The account kept its identity (workspace ownership is keyed by user ID).
	if len(b.users) != 1 {
		t.Fatalf("users after reset = %d, want 1", len(b.users))
	}
}

// Rotating the password in the config file must still work after a runtime
// change: the persisted hash only wins while the config password is unchanged.
func TestConfigPasswordRotationOverridesRuntimeChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	a, err := NewLocalAuthenticator(path, persistAdmin, configPassword)
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := a.Login(persistAdmin, configPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ChangePassword(token, configPassword, newPassword); err != nil {
		t.Fatal(err)
	}
	a.Shutdown()

	const rotated = "Rotated-Passw0rd!"
	b, err := NewLocalAuthenticator(path, persistAdmin, rotated)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Shutdown()
	if _, _, err := b.Login(persistAdmin, rotated); err != nil {
		t.Fatalf("rotated config password rejected: %v", err)
	}
	if _, _, err := b.Login(persistAdmin, newPassword); err == nil {
		t.Fatal("runtime password still works after the config password was rotated")
	}
}
