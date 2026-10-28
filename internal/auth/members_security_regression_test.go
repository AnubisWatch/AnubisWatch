package auth

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Regression tests from the members/invitations security review.

// U+017F folds to 's' under strings.EqualFold, so "opſ@example.com" was a
// distinct account treated as the configured admin "ops@example.com": owner
// everywhere, and its password change rewrote the admin's hash.
func TestUnicodeFoldedEmailCannotBecomeConfiguredAdmin(t *testing.T) {
	const adminEmail = "ops@example.com"
	const folded = "opſ@example.com"
	a := newTestLocalAuth(t, filepath.Join(t.TempDir(), "sessions.json"), adminEmail, dirAdminPass)
	owner, _, err := a.Login(adminEmail, dirAdminPass)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		run  func() error
	}{
		{"create", func() error {
			_, err := a.CreateUser(owner, api.CreateUserRequest{Email: folded, Password: dirUserPass, Role: core.RoleViewer})
			return err
		}},
		{"invite", func() error { _, _, err := a.CreateInvitation(owner, folded, core.RoleViewer); return err }},
		{"sso", func() error { _, err := a.ProvisionExternalUser(folded, "x", SourceOIDC); return err }},
		{"login", func() error { _, _, err := a.Login(folded, dirAdminPass); return err }},
		{"kelvin login", func() error { _, _, err := a.Login("adminK@example.com", dirAdminPass); return err }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); err == nil {
				t.Fatalf("non-ASCII look-alike of %s accepted", adminEmail)
			}
		})
	}

	// An identity that predates validation (e.g. from an old session file)
	// is still not the configured admin.
	a.mu.Lock()
	a.users["legacy"] = &api.User{ID: "legacy", Email: folded, Workspace: "team"}
	a.accounts["legacy"] = &accountRecord{Source: SourceLocal, Memberships: map[string]core.MemberRole{"team": core.RoleViewer}}
	a.mu.Unlock()
	res, err := a.ResolveExternalUser(&api.User{ID: "legacy", Workspace: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Role == "owner" || res.Workspace == "default" {
		t.Fatalf("folded identity resolved as %s of %s", res.Role, res.Workspace)
	}
}

// Reset links go to the account's stored address, and only canonical (ASCII
// case) variants of it match.
func TestPasswordResetRecipientIsTheStoredAddress(t *testing.T) {
	a := newTestLocalAuth(t, "", "kate@example.com", dirAdminPass)
	if tok, _, _ := a.RequestPasswordResetTo("Kate@example.com"); tok != "" {
		t.Fatal("Kelvin-sign spelling matched the account")
	}
	tok, to, err := a.RequestPasswordResetTo(" KATE@Example.com ")
	if err != nil || tok == "" || to != "kate@example.com" {
		t.Fatalf("reset = %q to %q, %v", tok, to, err)
	}
}

// Unauthenticated reset requests and repeated logins used to grow the
// replicated auth state until no write fit and nobody could sign in.
func TestAuthStateGrowthIsBounded(t *testing.T) {
	store := &memoryClusterAuthStore{}
	a := newTestClusterAuth(t, store)
	for i := 0; i < 300; i++ {
		if _, err := a.RequestPasswordReset("admin@example.com"); err != nil {
			t.Fatalf("reset %d: %v", i, err)
		}
	}
	if n := len(a.local.resetTokens); n != 1 {
		t.Fatalf("reset tokens for one account = %d, want 1", n)
	}
	var last string
	for i := 0; i < 300; i++ {
		_, tok, err := a.Login("admin@example.com", "TestPass1234!")
		if err != nil {
			t.Fatalf("login %d refused: %v", i, err)
		}
		last = tok
	}
	if n := len(a.local.tokens); n > maxSessionsPerUser {
		t.Fatalf("sessions for one account = %d, want <= %d", n, maxSessionsPerUser)
	}
	if _, err := a.Authenticate(last); err != nil {
		t.Fatalf("newest session evicted: %v", err)
	}
}

// Every bounded collection at its cap, with long values, still fits one
// replicated transaction, so the caps (not the size check) are what bind.
func TestFullAuthStateFitsTheReplicatedBudget(t *testing.T) {
	a := newTestLocalAuth(t, "", dirAdminEmail, dirAdminPass)
	long := func(prefix string, n int) string { return prefix + strings.Repeat("x", n-len(prefix)) }
	now := time.Now()
	a.mu.Lock()
	ids := []string{}
	for len(a.users) < maxAccounts {
		id, _ := generateID()
		email := long(id, 60) + "@example.com"
		a.users[id] = &api.User{ID: id, Email: email, Name: long("n", 60), Role: "viewer", Workspace: "default", CreatedAt: now}
		a.accounts[id] = &accountRecord{PasswordHash: strings.Repeat("h", 60), Source: SourceLocal, UpdatedAt: now, LastLoginAt: &now,
			Memberships: map[string]core.MemberRole{"default": core.RoleViewer, "team": core.RoleEditor}}
		ids = append(ids, id)
	}
	for i := 0; i < maxPendingInvitations; i++ {
		id, _ := generateID()
		a.invitations[id] = &invitationRecord{ID: id, Email: long(id, 60) + "@example.com", Role: core.RoleViewer, Workspace: "default",
			TokenHash: hashInviteToken(id), InvitedBy: long("i", 60) + "@example.com", InvitedByID: ids[0], CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	}
	for i := 0; i < maxSessions; i++ {
		tok, _ := generateToken()
		a.tokens[tok] = &session{UserID: ids[i%len(ids)], ExpiresAt: now.Add(time.Hour)}
	}
	for i := 0; i < maxResetTokens; i++ {
		tok, _ := generateToken()
		a.resetTokens[tok] = &resetToken{Email: long("r", 60) + "@example.com", ExpiresAt: now.Add(time.Hour)}
	}
	a.mu.Unlock()
	a.attemptsMu.Lock()
	for i := 0; i < maxSharedLockouts+100; i++ {
		a.loginAttempts[loginSourceKey(long("l", 60)+"@example.com", "2001:db8::"+strings.Repeat("f", i%30))+string(rune('a'+i%26))+time.Duration(i).String()] =
			&loginAttempt{Count: 3, LastTry: now, LockedUntil: now.Add(time.Minute)}
	}
	a.attemptsMu.Unlock()
	raw, err := snapshotClusterAuth(a)
	if err != nil {
		t.Fatalf("a full authentication state does not fit: %v", err)
	}
	var data sessionData
	_ = json.Unmarshal(raw, &data)
	if len(data.Lockouts) > maxSharedLockouts {
		t.Fatalf("shared lockouts = %d, want <= %d", len(data.Lockouts), maxSharedLockouts)
	}
	t.Logf("full state: %d of %d bytes", len(raw), MaxClusterAuthStateBytes)
}

// A refused update (role above the actor's) used to apply its name change
// first, and in HA mode mutate() committed it.
func TestRefusedMemberUpdateChangesNothing(t *testing.T) {
	store := &memoryClusterAuthStore{}
	a := newTestClusterAuth(t, store)
	owner, _, err := a.Login("admin@example.com", "TestPass1234!")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateUser(owner, api.CreateUserRequest{Email: "adm@example.com", Password: dirUserPass, Role: core.RoleAdmin}); err != nil {
		t.Fatal(err)
	}
	victim, err := a.CreateUser(owner, api.CreateUserRequest{Email: "v@example.com", Name: "Victim", Password: dirUserPass, Role: core.RoleViewer})
	if err != nil {
		t.Fatal(err)
	}
	adm, _, err := a.Login("adm@example.com", dirUserPass)
	if err != nil {
		t.Fatal(err)
	}
	before := store.state.Revision
	name, role := "renamed", core.RoleOwner
	if _, err := a.UpdateMember(adm, victim.ID, api.MemberUpdate{Name: &name, Role: &role}); err == nil {
		t.Fatal("expected refusal")
	}
	if store.state.Revision != before {
		t.Fatal("a refused update committed a change")
	}
	members, _ := a.ListMembers("default")
	for _, m := range members {
		if m.ID == victim.ID && m.Name != "Victim" {
			t.Fatalf("refused update renamed the member to %q", m.Name)
		}
	}
}

func TestInvitationFollowsTheInvitersAuthority(t *testing.T) {
	a := newTestLocalAuth(t, "", dirAdminEmail, dirAdminPass)
	owner, _ := loginOwner(t, a)
	admM := addUser(t, a, owner, "adm@example.com", core.RoleAdmin)
	adm := &api.User{ID: admM.ID, Email: admM.Email, Workspace: "default"}
	yes, viewer := true, core.RoleViewer

	cases := []struct {
		name   string
		revoke func()
	}{
		{"inviter demoted", func() { _, _ = a.UpdateMember(owner, admM.ID, api.MemberUpdate{Role: &viewer}) }},
		{"inviter disabled", func() { _, _ = a.UpdateMember(owner, admM.ID, api.MemberUpdate{Disabled: &yes}) }},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			admin, no := core.RoleAdmin, false
			if _, err := a.UpdateMember(owner, admM.ID, api.MemberUpdate{Role: &admin, Disabled: &no}); err != nil {
				t.Fatal(err)
			}
			email := []string{"x1@example.com", "x2@example.com"}[i]
			_, token, err := a.CreateInvitation(adm, email, core.RoleEditor)
			if err != nil {
				t.Fatal(err)
			}
			tc.revoke()
			if _, err := a.AcceptInvitation(token, "X", dirUserPass); errKind(err) != "validation" {
				t.Fatalf("invitation outlived its inviter's authority: %v", err)
			}
		})
	}
}

