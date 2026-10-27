package api

import (
	"errors"
	"html"
	"net/http"
	"net/url"
	"strings"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/mail"
)

// userDirectory returns the directory behind the configured authenticator,
// or nil when it has none (test doubles, custom authenticators).
func (s *RESTServer) userDirectory() UserDirectory {
	switch a := s.auth.(type) {
	case UserDirectory:
		return a
	case userDirectoryProvider:
		return a.UserDirectory()
	}
	return nil
}

// isWorkspaceMember reports whether the caller holds a role in workspace.
func (s *RESTServer) isWorkspaceMember(ctx *Context, workspace string) bool {
	dir := s.userDirectory()
	return dir != nil && ctx.User != nil && !ctx.User.IsAPIKey() && dir.IsMember(ctx.User.ID, workspace)
}

// membersUnavailable refuses member management without a directory, and
// for API keys: managing people is interactive-only, like key management.
func (s *RESTServer) membersUnavailable(ctx *Context) (UserDirectory, bool, error) {
	if done, err := rejectAPIKey(ctx); done {
		return nil, true, err
	}
	dir := s.userDirectory()
	if dir == nil {
		return nil, true, ctx.Error(http.StatusNotImplemented, "member management is not supported by this authenticator")
	}
	return dir, false, nil
}

// memberError maps a directory error to its HTTP answer.
func (s *RESTServer) memberError(ctx *Context, err error, action string) error {
	if done, herr := quotaExceeded(ctx, err); done {
		return herr
	}
	if done, herr := s.leaderUnavailable(ctx, err); done {
		return herr
	}
	var (
		verr *core.ValidationError
		ferr *core.ForbiddenError
		nerr *core.NotFoundError
		cerr *core.ConflictError
	)
	switch {
	case errors.As(err, &verr):
		return ctx.Error(http.StatusBadRequest, verr.Message)
	case errors.As(err, &ferr):
		return ctx.Error(http.StatusForbidden, ferr.Message)
	case errors.As(err, &nerr):
		return ctx.Error(http.StatusNotFound, nerr.Entity+" not found")
	case errors.As(err, &cerr):
		return ctx.Error(http.StatusConflict, cerr.Message)
	}
	return s.internalError(ctx, err, action)
}

// endUserStreams closes the live WebSocket/SSE streams of a user whose
// sessions were just revoked, and any other stream whose token no longer
// authenticates.
func (s *RESTServer) endUserStreams(userID string) {
	s.sessions.revoke(func(st liveStream) bool { return st.userID == userID })
	s.revokeInvalidLiveSessions()
}

func (s *RESTServer) auditMember(ctx *Context, target, action string, details map[string]any) {
	if s.audit != nil {
		s.audit.Log("member", ctx.User.ID, target, action, "success", details)
	}
}

func (s *RESTServer) handleListMembers(ctx *Context) error {
	dir, done, err := s.membersUnavailable(ctx)
	if done {
		return err
	}
	members, err := dir.ListMembers(contextWorkspace(ctx))
	if err != nil {
		return s.memberError(ctx, err, "list members")
	}
	return ctx.JSON(http.StatusOK, members)
}

func (s *RESTServer) handleCreateMember(ctx *Context) error {
	dir, done, err := s.membersUnavailable(ctx)
	if done {
		return err
	}
	var req CreateUserRequest
	if err := ctx.Bind(&req); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid request body")
	}
	member, err := dir.CreateUser(ctx.User, req)
	if err != nil {
		return s.memberError(ctx, err, "create member")
	}
	s.auditMember(ctx, "user/"+member.ID, "create", map[string]any{"role": string(member.Role), "workspace": member.Workspace})
	return ctx.JSON(http.StatusCreated, member)
}

func (s *RESTServer) handleUpdateMember(ctx *Context) error {
	dir, done, err := s.membersUnavailable(ctx)
	if done {
		return err
	}
	var req MemberUpdate
	if err := ctx.Bind(&req); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid request body")
	}
	if req.Role == nil && req.Disabled == nil && req.Name == nil {
		return ctx.Error(http.StatusBadRequest, "nothing to update: set role, disabled or name")
	}
	id := ctx.Params["id"]
	change, err := dir.UpdateMember(ctx.User, id, req)
	if err != nil {
		return s.memberError(ctx, err, "update member")
	}
	if change.Revoked {
		s.endUserStreams(id)
	}
	details := map[string]any{"workspace": change.Member.Workspace, "sessions_revoked": change.Revoked}
	if req.Role != nil {
		details["role"] = string(*req.Role)
	}
	if req.Disabled != nil {
		details["disabled"] = *req.Disabled
	}
	s.auditMember(ctx, "user/"+id, "update", details)
	return ctx.JSON(http.StatusOK, change.Member)
}

func (s *RESTServer) handleRemoveMember(ctx *Context) error {
	dir, done, err := s.membersUnavailable(ctx)
	if done {
		return err
	}
	id := ctx.Params["id"]
	change, err := dir.RemoveMember(ctx.User, id)
	if err != nil {
		return s.memberError(ctx, err, "remove member")
	}
	s.endUserStreams(id)
	s.auditMember(ctx, "user/"+id, "remove", map[string]any{"workspace": change.Member.Workspace})
	return ctx.JSON(http.StatusNoContent, nil)
}

