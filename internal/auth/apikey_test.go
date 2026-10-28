package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// memAPIKeyStore keeps records as JSON, as the real store does, so tests can
// inspect exactly what would be persisted.
type memAPIKeyStore struct {
	mu    sync.Mutex
	data  map[string][]byte // id -> record JSON
	usage map[string]time.Time
	uses  int
}

func newMemAPIKeyStore() *memAPIKeyStore {
	return &memAPIKeyStore{data: map[string][]byte{}, usage: map[string]time.Time{}}
}

func (m *memAPIKeyStore) SaveAPIKey(k *core.APIKey) error {
	raw, err := json.Marshal(k)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[k.ID] = raw
	return nil
}

func (m *memAPIKeyStore) GetAPIKey(id string) (*core.APIKey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, ok := m.data[id]
	if !ok {
		return nil, &core.NotFoundError{Entity: "api_key", ID: id}
	}
	var k core.APIKey
	if err := json.Unmarshal(raw, &k); err != nil {
		return nil, err
	}
	if at, ok := m.usage[id]; ok {
		k.LastUsedAt = &at
	}
	return &k, nil
}

func (m *memAPIKeyStore) ListAPIKeys(ws string) ([]*core.APIKey, error) {
	m.mu.Lock()
	ids := make([]string, 0, len(m.data))
	for id := range m.data {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	var out []*core.APIKey
	for _, id := range ids {
		k, _ := m.GetAPIKey(id)
		if k.WorkspaceID == ws {
			out = append(out, k)
		}
	}
	return out, nil
}

func (m *memAPIKeyStore) RecordAPIKeyUse(k *core.APIKey, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.usage[k.ID] = at
	m.uses++
	return nil
}

func (m *memAPIKeyStore) useCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.uses
}

func adminUser(ws string) *api.User {
	return &api.User{ID: "user-admin", Email: "admin@example.com", Role: "admin", Workspace: ws}
}

func TestAPIKeyCreateUseRevoke(t *testing.T) {
	store := newMemAPIKeyStore()
	svc := NewAPIKeyService(store, nil)

	key, plaintext, err := svc.CreateKey(adminUser("default"), "ci", core.RoleEditor, nil)
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}
	user, err := svc.AuthenticateKey(plaintext)
	if err != nil {
		t.Fatalf("AuthenticateKey: %v", err)
	}
	if user.ID != "apikey:"+key.ID || user.APIKeyID != key.ID || user.Role != "editor" || user.Workspace != "default" {
		t.Fatalf("unexpected principal %+v", user)
	}

	if _, err := svc.RevokeKey("default", key.ID, "user-admin"); err != nil {
		t.Fatalf("RevokeKey: %v", err)
	}
	if _, err := svc.AuthenticateKey(plaintext); !errors.Is(err, core.ErrAPIKeyRevoked) {
		t.Fatalf("revoked key: err = %v, want ErrAPIKeyRevoked", err)
	}
	// Revoking again is a no-op that keeps the first revocation.
	again, err := svc.RevokeKey("default", key.ID, "someone-else")
	if err != nil || again.RevokedBy != "user-admin" {
		t.Fatalf("second revoke = %+v, %v", again, err)
	}
}

