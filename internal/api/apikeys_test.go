package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// fakeKeyManager is a minimal in-memory APIKeyManager built on the core key
// helpers; internal/auth has the real service and its own tests.
type fakeKeyManager struct {
	mu   sync.Mutex
	keys map[string]*core.APIKey
}

func newFakeKeyManager() *fakeKeyManager { return &fakeKeyManager{keys: map[string]*core.APIKey{}} }

func (f *fakeKeyManager) AuthenticateKey(token string) (*User, error) {
	id, err := core.ParseAPIKey(token)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	k, ok := f.keys[id]
	if !ok || !core.VerifyAPIKeyHash(token, k.SecretHash) {
		return nil, core.ErrAPIKeyInvalid
	}
	if k.RevokedAt != nil {
		return nil, core.ErrAPIKeyRevoked
	}
	return &User{ID: "apikey:" + k.ID, Role: string(k.Role), Workspace: k.WorkspaceID, APIKeyID: k.ID}, nil
}

func (f *fakeKeyManager) CreateKey(creator *User, name string, role core.MemberRole, exp *time.Time) (*core.APIKey, string, error) {
	if !core.RoleWithin(role, core.MemberRole(creator.Role)) {
		return nil, "", &core.ForbiddenError{Message: "role exceeds creator"}
	}
	if strings.TrimSpace(name) == "" {
		return nil, "", &core.ValidationError{Field: "name", Message: "name is required"}
	}
	id, plain, err := core.GenerateAPIKey()
	if err != nil {
		return nil, "", err
	}
	k := &core.APIKey{ID: id, Name: name, WorkspaceID: creator.Workspace, OwnerID: creator.ID, Role: role,
		SecretHash: core.HashAPIKey(plain), CreatedAt: time.Now().UTC(), ExpiresAt: exp}
	f.mu.Lock()
	f.keys[id] = k
	f.mu.Unlock()
	return k, plain, nil
}

func (f *fakeKeyManager) ListKeys(ws string) ([]*core.APIKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*core.APIKey
	for _, k := range f.keys {
		if k.WorkspaceID == ws {
			out = append(out, k)
		}
	}
	return out, nil
}

func (f *fakeKeyManager) RevokeKey(ws, id, by string) (*core.APIKey, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k, ok := f.keys[id]
	if !ok || k.WorkspaceID != ws {
		return nil, &core.NotFoundError{Entity: "api_key", ID: id}
	}
	now := time.Now().UTC()
	k.RevokedAt, k.RevokedBy = &now, by
	return k, nil
}

type recordingAuditBackend struct {
	mu     sync.Mutex
	events []*AuditEvent
}

func (b *recordingAuditBackend) Write(e *AuditEvent) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, e)
	return nil
}
func (b *recordingAuditBackend) Query(AuditFilter) ([]*AuditEvent, error) { return nil, nil }

func (b *recordingAuditBackend) hasRequest(userID, method, path string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, e := range b.events {
		if e.EventType == "http_request" && e.UserID == userID && e.Action == method && e.Resource == path {
			return true
		}
	}
	return false
}

func apiKeyTestServer(t *testing.T, keys APIKeyManager) (*RESTServer, *recordingAuditBackend) {
	t.Helper()
	s := newTestServerWithStorage(newMockStorage())
	audit := &recordingAuditBackend{}
	s.audit = &AuditLogger{logger: newTestLogger(), backend: audit}
	if keys != nil {
		s.SetAPIKeys(keys)
	}
	t.Cleanup(func() { _ = s.Stop(t.Context()) })
	return s, audit
}

