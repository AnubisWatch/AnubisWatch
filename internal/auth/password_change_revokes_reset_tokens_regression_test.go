package auth

import (
	"testing"
)

// A password reset token is a standing credential: whoever holds one can set
// the admin password without knowing the old one. When the password is changed
// by any other route, every outstanding reset token must die with the old
// password, exactly as ResetLocalAdminPassword (the CLI route) already does.
// Otherwise a token obtained earlier (log, mail, shoulder-surf) still takes
// the account over after the owner rotated the password to lock someone out.
func TestChangePasswordRevokesOutstandingResetTokens(t *testing.T) {
	const adminEmail = "admin@example.com"
	const oldPassword = "Old-Password-123!"
	const newPassword = "New-Password-456!"
	const attackerPassword = "Attacker-Pass-789!"

	setup := func(t *testing.T) (*LocalAuthenticator, string, string) {
		t.Helper()
		a, err := NewLocalAuthenticator("", adminEmail, oldPassword)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(a.Shutdown)
		resetToken, err := a.RequestPasswordReset(adminEmail)
		if err != nil || resetToken == "" {
			t.Fatalf("reset token: %q %v", resetToken, err)
		}
		_, session, err := a.Login(adminEmail, oldPassword)
		if err != nil {
			t.Fatal(err)
		}
		return a, resetToken, session
	}

	// Control: with no password change in between, the reset token works.
	a, resetToken, _ := setup(t)
	if err := a.ConfirmPasswordReset(resetToken, attackerPassword); err != nil {
		t.Fatalf("control: reset token should be redeemable: %v", err)
	}

	// After the owner changes the password, the earlier token is dead.
	a, resetToken, session := setup(t)
	if err := a.ChangePassword(session, oldPassword, newPassword); err != nil {
		t.Fatal(err)
	}
	if err := a.ConfirmPasswordReset(resetToken, attackerPassword); err == nil {
		t.Fatal("reset token issued before ChangePassword still resets the password")
	}
	if _, _, err := a.Login(adminEmail, newPassword); err != nil {
		t.Fatalf("owner's new password no longer works: %v", err)
	}
}