func TestAPIKeyAuthenticateRejects(t *testing.T) {
	store := newMemAPIKeyStore()
	svc := NewAPIKeyService(store, nil)
	clock := time.Now().UTC()
	svc.now = func() time.Time { return clock }

	exp := clock.Add(time.Hour)
	key, plaintext, err := svc.CreateKey(adminUser("default"), "short-lived", core.RoleViewer, &exp)
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}
	wrongSecret := core.APIKeyPrefix + key.ID + "_" + strings.Repeat("A", 43)

	tests := []struct {
		name  string
		token string
		at    time.Time
		want  error
	}{
		{"valid before expiry", plaintext, clock, nil},
		{"wrong secret", wrongSecret, clock, core.ErrAPIKeyInvalid},
		{"unknown id", core.APIKeyPrefix + strings.Repeat("0", 24) + "_x", clock, core.ErrAPIKeyInvalid},
		{"malformed", "anb_nope", clock, core.ErrAPIKeyInvalid},
		{"at expiry", plaintext, exp, core.ErrAPIKeyExpired},
		{"after expiry", plaintext, exp.Add(time.Second), core.ErrAPIKeyExpired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc.now = func() time.Time { return tt.at }
			_, err := svc.AuthenticateKey(tt.token)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestAPIKeyCreateValidation(t *testing.T) {
	svc := NewAPIKeyService(newMemAPIKeyStore(), nil)
	now := time.Now()
	past, tooFar, ok := now.Add(-time.Hour), now.Add(400*24*time.Hour), now.Add(30*24*time.Hour)
	keyUser := &api.User{ID: "apikey:x", Role: "owner", Workspace: "default", APIKeyID: "x"}
	editor := &api.User{ID: "u-ed", Role: "editor", Workspace: "default"}
	owner := &api.User{ID: "u-own", Role: "owner", Workspace: "default"}

	var verr *core.ValidationError
	var ferr *core.ForbiddenError
	tests := []struct {
		name    string
		creator *api.User
		keyName string
		role    core.MemberRole
		exp     *time.Time
		want    any // nil, &verr or &ferr
	}{
		{"admin creates viewer", adminUser("default"), "k", core.RoleViewer, &ok, nil},
		{"owner creates owner", owner, "k", core.RoleOwner, nil, nil},
		{"admin cannot create owner", adminUser("default"), "k", core.RoleOwner, nil, &ferr},
		{"editor cannot create admin", editor, "k", core.RoleAdmin, nil, &ferr},
		{"key cannot create key", keyUser, "k", core.RoleViewer, nil, &ferr},
		{"no creator", nil, "k", core.RoleViewer, nil, &ferr},
		{"empty name", adminUser("default"), "  ", core.RoleViewer, nil, &verr},
		{"long name", adminUser("default"), strings.Repeat("n", 101), core.RoleViewer, nil, &verr},
		{"unknown role", adminUser("default"), "k", core.MemberRole("root"), nil, &verr},
		{"expiry in the past", adminUser("default"), "k", core.RoleViewer, &past, &verr},
		{"expiry beyond a year", adminUser("default"), "k", core.RoleViewer, &tooFar, &verr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, plaintext, err := svc.CreateKey(tt.creator, tt.keyName, tt.role, tt.exp)
			switch target := tt.want.(type) {
			case nil:
				if err != nil || plaintext == "" {
					t.Fatalf("CreateKey failed: %v", err)
				}
			case **core.ValidationError:
				if !errors.As(err, target) {
					t.Fatalf("err = %v, want ValidationError", err)
				}
			case **core.ForbiddenError:
				if !errors.As(err, target) {
					t.Fatalf("err = %v, want ForbiddenError", err)
				}
			}
		})
	}
}

func TestAPIKeyWorkspaceIsolation(t *testing.T) {
	store := newMemAPIKeyStore()
	svc := NewAPIKeyService(store, nil)
	keyA, plainA, err := svc.CreateKey(adminUser("team-a"), "a", core.RoleAdmin, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.CreateKey(adminUser("team-b"), "b", core.RoleAdmin, nil); err != nil {
		t.Fatal(err)
	}

	user, err := svc.AuthenticateKey(plainA)
	if err != nil || user.Workspace != "team-a" {
		t.Fatalf("key A principal = %+v, %v; want workspace team-a", user, err)
	}
	listB, _ := svc.ListKeys("team-b")
	for _, k := range listB {
		if k.ID == keyA.ID {
			t.Fatal("team-b listing includes team-a's key")
		}
	}
	if _, err := svc.RevokeKey("team-b", keyA.ID, "user-b"); !isNotFound(err) {
		t.Fatalf("revoking another workspace's key: err = %v, want not found", err)
	}
	if _, err := svc.AuthenticateKey(plainA); err != nil {
		t.Fatalf("cross-workspace revoke attempt revoked the key: %v", err)
	}
}

func isNotFound(err error) bool {
	var nf *core.NotFoundError
	return errors.As(err, &nf)
}

func TestAPIKeyPersistsOnlyTheHash(t *testing.T) {
	var logs bytes.Buffer
	store := newMemAPIKeyStore()
	svc := NewAPIKeyService(store, slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))

	key, plaintext, err := svc.CreateKey(adminUser("default"), "ci", core.RoleViewer, nil)
	if err != nil {
		t.Fatal(err)
	}
	secret := plaintext[strings.LastIndex(plaintext, "_")+1:]
	if _, err := svc.AuthenticateKey(plaintext); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RevokeKey("default", key.ID, "user-admin"); err != nil {
		t.Fatal(err)
	}
	_, _ = svc.AuthenticateKey(plaintext)

	stored := string(store.data[key.ID])
	if strings.Contains(stored, secret) || strings.Contains(stored, plaintext) {
		t.Fatalf("stored record contains the secret: %s", stored)
	}
	if !strings.Contains(stored, core.HashAPIKey(plaintext)) {
		t.Fatalf("stored record lacks the SHA-256 hash: %s", stored)
	}
	if strings.Contains(logs.String(), secret) {
		t.Fatalf("logs contain the secret:\n%s", logs.String())
	}
}

func TestAPIKeyLastUsedIsThrottled(t *testing.T) {
	store := newMemAPIKeyStore()
	svc := NewAPIKeyService(store, nil)
	clock := time.Now().UTC()
	svc.now = func() time.Time { return clock }
	_, plaintext, err := svc.CreateKey(adminUser("default"), "ci", core.RoleViewer, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitUses := func(want int) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for store.useCount() != want && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if got := store.useCount(); got != want {
			t.Fatalf("usage writes = %d, want %d", got, want)
		}
	}
	for i := 0; i < 5; i++ {
		if _, err := svc.AuthenticateKey(plaintext); err != nil {
			t.Fatal(err)
		}
	}
	waitUses(1)
	clock = clock.Add(DefaultAPIKeyTouchInterval + time.Second)
	if _, err := svc.AuthenticateKey(plaintext); err != nil {
		t.Fatal(err)
	}
	waitUses(2)
}
