package core

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// API keys are long-lived bearer credentials for automation. A key is
// presented as "anb_<id>_<secret>": the id locates the stored record and the
// 32-byte random secret proves possession. Only the SHA-256 of the full key
// is persisted; the plaintext is returned exactly once, at creation.
const (
	// APIKeyPrefix starts every API key, so keys are recognizable in logs,
	// secret scanners and the Authorization header.
	APIKeyPrefix = "anb_"
	// APIKeyMaxLifetime caps expires_at at creation.
	APIKeyMaxLifetime = 366 * 24 * time.Hour
	// APIKeyMaxNameLength bounds the human-readable name.
	APIKeyMaxNameLength = 100

	apiKeyIDBytes     = 12
	apiKeySecretBytes = 32
)

// API key errors. ErrAPIKeyInvalid deliberately covers unknown, malformed and
// wrong-secret keys alike so callers cannot probe which part was wrong.
var (
	ErrAPIKeyInvalid = errors.New("invalid API key")
	ErrAPIKeyRevoked = errors.New("API key revoked")
	ErrAPIKeyExpired = errors.New("API key expired")
)

// APIKey is the stored record of an API key. SecretHash is the hex SHA-256 of
// the full key string and must never be returned by an API.
type APIKey struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	WorkspaceID string     `json:"workspace_id"`
	OwnerID     string     `json:"owner_id"`
	OwnerEmail  string     `json:"owner_email,omitempty"`
	Role        MemberRole `json:"role"`
	SecretHash  string     `json:"secret_hash"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	RevokedBy   string     `json:"revoked_by,omitempty"`
}

// DisplayPrefix is the non-secret head of the key ("anb_<id>"), safe to show
// in lists so users can tell keys apart.
func (k *APIKey) DisplayPrefix() string {
	return APIKeyPrefix + k.ID
}

// Expired reports whether the key has an expiry at or before now.
func (k *APIKey) Expired(now time.Time) bool {
	return k.ExpiresAt != nil && !now.Before(*k.ExpiresAt)
}

// Status is "active", "expired" or "revoked".
func (k *APIKey) Status(now time.Time) string {
	switch {
	case k.RevokedAt != nil:
		return "revoked"
	case k.Expired(now):
		return "expired"
	default:
		return "active"
	}
}

// GenerateAPIKey returns a new key id and the full plaintext key.
func GenerateAPIKey() (id, plaintext string, err error) {
	idb := make([]byte, apiKeyIDBytes)
	if _, err := rand.Read(idb); err != nil {
		return "", "", fmt.Errorf("generate API key id: %w", err)
	}
	secret := make([]byte, apiKeySecretBytes)
	if _, err := rand.Read(secret); err != nil {
		return "", "", fmt.Errorf("generate API key secret: %w", err)
	}
	id = hex.EncodeToString(idb)
	return id, APIKeyPrefix + id + "_" + base64.RawURLEncoding.EncodeToString(secret), nil
}

// IsAPIKeyToken reports whether a bearer token is shaped like an API key.
// Session tokens are hex and can never start with "anb_".
func IsAPIKeyToken(token string) bool {
	return strings.HasPrefix(token, APIKeyPrefix)
}

// ParseAPIKey extracts the key id from a plaintext key.
func ParseAPIKey(token string) (string, error) {
	if !IsAPIKeyToken(token) {
		return "", ErrAPIKeyInvalid
	}
	rest := strings.TrimPrefix(token, APIKeyPrefix)
	id, secret, ok := strings.Cut(rest, "_")
	if !ok || len(id) != 2*apiKeyIDBytes || secret == "" || len(secret) > 128 {
		return "", ErrAPIKeyInvalid
	}
	if _, err := hex.DecodeString(id); err != nil {
		return "", ErrAPIKeyInvalid
	}
	return id, nil
}

// HashAPIKey returns the hex SHA-256 of a plaintext key. A fast hash is
// appropriate: the secret carries 256 bits of entropy, so it cannot be
// brute-forced from the digest.
func HashAPIKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// VerifyAPIKeyHash compares a plaintext key against a stored hash in
// constant time.
func VerifyAPIKeyHash(plaintext, storedHash string) bool {
	computed := HashAPIKey(plaintext)
	return subtle.ConstantTimeCompare([]byte(computed), []byte(storedHash)) == 1
}

// apiKeyRoleRank orders roles for granting: nobody grants a role ranked
// above their own. "api" (souls plus judgments) ranks below editor, which
// adds read access to channels and rules: were they equal, an api member
// could mint editor keys and so gain permissions it does not hold.
var apiKeyRoleRank = map[MemberRole]int{
	RoleViewer: 1,
	RoleAPI:    2,
	RoleEditor: 3,
	RoleAdmin:  4,
	RoleOwner:  5,
}

// ValidAPIKeyRole reports whether role can be assigned to a key.
func ValidAPIKeyRole(role MemberRole) bool {
	return apiKeyRoleRank[role] > 0
}

// RoleWithin reports whether a key with role stays within a creator holding
// limit: the key's rank may not exceed the creator's.
func RoleWithin(role, limit MemberRole) bool {
	r, l := apiKeyRoleRank[role], apiKeyRoleRank[limit]
	return r > 0 && l > 0 && r <= l
}
