package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"golang.org/x/crypto/bcrypt"
)

// The status page handlers decided whether a request carried "password" or
// "enabled" with a substring search over the raw body. A value equal to the
// key counted as the field, so a page named "password" cleared its own stored
// password; and a key spelled in another case, which encoding/json still
// binds, was missed, so a new password was silently ignored.

func createProtectedPage(t *testing.T, server *RESTServer, store *mockStorage, password string) *core.StatusPage {
	t.Helper()
	rec := statusPagePasswordRequest(t, server, "POST", "/api/v1/status-pages", map[string]any{
		"name": "Field Presence", "slug": "field-presence",
		"visibility": core.VisibilityProtected, "password": password,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var created core.StatusPage
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	return statusPagePasswordStored(t, store, created.ID)
}

func TestStatusPageValueEqualToPasswordKeyKeepsStoredPassword(t *testing.T) {
	store := newMockStorage()
	server := statusPagePasswordServer(store)
	page := createProtectedPage(t, server, store, passwordRegressionValue())
	original := page.PasswordHash

	// Renamed to "password"; the request has no password field.
	rec := statusPagePasswordRequest(t, server, "PUT", "/api/v1/status-pages/"+page.ID, map[string]any{
		"name": "password", "slug": "field-presence", "visibility": core.VisibilityProtected,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("rename to \"password\" = %d %s, want 200: a value was taken for the password field", rec.Code, rec.Body.String())
	}
	if got := statusPagePasswordStored(t, store, page.ID).PasswordHash; got != original {
		t.Fatal("renaming the page changed its stored password")
	}
}

func TestStatusPagePasswordKeyInAnotherCaseReplacesPassword(t *testing.T) {
	store := newMockStorage()
	server := statusPagePasswordServer(store)
	page := createProtectedPage(t, server, store, passwordRegressionValue())
	replacement := passwordRegressionValue()

	rec := statusPagePasswordRequest(t, server, "PUT", "/api/v1/status-pages/"+page.ID, map[string]any{
		"name": "Field Presence", "slug": "field-presence",
		"visibility": core.VisibilityProtected, "Password": replacement,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	hash := statusPagePasswordStored(t, store, page.ID).PasswordHash
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(replacement)) != nil {
		t.Fatal(`"Password" was bound as the new password but the old hash was kept`)
	}
}

func TestStatusPageEnabledPresenceIsByKey(t *testing.T) {
	store := newMockStorage()
	server := statusPagePasswordServer(store)

	// A name equal to "enabled" is not an explicit enabled field: the create
	// default (enabled) applies.
	rec := statusPagePasswordRequest(t, server, "POST", "/api/v1/status-pages", map[string]any{
		"name": "enabled", "slug": "named-enabled",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var created core.StatusPage
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if !statusPagePasswordStored(t, store, created.ID).Enabled {
		t.Fatal(`a page named "enabled" was created disabled, so its public URL 404s`)
	}

	// "Enabled": false is an explicit opt-out, whatever the key's case.
	rec = statusPagePasswordRequest(t, server, "POST", "/api/v1/status-pages", map[string]any{
		"name": "Opted Out", "slug": "opted-out", "Enabled": false,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if statusPagePasswordStored(t, store, created.ID).Enabled {
		t.Fatal(`"Enabled": false was ignored and the page was created enabled`)
	}
}

func TestJSONHasTopLevelKey(t *testing.T) {
	cases := []struct {
		body string
		want bool
	}{
		{`{"password": "x"}`, true},
		{`{"password": null}`, true},
		{`{"Password": "x"}`, true},
		{`{"password": "x"}`, true},
		{`{"name": "password"}`, false},
		{`{"password_set": true}`, false},
		{`{"theme": {"password": "x"}}`, false},
		{`not json`, false},
		{`["password"]`, false},
	}
	for _, tc := range cases {
		if got := jsonHasTopLevelKey([]byte(tc.body), "password"); got != tc.want {
			t.Errorf("jsonHasTopLevelKey(%s) = %v, want %v", tc.body, got, tc.want)
		}
	}
}
