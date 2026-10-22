package api

// A status-page slug names one public URL, so it may name one status page.
//
// The public /status/{slug} URL carries no workspace, and Storage resolves a
// slug on its own: CobaltDB.GetStatusPageBySlug returns the first enabled
// page with a matching slug, and the candidate list comes from a map
// iteration, so which page answers varies per request. When two published
// pages shared a slug, a password-protected page's URL could serve another
// page's content without asking for the password.
//
// These tests pin the write boundary that keeps the namespace unambiguous.

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// TestStatusPageSlugIsUniquePerPublicURL pins that a slug cannot be claimed
// by a second published status page, on either write path.
func TestStatusPageSlugIsUniquePerPublicURL(t *testing.T) {
	store := newMockStorage()
	server := statusPagePasswordServer(store)

	// A password-protected page owns its public URL.
	protectedRec := statusPagePasswordRequest(t, server, "POST", "/api/v1/status-pages", map[string]any{
		"name": "Protected", "slug": "shared-slug",
		"visibility": core.VisibilityProtected, "password": passwordRegressionValue(),
	})
	if protectedRec.Code != http.StatusCreated {
		t.Fatalf("create protected page: status %d, want 201: %s", protectedRec.Code, protectedRec.Body.String())
	}
	var protected core.StatusPage
	if err := json.Unmarshal(protectedRec.Body.Bytes(), &protected); err != nil {
		t.Fatalf("decode protected page: %v", err)
	}

	// A second page may not claim that URL: the protected page's URL would
	// otherwise answer with whichever page the lookup reached first.
	dupRec := statusPagePasswordRequest(t, server, "POST", "/api/v1/status-pages", map[string]any{
		"name": "Public", "slug": "shared-slug", "visibility": core.VisibilityPublic,
	})
	if dupRec.Code != http.StatusConflict {
		t.Errorf("a duplicate slug was accepted: status %d, want %d: %s",
			dupRec.Code, http.StatusConflict, dupRec.Body.String())
	}
	if stored := len(store.statusPages); stored != 1 {
		t.Errorf("store holds %d pages, want 1: the duplicate was persisted", stored)
	}

	// The update path reaches the same store, so it can create the same
	// ambiguity by renaming a page onto another page's slug.
	otherRec := statusPagePasswordRequest(t, server, "POST", "/api/v1/status-pages", map[string]any{
		"name": "Other", "slug": "other-slug", "visibility": core.VisibilityPublic,
	})
	if otherRec.Code != http.StatusCreated {
		t.Fatalf("create other page: status %d, want 201: %s", otherRec.Code, otherRec.Body.String())
	}
	var other core.StatusPage
	if err := json.Unmarshal(otherRec.Body.Bytes(), &other); err != nil {
		t.Fatalf("decode other page: %v", err)
	}
	renameRec := statusPagePasswordRequest(t, server, "PUT", "/api/v1/status-pages/"+other.ID, map[string]any{
		"name": "Other", "slug": "shared-slug", "visibility": core.VisibilityPublic,
	})
	if renameRec.Code != http.StatusConflict {
		t.Errorf("renaming a page onto an existing slug was accepted: status %d, want %d: %s",
			renameRec.Code, http.StatusConflict, renameRec.Body.String())
	}
	if stored := len(store.statusPages); stored != 2 {
		t.Errorf("store holds %d pages, want 2: the rename was persisted", stored)
	}
}

// TestStatusPageSlugUpdateKeepsItsOwnSlug pins that the uniqueness check
// excludes the page being updated. An update re-sends its own slug, and
// treating that as a collision would refuse every legitimate edit.
func TestStatusPageSlugUpdateKeepsItsOwnSlug(t *testing.T) {
	store := newMockStorage()
	server := statusPagePasswordServer(store)

	rec := statusPagePasswordRequest(t, server, "POST", "/api/v1/status-pages", map[string]any{
		"name": "Self", "slug": "self-slug", "visibility": core.VisibilityPublic,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create page: status %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var page core.StatusPage
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}

	// The same slug, changed name: not a collision.
	keepRec := statusPagePasswordRequest(t, server, "PUT", "/api/v1/status-pages/"+page.ID, map[string]any{
		"name": "Self Renamed", "slug": "self-slug", "visibility": core.VisibilityPublic,
	})
	if keepRec.Code != http.StatusOK {
		t.Errorf("updating a page with its own slug: status %d, want 200: %s",
			keepRec.Code, keepRec.Body.String())
	}

	// A new, unused slug: not a collision.
	moveRec := statusPagePasswordRequest(t, server, "PUT", "/api/v1/status-pages/"+page.ID, map[string]any{
		"name": "Self Renamed", "slug": "moved-slug", "visibility": core.VisibilityPublic,
	})
	if moveRec.Code != http.StatusOK {
		t.Errorf("renaming a page to an unused slug: status %d, want 200: %s",
			moveRec.Code, moveRec.Body.String())
	}
	if got := statusPagePasswordStored(t, store, page.ID).Slug; got != "moved-slug" {
		t.Errorf("stored slug = %q, want moved-slug", got)
	}
}

// TestStatusPageUnpublishedSlugIsReusable pins that only published pages
// occupy a public URL. A disabled page is not reachable at /status/{slug}, so
// it does not shadow one that is, and it may be replaced.
func TestStatusPageUnpublishedSlugIsReusable(t *testing.T) {
	store := newMockStorage()
	server := statusPagePasswordServer(store)

	rec := statusPagePasswordRequest(t, server, "POST", "/api/v1/status-pages", map[string]any{
		"name": "Hidden", "slug": "hidden-slug", "visibility": core.VisibilityPublic, "enabled": false,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create disabled page: status %d, want 201: %s", rec.Code, rec.Body.String())
	}

	// The disabled page does not hold the public URL, so a published page may
	// take it.
	reuseRec := statusPagePasswordRequest(t, server, "POST", "/api/v1/status-pages", map[string]any{
		"name": "Live", "slug": "hidden-slug", "visibility": core.VisibilityPublic,
	})
	if reuseRec.Code != http.StatusCreated {
		t.Errorf("reusing a disabled page's slug: status %d, want 201: %s",
			reuseRec.Code, reuseRec.Body.String())
	}
}
