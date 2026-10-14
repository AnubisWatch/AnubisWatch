package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"golang.org/x/crypto/bcrypt"
)

// Password-protected status pages: the API must set and clear the stored hash, and
// the hash must never appear in a response. The page-side behaviour (the correct
// password unlocks the page, a wrong one does not) is pinned in
// internal/statuspage/password_protection_regression_test.go.
//
// Passwords are generated per run so no credential-shaped literal lives here.

func passwordRegressionValue() string {
	return "pw-" + core.GenerateID() + "-" + core.GenerateID()
}

func statusPagePasswordServer(store Storage) *RESTServer {
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config:     core.ServerConfig{Host: "localhost", Port: 8080},
		authConfig: core.AuthConfig{Enabled: core.BoolPtr(true)},
		store:      store,
		router:     router,
		auth:       &mockAuthenticator{},
		logger:     newTestLogger(),
		cluster:    &mockClusterManager{},
	}
	router.Handle("POST", "/api/v1/status-pages", server.requireAuth(server.handleCreateStatusPage))
	router.Handle("GET", "/api/v1/status-pages", server.requireAuth(server.handleListStatusPages))
	router.Handle("GET", "/api/v1/status-pages/:id", server.requireAuth(server.handleGetStatusPage))
	router.Handle("PUT", "/api/v1/status-pages/:id", server.requireAuth(server.handleUpdateStatusPage))
	return server
}

func statusPagePasswordRequest(t *testing.T, server *RESTServer, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewBuffer(payload))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.router.ServeHTTP(rec, req)
	return rec
}

func statusPagePasswordStored(t *testing.T, store *mockStorage, id string) *core.StatusPage {
	t.Helper()
	page, ok := store.statusPages[id]
	if !ok {
		t.Fatalf("page %q was not stored", id)
	}
	return page
}

// TestStatusPagePasswordIsStoredAndNeverReturned covers the create path: the
// password is hashed into the stored page, and no response field carries the hash.
func TestStatusPagePasswordIsStoredAndNeverReturned(t *testing.T) {
	store := newMockStorage()
	server := statusPagePasswordServer(store)
	password := passwordRegressionValue()

	rec := statusPagePasswordRequest(t, server, "POST", "/api/v1/status-pages", map[string]any{
		"name": "Password Regression", "slug": "password-regression",
		"visibility": core.VisibilityProtected, "password": password,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if strings.Contains(body, "password_hash") {
		t.Errorf("the create response leaked the hash: %s", body)
	}
	if !strings.Contains(body, `"password_set":true`) {
		t.Errorf("the create response does not report password_set: %s", body)
	}
	if strings.Contains(body, password) {
		t.Errorf("the create response echoed the plaintext password: %s", body)
	}

	var created core.StatusPage
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	stored := statusPagePasswordStored(t, store, created.ID)
	if stored.PasswordHash == "" {
		t.Fatal("the stored page has no password hash")
	}
	if bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte(password)) != nil {
		t.Errorf("the stored hash does not match the supplied password: %q", stored.PasswordHash)
	}

	// Get and list must redact too.
	getRec := statusPagePasswordRequest(t, server, "GET", "/api/v1/status-pages/"+created.ID, nil)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200: %s", getRec.Code, getRec.Body.String())
	}
	if strings.Contains(getRec.Body.String(), "password_hash") {
		t.Errorf("the get response leaked the hash: %s", getRec.Body.String())
	}

	listRec := statusPagePasswordRequest(t, server, "GET", "/api/v1/status-pages", nil)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200: %s", listRec.Code, listRec.Body.String())
	}
	if strings.Contains(listRec.Body.String(), "password_hash") {
		t.Errorf("the list response leaked the hash: %s", listRec.Body.String())
	}
}

