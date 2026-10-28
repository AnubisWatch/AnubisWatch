package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// The whole-page PUT and create must not write incidents or announcements:
// that path skips their validation (control characters, lengths, caps,
// affected souls) and lets a GET-then-PUT client drop updates posted in
// between. The stored values always win.
func TestStatusPagePutCannotWriteIncidentsOrAnnouncements(t *testing.T) {
	server, store := newIncidentServer(t)
	rec := incidentRequest(t, server, "valid-token", "POST", "/api/v1/status-pages/page-1/incidents", map[string]any{"title": "Real", "message": "posted"})
	inc := decodeIncident(t, rec)

	rec = incidentRequest(t, server, "valid-token", "PUT", "/api/v1/status-pages/page-1", map[string]any{
		"name": "Acme", "slug": "acme",
		"incidents": []map[string]any{{"id": "forged", "title": "bad\x01title", "status": "investigating",
			"affected_souls": []string{"soul-x"}}},
		"announcements": []map[string]any{{"id": "forged-a", "title": "x"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d %s", rec.Code, rec.Body.String())
	}
	page, _ := store.GetStatusPageNoCtx("page-1")
	if len(page.Incidents) != 1 || page.Incidents[0].ID != inc.ID || len(page.Announcements) != 0 {
		t.Fatalf("PUT wrote incidents/announcements: %+v / %+v", page.Incidents, page.Announcements)
	}
	// An explicit empty list does not clear them either.
	incidentRequest(t, server, "valid-token", "PUT", "/api/v1/status-pages/page-1", map[string]any{"name": "Acme", "slug": "acme", "incidents": []any{}})
	if page, _ = store.GetStatusPageNoCtx("page-1"); len(page.Incidents) != 1 {
		t.Fatalf("PUT cleared incidents")
	}

	rec = incidentRequest(t, server, "valid-token", "POST", "/api/v1/status-pages", map[string]any{
		"name": "New", "slug": "new-page",
		"incidents": []map[string]any{{"id": "forged", "title": "x\x00"}},
	})
	if rec.Code != http.StatusCreated || strings.Contains(rec.Body.String(), "forged") {
		t.Fatalf("create accepted incidents: %d %s", rec.Code, rec.Body.String())
	}
}

// One page is one storage record (and one Raft/WAL entry in a cluster), so
// its serialized size is capped well below the 1 MiB WAL entry limit.
func TestStatusPageRecordSizeIsBounded(t *testing.T) {
	server, store := newIncidentServer(t)
	rec := incidentRequest(t, server, "valid-token", "POST", "/api/v1/status-pages/page-1/incidents", map[string]any{"title": "t", "message": "m"})
	inc := decodeIncident(t, rec)
	path := "/api/v1/status-pages/page-1/incidents/" + inc.ID + "/updates"
	big := strings.Repeat("x", 5000)
	refused := false
	for i := 0; i < maxUpdatesPerIncident; i++ {
		rec = incidentRequest(t, server, "valid-token", "POST", path, map[string]any{"message": big, "status": "monitoring"})
		if rec.Code == http.StatusRequestEntityTooLarge {
			refused = true
			break
		}
		if rec.Code != http.StatusCreated {
			t.Fatalf("update %d = %d %s", i, rec.Code, rec.Body.String())
		}
	}
	if !refused {
		t.Fatal("page grew past its byte budget")
	}
	raw := store.pages["page-1"]
	if len(raw) > maxStatusPageRecordBytes {
		t.Fatalf("stored page is %d bytes, budget %d", len(raw), maxStatusPageRecordBytes)
	}
	// Shrinking an over-full page is always allowed.
	if rec = incidentRequest(t, server, "valid-token", "DELETE", "/api/v1/status-pages/page-1/incidents/"+inc.ID, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete on full page = %d", rec.Code)
	}
}

// Resolved incidents and finished maintenance older than the public 90-day
// history are dropped on the next write, so the record holds only what the
// page shows.
func TestStatusPageCompactsAgedOutHistory(t *testing.T) {
	server, store := newIncidentServer(t)
	page, _ := store.GetStatusPageNoCtx("page-1")
	old := time.Now().Add(-100 * 24 * time.Hour)
	recent := time.Now().Add(-10 * 24 * time.Hour)
	page.Incidents = []core.StatusIncident{
		{ID: "old-resolved", Title: "old", Status: core.StatusResolved, StartedAt: old, ResolvedAt: &old},
		{ID: "old-active", Title: "still open", Status: core.StatusMonitoring, StartedAt: old},
		{ID: "recent", Title: "recent", Status: core.StatusResolved, StartedAt: recent, ResolvedAt: &recent},
	}
	page.Announcements = []core.StatusAnnouncement{
		{ID: "old-a", Title: "old", StartsAt: old, EndsAt: old.Add(time.Hour)},
		{ID: "next-a", Title: "next", StartsAt: time.Now().Add(time.Hour), EndsAt: time.Now().Add(2 * time.Hour)},
	}
	if err := store.SaveStatusPageNoCtx(page); err != nil {
		t.Fatal(err)
	}
	if rec := incidentRequest(t, server, "valid-token", "POST", "/api/v1/status-pages/page-1/incidents", map[string]any{"title": "new", "message": "m"}); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d", rec.Code)
	}
	page, _ = store.GetStatusPageNoCtx("page-1")
	ids := map[string]bool{}
	for _, inc := range page.Incidents {
		ids[inc.ID] = true
	}
	for _, a := range page.Announcements {
		ids[a.ID] = true
	}
	if ids["old-resolved"] || ids["old-a"] || !ids["old-active"] || !ids["recent"] || !ids["next-a"] || len(page.Incidents) != 3 {
		t.Fatalf("after compaction: %v", ids)
	}
}
