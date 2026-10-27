package auth

import (
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// OIDCAuthenticator indexes each account twice (by ID and by email) so the
// callback can look users up by email. GetUsers documents "all OIDC users" and
// must list every account exactly once.
func TestOIDCGetUsersListsEachAccountOnce(t *testing.T) {
	a := newTestOIDCAuth(t, core.OIDCAuth{
		Issuer: "https://example.com", ClientID: "c", RedirectURL: "http://localhost:8080/callback",
	}, "", "admin@test.com", "TestPass1234!")

	// Control: nothing added yet, nothing listed.
	if got := a.GetUsers(); len(got) != 0 {
		t.Fatalf("control: GetUsers() = %d users, want 0", len(got))
	}

	want := map[string]string{} // email -> id
	for _, email := range []string{"one@example.com", "two@example.com", "three@example.com"} {
		u, err := a.AddUser(email, email, "viewer")
		if err != nil {
			t.Fatal(err)
		}
		want[email] = u.ID
	}

	got := a.GetUsers()
	if len(got) != len(want) {
		t.Fatalf("GetUsers() returned %d entries for %d accounts", len(got), len(want))
	}
	seen := map[string]int{}
	for _, u := range got {
		seen[u.Email]++
	}
	for email := range want {
		if seen[email] != 1 {
			t.Errorf("account %s listed %d times, want 1", email, seen[email])
		}
	}
}
