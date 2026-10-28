package core

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateAPIKeyRoundTrip(t *testing.T) {
	id, plaintext, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	if !strings.HasPrefix(plaintext, APIKeyPrefix+id+"_") {
		t.Fatalf("plaintext %q does not start with anb_<id>_", plaintext)
	}
	got, err := ParseAPIKey(plaintext)
	if err != nil || got != id {
		t.Fatalf("ParseAPIKey = %q, %v; want %q", got, err, id)
	}
	hash := HashAPIKey(plaintext)
	if strings.Contains(hash, plaintext) || len(hash) != 64 {
		t.Fatalf("hash %q is not a hex SHA-256", hash)
	}
	if !VerifyAPIKeyHash(plaintext, hash) {
		t.Fatal("VerifyAPIKeyHash rejected the matching key")
	}
	if VerifyAPIKeyHash(plaintext+"x", hash) {
		t.Fatal("VerifyAPIKeyHash accepted a different key")
	}
	_, other, _ := GenerateAPIKey()
	if other == plaintext {
		t.Fatal("two generated keys are equal")
	}
}

func TestParseAPIKeyRejectsMalformed(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"session token", strings.Repeat("ab", 32)},
		{"prefix only", "anb_"},
		{"no secret", "anb_0123456789abcdef01234567_"},
		{"no separator", "anb_0123456789abcdef01234567"},
		{"short id", "anb_0123_secret"},
		{"non-hex id", "anb_0123456789abcdef0123456z_secret"},
		{"oversized secret", "anb_0123456789abcdef01234567_" + strings.Repeat("a", 200)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseAPIKey(tt.token); err == nil {
				t.Fatalf("ParseAPIKey(%q) accepted a malformed key", tt.token)
			}
		})
	}
}

func TestRoleWithin(t *testing.T) {
	tests := []struct {
		role, limit MemberRole
		want        bool
	}{
		{RoleOwner, RoleOwner, true},
		{RoleAdmin, RoleOwner, true},
		{RoleOwner, RoleAdmin, false},
		{RoleAdmin, RoleAdmin, true},
		{RoleEditor, RoleAdmin, true},
		{RoleAPI, RoleAdmin, true},
		{RoleViewer, RoleAdmin, true},
		{RoleAdmin, RoleEditor, false},
		{RoleAPI, RoleEditor, true},
		{RoleEditor, RoleViewer, false},
		{RoleViewer, RoleViewer, true},
		{MemberRole("superuser"), RoleOwner, false},
		{RoleViewer, MemberRole(""), false},
	}
	for _, tt := range tests {
		if got := RoleWithin(tt.role, tt.limit); got != tt.want {
			t.Errorf("RoleWithin(%q, %q) = %v, want %v", tt.role, tt.limit, got, tt.want)
		}
	}
}

func TestAPIKeyStatus(t *testing.T) {
	now := time.Now()
	past, future := now.Add(-time.Minute), now.Add(time.Hour)
	tests := []struct {
		name string
		key  APIKey
		want string
	}{
		{"active without expiry", APIKey{}, "active"},
		{"active before expiry", APIKey{ExpiresAt: &future}, "active"},
		{"expired", APIKey{ExpiresAt: &past}, "expired"},
		{"revoked wins", APIKey{ExpiresAt: &past, RevokedAt: &past}, "revoked"},
	}
	for _, tt := range tests {
		if got := tt.key.Status(now); got != tt.want {
			t.Errorf("%s: Status = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestAdminCanManageAPIKeys(t *testing.T) {
	for role, want := range map[MemberRole]bool{RoleOwner: true, RoleAdmin: true, RoleEditor: false, RoleViewer: false, RoleAPI: false} {
		if got := role.Can("apikeys:*"); got != want {
			t.Errorf("%s.Can(apikeys:*) = %v, want %v", role, got, want)
		}
	}
}