// An existing account is never added to another workspace without its
// consent: CreateUser refuses, an invitation is accepted with its password.
func TestExistingAccountJoinsOnlyByConsent(t *testing.T) {
	a := newTestLocalAuth(t, "", dirAdminEmail, dirAdminPass)
	owner, _ := loginOwner(t, a)
	team := *owner
	team.Workspace = "team"
	addUser(t, a, &team, "bob@example.com", core.RoleViewer)

	if _, err := a.CreateUser(owner, api.CreateUserRequest{Email: "BOB@example.com", Role: core.RoleViewer}); errKind(err) != "conflict" {
		t.Fatalf("existing account added without consent: %v", err)
	}
	if a.IsMember(a.findByEmailLocked("bob@example.com").ID, "default") {
		t.Fatal("bob became a member of default")
	}
	_, token, err := a.CreateInvitation(owner, "bob@example.com", core.RoleEditor)
	if err != nil {
		t.Fatalf("inviting an existing account: %v", err)
	}
	if _, err := a.AcceptInvitation(token, "", "wrong-password-1A!"); errKind(err) != "validation" {
		t.Fatalf("accepted with a wrong password: %v", err)
	}
	m, err := a.AcceptInvitation(token, "", dirUserPass)
	if err != nil || m.Role != core.RoleEditor || m.Workspace != "default" {
		t.Fatalf("consenting accept = %+v, %v", m, err)
	}
	// Names are account-wide: an admin of one workspace cannot rename an
	// account that also belongs to another.
	admM := addUser(t, a, owner, "adm@example.com", core.RoleAdmin)
	name := "Renamed"
	if _, err := a.UpdateMember(&api.User{ID: admM.ID, Email: admM.Email, Workspace: "default"}, m.ID, api.MemberUpdate{Name: &name}); errKind(err) != "forbidden" {
		t.Fatalf("admin renamed a multi-workspace account: %v", err)
	}
}

// An api member could mint editor keys (equal rank) and gain channel/rule
// read access it does not hold.
func TestAPIMemberCannotMintEditorKeys(t *testing.T) {
	keys := NewAPIKeyService(newMemAPIKeyStore(), nil)
	creator := &api.User{ID: "u", Email: "api@example.com", Role: string(core.RoleAPI), Workspace: "default"}
	if _, _, err := keys.CreateKey(creator, "k", core.RoleEditor, nil); err == nil {
		t.Fatal("api member minted an editor key")
	}
	if _, _, err := keys.CreateKey(creator, "k", core.RoleAPI, nil); err != nil {
		t.Fatalf("api member could not mint an api key: %v", err)
	}
}

func TestSSOProvisioningRespectsMemberQuota(t *testing.T) {
	a := newTestLocalAuth(t, "", dirAdminEmail, dirAdminPass)
	a.SetMemberQuota(func(ws string, current int) error {
		return core.CheckQuotaCount(ws, core.QuotaMaxTeamMembers, 1, current)
	})
	if _, err := a.ProvisionExternalUser("sso@example.com", "", SourceOIDC); err == nil {
		t.Fatal("SSO sign-in exceeded max_team_members")
	}
}
