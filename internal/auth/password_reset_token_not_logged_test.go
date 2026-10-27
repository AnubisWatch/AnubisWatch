package auth

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// The reset token is a bearer credential for the admin account. Now that it
// is emailed, it must still never reach the log; the CLI recovery path keeps
// working without it.
func TestRequestPasswordResetNeverLogsToken(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	a, err := NewLocalAuthenticator("", "admin@example.com", "Str0ng-Passw0rd!")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Shutdown()
	token, err := a.RequestPasswordReset("admin@example.com")
	if err != nil || token == "" {
		t.Fatalf("RequestPasswordReset = %q, %v", token, err)
	}
	if strings.Contains(logs.String(), token) {
		t.Fatalf("reset token logged:\n%s", logs.String())
	}
	if err := a.ConfirmPasswordReset(token, "An0ther-Passw0rd!"); err != nil {
		t.Fatalf("token from RequestPasswordReset does not reset: %v", err)
	}
	if strings.Contains(logs.String(), token) {
		t.Fatalf("reset token logged on confirm:\n%s", logs.String())
	}
}