// TestStatusPagePasswordUpdateReplacesClearsAndPreserves pins the three update
// semantics: a new password replaces the hash, an empty one clears it, and an
// absent one leaves the stored hash alone.
func TestStatusPagePasswordUpdateReplacesClearsAndPreserves(t *testing.T) {
	store := newMockStorage()
	server := statusPagePasswordServer(store)
	first, second := passwordRegressionValue(), passwordRegressionValue()

	rec := statusPagePasswordRequest(t, server, "POST", "/api/v1/status-pages", map[string]any{
		"name": "Password Regression", "slug": "password-regression",
		"visibility": core.VisibilityProtected, "password": first,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var created core.StatusPage
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	original := statusPagePasswordStored(t, store, created.ID).PasswordHash

	// Omitting the password preserves the stored hash (a client cannot echo a hash
	// it never received).
	rec = statusPagePasswordRequest(t, server, "PUT", "/api/v1/status-pages/"+created.ID, map[string]any{
		"name": "Password Regression", "slug": "password-regression",
		"visibility": core.VisibilityProtected,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update (no password) status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := statusPagePasswordStored(t, store, created.ID).PasswordHash; got != original {
		t.Errorf("an update that omits the password changed the hash")
	}

	// A new password replaces it.
	rec = statusPagePasswordRequest(t, server, "PUT", "/api/v1/status-pages/"+created.ID, map[string]any{
		"name": "Password Regression", "slug": "password-regression",
		"visibility": core.VisibilityProtected, "password": second,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update (new password) status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	replaced := statusPagePasswordStored(t, store, created.ID).PasswordHash
	if replaced == original {
		t.Error("a new password did not change the stored hash")
	}
	if bcrypt.CompareHashAndPassword([]byte(replaced), []byte(second)) != nil {
		t.Error("the replaced hash does not match the new password")
	}
	if strings.Contains(rec.Body.String(), "password_hash") {
		t.Errorf("the update response leaked the hash: %s", rec.Body.String())
	}

	// An empty password clears it, and the page is no longer protected.
	rec = statusPagePasswordRequest(t, server, "PUT", "/api/v1/status-pages/"+created.ID, map[string]any{
		"name": "Password Regression", "slug": "password-regression",
		"visibility": core.VisibilityPublic, "password": "",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update (clear password) status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := statusPagePasswordStored(t, store, created.ID).PasswordHash; got != "" {
		t.Errorf("clearing the password left a hash: %q", got)
	}
	if strings.Contains(rec.Body.String(), `"password_set":true`) {
		t.Errorf("the response still reports password_set after clearing: %s", rec.Body.String())
	}
}

// TestStatusPagePasswordRejectsUnusableConfigurations covers the error paths: a
// client cannot set the hash directly, and a protected page must have a password
// (otherwise it would be unreachable forever).
func TestStatusPagePasswordRejectsUnusableConfigurations(t *testing.T) {
	store := newMockStorage()
	server := statusPagePasswordServer(store)

	// A client-supplied hash must be ignored: the stored hash is derived from the
	// plaintext password field only.
	password := passwordRegressionValue()
	rec := statusPagePasswordRequest(t, server, "POST", "/api/v1/status-pages", map[string]any{
		"name": "Password Regression", "slug": "password-regression",
		"visibility": core.VisibilityProtected, "password": password,
		"password_hash": "$2a$10$clientSuppliedHashValueThatMustBeIgnored",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var created core.StatusPage
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	stored := statusPagePasswordStored(t, store, created.ID).PasswordHash
	if stored == "$2a$10$clientSuppliedHashValueThatMustBeIgnored" {
		t.Error("a client-supplied password_hash was stored verbatim")
	}
	if bcrypt.CompareHashAndPassword([]byte(stored), []byte(password)) != nil {
		t.Error("the stored hash is not derived from the supplied password")
	}

	// A protected page with no password is refused: nothing could ever unlock it.
	rec = statusPagePasswordRequest(t, server, "POST", "/api/v1/status-pages", map[string]any{
		"name": "Locked Out", "slug": "locked-out", "visibility": core.VisibilityProtected,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("protected page without a password: status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "requires a password") {
		t.Errorf("unhelpful error for a protected page without a password: %s", rec.Body.String())
	}
}
