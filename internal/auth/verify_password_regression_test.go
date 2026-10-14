package auth

import "testing"

// VerifyPassword is the counterpart to HashPassword: the status-page password
// check compares against a stored bcrypt hash with it, so its contract matters --
// an empty hash or an empty password can never match.
func TestVerifyPassword(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	for _, tc := range []struct {
		name     string
		hash     string
		password string
		want     bool
	}{
		{"matching password", hash, "correct-horse-battery", true},
		{"wrong password", hash, "not-the-password", false},
		{"empty password against a real hash", hash, "", false},
		{"empty hash", "", "correct-horse-battery", false},
		{"both empty", "", "", false},
		{"not a bcrypt hash", "plaintext", "plaintext", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := VerifyPassword(tc.hash, tc.password); got != tc.want {
				t.Errorf("VerifyPassword(%q, %q) = %v, want %v", tc.hash, tc.password, got, tc.want)
			}
		})
	}
}