func (s *RESTServer) handleDeleteUser(ctx *Context) error {
	dir, done, err := s.membersUnavailable(ctx)
	if done {
		return err
	}
	id := ctx.Params["id"]
	if _, err := dir.DeleteUser(ctx.User, id); err != nil {
		return s.memberError(ctx, err, "delete user")
	}
	s.endUserStreams(id)
	s.auditMember(ctx, "user/"+id, "delete", nil)
	return ctx.JSON(http.StatusNoContent, nil)
}

// invitationLink is where an invitee accepts: absolute when the server
// knows its public URL, otherwise a path the dashboard completes with its
// own origin.
func (s *RESTServer) invitationLink(token string) string {
	path := "/accept-invite?" + url.Values{"token": {token}}.Encode()
	if s.resetMail != nil && s.resetMail.publicURL != "" {
		return s.resetMail.publicURL + path
	}
	return path
}

func invitationMessage(to, link, inviter, workspace string, role core.MemberRole) mail.Message {
	return mail.Message{
		To:      to,
		Subject: "You are invited to AnubisWatch",
		Kind:    "invitation",
		Text: inviter + " invited you to the AnubisWatch workspace \"" + workspace + "\" as " + string(role) + ".\n\n" +
			"Set your name and password within seven days:\n" + link + "\n\n" +
			"If you did not expect this, ignore this email.",
		HTML: `<!DOCTYPE html><html><body style="font-family:Arial,sans-serif;line-height:1.6;color:#333">` +
			`<p>` + html.EscapeString(inviter) + ` invited you to the AnubisWatch workspace <strong>` + html.EscapeString(workspace) +
			`</strong> as ` + html.EscapeString(string(role)) + `.</p>` +
			`<p><a href="` + html.EscapeString(link) + `">Accept the invitation</a> within seven days.</p>` +
			`<p>If you did not expect this, ignore this email.</p></body></html>`,
	}
}

func (s *RESTServer) handleCreateInvitation(ctx *Context) error {
	dir, done, err := s.membersUnavailable(ctx)
	if done {
		return err
	}
	var req struct {
		Email string          `json:"email"`
		Role  core.MemberRole `json:"role"`
	}
	if err := ctx.Bind(&req); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid request body")
	}
	inv, token, err := dir.CreateInvitation(ctx.User, req.Email, req.Role)
	if err != nil {
		return s.memberError(ctx, err, "create invitation")
	}
	link := s.invitationLink(token)
	resp := struct {
		Invitation
		// EmailSent reports that the link was queued to the invitee. When
		// it is false, InviteURL carries the link -- shown this once -- for
		// the administrator to pass on.
		EmailSent bool   `json:"email_sent"`
		InviteURL string `json:"invite_url,omitempty"`
	}{Invitation: *inv}
	if s.resetMail != nil {
		resp.EmailSent = s.resetMail.queue.Enqueue(invitationMessage(inv.Email, link, ctx.User.Email, inv.Workspace, inv.Role))
		if !resp.EmailSent {
			s.logger.Warn("invitation email could not be queued", "invitation_id", inv.ID)
		}
	}
	if !resp.EmailSent {
		resp.InviteURL = link
	}
	s.auditMember(ctx, "invitation/"+inv.ID, "create", map[string]any{"role": string(inv.Role), "workspace": inv.Workspace, "email_sent": resp.EmailSent})
	return ctx.JSON(http.StatusCreated, resp)
}

func (s *RESTServer) handleListInvitations(ctx *Context) error {
	dir, done, err := s.membersUnavailable(ctx)
	if done {
		return err
	}
	invs, err := dir.ListInvitations(contextWorkspace(ctx))
	if err != nil {
		return s.memberError(ctx, err, "list invitations")
	}
	return ctx.JSON(http.StatusOK, invs)
}

func (s *RESTServer) handleRevokeInvitation(ctx *Context) error {
	dir, done, err := s.membersUnavailable(ctx)
	if done {
		return err
	}
	id := ctx.Params["id"]
	if err := dir.RevokeInvitation(ctx.User, id); err != nil {
		return s.memberError(ctx, err, "revoke invitation")
	}
	s.auditMember(ctx, "invitation/"+id, "revoke", nil)
	return ctx.JSON(http.StatusNoContent, nil)
}

// handleAcceptInvitation is public: the token is the credential. It creates
// the account; the invitee then signs in normally.
func (s *RESTServer) handleAcceptInvitation(ctx *Context) error {
	dir := s.userDirectory()
	if dir == nil {
		return ctx.Error(http.StatusNotImplemented, "member management is not supported by this authenticator")
	}
	var req struct {
		Token    string `json:"token"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := ctx.Bind(&req); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid request body")
	}
	member, err := dir.AcceptInvitation(strings.TrimSpace(req.Token), req.Name, req.Password)
	if err != nil {
		return s.memberError(ctx, err, "accept invitation")
	}
	if s.audit != nil {
		s.audit.Log("member", member.ID, "user/"+member.ID, "accept_invitation", "success", map[string]any{"workspace": member.Workspace})
	}
	return ctx.JSON(http.StatusCreated, member)
}
