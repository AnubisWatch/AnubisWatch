package auth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/core"
)

const (
	dirAdminEmail = "admin@example.com"
	dirAdminPass  = "TestPass1234!"
	dirUserPass   = "UserPass5678!"
)

func loginOwner(t *testing.T, a *LocalAuthenticator) (*api.User, string) {
	t.Helper()
	u, tok, err := a.Login(dirAdminEmail, dirAdminPass)
	if err != nil {
		t.Fatalf("admin login: %v", err)
	}
	return u, tok
}

func addUser(t *testing.T, a *LocalAuthenticator, actor *api.User, email string, role core.MemberRole) *api.Member {
	t.Helper()
	m, err := a.CreateUser(actor, api.CreateUserRequest{Email: email, Name: email, Password: dirUserPass, Role: role})
	if err != nil {
		t.Fatalf("CreateUser(%s, %s): %v", email, role, err)
	}
	return m
}

func errKind(err error) string {
	var (
		v *core.ValidationError
		f *core.ForbiddenError
		n *core.NotFoundError
		c *core.ConflictError
	)
	switch {
	case err == nil:
		return ""
	case errors.As(err, &v):
		return "validation"
	case errors.As(err, &f):
		return "forbidden"
	case errors.As(err, &n):
		return "notfound"
	case errors.As(err, &c):
		return "conflict"
	}
	if _, ok := core.AsQuotaExceeded(err); ok {
		return "quota"
	}
	return "other"
}

func TestMultiUserLogin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	a := newTestLocalAuth(t, path, dirAdminEmail, dirAdminPass)
	owner, _ := loginOwner(t, a)
	if owner.Role != "owner" || owner.Workspace != "default" {
		t.Fatalf("configured admin = %s in %s, want owner in default", owner.Role, owner.Workspace)
	}
	addUser(t, a, owner, "Editor@Example.com", core.RoleEditor)

	// Email is matched case-insensitively.
	u, tok, err := a.LoginFrom("editor@example.COM", dirUserPass, "192.0.2.1")
	if err != nil {
		t.Fatalf("second user login: %v", err)
	}
	if u.Role != "editor" || !u.MustChangePassword {
		t.Fatalf("created user = role %s mustChange %v", u.Role, u.MustChangePassword)
	}
	// The admin's password does not open the other account, and vice versa.
	if _, _, err := a.Login("editor@example.com", dirAdminPass); err == nil {
		t.Fatal("admin password opened another account")
	}
	if _, _, err := a.Login(dirAdminEmail, dirUserPass); err == nil {
		t.Fatal("user password opened the admin account")
	}
	// Changing the initial password clears the flag and ends only that
	// account's sessions.
	_, adminTok := loginOwner(t, a)
	if err := a.ChangePassword(tok, dirUserPass, "Changed9876!x"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if _, err := a.Authenticate(tok); err == nil {
		t.Fatal("old session survived password change")
	}
	if _, err := a.Authenticate(adminTok); err != nil {
		t.Fatalf("another account's session was revoked: %v", err)
	}
	u, _, err = a.Login("editor@example.com", "Changed9876!x")
	if err != nil || u.MustChangePassword {
		t.Fatalf("login after change: %v mustChange=%v", err, u != nil && u.MustChangePassword)
	}

	// Everything survives a restart, including the admin's identity.
	a.Shutdown()
	b := newTestLocalAuth(t, path, dirAdminEmail, dirAdminPass)
	if _, _, err := b.Login("editor@example.com", "Changed9876!x"); err != nil {
		t.Fatalf("user lost on restart: %v", err)
	}
	if again, _ := loginOwner(t, b); again.ID != owner.ID {
		t.Fatalf("admin identity changed on restart")
	}
}

