package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// A PUT body is a partial update: the StatusPageCreate schema in openapi.yaml
// requires only "name" and "slug", and documents the same "omit to keep the
// stored password" rule for "password" that "enabled" and "visibility"
// already follow by peeking the raw body.
//
// handleUpdateStatusPage bound the body into a fresh core.StatusPage, and a
// Go string, int or slice cannot tell an omitted field from an explicit zero
// value. Every optional field the body did not mention was therefore
// overwritten with its zero value: the description, logo, favicon, custom
// domain and uptime window were cleared, the theme was reset to the default,
// and the soul list, groups and incidents were dropped.

func statusPageSiblingFieldsStore(t *testing.T, store *mockStorage) {
	t.Helper()
	store.souls["soul-1"] = &core.Soul{ID: "soul-1", Name: "Checkout", WorkspaceID: "default", Enabled: true}
}

func statusPageSiblingFieldsCreate(t *testing.T, server *RESTServer) core.StatusPage {
	t.Helper()
	rec := statusPagePasswordRequest(t, server, "POST", "/api/v1/status-pages", map[string]any{
		"name":          "Sibling Fields",
		"slug":          "sibling-fields",
		"description":   "kept description",
		"logo_url":      "https://example.com/logo.png",
		"favicon_url":   "https://example.com/favicon.png",
		"custom_domain": "status.example.com",
		"theme": map[string]any{
			"primary_color":    "#123456",
			"background_color": "#654321",
			"text_color":       "#abcdef",
			"accent_color":     "#fedcba",
			"font_family":      "Inter",
			"custom_css":       ".x{}",
		},
		"uptime_days": 30,
		"souls":       []string{"soul-1"},
		"groups": []map[string]any{{
			"id": "group-1", "name": "Group One", "soul_ids": []string{"soul-1"},
		}},
		"incidents": []map[string]any{{
			"id": "incident-1", "title": "Checkout degraded", "status": "open",
		}},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var created core.StatusPage
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	return created
}

// TestStatusPageUpdateKeepsOmittedSiblingFields pins the partial-update
// contract: a PUT that mentions only the required fields keeps every optional
// field it left out.
func TestStatusPageUpdateKeepsOmittedSiblingFields(t *testing.T) {
	store := newMockStorage()
	statusPageSiblingFieldsStore(t, store)
	server := statusPagePasswordServer(store)
	created := statusPageSiblingFieldsCreate(t, server)

	rec := statusPagePasswordRequest(t, server, "PUT", "/api/v1/status-pages/"+created.ID, map[string]any{
		"name": "Sibling Fields Renamed",
		"slug": "sibling-fields",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got core.StatusPage
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode update response: %v", err)
	}

	if got.Description != "kept description" {
		t.Errorf("description = %q, want %q: an omitted field was cleared", got.Description, "kept description")
	}
	if got.LogoURL != "https://example.com/logo.png" {
		t.Errorf("logo_url = %q, want %q: an omitted field was cleared", got.LogoURL, "https://example.com/logo.png")
	}
	if got.FaviconURL != "https://example.com/favicon.png" {
		t.Errorf("favicon_url = %q, want %q: an omitted field was cleared", got.FaviconURL, "https://example.com/favicon.png")
	}
	if got.CustomDomain != "status.example.com" {
		t.Errorf("custom_domain = %q, want %q: an omitted field was cleared", got.CustomDomain, "status.example.com")
	}
	if got.Theme.PrimaryColor != "#123456" || got.Theme.BackgroundColor != "#654321" ||
		got.Theme.TextColor != "#abcdef" || got.Theme.AccentColor != "#fedcba" ||
		got.Theme.FontFamily != "Inter" || got.Theme.CustomCSS != ".x{}" {
		t.Errorf("theme = %+v, want the stored theme: an omitted field was reset", got.Theme)
	}
	if got.UptimeDays != 30 {
		t.Errorf("uptime_days = %d, want 30: an omitted field was reset", got.UptimeDays)
	}
	if len(got.Souls) != 1 || got.Souls[0] != "soul-1" {
		t.Errorf("souls = %v, want [soul-1]: an omitted field was dropped", got.Souls)
	}
	if len(got.Groups) != 1 || got.Groups[0].ID != "group-1" {
		t.Errorf("groups = %+v, want the stored group: an omitted field was dropped", got.Groups)
	}
	if len(got.Incidents) != 1 || got.Incidents[0].ID != "incident-1" {
		t.Errorf("incidents = %+v, want the stored incident: an omitted field was dropped", got.Incidents)
	}
}

// TestStatusPageUpdateAppliesExplicitSiblingFields is the control: a field the
// body does mention still takes effect, and an explicit empty value still
// clears it.
func TestStatusPageUpdateAppliesExplicitSiblingFields(t *testing.T) {
	store := newMockStorage()
	statusPageSiblingFieldsStore(t, store)
	server := statusPagePasswordServer(store)
	created := statusPageSiblingFieldsCreate(t, server)

	rec := statusPagePasswordRequest(t, server, "PUT", "/api/v1/status-pages/"+created.ID, map[string]any{
		"name":        "Sibling Fields",
		"slug":        "sibling-fields",
		"description": "new description",
		"uptime_days": 7,
		"souls":       []string{"soul-1"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got core.StatusPage
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if got.Description != "new description" {
		t.Errorf("description = %q, want %q: an explicit field was ignored", got.Description, "new description")
	}
	if got.UptimeDays != 7 {
		t.Errorf("uptime_days = %d, want 7: an explicit field was ignored", got.UptimeDays)
	}
	if len(got.Souls) != 1 || got.Souls[0] != "soul-1" {
		t.Errorf("souls = %v, want [soul-1]: an explicit field was ignored", got.Souls)
	}
}

// TestStatusPageUpdateClearsExplicitlyEmptySiblingFields pins the other half
// of the contract: a field the body does mention is authoritative, so an
// explicit "" or [] clears it rather than being treated as "omitted".
func TestStatusPageUpdateClearsExplicitlyEmptySiblingFields(t *testing.T) {
	store := newMockStorage()
	statusPageSiblingFieldsStore(t, store)
	server := statusPagePasswordServer(store)
	created := statusPageSiblingFieldsCreate(t, server)

	rec := statusPagePasswordRequest(t, server, "PUT", "/api/v1/status-pages/"+created.ID, map[string]any{
		"name":        "Sibling Fields",
		"slug":        "sibling-fields",
		"description": "",
		"souls":       []string{},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got core.StatusPage
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if got.Description != "" {
		t.Errorf("description = %q, want %q: an explicit empty value was ignored", got.Description, "")
	}
	if len(got.Souls) != 0 {
		t.Errorf("souls = %v, want []: an explicit empty list was ignored", got.Souls)
	}
}