func doAPIKeyRequest(t *testing.T, s *RESTServer, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.RemoteAddr = "192.0.2.10:1234"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func createKeyViaAPI(t *testing.T, s *RESTServer, body string) (id, plaintext string) {
	t.Helper()
	rec := doAPIKeyRequest(t, s, http.MethodPost, "/api/v1/api-keys", "valid-token", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create key: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Key == "" {
		t.Fatalf("create response %s: %v", rec.Body.String(), err)
	}
	return out.ID, out.Key
}

func TestAPIKeyRESTLifecycle(t *testing.T) {
	keys := newFakeKeyManager()
	s, audit := apiKeyTestServer(t, keys)

	id, plaintext := createKeyViaAPI(t, s, `{"name":"ci","role":"viewer"}`)

	// The key authenticates as itself, with its role and workspace.
	rec := doAPIKeyRequest(t, s, http.MethodGet, "/api/v1/auth/me", plaintext, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"api_key_id":"`+id+`"`) {
		t.Fatalf("GET /auth/me with key: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doAPIKeyRequest(t, s, http.MethodGet, "/api/v1/souls", plaintext, ""); rec.Code != http.StatusOK {
		t.Fatalf("GET /souls with key: %d %s", rec.Code, rec.Body.String())
	}
	// A viewer key cannot write; the audit record names the key.
	if rec := doAPIKeyRequest(t, s, http.MethodPost, "/api/v1/souls", plaintext, `{"name":"x"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("POST /souls with viewer key: %d, want 403", rec.Code)
	}
	if !audit.hasRequest("apikey:"+id, http.MethodPost, "/api/v1/souls") {
		t.Fatal("audit log has no request attributed to apikey:" + id)
	}

	// Listing never exposes the hash or the plaintext.
	rec = doAPIKeyRequest(t, s, http.MethodGet, "/api/v1/api-keys", "valid-token", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "secret_hash") || strings.Contains(body, plaintext) || strings.Contains(body, core.HashAPIKey(plaintext)) {
		t.Fatalf("list leaks secret material: %s", body)
	}
	if !strings.Contains(body, `"prefix":"anb_`+id+`"`) || !strings.Contains(body, `"status":"active"`) {
		t.Fatalf("list misses the key: %s", body)
	}

	// Revoke, then the key is rejected.
	if rec := doAPIKeyRequest(t, s, http.MethodDelete, "/api/v1/api-keys/"+id, "valid-token", ""); rec.Code != http.StatusOK {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body.String())
	}
	if rec := doAPIKeyRequest(t, s, http.MethodGet, "/api/v1/souls", plaintext, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked key: %d, want 401", rec.Code)
	}
}

func TestAPIKeyCannotUseInteractiveEndpoints(t *testing.T) {
	s, _ := apiKeyTestServer(t, newFakeKeyManager())
	_, plaintext := createKeyViaAPI(t, s, `{"name":"admin-key","role":"admin"}`)

	tests := []struct {
		method, path, body string
	}{
		{http.MethodGet, "/api/v1/api-keys", ""},
		{http.MethodPost, "/api/v1/api-keys", `{"name":"child","role":"viewer"}`},
		{http.MethodDelete, "/api/v1/api-keys/0123456789abcdef01234567", ""},
		{http.MethodPut, "/api/v1/auth/change-password", `{"current_password":"a","new_password":"b"}`},
		{http.MethodPost, "/api/v1/auth/workspace", `{"workspace":"other"}`},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			if rec := doAPIKeyRequest(t, s, tt.method, tt.path, plaintext, tt.body); rec.Code != http.StatusForbidden {
				t.Fatalf("%s %s with an API key: %d %s, want 403", tt.method, tt.path, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAPIKeyEndpointPermissions(t *testing.T) {
	s, _ := apiKeyTestServer(t, newFakeKeyManager())
	tests := []struct {
		name, method, path, token, body string
		want                            int
	}{
		{"viewer cannot list", http.MethodGet, "/api/v1/api-keys", "viewer-token", "", http.StatusForbidden},
		{"viewer cannot create", http.MethodPost, "/api/v1/api-keys", "viewer-token", `{"name":"k"}`, http.StatusForbidden},
		{"admin cannot mint owner", http.MethodPost, "/api/v1/api-keys", "valid-token", `{"name":"k","role":"owner"}`, http.StatusForbidden},
		{"name required", http.MethodPost, "/api/v1/api-keys", "valid-token", `{"role":"viewer"}`, http.StatusBadRequest},
		{"both expiries", http.MethodPost, "/api/v1/api-keys", "valid-token", `{"name":"k","expires_in_days":5,"expires_at":"2099-01-01T00:00:00Z"}`, http.StatusBadRequest},
		{"negative days", http.MethodPost, "/api/v1/api-keys", "valid-token", `{"name":"k","expires_in_days":-1}`, http.StatusBadRequest},
		{"unknown key", http.MethodDelete, "/api/v1/api-keys/0123456789abcdef01234567", "valid-token", "", http.StatusNotFound},
		{"unauthenticated", http.MethodGet, "/api/v1/api-keys", "", "", http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := doAPIKeyRequest(t, s, tt.method, tt.path, tt.token, tt.body); rec.Code != tt.want {
				t.Fatalf("%d %s, want %d", rec.Code, rec.Body.String(), tt.want)
			}
		})
	}
}

func TestAPIKeysDisabledWithoutManager(t *testing.T) {
	s, _ := apiKeyTestServer(t, nil)
	if rec := doAPIKeyRequest(t, s, http.MethodGet, "/api/v1/api-keys", "valid-token", ""); rec.Code != http.StatusNotImplemented {
		t.Fatalf("list without manager: %d, want 501", rec.Code)
	}
	_, plaintext, _ := core.GenerateAPIKey()
	if rec := doAPIKeyRequest(t, s, http.MethodGet, "/api/v1/souls", plaintext, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("key without manager: %d, want 401", rec.Code)
	}
}

func TestKeyAwareAuthenticatorRoutesByPrefix(t *testing.T) {
	keys := newFakeKeyManager()
	_, plaintext, err := keys.CreateKey(&User{ID: "u", Role: "admin", Workspace: "default"}, "k", core.RoleViewer, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := &KeyAwareAuthenticator{Authenticator: &mockAuthenticator{}, Keys: keys}
	if u, err := a.Authenticate("valid-token"); err != nil || u.IsAPIKey() {
		t.Fatalf("session token: %+v, %v", u, err)
	}
	if u, err := a.Authenticate(plaintext); err != nil || !u.IsAPIKey() {
		t.Fatalf("API key: %+v, %v", u, err)
	}
	if _, err := a.Authenticate("anb_garbage"); err == nil {
		t.Fatal("malformed key accepted")
	}
}