func TestLockoutIsPerUser(t *testing.T) {
	a := newTestLocalAuth(t, "", dirAdminEmail, dirAdminPass)
	owner, _ := loginOwner(t, a)
	addUser(t, a, owner, "alice@example.com", core.RoleViewer)
	addUser(t, a, owner, "bob@example.com", core.RoleViewer)

	const ip = "198.51.100.7"
	for i := 0; i < maxLoginAttempts; i++ {
		_, _, _ = a.LoginFrom("Alice@example.com", "wrong-password", ip)
	}
	if _, _, err := a.LoginFrom("alice@example.com", dirUserPass, ip); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("alice not locked from %s (case variants must share the counter): %v", ip, err)
	}
	if _, _, err := a.LoginFrom("bob@example.com", dirUserPass, ip); err != nil {
		t.Fatalf("bob locked by alice's failures: %v", err)
	}
	if _, _, err := a.LoginFrom(dirAdminEmail, dirAdminPass, ip); err != nil {
		t.Fatalf("admin locked by alice's failures: %v", err)
	}
	// Unknown addresses are not tracked at all.
	for i := 0; i < 3; i++ {
		_, _, _ = a.LoginFrom("ghost@example.com", "x", ip)
	}
	a.attemptsMu.RLock()
	_, tracked := a.loginAttempts[loginSourceKey("ghost@example.com", ip)]
	a.attemptsMu.RUnlock()
	if tracked {
		t.Fatal("failures against an unknown address were recorded")
	}
}

