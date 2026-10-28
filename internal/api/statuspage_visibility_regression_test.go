package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// TestStatusPageUpdateKeepsVisibilityWhenOmitted pins the update rule that a
// body which omits "visibility" must not change the stored visibility.
//
// Binding an omitted field leaves the zero value, and
// normalizeStatusPageForSave defaults an empty visibility to public, so
// without this rule an update that only renamed a protected page silently
// published that page's contents at the public /status/{slug} URL without its
// password. The dashboard always sends the field (web/src/pages/StatusPages.tsx
// sets visibility on every save), so this guards API clients that do not.
func TestStatusPageUpdateKeepsVisibilityWhenOmitted(t *testing.T) {
	store := newMockStorage()
	server := statusPagePasswordServer(store)
	password := passwordRegressionValue()

	rec := statusPagePasswordRequest(t, server, "POST", "/api/v1/status-pages", map[string]any{
		"name": "Visibility Regression", "slug": "visibility-regression",
		"visibility": core.VisibilityProtected, "password": password,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var created core.StatusPage
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	// An update that omits "visibility" must leave the page protected.
	upd := statusPagePasswordRequest(t, server, "PUT", "/api/v1/status-pages/"+created.ID, map[string]any{
		"name": "Visibility Regression Renamed", "slug": "visibility-regression",
	})
	if upd.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200: %s", upd.Code, upd.Body.String())
	}
	if !strings.Contains(upd.Body.String(), `"visibility":"`+string(core.VisibilityProtected)+`"`) {
		t.Errorf("the update response does not report the page as protected: %s", upd.Body.String())
	}
	stored := statusPagePasswordStored(t, store, created.ID)
	if stored.Visibility != core.VisibilityProtected {
		t.Errorf("an update that omitted visibility stored %q, want %q", stored.Visibility, core.VisibilityProtected)
	}
	if stored.PasswordHash == "" {
		t.Error("an update that omitted visibility cleared the stored password hash")
	}

	// An explicit change still applies: the page becomes public.
	pub := statusPagePasswordRequest(t, server, "PUT", "/api/v1/status-pages/"+created.ID, map[string]any{
		"name": "Visibility Regression", "slug": "visibility-regression",
		"visibility": core.VisibilityPublic,
	})
	if pub.Code != http.StatusOK {
		t.Fatalf("update to public status = %d, want 200: %s", pub.Code, pub.Body.String())
	}
	if got := statusPagePasswordStored(t, store, created.ID).Visibility; got != core.VisibilityPublic {
		t.Errorf("an explicit visibility change stored %q, want %q", got, core.VisibilityPublic)
	}

	// An explicit change back to protected still applies too.
	prot := statusPagePasswordRequest(t, server, "PUT", "/api/v1/status-pages/"+created.ID, map[string]any{
		"name": "Visibility Regression", "slug": "visibility-regression",
		"visibility": core.VisibilityProtected, "password": password,
	})
	if prot.Code != http.StatusOK {
		t.Fatalf("update to protected status = %d, want 200: %s", prot.Code, prot.Body.String())
	}
	if got := statusPagePasswordStored(t, store, created.ID).Visibility; got != core.VisibilityProtected {
		t.Errorf("an explicit visibility change stored %q, want %q", got, core.VisibilityProtected)
	}
}
