package api

import (
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Member is one account's membership of a workspace, as the members API
// reports it. It never carries a credential.
type Member struct {
	ID                 string          `json:"id"`
	Email              string          `json:"email"`
	Name               string          `json:"name"`
	Role               core.MemberRole `json:"role"`
	Workspace          string          `json:"workspace"`
	Source             string          `json:"source"` // local, oidc or ldap
	Disabled           bool            `json:"disabled"`
	MustChangePassword bool            `json:"must_change_password,omitempty"`
	// ConfiguredAdmin marks the account named by auth.local.admin_email. It
	// can be demoted but not disabled or deleted: the next start would
	// recreate it from the config file.
	ConfiguredAdmin bool       `json:"configured_admin,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	LastLoginAt     *time.Time `json:"last_login_at,omitempty"`
}

// Invitation is a pending invitation. The token is never part of it: only
// its hash is stored and it is shown exactly once, when the invitation is
// created.
type Invitation struct {
	ID        string          `json:"id"`
	Email     string          `json:"email"`
	Role      core.MemberRole `json:"role"`
	Workspace string          `json:"workspace"`
	InvitedBy string          `json:"invited_by"`
	CreatedAt time.Time       `json:"created_at"`
	ExpiresAt time.Time       `json:"expires_at"`
}

// CreateUserRequest creates a local account with an initial password, or,
// when the email already belongs to an account and Password is empty, adds
// that account to the actor's workspace.
type CreateUserRequest struct {
	Email    string          `json:"email"`
	Name     string          `json:"name"`
	Password string          `json:"password"`
	Role     core.MemberRole `json:"role"`
}

// MemberUpdate changes a member. Nil fields are left as they are.
type MemberUpdate struct {
	Role     *core.MemberRole `json:"role,omitempty"`
	Disabled *bool            `json:"disabled,omitempty"`
	Name     *string          `json:"name,omitempty"`
}

// MemberChange reports what a directory mutation did, so the REST layer
// can end live streams and revoke API keys of an account that lost access.
type MemberChange struct {
	Member *Member
	// Revoked is true when the account's sessions were ended (disable,
	// delete, removal or a role downgrade).
	Revoked bool
}

// UserDirectory manages accounts, workspace memberships and invitations.
// The local authenticator implements it (and the HA cluster authenticator,
// through its shared state); OIDC and LDAP expose the local directory they
// provision their users into. Every mutation is authorised against actor:
// it must be an interactive user (not an API key) holding members:* in its
// current workspace, may not grant or touch a role above its own, and may
// not leave a workspace without an enabled owner.
type UserDirectory interface {
	ListMembers(workspace string) ([]Member, error)
	CreateUser(actor *User, req CreateUserRequest) (*Member, error)
	UpdateMember(actor *User, userID string, update MemberUpdate) (*MemberChange, error)
	// RemoveMember removes userID from the actor's workspace.
	RemoveMember(actor *User, userID string) (*MemberChange, error)
	// DeleteUser deletes the account everywhere.
	DeleteUser(actor *User, userID string) (*MemberChange, error)
	// CreateInvitation returns the invitation and its one-time token.
	CreateInvitation(actor *User, email string, role core.MemberRole) (*Invitation, string, error)
	ListInvitations(workspace string) ([]Invitation, error)
	RevokeInvitation(actor *User, id string) error
	// AcceptInvitation redeems token, creating the account (or adding the
	// membership to an existing local account whose password matches).
	AcceptInvitation(token, name, password string) (*Member, error)
	// IsMember reports whether userID holds a role in workspace.
	IsMember(userID, workspace string) bool
	// CountMembers is the member count max_team_members is checked against:
	// members plus pending invitations.
	CountMembers(workspace string) (int, error)
}

// userDirectoryProvider is implemented by authenticators that keep their
// directory in an embedded authenticator (OIDC, LDAP).
type userDirectoryProvider interface {
	UserDirectory() UserDirectory
}