func TestMemberAuthorization(t *testing.T) {
	a := newTestLocalAuth(t, "", dirAdminEmail, dirAdminPass)
	owner, _ := loginOwner(t, a)
	adminM := addUser(t, a, owner, "admin2@example.com", core.RoleAdmin)
	editorM := addUser(t, a, owner, "editor@example.com", core.RoleEditor)
	admin := &api.User{ID: adminM.ID, Email: adminM.Email, Workspace: "default"}
	editor := &api.User{ID: editorM.ID, Email: editorM.Email, Workspace: "default"}
	apiKey := &api.User{ID: "apikey:k1", APIKeyID: "k1", Role: "owner", Workspace: "default"}
	role := func(r core.MemberRole) *core.MemberRole { return &r }
	yes := true

	cases := []struct {
		name string
		run  func() error
		want string
	}{
		{"admin cannot create owner", func() error {
			_, err := a.CreateUser(admin, api.CreateUserRequest{Email: "x1@example.com", Password: dirUserPass, Role: core.RoleOwner})
			return err
		}, "forbidden"},
		{"admin cannot promote to owner", func() error {
			_, err := a.UpdateMember(admin, editorM.ID, api.MemberUpdate{Role: role(core.RoleOwner)})
			return err
		}, "forbidden"},
		{"admin cannot demote the owner", func() error {
			_, err := a.UpdateMember(admin, owner.ID, api.MemberUpdate{Role: role(core.RoleViewer)})
			return err
		}, "forbidden"},
		{"admin cannot invite an owner", func() error {
			_, _, err := a.CreateInvitation(admin, "x2@example.com", core.RoleOwner)
			return err
		}, "forbidden"},
		{"editor cannot manage members", func() error {
			_, err := a.CreateUser(editor, api.CreateUserRequest{Email: "x3@example.com", Password: dirUserPass, Role: core.RoleViewer})
			return err
		}, "forbidden"},
		{"editor cannot raise itself", func() error {
			_, err := a.UpdateMember(editor, editorM.ID, api.MemberUpdate{Role: role(core.RoleAdmin)})
			return err
		}, "forbidden"},
		{"API key cannot create users", func() error {
			_, err := a.CreateUser(apiKey, api.CreateUserRequest{Email: "x4@example.com", Password: dirUserPass, Role: core.RoleViewer})
			return err
		}, "forbidden"},
		{"API key cannot invite", func() error {
			_, _, err := a.CreateInvitation(apiKey, "x5@example.com", core.RoleViewer)
			return err
		}, "forbidden"},
		{"cannot disable yourself", func() error {
			_, err := a.UpdateMember(admin, adminM.ID, api.MemberUpdate{Disabled: &yes})
			return err
		}, "forbidden"},
		{"configured admin cannot be deleted", func() error {
			_, err := a.DeleteUser(admin, owner.ID)
			return err
		}, "forbidden"},
		{"last owner cannot demote itself", func() error {
			_, err := a.UpdateMember(owner, owner.ID, api.MemberUpdate{Role: role(core.RoleAdmin)})
			return err
		}, "conflict"},
		{"last owner cannot leave", func() error {
			_, err := a.RemoveMember(owner, owner.ID)
			return err
		}, "conflict"},
		{"weak initial password", func() error {
			_, err := a.CreateUser(owner, api.CreateUserRequest{Email: "x6@example.com", Password: "short", Role: core.RoleViewer})
			return err
		}, "validation"},
		{"duplicate email, any case", func() error {
			_, err := a.CreateUser(owner, api.CreateUserRequest{Email: "EDITOR@example.com", Password: dirUserPass, Role: core.RoleViewer})
			return err
		}, "conflict"},
		{"admin may manage an editor", func() error {
			_, err := a.UpdateMember(admin, editorM.ID, api.MemberUpdate{Role: role(core.RoleViewer)})
			return err
		}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := errKind(tc.run()); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}

	// With a second owner the first may step down.
	second := addUser(t, a, owner, "owner2@example.com", core.RoleOwner)
	if _, err := a.UpdateMember(owner, owner.ID, api.MemberUpdate{Role: role(core.RoleAdmin)}); err != nil {
		t.Fatalf("demotion with another owner: %v", err)
	}
	// ...but the remaining owner cannot then be disabled by them.
	ownerNow := &api.User{ID: owner.ID, Email: owner.Email, Workspace: "default"}
	if _, err := a.UpdateMember(ownerNow, second.ID, api.MemberUpdate{Disabled: &yes}); errKind(err) != "forbidden" {
		t.Fatalf("admin disabled the only owner: %v", err)
	}
}

func TestRevocationOnDisableAndDowngrade(t *testing.T) {
	a := newTestLocalAuth(t, "", dirAdminEmail, dirAdminPass)
	owner, ownerTok := loginOwner(t, a)
	m := addUser(t, a, owner, "ed@example.com", core.RoleEditor)
	_, tok, err := a.Login("ed@example.com", dirUserPass)
	if err != nil {
		t.Fatal(err)
	}
	var revoked []string
	a.onRevokeUser = func(id string) { revoked = append(revoked, id) }

	// An upgrade keeps the session, and takes effect at once.
	up := core.RoleAdmin
	change, err := a.UpdateMember(owner, m.ID, api.MemberUpdate{Role: &up})
	if err != nil || change.Revoked {
		t.Fatalf("upgrade: %v revoked=%v", err, change != nil && change.Revoked)
	}
	if u, err := a.Authenticate(tok); err != nil || u.Role != "admin" {
		t.Fatalf("after upgrade: %v %v", u, err)
	}
	// A downgrade ends it.
	down := core.RoleViewer
	if change, err = a.UpdateMember(owner, m.ID, api.MemberUpdate{Role: &down}); err != nil || !change.Revoked {
		t.Fatalf("downgrade: %v", err)
	}
	if _, err := a.Authenticate(tok); err == nil {
		t.Fatal("session survived downgrade")
	}
	// Disabled accounts cannot sign in even with the right password.
	_, tok, _ = a.Login("ed@example.com", dirUserPass)
	yes := true
	if _, err := a.UpdateMember(owner, m.ID, api.MemberUpdate{Disabled: &yes}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Authenticate(tok); err == nil {
		t.Fatal("session survived disable")
	}
	if _, _, err := a.Login("ed@example.com", dirUserPass); err == nil {
		t.Fatal("disabled account signed in")
	}
	if tok, _ := a.RequestPasswordReset("ed@example.com"); tok != "" {
		t.Fatal("disabled account got a reset token")
	}
	if _, err := a.Authenticate(ownerTok); err != nil {
		t.Fatalf("actor's own session revoked: %v", err)
	}
	if len(revoked) != 2 || revoked[0] != m.ID {
		t.Fatalf("onRevokeUser calls = %v", revoked)
	}
	// Deletion.
	if _, err := a.DeleteUser(owner, m.ID); err != nil {
		t.Fatal(err)
	}
	if members, _ := a.ListMembers("default"); len(members) != 1 {
		t.Fatalf("members after delete = %d", len(members))
	}
}

func TestInvitationFlow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	a := newTestLocalAuth(t, path, dirAdminEmail, dirAdminPass)
	owner, _ := loginOwner(t, a)

	inv, token, err := a.CreateInvitation(owner, "New@Example.com", core.RoleEditor)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), token) {
		t.Fatal("invitation token stored in plaintext")
	}
	if !strings.Contains(string(raw), hashInviteToken(token)) {
		t.Fatal("invitation hash not persisted")
	}
	if invs, _ := a.ListInvitations("default"); len(invs) != 1 || invs[0].ID != inv.ID {
		t.Fatalf("ListInvitations = %+v", invs)
	}
	// An existing email cannot be invited again as a new account.
	if _, _, err := a.CreateInvitation(owner, "admin@EXAMPLE.com", core.RoleViewer); errKind(err) != "conflict" {
		t.Fatalf("inviting an existing account: %v", err)
	}

	cases := []struct {
		name, token, pass string
		want              string
	}{
		{"wrong token", "nope", dirUserPass, "validation"},
		{"weak password", token, "weak", "validation"},
		{"accepted", token, dirUserPass, ""},
		{"single use", token, dirUserPass, "validation"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := a.AcceptInvitation(tc.token, "New Person", tc.pass)
			if got := errKind(err); got != tc.want {
				t.Fatalf("got %q (%v), want %q", got, err, tc.want)
			}
		})
	}
	u, _, err := a.Login("new@example.com", dirUserPass)
	if err != nil || u.Role != "editor" || u.Name != "New Person" || u.MustChangePassword {
		t.Fatalf("invitee login = %+v, %v", u, err)
	}
	if invs, _ := a.ListInvitations("default"); len(invs) != 0 {
		t.Fatal("accepted invitation still pending")
	}

	// Expiry and revocation.
	_, expired, _ := a.CreateInvitation(owner, "late@example.com", core.RoleViewer)
	a.mu.Lock()
	for _, rec := range a.invitations {
		rec.ExpiresAt = time.Now().Add(-time.Minute)
	}
	a.mu.Unlock()
	if _, err := a.AcceptInvitation(expired, "Late", dirUserPass); errKind(err) != "validation" {
		t.Fatalf("expired invitation accepted: %v", err)
	}
	revoked, revokedTok, _ := a.CreateInvitation(owner, "gone@example.com", core.RoleViewer)
	if err := a.RevokeInvitation(owner, revoked.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AcceptInvitation(revokedTok, "Gone", dirUserPass); errKind(err) != "validation" {
		t.Fatalf("revoked invitation accepted: %v", err)
	}
}

