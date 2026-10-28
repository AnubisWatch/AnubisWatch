package auth

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// APIKeyStore persists API key records. Implementations keep keys under the
// workspace prefix ({workspace}/apikeys/{id}) and, in cluster mode, write
// through Raft like every other workspace resource.
type APIKeyStore interface {
	SaveAPIKey(key *core.APIKey) error
	// GetAPIKey finds a key by id in any workspace; *core.NotFoundError when
	// absent.
	GetAPIKey(id string) (*core.APIKey, error)
	ListAPIKeys(workspace string) ([]*core.APIKey, error)
	// RecordAPIKeyUse stores the last-use time separately from the key
	// record, so a usage write can never overwrite a concurrent revocation.
	RecordAPIKeyUse(key *core.APIKey, at time.Time) error
}

// DefaultAPIKeyTouchInterval throttles last_used_at writes per key.
const DefaultAPIKeyTouchInterval = 5 * time.Minute

// APIKeyService implements api.APIKeyManager.
type APIKeyService struct {
	store         APIKeyStore
	logger        *slog.Logger
	now           func() time.Time
	touchInterval time.Duration

	mu        sync.Mutex
	lastTouch map[string]time.Time
	// ownerCheck, when set, must accept the key's owner on every use: a key
	// stops working while its owner is disabled, deleted, removed from the
	// key's workspace or holds a role below the key's.
	ownerCheck func(ownerID, workspace string, role core.MemberRole) error
}

// SetOwnerCheck installs the owner check (see ownerCheck). Call before the
// service authenticates requests.
func (s *APIKeyService) SetOwnerCheck(check func(ownerID, workspace string, role core.MemberRole) error) {
	s.ownerCheck = check
}

var _ api.APIKeyManager = (*APIKeyService)(nil)

// NewAPIKeyService returns a service backed by store.
func NewAPIKeyService(store APIKeyStore, logger *slog.Logger) *APIKeyService {
	if logger == nil {
		logger = slog.Default()
	}
	return &APIKeyService{
		store:         store,
		logger:        logger.With("component", "api_keys"),
		now:           time.Now,
		touchInterval: DefaultAPIKeyTouchInterval,
		lastTouch:     make(map[string]time.Time),
	}
}

// AuthenticateKey implements api.APIKeyManager. Every failure mode answers
// with an error wrapping core.ErrAPIKeyInvalid, ErrAPIKeyRevoked or
// ErrAPIKeyExpired; the plaintext is never logged.
func (s *APIKeyService) AuthenticateKey(token string) (*api.User, error) {
	id, err := core.ParseAPIKey(token)
	if err != nil {
		return nil, core.ErrAPIKeyInvalid
	}
	key, err := s.store.GetAPIKey(id)
	if err != nil || key == nil {
		return nil, core.ErrAPIKeyInvalid
	}
	if !core.VerifyAPIKeyHash(token, key.SecretHash) {
		return nil, core.ErrAPIKeyInvalid
	}
	now := s.now().UTC()
	if key.RevokedAt != nil {
		return nil, core.ErrAPIKeyRevoked
	}
	if key.Expired(now) {
		return nil, core.ErrAPIKeyExpired
	}
	if !core.ValidAPIKeyRole(key.Role) || key.WorkspaceID == "" {
		return nil, core.ErrAPIKeyInvalid
	}
	if s.ownerCheck != nil {
		if err := s.ownerCheck(key.OwnerID, key.WorkspaceID, key.Role); err != nil {
			return nil, fmt.Errorf("%w: %v", core.ErrAPIKeyInvalid, err)
		}
	}
	s.touch(key, now)
	return &api.User{
		ID:        "apikey:" + key.ID,
		Email:     key.OwnerEmail,
		Name:      key.Name,
		Role:      string(key.Role),
		Workspace: key.WorkspaceID,
		CreatedAt: key.CreatedAt,
		APIKeyID:  key.ID,
	}, nil
}

