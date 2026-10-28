package auth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSecurityMutationPersistenceFailure(t *testing.T) {
	for _, op := range []string{"password", "logout", "reset", "request-reset", "workspace"} {
		t.Run(op, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sessions.json")
			a := newTestLocalAuth(t, path, "admin@example.com", "TestPass1234!")
			_, token, err := a.Login("admin@example.com", "TestPass1234!")
			if err != nil {
				t.Fatal(err)
			}
			reset := ""
			if op == "reset" {
				reset, err = a.RequestPasswordReset("admin@example.com")
				if err != nil {
					t.Fatal(err)
				}
			}
			if err = os.Mkdir(path+".tmp", 0700); err != nil {
				t.Fatal(err)
			}
			switch op {
			case "password":
				err = a.ChangePassword(token, "TestPass1234!", "NewTestPass1234!")
			case "logout":
				err = a.Logout(token)
			case "reset":
				err = a.ConfirmPasswordReset(reset, "NewTestPass1234!")
			case "request-reset":
				_, err = a.RequestPasswordReset("admin@example.com")
			case "workspace":
				_, err = a.SwitchWorkspace(token, "other")
			}
			if err == nil {
				t.Fatal("security mutation reported success despite persistence failure")
			}
			if _, err = a.Authenticate(token); err != nil {
				t.Fatalf("failed mutation invalidated prior session: %v", err)
			}
			if a.users[a.tokens[token].UserID].Workspace != "default" {
				t.Fatal("workspace was not rolled back")
			}
			if err = os.Remove(path + ".tmp"); err != nil {
				t.Fatal(err)
			}
			if op == "reset" {
				if err = a.ConfirmPasswordReset(reset, "NewTestPass1234!"); err != nil {
					t.Fatal("failed reset consumed token", err)
				}
			}
			a.Shutdown()
			b := newTestLocalAuth(t, path, "admin@example.com", "TestPass1234!")
			password := "TestPass1234!"
			if op == "reset" {
				password = "NewTestPass1234!"
			}
			if _, _, err = b.Login("admin@example.com", password); err != nil {
				t.Fatal("restart credential mismatch", err)
			}
		})
	}
}

func TestAuthenticationFileSyncFailures(t *testing.T) {
	for _, phase := range []string{"file", "directory"} {
		t.Run(phase, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sessions.json")
			a := newTestLocalAuth(t, path, "admin@example.com", "TestPass1234!")
			_, token, err := a.Login("admin@example.com", "TestPass1234!")
			if err != nil {
				t.Fatal(err)
			}
			oldFileSync, oldDirSync := sessionFileSync, sessionDirectorySync
			defer func() { sessionFileSync = oldFileSync; sessionDirectorySync = oldDirSync }()
			failure := errors.New("injected fsync failure")
			if phase == "file" {
				sessionFileSync = func(*os.File) error { return failure }
			} else {
				sessionDirectorySync = func(string) error { return failure }
			}
			err = a.ChangePassword(token, "TestPass1234!", "NewTestPass1234!")
			if !errors.Is(err, failure) {
				t.Fatalf("sync failure not surfaced: %v", err)
			}
			sessionFileSync, sessionDirectorySync = oldFileSync, oldDirSync
			password := "TestPass1234!"
			if phase == "directory" {
				password = "NewTestPass1234!"
			}
			if _, _, err = a.Login("admin@example.com", password); err != nil {
				t.Fatalf("memory inconsistent after %s fsync failure: %v", phase, err)
			}
			a.Shutdown()
			b := newTestLocalAuth(t, path, "admin@example.com", "TestPass1234!")
			if _, _, err = b.Login("admin@example.com", password); err != nil {
				t.Fatal("restart inconsistent", err)
			}
		})
	}
}
