package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"golang.org/x/crypto/bcrypt"
)

// Multi-user local accounts.
//
// Identities stay in sessionData.Users (api.User: ID, email, name, current
// workspace) exactly as before, so a session file written by this version
// still names the configured admin the way older binaries expect. What an
// account needs beyond that -- its own password hash, sign-in source,
// disabled flag and per-workspace roles -- lives in sessionData.Accounts,
// keyed by the same user ID. The configured admin's password stays in
// AdminPasswordHash, keeping its config/env seed semantics and
// `anubis admin reset-password`.
//
// The whole directory is part of the authentication state, so in a cluster
// every change goes through the same compare-and-swap as sessions
// (cluster.go). That state is one Raft entry capped at
// MaxClusterAuthStateBytes. Every collection in that state is capped
// (accounts and invitations here; sessions, reset tokens and replicated
// lockouts in local.go and cluster.go) so that all of them at their caps, with
// long values, still fit (TestFullAuthStateFitsTheReplicatedBudget): the caps
// bind, by evicting or refusing, before the size check ever does.
const (
	maxAccounts           = 150
	maxPendingInvitations = 25
	invitationLifetime    = 7 * 24 * time.Hour
	maxNameLength         = 100
	maxEmailLength        = 254
)

// Account sources.
const (
	SourceLocal = "local"
	SourceOIDC  = "oidc"
	SourceLDAP  = "ldap"
)

// accountRecord is what an account holds beyond its api.User identity.
type accountRecord struct {
	// PasswordHash is the bcrypt hash of a local account's password. The
	// configured admin's lives in AdminPasswordHash instead; OIDC and LDAP
	// accounts have none.
	PasswordHash       string                     `json:"password_hash,omitempty"`
	Source             string                     `json:"source,omitempty"`
	Disabled           bool                       `json:"disabled,omitempty"`
	MustChangePassword bool                       `json:"must_change_password,omitempty"`
	Memberships        map[string]core.MemberRole `json:"memberships,omitempty"`
	UpdatedAt          time.Time                  `json:"updated_at"`
	LastLoginAt        *time.Time                 `json:"last_login_at,omitempty"`
}

func (r *accountRecord) clone() *accountRecord {
	if r == nil {
		return nil
	}
	c := *r
	c.Memberships = maps.Clone(r.Memberships)
	if r.LastLoginAt != nil {
		t := *r.LastLoginAt
		c.LastLoginAt = &t
	}
	return &c
}

func (r *accountRecord) source() string {
	if r == nil || r.Source == "" {
		return SourceLocal
	}
	return r.Source
}

