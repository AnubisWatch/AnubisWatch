package auth

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func regSessionsFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sessions.json")
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

// regConstruct runs the real constructor and reports a panic instead of letting
// it abort the test binary, so a failure names the malformed input.
func regConstruct(t *testing.T, path, email, password string) (auth *LocalAuthenticator, panicked any) {
	t.Helper()
	defer func() { panicked = recover() }()

	auth, err := NewLocalAuthenticator(path, email, password)
	if err != nil {
		t.Fatalf("NewLocalAuthenticator returned an error: %v", err)
	}
	return auth, nil
}

// TestLoadSessionsIgnoresNullRecords pins that a malformed record in the
// persisted session file is skipped rather than dereferenced.
//
// loadSessions already documents and tests that intent for the JSON parse
// failure path ("Corrupted file, start fresh") and ignores malformed lockout and
// user records; the session and reset-token loops must behave the same way.
// NewLocalAuthenticator is called on the server startup path with no recover
// anywhere above it, so a dereference here stops the process from starting.
func TestLoadSessionsIgnoresNullRecords(t *testing.T) {
	future := time.Now().Add(time.Hour).Format(time.RFC3339Nano)

	cases := []struct {
		name       string
		contents   string
		wantTokens int
		wantResets int
	}{
		{
			name:       "null session record",
			contents:   `{"tokens":{"t":null},"users":{}}`,
			wantTokens: 0,
		},
		{
			name:       "null reset token record",
			contents:   `{"tokens":{},"reset_tokens":{"r":null}}`,
			wantResets: 0,
		},
		{
			name: "null records alongside valid ones keep the valid ones",
			contents: `{"tokens":{"bad":null,"good":{"user_id":"u1","expires_at":"` + future + `"}},` +
				`"users":{"u1":{"id":"u1","email":"live@example.com"}},` +
				`"reset_tokens":{"bad":null,"good":{"email":"live@example.com","expires_at":"` + future + `"}}}`,
			wantTokens: 1,
			wantResets: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := regSessionsFile(t, tc.contents)

			a, panicked := regConstruct(t, path, "admin@example.com", "TestPass1234!")
			if panicked != nil {
				t.Fatalf("NewLocalAuthenticator panicked on %s: %v", tc.contents, panicked)
			}
			defer a.Shutdown()

			if len(a.tokens) != tc.wantTokens {
				t.Errorf("tokens loaded = %d, want %d", len(a.tokens), tc.wantTokens)
			}
			if len(a.resetTokens) != tc.wantResets {
				t.Errorf("reset tokens loaded = %d, want %d", len(a.resetTokens), tc.wantResets)
			}
		})
	}
}

// TestLoadSessionsKeepsValidRecords is the neighbouring control: the guards must
// not stop well-formed persisted state from loading and working.
func TestLoadSessionsKeepsValidRecords(t *testing.T) {
	future := time.Now().Add(time.Hour).Format(time.RFC3339Nano)
	path := regSessionsFile(t,
		`{"tokens":{"live":{"user_id":"u1","expires_at":"`+future+`"}},`+
			`"users":{"u1":{"id":"u1","email":"live@example.com"}}}`)

	a, panicked := regConstruct(t, path, "admin@example.com", "TestPass1234!")
	if panicked != nil {
		t.Fatalf("NewLocalAuthenticator panicked on valid state: %v", panicked)
	}
	defer a.Shutdown()

	if len(a.tokens) != 1 {
		t.Fatalf("tokens loaded = %d, want 1", len(a.tokens))
	}
	user, err := a.Authenticate("live")
	if err != nil {
		t.Fatalf("the restored session is not usable: %v", err)
	}
	if user.Email != "live@example.com" {
		t.Errorf("restored session resolved to %q, want live@example.com", user.Email)
	}
}