func TestMemberQuotaCountsInvitations(t *testing.T) {
	a := newTestLocalAuth(t, "", dirAdminEmail, dirAdminPass)
	owner, _ := loginOwner(t, a)
	a.SetMemberQuota(func(ws string, current int) error {
		return core.CheckQuotaCount(ws, core.QuotaMaxTeamMembers, 3, current)
	})
	addUser(t, a, owner, "two@example.com", core.RoleViewer)
	if _, _, err := a.CreateInvitation(owner, "three@example.com", core.RoleViewer); err != nil {
		t.Fatalf("third member refused: %v", err)
	}
	_, _, err := a.CreateInvitation(owner, "four@example.com", core.RoleViewer)
	if qe, ok := core.AsQuotaExceeded(err); !ok || qe.Quota != core.QuotaMaxTeamMembers || qe.Current != 3 {
		t.Fatalf("fourth invitation = %v", err)
	}
	if _, err := a.CreateUser(owner, api.CreateUserRequest{Email: "five@example.com", Password: dirUserPass}); errKind(err) != "quota" {
		t.Fatalf("direct create past quota = %v", err)
	}
	// Re-inviting the same address replaces its invitation, so it fits.
	if _, _, err := a.CreateInvitation(owner, "three@example.com", core.RoleEditor); err != nil {
		t.Fatalf("re-invite refused: %v", err)
	}
}

