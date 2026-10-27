package auth

import (
	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// The directory in HA mode: reads evaluate the quorum-confirmed shared state,
// writes are re-evaluated against it and committed with compare-and-swap,
// exactly like sessions. A rejected change (last owner, quota, role ceiling)
// leaves the shared state untouched because the snapshot does not change.

var _ api.UserDirectory = (*ClusterAuthenticator)(nil)
var _ api.UserDirectory = (*LocalAuthenticator)(nil)

// SetMemberQuota installs the max_team_members check on the evaluator.
func (a *ClusterAuthenticator) SetMemberQuota(check func(workspace string, current int) error) {
	a.local.SetMemberQuota(check)
}

func (a *ClusterAuthenticator) read(fn func()) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, err := a.load(); err != nil {
		return err
	}
	fn()
	return nil
}

// ListMembers implements api.UserDirectory.
func (a *ClusterAuthenticator) ListMembers(workspace string) (out []api.Member, err error) {
	if rerr := a.read(func() { out, err = a.local.ListMembers(workspace) }); rerr != nil {
		return nil, rerr
	}
	return out, err
}

// ListInvitations implements api.UserDirectory.
func (a *ClusterAuthenticator) ListInvitations(workspace string) (out []api.Invitation, err error) {
	if rerr := a.read(func() { out, err = a.local.ListInvitations(workspace) }); rerr != nil {
		return nil, rerr
	}
	return out, err
}

// IsMember implements api.UserDirectory.
func (a *ClusterAuthenticator) IsMember(userID, workspace string) (ok bool) {
	if err := a.read(func() { ok = a.local.IsMember(userID, workspace) }); err != nil {
		return false
	}
	return ok
}

// CountMembers implements api.UserDirectory.
func (a *ClusterAuthenticator) CountMembers(workspace string) (n int, err error) {
	if rerr := a.read(func() { n, err = a.local.CountMembers(workspace) }); rerr != nil {
		return 0, rerr
	}
	return n, err
}

// CreateUser implements api.UserDirectory.
func (a *ClusterAuthenticator) CreateUser(actor *api.User, req api.CreateUserRequest) (m *api.Member, err error) {
	err = a.mutate(func() error { var e error; m, e = a.local.CreateUser(actor, req); return e })
	if err != nil {
		return nil, err
	}
	return m, nil
}

// UpdateMember implements api.UserDirectory.
func (a *ClusterAuthenticator) UpdateMember(actor *api.User, userID string, update api.MemberUpdate) (c *api.MemberChange, err error) {
	err = a.mutate(func() error { var e error; c, e = a.local.UpdateMember(actor, userID, update); return e })
	if err != nil {
		return nil, err
	}
	return c, nil
}

// RemoveMember implements api.UserDirectory.
func (a *ClusterAuthenticator) RemoveMember(actor *api.User, userID string) (c *api.MemberChange, err error) {
	err = a.mutate(func() error { var e error; c, e = a.local.RemoveMember(actor, userID); return e })
	if err != nil {
		return nil, err
	}
	return c, nil
}

// DeleteUser implements api.UserDirectory.
func (a *ClusterAuthenticator) DeleteUser(actor *api.User, userID string) (c *api.MemberChange, err error) {
	err = a.mutate(func() error { var e error; c, e = a.local.DeleteUser(actor, userID); return e })
	if err != nil {
		return nil, err
	}
	return c, nil
}

// CreateInvitation implements api.UserDirectory. The token is returned only
// once its invitation is committed.
func (a *ClusterAuthenticator) CreateInvitation(actor *api.User, email string, role core.MemberRole) (inv *api.Invitation, token string, err error) {
	err = a.mutate(func() error { var e error; inv, token, e = a.local.CreateInvitation(actor, email, role); return e })
	if err != nil {
		return nil, "", err
	}
	return inv, token, nil
}

// RevokeInvitation implements api.UserDirectory.
func (a *ClusterAuthenticator) RevokeInvitation(actor *api.User, id string) error {
	return a.mutate(func() error { return a.local.RevokeInvitation(actor, id) })
}

// AcceptInvitation implements api.UserDirectory.
func (a *ClusterAuthenticator) AcceptInvitation(token, name, password string) (m *api.Member, err error) {
	err = a.mutate(func() error { var e error; m, e = a.local.AcceptInvitation(token, name, password); return e })
	if err != nil {
		return nil, err
	}
	return m, nil
}

// CheckKeyOwner implements the API key owner check against shared state.
func (a *ClusterAuthenticator) CheckKeyOwner(ownerID, workspace string, role core.MemberRole) (err error) {
	if rerr := a.read(func() { err = a.local.CheckKeyOwner(ownerID, workspace, role) }); rerr != nil {
		return rerr
	}
	return err
}

// RequestPasswordResetTo issues a reset token through the shared state and
// returns the account's stored address.
func (a *ClusterAuthenticator) RequestPasswordResetTo(email string) (token, recipient string, err error) {
	err = a.mutate(func() error { var e error; token, recipient, e = a.local.RequestPasswordResetTo(email); return e })
	if err != nil {
		return "", "", err
	}
	return token, recipient, nil
}
