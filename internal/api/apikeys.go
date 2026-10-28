package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// APIKeyManager issues, lists, revokes and authenticates API keys
// ("anb_<id>_<secret>"). internal/auth implements it; the REST server, the
// WebSocket/SSE streams and (through KeyAwareAuthenticator) gRPC consult it
// for any bearer token carrying the API key prefix.
type APIKeyManager interface {
	// AuthenticateKey resolves a plaintext key to its principal: User.ID is
	// "apikey:<id>", Role and Workspace come from the key, APIKeyID is set.
	AuthenticateKey(token string) (*User, error)
	// CreateKey issues a key for creator in creator's workspace and returns
	// the stored record and the plaintext, which is never retrievable again.
	CreateKey(creator *User, name string, role core.MemberRole, expiresAt *time.Time) (*core.APIKey, string, error)
	ListKeys(workspace string) ([]*core.APIKey, error)
	RevokeKey(workspace, id, revokedBy string) (*core.APIKey, error)
}

// SetAPIKeys enables API key authentication and the /api/v1/api-keys
// endpoints. Call before the server starts serving requests.
func (s *RESTServer) SetAPIKeys(keys APIKeyManager) {
	s.apiKeys = keys
	if s.ws != nil && keys != nil && s.ws.authenticator != nil {
		s.ws.authenticator = &KeyAwareAuthenticator{Authenticator: s.ws.authenticator, Keys: keys}
	}
}

// tokenAuth is the authenticator for bearer tokens: API-key aware when keys
// are enabled, otherwise the configured session authenticator.
func (s *RESTServer) tokenAuth() Authenticator {
	if s.apiKeys == nil || s.auth == nil {
		return s.auth
	}
	return &KeyAwareAuthenticator{Authenticator: s.auth, Keys: s.apiKeys}
}

// KeyAwareAuthenticator routes tokens with the API key prefix to Keys and
// every other token, and every other method, to the wrapped Authenticator.
// It is only used where Authenticate is called; login, OIDC and workspace
// switching keep talking to the session authenticator directly.
type KeyAwareAuthenticator struct {
	Authenticator
	Keys APIKeyManager
}

// Authenticate implements Authenticator.
func (a *KeyAwareAuthenticator) Authenticate(token string) (*User, error) {
	if a.Keys != nil && core.IsAPIKeyToken(token) {
		return a.Keys.AuthenticateKey(token)
	}
	return a.Authenticator.Authenticate(token)
}

// rejectAPIKey refuses endpoints that need an interactive user (key
// management, password changes, workspace switching). It reports true when it
// wrote the refusal.
func rejectAPIKey(ctx *Context) (bool, error) {
	if ctx.User.IsAPIKey() {
		return true, ctx.Error(http.StatusForbidden, "API keys cannot call this endpoint; sign in as a user")
	}
	return false, nil
}

// apiKeyView is the API representation of a key: never the hash.
type apiKeyView struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Prefix      string     `json:"prefix"`
	WorkspaceID string     `json:"workspace_id"`
	OwnerID     string     `json:"owner_id"`
	OwnerEmail  string     `json:"owner_email,omitempty"`
	Role        string     `json:"role"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	RevokedBy   string     `json:"revoked_by,omitempty"`
}

func newAPIKeyView(k *core.APIKey, now time.Time) apiKeyView {
	return apiKeyView{
		ID:          k.ID,
		Name:        k.Name,
		Prefix:      k.DisplayPrefix(),
		WorkspaceID: k.WorkspaceID,
		OwnerID:     k.OwnerID,
		OwnerEmail:  k.OwnerEmail,
		Role:        string(k.Role),
		Status:      k.Status(now),
		CreatedAt:   k.CreatedAt,
		ExpiresAt:   k.ExpiresAt,
		LastUsedAt:  k.LastUsedAt,
		RevokedAt:   k.RevokedAt,
		RevokedBy:   k.RevokedBy,
	}
}

func (s *RESTServer) apiKeysUnavailable(ctx *Context) (bool, error) {
	if s.apiKeys == nil {
		return true, ctx.Error(http.StatusNotImplemented, "API keys are not enabled on this server")
	}
	return rejectAPIKey(ctx)
}

func (s *RESTServer) handleListAPIKeys(ctx *Context) error {
	if done, err := s.apiKeysUnavailable(ctx); done {
		return err
	}
	keys, err := s.apiKeys.ListKeys(contextWorkspace(ctx))
	if err != nil {
		return s.internalError(ctx, err, "list API keys")
	}
	now := time.Now().UTC()
	out := make([]apiKeyView, 0, len(keys))
	for _, k := range keys {
		out = append(out, newAPIKeyView(k, now))
	}
	return ctx.JSON(http.StatusOK, out)
}

type createAPIKeyRequest struct {
	Name      string     `json:"name"`
	Role      string     `json:"role"`
	ExpiresAt *time.Time `json:"expires_at"`
	// ExpiresInDays is a convenience alternative to expires_at (0 = never).
	ExpiresInDays int `json:"expires_in_days"`
}

func (s *RESTServer) handleCreateAPIKey(ctx *Context) error {
	if done, err := s.apiKeysUnavailable(ctx); done {
		return err
	}
	var req createAPIKeyRequest
	if err := ctx.Bind(&req); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid JSON")
	}
	if req.ExpiresAt != nil && req.ExpiresInDays != 0 {
		return ctx.Error(http.StatusBadRequest, "set expires_at or expires_in_days, not both")
	}
	if req.ExpiresInDays < 0 {
		return ctx.Error(http.StatusBadRequest, "expires_in_days must not be negative")
	}
	expires := req.ExpiresAt
	if req.ExpiresInDays > 0 {
		t := time.Now().UTC().Add(time.Duration(req.ExpiresInDays) * 24 * time.Hour)
		expires = &t
	}
	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = ctx.User.Role
	}
	key, plaintext, err := s.apiKeys.CreateKey(ctx.User, req.Name, core.MemberRole(role), expires)
	if err != nil {
		var verr *core.ValidationError
		var ferr *core.ForbiddenError
		switch {
		case errors.As(err, &verr):
			return ctx.Error(http.StatusBadRequest, verr.Message)
		case errors.As(err, &ferr):
			return ctx.Error(http.StatusForbidden, ferr.Error())
		}
		return s.internalError(ctx, err, "create API key")
	}
	if s.audit != nil {
		s.audit.Log("api_key", ctx.User.ID, "api_key/"+key.ID, "create", "success",
			map[string]any{"api_key_id": key.ID, "role": string(key.Role), "workspace": key.WorkspaceID})
	}
	return ctx.JSON(http.StatusCreated, struct {
		apiKeyView
		Key string `json:"key"`
	}{newAPIKeyView(key, time.Now().UTC()), plaintext})
}

func (s *RESTServer) handleRevokeAPIKey(ctx *Context) error {
	if done, err := s.apiKeysUnavailable(ctx); done {
		return err
	}
	id := ctx.Params["id"]
	key, err := s.apiKeys.RevokeKey(contextWorkspace(ctx), id, ctx.User.ID)
	if err != nil {
		if isNotFoundError(err) {
			return ctx.Error(http.StatusNotFound, "API key not found")
		}
		return s.internalError(ctx, err, "revoke API key")
	}
	if s.audit != nil {
		s.audit.Log("api_key", ctx.User.ID, "api_key/"+key.ID, "revoke", "success",
			map[string]any{"api_key_id": key.ID, "workspace": key.WorkspaceID})
	}
	return ctx.JSON(http.StatusOK, newAPIKeyView(key, time.Now().UTC()))
}