// A session file written before multi-user support has the admin identity,
// AdminPasswordHash and sessions, but no accounts.
func TestMigratesSingleAdminSessionFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	hash, err := HashPassword("Rotated9999!x")
	if err != nil {
		t.Fatal(err)
	}
	seed, _ := HashPassword(dirAdminPass)
	old := map[string]any{
		"users": map[string]any{
			"admin-id": map[string]any{"id": "admin-id", "email": dirAdminEmail, "name": "Administrator", "role": "admin", "workspace": "default"},
		},
		"tokens": map[string]any{
			"old-token": map[string]any{"user_id": "admin-id", "expires_at": time.Now().Add(time.Hour)},
		},
		"admin_email":         dirAdminEmail,
		"admin_password_hash": hash,
		"admin_seed_hash":     seed,
	}
	raw, _ := json.Marshal(old)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}

	a := newTestLocalAuth(t, path, dirAdminEmail, dirAdminPass)
	u, err := a.Authenticate("old-token")
	if err != nil || u.ID != "admin-id" || u.Role != "owner" {
		t.Fatalf("existing session after migration = %+v, %v", u, err)
	}
	if _, _, err := a.Login(dirAdminEmail, "Rotated9999!x"); err != nil {
		t.Fatalf("runtime-changed admin password lost: %v", err)
	}
	members, _ := a.ListMembers("default")
	if len(members) != 1 || members[0].ID != "admin-id" || members[0].Role != core.RoleOwner || !members[0].ConfiguredAdmin {
		t.Fatalf("migrated members = %+v", members)
	}

	// The admin record keeps the fields older binaries read.
	a.Shutdown()
	var data sessionData
	raw, _ = os.ReadFile(path)
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	if data.AdminEmail != dirAdminEmail || data.AdminPasswordHash == "" || data.Users["admin-id"] == nil || data.Users["admin-id"].Email != dirAdminEmail {
		t.Fatalf("admin record not preserved: %+v", data)
	}
	if data.Accounts["admin-id"] == nil || data.Accounts["admin-id"].PasswordHash != "" {
		t.Fatal("admin account record missing, or the admin hash was duplicated into it")
	}

	// `anubis admin reset-password` keeps the directory intact.
	if err := ResetLocalAdminPassword(path, dirAdminEmail, "Offline7777!x"); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(path)
	data = sessionData{}
	_ = json.Unmarshal(raw, &data)
	if data.Accounts["admin-id"] == nil {
		t.Fatal("reset-password dropped the directory")
	}
}

func TestExternalProvisioning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	a := newTestLocalAuth(t, path, dirAdminEmail, dirAdminPass)
	owner, _ := loginOwner(t, a)

	u, err := a.ProvisionExternalUser("Sso@Example.com", "SSO User", SourceOIDC)
	if err != nil || u.Role != "viewer" || u.Workspace != "default" {
		t.Fatalf("first SSO sign-in = %+v, %v", u, err)
	}
	editor := core.RoleEditor
	if _, err := a.UpdateMember(owner, u.ID, api.MemberUpdate{Role: &editor}); err != nil {
		t.Fatal(err)
	}
	a.Shutdown()
	b := newTestLocalAuth(t, path, dirAdminEmail, dirAdminPass)
	again, err := b.ProvisionExternalUser("sso@example.com", "SSO User", SourceOIDC)
	if err != nil || again.ID != u.ID || again.Role != "editor" {
		t.Fatalf("role not honoured after restart: %+v, %v", again, err)
	}
	// An IdP cannot claim a local password account, nor another source's.
	if _, err := b.ProvisionExternalUser(dirAdminEmail, "", SourceOIDC); err == nil {
		t.Fatal("OIDC sign-in took over the local admin")
	}
	if _, err := b.ProvisionExternalUser("sso@example.com", "", SourceLDAP); err == nil {
		t.Fatal("LDAP sign-in took over an OIDC account")
	}
	// No password sign-in for an external account.
	if _, _, err := b.Login("sso@example.com", dirUserPass); err == nil {
		t.Fatal("external account signed in with a password")
	}
	// Disabled accounts are refused, and their sessions resolve to errors.
	owner2, _ := loginOwner(t, b)
	yes := true
	if _, err := b.UpdateMember(owner2, u.ID, api.MemberUpdate{Disabled: &yes}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ProvisionExternalUser("sso@example.com", "", SourceOIDC); err == nil {
		t.Fatal("disabled SSO account signed in")
	}
	if _, err := b.ResolveExternalUser(again); err == nil {
		t.Fatal("disabled SSO session still resolves")
	}
}