// touch records last use at most once per touchInterval per key, off the
// request path. Failures (e.g. no Raft leader) only cost accuracy.
func (s *APIKeyService) touch(key *core.APIKey, now time.Time) {
	s.mu.Lock()
	last, ok := s.lastTouch[key.ID]
	if ok && now.Sub(last) < s.touchInterval {
		s.mu.Unlock()
		return
	}
	if key.LastUsedAt != nil && now.Sub(*key.LastUsedAt) < s.touchInterval {
		s.lastTouch[key.ID] = *key.LastUsedAt
		s.mu.Unlock()
		return
	}
	s.lastTouch[key.ID] = now
	s.mu.Unlock()
	go func() {
		if err := s.store.RecordAPIKeyUse(key, now); err != nil {
			s.logger.Debug("record API key use failed", "api_key_id", key.ID, "err", err)
		}
	}()
}

// CreateKey implements api.APIKeyManager.
func (s *APIKeyService) CreateKey(creator *api.User, name string, role core.MemberRole, expiresAt *time.Time) (*core.APIKey, string, error) {
	if creator == nil || creator.ID == "" {
		return nil, "", &core.ForbiddenError{Message: "an authenticated user is required"}
	}
	if creator.IsAPIKey() {
		return nil, "", &core.ForbiddenError{Message: "API keys cannot create API keys"}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, "", &core.ValidationError{Field: "name", Message: "name is required"}
	}
	if len(name) > core.APIKeyMaxNameLength {
		return nil, "", &core.ValidationError{Field: "name", Message: fmt.Sprintf("name must be at most %d characters", core.APIKeyMaxNameLength)}
	}
	if !core.ValidAPIKeyRole(role) {
		return nil, "", &core.ValidationError{Field: "role", Message: "role must be one of owner, admin, editor, api, viewer"}
	}
	if !core.RoleWithin(role, core.MemberRole(creator.Role)) {
		return nil, "", &core.ForbiddenError{Message: fmt.Sprintf("a key with role %q would exceed your role %q", role, creator.Role)}
	}
	now := s.now().UTC()
	if expiresAt != nil {
		exp := expiresAt.UTC()
		if !exp.After(now) {
			return nil, "", &core.ValidationError{Field: "expires_at", Message: "expires_at must be in the future"}
		}
		if exp.Sub(now) > core.APIKeyMaxLifetime {
			return nil, "", &core.ValidationError{Field: "expires_at", Message: "expires_at must be at most 366 days away"}
		}
		expiresAt = &exp
	}
	workspace := creator.Workspace
	if workspace == "" {
		workspace = "default"
	}
	id, plaintext, err := core.GenerateAPIKey()
	if err != nil {
		return nil, "", err
	}
	key := &core.APIKey{
		ID:          id,
		Name:        name,
		WorkspaceID: workspace,
		OwnerID:     creator.ID,
		OwnerEmail:  creator.Email,
		Role:        role,
		SecretHash:  core.HashAPIKey(plaintext),
		CreatedAt:   now,
		ExpiresAt:   expiresAt,
	}
	if err := s.store.SaveAPIKey(key); err != nil {
		return nil, "", err
	}
	s.logger.Info("API key created", "api_key_id", id, "workspace", workspace, "role", role, "owner_id", creator.ID)
	return key, plaintext, nil
}

// ListKeys implements api.APIKeyManager, newest first.
func (s *APIKeyService) ListKeys(workspace string) ([]*core.APIKey, error) {
	keys, err := s.store.ListAPIKeys(workspace)
	if err != nil {
		return nil, err
	}
	out := keys[:0]
	for _, k := range keys {
		if k != nil && k.WorkspaceID == workspace {
			out = append(out, k)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// RevokeKey implements api.APIKeyManager. A key in another workspace is
// reported as not found.
func (s *APIKeyService) RevokeKey(workspace, id, revokedBy string) (*core.APIKey, error) {
	key, err := s.store.GetAPIKey(id)
	if err != nil {
		var nf *core.NotFoundError
		if errors.As(err, &nf) {
			return nil, &core.NotFoundError{Entity: "api_key", ID: id}
		}
		return nil, err
	}
	if key == nil || key.WorkspaceID != workspace {
		return nil, &core.NotFoundError{Entity: "api_key", ID: id}
	}
	if key.RevokedAt != nil {
		return key, nil
	}
	now := s.now().UTC()
	key.RevokedAt = &now
	key.RevokedBy = revokedBy
	if err := s.store.SaveAPIKey(key); err != nil {
		return nil, err
	}
	s.logger.Info("API key revoked", "api_key_id", id, "workspace", workspace, "revoked_by", revokedBy)
	return key, nil
}