// invitationRecord is a pending invitation. Only the SHA-256 of its token is
// kept; the token itself is returned once, to be emailed or shown.
type invitationRecord struct {
	ID        string          `json:"id"`
	Email     string          `json:"email"`
	Role      core.MemberRole `json:"role"`
	Workspace string          `json:"workspace"`
	TokenHash string          `json:"token_hash"`
	InvitedBy string          `json:"invited_by"`
	// InvitedByID is re-checked at acceptance: an invitation is only as good
	// as its inviter's current authority.
	InvitedByID string    `json:"invited_by_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

var (
	errAccountDisabled   = errors.New("account disabled")
	errNoWorkspaceAccess = errors.New("account has no workspace access")
)

// normalizeEmail is the one canonical form every email comparison uses:
// trimmed, with ASCII letters lowered and nothing else touched. Unicode case
// folding (strings.EqualFold, strings.ToLower) is deliberately avoided: it
// maps U+017F to 's' and U+212A (Kelvin) to 'k', so a different address could
// match an existing account -- including the configured admin. Account
// emails are ASCII-only (validateEmail), so a non-ASCII input never matches.
func normalizeEmail(email string) string {
	email = strings.TrimSpace(email)
	b := []byte(email)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// sameEmail compares two addresses in canonical form.
func sameEmail(a, b string) bool {
	na, nb := normalizeEmail(a), normalizeEmail(b)
	return na != "" && na == nb
}

// validateEmail accepts printable-ASCII addresses only. Internationalised
// addresses would need NFKC plus IDNA handling at every entry point to compare
// safely; refusing them keeps the comparison a byte comparison.
func validateEmail(email string) error {
	if email == "" || len(email) > maxEmailLength || strings.ContainsAny(email, " \t\r\n<>\"") {
		return &core.ValidationError{Field: "email", Message: "a valid email address is required"}
	}
	for i := 0; i < len(email); i++ {
		if email[i] < 0x21 || email[i] > 0x7e {
			return &core.ValidationError{Field: "email", Message: "email addresses must be ASCII"}
		}
	}
	at := strings.LastIndex(email, "@")
	if at <= 0 || at == len(email)-1 || !strings.Contains(email[at+1:], ".") {
		return &core.ValidationError{Field: "email", Message: "a valid email address is required"}
	}
	return nil
}

func validMemberRole(role core.MemberRole) bool { return core.ValidAPIKeyRole(role) }

// strictUpgrade reports whether moving from old to next only adds
// permissions. Anything else (a downgrade, or editor <-> api) ends the
// account's sessions.
func strictUpgrade(old, next core.MemberRole) bool {
	return core.RoleWithin(old, next) && !core.RoleWithin(next, old)
}

func hashInviteToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func workspaceOrDefault(ws string) string {
	if ws == "" {
		return "default"
	}
	return ws
}

// SetMemberQuota installs the max_team_members check, called with a
// workspace and its current member count (members plus pending invitations)
// before an invitation or account is added to it.
func (a *LocalAuthenticator) SetMemberQuota(check func(workspace string, current int) error) {
	a.mu.Lock()
	a.memberQuota = check
	a.mu.Unlock()
}

// isConfiguredAdminLocked reports whether u is the account named by
// auth.local.admin_email.
func (a *LocalAuthenticator) isConfiguredAdminLocked(u *api.User) bool {
	return u != nil && sameEmail(u.Email, a.adminEmail)
}

// findByEmailLocked returns the account with this email, compared
// case-insensitively.
func (a *LocalAuthenticator) findByEmailLocked(email string) *api.User {
	key := normalizeEmail(email)
	if key == "" {
		return nil
	}
	for _, u := range a.users {
		if u != nil && normalizeEmail(u.Email) == key {
			return u
		}
	}
	return nil
}

// knownEmailLocked reports whether email can sign in with a password here,
// so failed attempts against it are worth tracking.
func (a *LocalAuthenticator) knownEmailLocked(email string) bool {
	if sameEmail(email, a.adminEmail) {
		return true
	}
	u := a.findByEmailLocked(email)
	return u != nil && a.accounts[u.ID] != nil && a.accounts[u.ID].PasswordHash != ""
}

// migrateDirectoryLocked gives every identity an account record. A session
// file written before multi-user support holds the configured admin (and
// possibly the identity of a previous admin address); the admin becomes the
// owner of the default workspace, any other identity keeps the role and
// workspace it had.
func (a *LocalAuthenticator) migrateDirectoryLocked() {
	if a.accounts == nil {
		a.accounts = make(map[string]*accountRecord)
	}
	if a.invitations == nil {
		a.invitations = make(map[string]*invitationRecord)
	}
	for id, rec := range a.accounts {
		if rec == nil || a.users[id] == nil {
			delete(a.accounts, id)
		}
	}
	for id, u := range a.users {
		if u == nil || a.accounts[id] != nil {
			continue
		}
		rec := &accountRecord{Source: SourceLocal, Memberships: map[string]core.MemberRole{}, UpdatedAt: u.CreatedAt}
		if a.isConfiguredAdminLocked(u) {
			rec.Memberships["default"] = core.RoleOwner
		} else {
			role := core.MemberRole(u.Role)
			if !validMemberRole(role) {
				role = core.RoleViewer
			}
			rec.Memberships[workspaceOrDefault(u.Workspace)] = role
		}
		a.accounts[id] = rec
	}
}

// resolveLocked returns a copy of u with Role and Workspace set from its
// memberships. A user without an account record (none exists for test
// doubles or OIDC/LDAP users not yet provisioned) is returned unchanged.
func (a *LocalAuthenticator) resolveLocked(u *api.User) (*api.User, error) {
	c := copyUser(u)
	if c == nil {
		return nil, errors.New("user not found")
	}
	rec := a.accounts[u.ID]
	if rec == nil {
		return c, nil
	}
	if rec.Disabled {
		return nil, errAccountDisabled
	}
	ws := workspaceOrDefault(c.Workspace)
	switch role, ok := rec.Memberships[ws]; {
	case ok:
		c.Workspace, c.Role = ws, string(role)
	case a.isConfiguredAdminLocked(u):
		// The configured admin keeps full access to the workspaces it may
		// switch to (the API checks ownership) without being listed there.
		c.Workspace, c.Role = ws, string(core.RoleOwner)
	case len(rec.Memberships) > 0:
		first := sortedWorkspaces(rec.Memberships)[0]
		c.Workspace, c.Role = first, string(rec.Memberships[first])
	default:
		return nil, errNoWorkspaceAccess
	}
	c.MustChangePassword = rec.MustChangePassword
	return c, nil
}

func sortedWorkspaces(m map[string]core.MemberRole) []string {
	out := make([]string, 0, len(m))
	for ws := range m {
		out = append(out, ws)
	}
	sort.Strings(out)
	return out
}

// revokeUserSessionsLocked drops every session of userID. The caller
// persists, then calls notifyRevokedLocked.
func (a *LocalAuthenticator) revokeUserSessionsLocked(userID string) {
	for token, sess := range a.tokens {
		if sess != nil && sess.UserID == userID {
			delete(a.tokens, token)
		}
	}
}

func (a *LocalAuthenticator) notifyRevoked(userID string) {
	if a.onRevokeUser != nil {
		a.onRevokeUser(userID)
	}
}

// ownersLocked counts the enabled explicit owners of ws, skipping skipID.
func (a *LocalAuthenticator) ownersLocked(ws, skipID string) int {
	n := 0
	for id, rec := range a.accounts {
		if id == skipID || rec == nil || rec.Disabled {
			continue
		}
		if rec.Memberships[ws] == core.RoleOwner {
			n++
		}
	}
	return n
}

func lastOwnerError(ws string) error {
	return &core.ConflictError{Message: fmt.Sprintf("workspace %q must keep at least one enabled owner", ws)}
}

// membersLocked counts ws's members plus its pending invitations.
func (a *LocalAuthenticator) membersLocked(ws string, now time.Time) int {
	n := 0
	for _, rec := range a.accounts {
		if rec != nil {
			if _, ok := rec.Memberships[ws]; ok {
				n++
			}
		}
	}
	for _, inv := range a.invitations {
		if inv != nil && inv.Workspace == ws && now.Before(inv.ExpiresAt) {
			n++
		}
	}
	return n
}

func (a *LocalAuthenticator) checkMemberQuotaLocked(ws string, now time.Time) error {
	if a.memberQuota == nil {
		return nil
	}
	return a.memberQuota(ws, a.membersLocked(ws, now))
}

// actorLocked authorises a directory mutation and returns the actor's
// workspace and role there.
func (a *LocalAuthenticator) actorLocked(actor *api.User) (string, core.MemberRole, error) {
	if actor == nil || actor.ID == "" {
		return "", "", &core.ForbiddenError{Message: "an authenticated user is required"}
	}
	if actor.IsAPIKey() {
		return "", "", &core.ForbiddenError{Message: "API keys cannot manage members"}
	}
	stored := a.users[actor.ID]
	if stored == nil {
		return "", "", &core.ForbiddenError{Message: "only accounts in this directory can manage members"}
	}
	view := copyUser(stored)
	if actor.Workspace != "" {
		view.Workspace = actor.Workspace
	}
	resolved, err := a.resolveLocked(view)
	if err != nil {
		return "", "", &core.ForbiddenError{Message: err.Error()}
	}
	role := core.MemberRole(resolved.Role)
	if !role.Can("members:*") {
		return "", "", &core.ForbiddenError{Message: fmt.Sprintf("role %q cannot manage members", role)}
	}
	return resolved.Workspace, role, nil
}

func (a *LocalAuthenticator) memberViewLocked(u *api.User, ws string) api.Member {
	rec := a.accounts[u.ID]
	m := api.Member{
		ID:              u.ID,
		Email:           u.Email,
		Name:            u.Name,
		Workspace:       ws,
		Source:          rec.source(),
		CreatedAt:       u.CreatedAt,
		ConfiguredAdmin: a.isConfiguredAdminLocked(u),
	}
	if rec != nil {
		m.Role = rec.Memberships[ws]
		m.Disabled = rec.Disabled
		m.MustChangePassword = rec.MustChangePassword
		m.UpdatedAt = rec.UpdatedAt
		if rec.LastLoginAt != nil {
			t := *rec.LastLoginAt
			m.LastLoginAt = &t
		}
	}
	return m
}

// ListMembers implements api.UserDirectory.
func (a *LocalAuthenticator) ListMembers(workspace string) ([]api.Member, error) {
	ws := workspaceOrDefault(workspace)
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]api.Member, 0)
	for id, rec := range a.accounts {
		if rec == nil || a.users[id] == nil {
			continue
		}
		if _, ok := rec.Memberships[ws]; ok {
			out = append(out, a.memberViewLocked(a.users[id], ws))
		}
	}
	sort.Slice(out, func(i, j int) bool { return normalizeEmail(out[i].Email) < normalizeEmail(out[j].Email) })
	return out, nil
}

// IsMember implements api.UserDirectory.
func (a *LocalAuthenticator) IsMember(userID, workspace string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	rec := a.accounts[userID]
	if rec == nil || rec.Disabled {
		return false
	}
	_, ok := rec.Memberships[workspaceOrDefault(workspace)]
	return ok
}

// CountMembers implements api.UserDirectory.
func (a *LocalAuthenticator) CountMembers(workspace string) (int, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.membersLocked(workspaceOrDefault(workspace), time.Now()), nil
}

// CreateUser implements api.UserDirectory.
func (a *LocalAuthenticator) CreateUser(actor *api.User, req api.CreateUserRequest) (*api.Member, error) {
	email := strings.TrimSpace(req.Email)
	if err := validateEmail(email); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Name)
	if len(name) > maxNameLength {
		return nil, &core.ValidationError{Field: "name", Message: fmt.Sprintf("name must be at most %d characters", maxNameLength)}
	}
	role := req.Role
	if role == "" {
		role = core.RoleViewer
	}
	if !validMemberRole(role) {
		return nil, &core.ValidationError{Field: "role", Message: "role must be one of owner, admin, editor, api, viewer"}
	}
	// Hash before taking the lock: bcrypt takes hundreds of milliseconds and
	// every authenticated request needs a.mu.
	var hash string
	if req.Password != "" {
		if err := validatePassword(req.Password); err != nil {
			return nil, &core.ValidationError{Field: "password", Message: err.Error()}
		}
		h, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcryptCost)
		if err != nil {
			return nil, fmt.Errorf("hash password: %w", err)
		}
		hash = string(h)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	ws, actorRole, err := a.actorLocked(actor)
	if err != nil {
		return nil, err
	}
	if !core.RoleWithin(role, actorRole) {
		return nil, &core.ForbiddenError{Message: fmt.Sprintf("you cannot grant role %q above your own role %q", role, actorRole)}
	}
	now := time.Now().UTC()
	rollback := a.rollbackStateLocked()

	// An existing account is never added to a workspace without its
	// consent: that goes through an invitation it accepts with its own
	// password. The refusal reads the same whether or not the address is a
	// member elsewhere.
	if a.findByEmailLocked(email) != nil {
		return nil, errEmailUnavailable
	}

	if hash == "" {
		return nil, &core.ValidationError{Field: "password", Message: "an initial password is required (or send an invitation)"}
	}
	if len(a.users) >= maxAccounts {
		return nil, &core.ConflictError{Message: fmt.Sprintf("this server holds at most %d accounts", maxAccounts)}
	}
	if err := a.checkMemberQuotaLocked(ws, now); err != nil {
		return nil, err
	}
	id, _ := generateID()
	if name == "" {
		name = email[:strings.LastIndex(email, "@")]
	}
	user := &api.User{ID: id, Email: email, Name: name, Role: string(role), Workspace: ws, CreatedAt: now}
	a.users[id] = user
	a.accounts[id] = &accountRecord{
		PasswordHash:       hash,
		Source:             SourceLocal,
		MustChangePassword: true,
		Memberships:        map[string]core.MemberRole{ws: role},
		UpdatedAt:          now,
	}
	if err := a.persistOrRollbackLocked(rollback); err != nil {
		return nil, err
	}
	m := a.memberViewLocked(user, ws)
	return &m, nil
}

// targetLocked finds a member of ws the actor may manage.
func (a *LocalAuthenticator) targetLocked(ws string, actorRole core.MemberRole, userID string) (*api.User, *accountRecord, core.MemberRole, error) {
	user, rec := a.users[userID], a.accounts[userID]
	if user == nil || rec == nil {
		return nil, nil, "", &core.NotFoundError{Entity: "member", ID: userID}
	}
	role, ok := rec.Memberships[ws]
	if !ok {
		return nil, nil, "", &core.NotFoundError{Entity: "member", ID: userID}
	}
	if !core.RoleWithin(role, actorRole) {
		return nil, nil, "", &core.ForbiddenError{Message: fmt.Sprintf("you cannot manage a member whose role %q is above your own role %q", role, actorRole)}
	}
	return user, rec, role, nil
}

// accountWideLocked authorises disabling or deleting an account, which
// affects every workspace it belongs to: only the configured admin may do
// that to an account that also belongs to other workspaces.
func (a *LocalAuthenticator) accountWideLocked(actor *api.User, user *api.User, rec *accountRecord, ws string) error {
	if user.ID == actor.ID {
		return &core.ForbiddenError{Message: "you cannot disable or delete your own account"}
	}
	if a.isConfiguredAdminLocked(user) {
		return &core.ForbiddenError{Message: "the configured administrator (auth.local.admin_email) cannot be disabled or deleted"}
	}
	if !a.isConfiguredAdminLocked(a.users[actor.ID]) {
		for other := range rec.Memberships {
			if other != ws {
				return &core.ForbiddenError{Message: "this account also belongs to other workspaces; remove it from this workspace instead"}
			}
		}
	}
	for other, role := range rec.Memberships {
		if role == core.RoleOwner && a.ownersLocked(other, user.ID) == 0 {
			return lastOwnerError(other)
		}
	}
	return nil
}

// UpdateMember implements api.UserDirectory.
func (a *LocalAuthenticator) UpdateMember(actor *api.User, userID string, update api.MemberUpdate) (*api.MemberChange, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ws, actorRole, err := a.actorLocked(actor)
	if err != nil {
		return nil, err
	}
	user, rec, current, err := a.targetLocked(ws, actorRole, userID)
	if err != nil {
		return nil, err
	}

	// Validate the whole request before changing anything, so a refused
	// request changes nothing (in a cluster, mutate would otherwise commit
	// whatever part was applied before the refusal).
	var name string
	if update.Name != nil {
		name = strings.TrimSpace(*update.Name)
		if name == "" || len(name) > maxNameLength {
			return nil, &core.ValidationError{Field: "name", Message: fmt.Sprintf("name must be 1-%d characters", maxNameLength)}
		}
		// A name is account-wide: only the user or someone with authority
		// over every workspace the account is in may change it.
		if user.ID != actor.ID {
			if err := a.accountScopeLocked(actor, rec, ws); err != nil {
				return nil, err
			}
		}
	}
	roleChange := update.Role != nil && *update.Role != current
	if roleChange {
		next := *update.Role
		if !validMemberRole(next) {
			return nil, &core.ValidationError{Field: "role", Message: "role must be one of owner, admin, editor, api, viewer"}
		}
		if !core.RoleWithin(next, actorRole) {
			return nil, &core.ForbiddenError{Message: fmt.Sprintf("you cannot grant role %q above your own role %q", next, actorRole)}
		}
		if current == core.RoleOwner && a.ownersLocked(ws, userID) == 0 {
			return nil, lastOwnerError(ws)
		}
	}
	disableChange := update.Disabled != nil && *update.Disabled != rec.Disabled
	if disableChange {
		if *update.Disabled {
			if err := a.accountWideLocked(actor, user, rec, ws); err != nil {
				return nil, err
			}
		} else if err := a.accountScopeLocked(actor, rec, ws); err != nil {
			return nil, err
		}
	}

	rollback := a.rollbackStateLocked()
	revoke := false
	if update.Name != nil {
		user.Name = name
	}
	if roleChange {
		rec.Memberships[ws] = *update.Role
		revoke = !strictUpgrade(current, *update.Role)
	}
	if disableChange {
		rec.Disabled = *update.Disabled
		revoke = revoke || rec.Disabled
	}
	rec.UpdatedAt = time.Now().UTC()
	if revoke {
		a.revokeUserSessionsLocked(userID)
	}
	if err := a.persistOrRollbackLocked(rollback); err != nil {
		return nil, err
	}
	if revoke {
		a.notifyRevoked(userID)
	}
	m := a.memberViewLocked(a.users[userID], ws)
	return &api.MemberChange{Member: &m, Revoked: revoke}, nil
}

// accountScopeLocked authorises an account-wide change (name, enabling):
// the configured admin may make it to anyone, everyone else only to accounts
// that belong to no workspace but the actor's.
func (a *LocalAuthenticator) accountScopeLocked(actor *api.User, rec *accountRecord, ws string) error {
	if a.isConfiguredAdminLocked(a.users[actor.ID]) {
		return nil
	}
	for other := range rec.Memberships {
		if other != ws {
			return &core.ForbiddenError{Message: "this account also belongs to other workspaces; only the account itself or the configured administrator can change it"}
		}
	}
	return nil
}

// RemoveMember implements api.UserDirectory. An account left without any
// workspace is deleted.
func (a *LocalAuthenticator) RemoveMember(actor *api.User, userID string) (*api.MemberChange, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ws, actorRole, err := a.actorLocked(actor)
	if err != nil {
		return nil, err
	}
	user, rec, role, err := a.targetLocked(ws, actorRole, userID)
	if err != nil {
		return nil, err
	}
	if role == core.RoleOwner && a.ownersLocked(ws, userID) == 0 {
		return nil, lastOwnerError(ws)
	}
	if a.isConfiguredAdminLocked(user) && len(rec.Memberships) == 1 {
		return nil, &core.ForbiddenError{Message: "the configured administrator (auth.local.admin_email) cannot be removed from its last workspace"}
	}
	rollback := a.rollbackStateLocked()
	m := a.memberViewLocked(user, ws)
	delete(rec.Memberships, ws)
	if len(rec.Memberships) == 0 {
		a.deleteAccountLocked(user)
	} else {
		rec.UpdatedAt = time.Now().UTC()
		if workspaceOrDefault(user.Workspace) == ws {
			user.Workspace = sortedWorkspaces(rec.Memberships)[0]
		}
	}
	a.revokeUserSessionsLocked(userID)
	if err := a.persistOrRollbackLocked(rollback); err != nil {
		return nil, err
	}
	a.notifyRevoked(userID)
	return &api.MemberChange{Member: &m, Revoked: true}, nil
}

func (a *LocalAuthenticator) deleteAccountLocked(user *api.User) {
	delete(a.users, user.ID)
	delete(a.accounts, user.ID)
	for token, rt := range a.resetTokens {
		if rt != nil && sameEmail(rt.Email, user.Email) {
			delete(a.resetTokens, token)
		}
	}
}

// DeleteUser implements api.UserDirectory.
func (a *LocalAuthenticator) DeleteUser(actor *api.User, userID string) (*api.MemberChange, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ws, actorRole, err := a.actorLocked(actor)
	if err != nil {
		return nil, err
	}
	user, rec, _, err := a.targetLocked(ws, actorRole, userID)
	if err != nil {
		return nil, err
	}
	if err := a.accountWideLocked(actor, user, rec, ws); err != nil {
		return nil, err
	}
	rollback := a.rollbackStateLocked()
	m := a.memberViewLocked(user, ws)
	a.deleteAccountLocked(user)
	a.revokeUserSessionsLocked(userID)
	if err := a.persistOrRollbackLocked(rollback); err != nil {
		return nil, err
	}
	a.notifyRevoked(userID)
	return &api.MemberChange{Member: &m, Revoked: true}, nil
}

func invitationView(inv *invitationRecord) api.Invitation {
	return api.Invitation{
		ID:        inv.ID,
		Email:     inv.Email,
		Role:      inv.Role,
		Workspace: inv.Workspace,
		InvitedBy: inv.InvitedBy,
		CreatedAt: inv.CreatedAt,
		ExpiresAt: inv.ExpiresAt,
	}
}

func (a *LocalAuthenticator) pruneInvitationsLocked(now time.Time) {
	for id, inv := range a.invitations {
		if inv == nil || !now.Before(inv.ExpiresAt) {
			delete(a.invitations, id)
		}
	}
}

// CreateInvitation implements api.UserDirectory. Inviting an address that
// already has a pending invitation to the workspace replaces it.
func (a *LocalAuthenticator) CreateInvitation(actor *api.User, email string, role core.MemberRole) (*api.Invitation, string, error) {
	email = strings.TrimSpace(email)
	if err := validateEmail(email); err != nil {
		return nil, "", err
	}
	if role == "" {
		role = core.RoleViewer
	}
	if !validMemberRole(role) {
		return nil, "", &core.ValidationError{Field: "role", Message: "role must be one of owner, admin, editor, api, viewer"}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	ws, actorRole, err := a.actorLocked(actor)
	if err != nil {
		return nil, "", err
	}
	if !core.RoleWithin(role, actorRole) {
		return nil, "", &core.ForbiddenError{Message: fmt.Sprintf("you cannot grant role %q above your own role %q", role, actorRole)}
	}
	// An existing account may be invited too: it accepts with its own
	// password. Members of this workspace are visible in its list already,
	// so refusing them reveals nothing.
	if existing := a.findByEmailLocked(email); existing != nil {
		if rec := a.accounts[existing.ID]; rec != nil {
			if _, ok := rec.Memberships[ws]; ok {
				return nil, "", &core.ConflictError{Message: "this account is already a member of the workspace"}
			}
		}
	}
	now := time.Now().UTC()
	rollback := a.rollbackStateLocked()
	a.pruneInvitationsLocked(now)
	for id, inv := range a.invitations {
		if inv.Workspace == ws && sameEmail(inv.Email, email) {
			delete(a.invitations, id)
		}
	}
	if len(a.invitations) >= maxPendingInvitations {
		rollback()
		return nil, "", &core.ConflictError{Message: fmt.Sprintf("at most %d invitations may be pending; revoke some first", maxPendingInvitations)}
	}
	if err := a.checkMemberQuotaLocked(ws, now); err != nil {
		rollback()
		return nil, "", err
	}
	id, _ := generateID()
	token, _ := generateToken()
	inv := &invitationRecord{
		ID:        id,
		Email:     email,
		Role:      role,
		Workspace: ws,
		TokenHash: hashInviteToken(token),
		InvitedBy: actor.Email,
		CreatedAt: now,

		InvitedByID: actor.ID,
		ExpiresAt:   now.Add(invitationLifetime),
	}
	a.invitations[id] = inv
	if err := a.persistOrRollbackLocked(rollback); err != nil {
		return nil, "", err
	}
	view := invitationView(inv)
	return &view, token, nil
}

// ListInvitations implements api.UserDirectory: the pending, unexpired
// invitations of workspace, newest first.
func (a *LocalAuthenticator) ListInvitations(workspace string) ([]api.Invitation, error) {
	ws := workspaceOrDefault(workspace)
	now := time.Now()
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]api.Invitation, 0)
	for _, inv := range a.invitations {
		if inv != nil && inv.Workspace == ws && now.Before(inv.ExpiresAt) {
			out = append(out, invitationView(inv))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// RevokeInvitation implements api.UserDirectory.
func (a *LocalAuthenticator) RevokeInvitation(actor *api.User, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	ws, actorRole, err := a.actorLocked(actor)
	if err != nil {
		return err
	}
	inv := a.invitations[id]
	if inv == nil || inv.Workspace != ws {
		return &core.NotFoundError{Entity: "invitation", ID: id}
	}
	if !core.RoleWithin(inv.Role, actorRole) {
		return &core.ForbiddenError{Message: fmt.Sprintf("you cannot revoke an invitation for role %q above your own role %q", inv.Role, actorRole)}
	}
	rollback := a.rollbackStateLocked()
	delete(a.invitations, id)
	return a.persistOrRollbackLocked(rollback)
}

// findInvitationLocked finds the invitation whose token hashes to hash.
func (a *LocalAuthenticator) findInvitationLocked(hash string) *invitationRecord {
	var found *invitationRecord
	for _, inv := range a.invitations {
		if inv != nil && subtle.ConstantTimeCompare([]byte(inv.TokenHash), []byte(hash)) == 1 {
			found = inv
		}
	}
	return found
}

// errEmailUnavailable refuses creating an account for an address that has
// one, without saying where.
var errEmailUnavailable = &core.ConflictError{Message: "this email cannot be used for a new account; send an invitation instead"}

var errInvalidInvitation = &core.ValidationError{Field: "token", Message: "invalid or expired invitation"}

// AcceptInvitation implements api.UserDirectory. The invitation is consumed
// in the same state transition that creates the account, so a token can be
// redeemed once.
func (a *LocalAuthenticator) AcceptInvitation(token, name, password string) (*api.Member, error) {
	name = strings.TrimSpace(name)
	if token == "" {
		return nil, errInvalidInvitation
	}
	hash := hashInviteToken(token)
	// Look the token up before paying for bcrypt, so a guessed token costs
	// the server nothing.
	a.mu.RLock()
	inv := a.findInvitationLocked(hash)
	valid := inv != nil && time.Now().Before(inv.ExpiresAt) && a.inviterValidLocked(inv)
	var existingHash string
	existing := false
	if valid {
		existing = a.findByEmailLocked(inv.Email) != nil
		existingHash = a.passwordHashForLocked(inv.Email)
	}
	a.mu.RUnlock()
	if !valid {
		return nil, errInvalidInvitation
	}

	var pw []byte
	if existing {
		// An existing account consents by proving its own password; an
		// account without one (OIDC/LDAP) cannot accept this way.
		if existingHash == "" || bcrypt.CompareHashAndPassword([]byte(existingHash), []byte(password)) != nil {
			return nil, &core.ValidationError{Field: "password", Message: "this address already has an account; enter its current password to accept"}
		}
	} else {
		if name == "" || len(name) > maxNameLength {
			return nil, &core.ValidationError{Field: "name", Message: fmt.Sprintf("name must be 1-%d characters", maxNameLength)}
		}
		if err := validatePassword(password); err != nil {
			return nil, &core.ValidationError{Field: "password", Message: err.Error()}
		}
		var err error
		if pw, err = bcrypt.GenerateFromPassword([]byte(password), bcryptCost); err != nil {
			return nil, fmt.Errorf("hash password: %w", err)
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now().UTC()
	inv = a.findInvitationLocked(hash)
	if inv == nil || !now.Before(inv.ExpiresAt) || !a.inviterValidLocked(inv) {
		return nil, errInvalidInvitation
	}
	rollback := a.rollbackStateLocked()
	user := a.findByEmailLocked(inv.Email)
	switch {
	case existing && user != nil && a.accounts[user.ID] != nil && a.passwordHashForLocked(inv.Email) == existingHash:
		rec := a.accounts[user.ID]
		if rec.Disabled {
			return nil, errInvalidInvitation
		}
		if _, ok := rec.Memberships[inv.Workspace]; !ok {
			rec.Memberships[inv.Workspace] = inv.Role
		}
		rec.UpdatedAt = now
	case !existing && user == nil:
		if len(a.users) >= maxAccounts {
			return nil, &core.ConflictError{Message: fmt.Sprintf("this server holds at most %d accounts", maxAccounts)}
		}
		id, _ := generateID()
		user = &api.User{ID: id, Email: inv.Email, Name: name, Role: string(inv.Role), Workspace: inv.Workspace, CreatedAt: now}
		a.users[id] = user
		a.accounts[id] = &accountRecord{
			PasswordHash: string(pw),
			Source:       SourceLocal,
			Memberships:  map[string]core.MemberRole{inv.Workspace: inv.Role},
			UpdatedAt:    now,
		}
	default:
		// The account appeared, disappeared or changed its password while
		// the password was being checked.
		return nil, &core.ConflictError{Message: "the account changed while accepting; try again"}
	}
	delete(a.invitations, inv.ID)
	if err := a.persistOrRollbackLocked(rollback); err != nil {
		return nil, err
	}
	m := a.memberViewLocked(user, inv.Workspace)
	return &m, nil
}

// inviterValidLocked reports whether the invitation's inviter still could
// issue it: an enabled account holding members:* in the invited workspace,
// at or above the invited role.
func (a *LocalAuthenticator) inviterValidLocked(inv *invitationRecord) bool {
	inviter := a.users[inv.InvitedByID]
	if inv.InvitedByID == "" || inviter == nil || a.accounts[inv.InvitedByID] == nil {
		return false
	}
	view := copyUser(inviter)
	view.Workspace = inv.Workspace
	resolved, err := a.resolveLocked(view)
	if err != nil || resolved.Workspace != inv.Workspace {
		return false
	}
	role := core.MemberRole(resolved.Role)
	return role.Can("members:*") && core.RoleWithin(inv.Role, role)
}

// ProvisionExternalUser records an OIDC or LDAP sign-in in the directory and
// returns the account as it should be authenticated: a first sign-in creates
// the account as a viewer of the default workspace, later ones keep the
// roles an administrator gave it. An email that belongs to an account of
// another source is refused rather than linked, so an identity provider can
// never take over a local password account.
func (a *LocalAuthenticator) ProvisionExternalUser(email, name, source string) (*api.User, error) {
	email = strings.TrimSpace(email)
	if err := validateEmail(email); err != nil {
		return nil, fmt.Errorf("identity email refused: %w", err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	rollback := a.rollbackStateLocked()
	now := time.Now().UTC()
	user := a.findByEmailLocked(email)
	if user != nil {
		rec := a.accounts[user.ID]
		if rec == nil || rec.source() != source {
			return nil, fmt.Errorf("an account with this email already signs in with another method")
		}
		if rec.Disabled {
			return nil, errAccountDisabled
		}
		if name != "" && name != user.Name && len(name) <= maxNameLength {
			user.Name = name
		}
		rec.LastLoginAt = &now
	} else {
		if len(a.users) >= maxAccounts {
			return nil, fmt.Errorf("this server holds at most %d accounts", maxAccounts)
		}
		// A first sign-in joins the default workspace, so it counts against
		// max_team_members like an invitation would.
		if err := a.checkMemberQuotaLocked("default", now); err != nil {
			return nil, err
		}
		id, _ := generateID()
		if name == "" || len(name) > maxNameLength {
			name = email
		}
		user = &api.User{ID: id, Email: email, Name: name, Role: string(core.RoleViewer), Workspace: "default", CreatedAt: now}
		a.users[id] = user
		a.accounts[id] = &accountRecord{
			Source:      source,
			Memberships: map[string]core.MemberRole{"default": core.RoleViewer},
			UpdatedAt:   now,
			LastLoginAt: &now,
		}
	}
	if err := a.persistOrRollbackLocked(rollback); err != nil {
		return nil, err
	}
	return a.resolveLocked(user)
}

// ResolveExternalUser applies the directory to a user an OIDC or LDAP
// session carries: its current role in the session's workspace, or an error
// once the account was disabled or deleted. Users the directory does not
// know are returned unchanged.
func (a *LocalAuthenticator) ResolveExternalUser(u *api.User) (*api.User, error) {
	if u == nil {
		return nil, errors.New("user not found")
	}
	if a == nil {
		return copyUser(u), nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	stored := a.users[u.ID]
	if stored == nil {
		// Not provisioned here (deleted accounts have their sessions
		// revoked through onRevokeUser).
		return copyUser(u), nil
	}
	view := copyUser(stored)
	view.Workspace = u.Workspace
	return a.resolveLocked(view)
}

// CheckKeyOwner accepts an API key's owner while it is an enabled account
// holding at least role in workspace. It backs APIKeyService.SetOwnerCheck.
func (a *LocalAuthenticator) CheckKeyOwner(ownerID, workspace string, role core.MemberRole) error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	user := a.users[ownerID]
	if user == nil || a.accounts[ownerID] == nil {
		return errors.New("key owner no longer exists")
	}
	view := copyUser(user)
	view.Workspace = workspaceOrDefault(workspace)
	resolved, err := a.resolveLocked(view)
	if err != nil {
		return fmt.Errorf("key owner: %w", err)
	}
	if resolved.Workspace != view.Workspace || !core.RoleWithin(role, core.MemberRole(resolved.Role)) {
		return errors.New("key owner no longer holds the key's role in its workspace")
	}
	return nil
}