func TestOIDCSessionsFollowTheDirectory(t *testing.T) {
	o, err := NewOIDCAuthenticator(core.OIDCAuth{}, "", dirAdminEmail, dirAdminPass)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(o.Shutdown)
	u, err := o.local.ProvisionExternalUser("idp@example.com", "IdP", SourceOIDC)
	if err != nil {
		t.Fatal(err)
	}
	o.mu.Lock()
	o.users[u.ID] = u
	o.tokens["oidc-token"] = &session{UserID: u.ID, ExpiresAt: time.Now().Add(time.Hour)}
	o.mu.Unlock()

	owner, _, err := o.Login(dirAdminEmail, dirAdminPass)
	if err != nil {
		t.Fatal(err)
	}
	dir := o.UserDirectory()
	admin := core.RoleAdmin
	if _, err := dir.UpdateMember(owner, u.ID, api.MemberUpdate{Role: &admin}); err != nil {
		t.Fatal(err)
	}
	if got, err := o.Authenticate("oidc-token"); err != nil || got.Role != "admin" {
		t.Fatalf("upgraded OIDC session = %+v, %v", got, err)
	}
	viewer := core.RoleViewer
	if _, err := dir.UpdateMember(owner, u.ID, api.MemberUpdate{Role: &viewer}); err != nil {
		t.Fatal(err)
	}
	if _, err := o.Authenticate("oidc-token"); err == nil {
		t.Fatal("OIDC session survived a downgrade")
	}
}

func TestAPIKeyOwnerCheck(t *testing.T) {
	a := newTestLocalAuth(t, "", dirAdminEmail, dirAdminPass)
	owner, _ := loginOwner(t, a)
	m := addUser(t, a, owner, "keys@example.com", core.RoleAdmin)
	cases := []struct {
		name  string
		owner string
		ws    string
		role  core.MemberRole
		ok    bool
	}{
		{"within role", m.ID, "default", core.RoleEditor, true},
		{"equal role", m.ID, "default", core.RoleAdmin, true},
		{"above role", m.ID, "default", core.RoleOwner, false},
		{"other workspace", m.ID, "elsewhere", core.RoleViewer, false},
		{"unknown owner", "ghost", "default", core.RoleViewer, false},
		{"configured admin", owner.ID, "default", core.RoleOwner, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := a.CheckKeyOwner(tc.owner, tc.ws, tc.role); (err == nil) != tc.ok {
				t.Fatalf("CheckKeyOwner = %v, want ok=%v", err, tc.ok)
			}
		})
	}
	yes := true
	if _, err := a.UpdateMember(owner, m.ID, api.MemberUpdate{Disabled: &yes}); err != nil {
		t.Fatal(err)
	}
	if err := a.CheckKeyOwner(m.ID, "default", core.RoleViewer); err == nil {
		t.Fatal("a disabled owner's key still works")
	}
}

func TestAccountLimit(t *testing.T) {
	a := newTestLocalAuth(t, "", dirAdminEmail, dirAdminPass)
	owner, _ := loginOwner(t, a)
	a.mu.Lock()
	for i := len(a.users); i < maxAccounts; i++ {
		id, _ := generateID()
		a.users[id] = &api.User{ID: id, Email: id + "@example.com", Workspace: "default"}
		a.accounts[id] = &accountRecord{Memberships: map[string]core.MemberRole{"default": core.RoleViewer}}
	}
	a.mu.Unlock()
	_, err := a.CreateUser(owner, api.CreateUserRequest{Email: "one-more@example.com", Password: dirUserPass})
	if errKind(err) != "conflict" || !strings.Contains(err.Error(), "at most") {
		t.Fatalf("account limit not enforced: %v", err)
	}
	// The full directory still fits one replicated HA transaction.
	raw, err := snapshotClusterAuth(a)
	if err != nil {
		t.Fatalf("a full directory does not fit the shared state: %v", err)
	}
	if len(raw) > MaxClusterAuthStateBytes/2 {
		t.Fatalf("a full directory takes %d of %d bytes, leaving too little for sessions", len(raw), MaxClusterAuthStateBytes)
	}
}
