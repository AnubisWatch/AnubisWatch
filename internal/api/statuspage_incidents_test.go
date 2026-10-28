package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// incidentStore persists pages as JSON, like the real store, and serialises
// access so concurrent requests exercise the handler's own locking rather
// than the mock's maps. It also records every save, the hook the server uses
// to email subscribers.
type incidentStore struct {
	*mockStorage
	mu    sync.Mutex
	pages map[string][]byte
	saves []*core.StatusPage
}

func newIncidentStore() *incidentStore {
	return &incidentStore{mockStorage: newMockStorage(), pages: map[string][]byte{}}
}

func (s *incidentStore) GetStatusPageNoCtx(id string) (*core.StatusPage, error) {
	s.mu.Lock()
	raw, ok := s.pages[id]
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("status page not found")
	}
	var page core.StatusPage
	if err := json.Unmarshal(raw, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

func (s *incidentStore) SaveStatusPageNoCtx(page *core.StatusPage) error {
	raw, err := json.Marshal(page)
	if err != nil {
		return err
	}
	// Widen the read-modify-write race window a little.
	time.Sleep(time.Millisecond)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pages[page.ID] = raw
	cp := *page
	s.saves = append(s.saves, &cp)
	return nil
}

func (s *incidentStore) ListStatusPagesNoCtx() ([]*core.StatusPage, error) {
	s.mu.Lock()
	ids := make([]string, 0, len(s.pages))
	for id := range s.pages {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	out := make([]*core.StatusPage, 0, len(ids))
	for _, id := range ids {
		page, err := s.GetStatusPageNoCtx(id)
		if err == nil {
			out = append(out, page)
		}
	}
	return out, nil
}

func newIncidentServer(t *testing.T) (*RESTServer, *incidentStore) {
	t.Helper()
	store := newIncidentStore()
	store.souls["soul-a"] = &core.Soul{ID: "soul-a", Name: "API", WorkspaceID: "default"}
	store.souls["soul-b"] = &core.Soul{ID: "soul-b", Name: "Web", WorkspaceID: "default"}
	store.souls["soul-x"] = &core.Soul{ID: "soul-x", Name: "Elsewhere", WorkspaceID: "default"}
	if err := store.SaveStatusPageNoCtx(&core.StatusPage{
		ID: "page-1", WorkspaceID: "default", Name: "Acme", Slug: "acme", Enabled: true,
		Souls: []string{"soul-a", "soul-b"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveStatusPageNoCtx(&core.StatusPage{
		ID: "page-other", WorkspaceID: "tenant-b", Name: "Other", Slug: "other", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	store.saves = nil
	server := NewRESTServer(core.ServerConfig{Port: 8080}, core.AuthConfig{Enabled: core.BoolPtr(true)}, store,
		&mockProbeEngine{}, nil, &mockAuthenticator{}, &mockClusterManager{}, nil, nil, nil, nil, newTestLogger())
	return server, store
}

func incidentRequest(t *testing.T, server *RESTServer, token, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.router.ServeHTTP(rec, req)
	return rec
}

func decodeIncident(t *testing.T, rec *httptest.ResponseRecorder) core.StatusIncident {
	t.Helper()
	var inc core.StatusIncident
	if err := json.Unmarshal(rec.Body.Bytes(), &inc); err != nil {
		t.Fatalf("decode incident: %v: %s", err, rec.Body.String())
	}
	return inc
}

func TestStatusPageIncidentLifecycle(t *testing.T) {
	server, store := newIncidentServer(t)
	base := "/api/v1/status-pages/page-1/incidents"

	rec := incidentRequest(t, server, "valid-token", "POST", base, map[string]any{
		"title": "  API errors  ", "impact": "major", "message": "We are investigating elevated errors.",
		"affected_souls": []string{"soul-a", "soul-a"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	inc := decodeIncident(t, rec)
	if inc.Title != "API errors" || inc.Status != core.StatusInvestigating || inc.Impact != core.ImpactMajor ||
		len(inc.Updates) != 1 || len(inc.AffectedSouls) != 1 || inc.ResolvedAt != nil {
		t.Fatalf("created incident = %+v", inc)
	}
	item := base + "/" + inc.ID

	rec = incidentRequest(t, server, "valid-token", "POST", item+"/updates", map[string]any{"message": "Root cause found", "status": "identified"})
	if rec.Code != http.StatusCreated || decodeIncident(t, rec).Status != core.StatusIdentified {
		t.Fatalf("update = %d %s", rec.Code, rec.Body.String())
	}
	rec = incidentRequest(t, server, "valid-token", "PATCH", item, map[string]any{"impact": "minor", "title": "API latency"})
	if got := decodeIncident(t, rec); rec.Code != http.StatusOK || got.Impact != core.ImpactMinor || got.Title != "API latency" || got.Status != core.StatusIdentified {
		t.Fatalf("patch = %d %s", rec.Code, rec.Body.String())
	}
	if rec = incidentRequest(t, server, "valid-token", "PATCH", item, map[string]any{"status": "resolved"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("patching status = %d, want 400", rec.Code)
	}
	rec = incidentRequest(t, server, "valid-token", "POST", item+"/resolve", nil)
	resolved := decodeIncident(t, rec)
	if rec.Code != http.StatusCreated || resolved.Status != core.StatusResolved || resolved.ResolvedAt == nil || len(resolved.Updates) != 3 ||
		resolved.Updates[2].Message != "This incident has been resolved." {
		t.Fatalf("resolve = %d %+v", rec.Code, resolved)
	}
	// Reopening clears resolved_at.
	rec = incidentRequest(t, server, "valid-token", "POST", item+"/updates", map[string]any{"message": "Errors are back", "status": "investigating"})
	if got := decodeIncident(t, rec); got.ResolvedAt != nil || got.Status != core.StatusInvestigating {
		t.Fatalf("reopen = %+v", got)
	}

	rec = incidentRequest(t, server, "viewer-token", "GET", base, nil)
	var list []core.StatusIncident
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list) != 1 {
		t.Fatalf("list = %d %s", rec.Code, rec.Body.String())
	}
	if rec = incidentRequest(t, server, "valid-token", "DELETE", item, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", rec.Code)
	}
	if page, _ := store.GetStatusPageNoCtx("page-1"); len(page.Incidents) != 0 {
		t.Fatalf("incident survived delete: %+v", page.Incidents)
	}
	if rec = incidentRequest(t, server, "valid-token", "DELETE", item, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("second delete = %d, want 404", rec.Code)
	}
}

func TestStatusPageIncidentValidation(t *testing.T) {
	server, _ := newIncidentServer(t)
	base := "/api/v1/status-pages/page-1/incidents"
	cases := map[string]map[string]any{
		"missing title":     {"message": "m"},
		"missing message":   {"title": "t"},
		"bad impact":        {"title": "t", "message": "m", "impact": "apocalyptic"},
		"bad status":        {"title": "t", "message": "m", "status": "open"},
		"multiline title":   {"title": "a\nb", "message": "m"},
		"control character": {"title": "t", "message": "m\x07"},
		"long title":        {"title": strings.Repeat("x", 201), "message": "m"},
		"long message":      {"title": "t", "message": strings.Repeat("x", 5001)},
		"soul not on page":  {"title": "t", "message": "m", "affected_souls": []string{"soul-x"}},
	}
	for name, body := range cases {
		if rec := incidentRequest(t, server, "valid-token", "POST", base, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400: %s", name, rec.Code, rec.Body.String())
		}
	}
	// Markup is plain text: accepted and stored verbatim, escaped on output.
	rec := incidentRequest(t, server, "valid-token", "POST", base, map[string]any{"title": "<b>t</b>", "message": "<script>alert(1)</script>"})
	if rec.Code != http.StatusCreated || strings.Contains(rec.Body.String(), "<script>") {
		t.Fatalf("markup create = %d %s", rec.Code, rec.Body.String())
	}
}

func TestStatusPageIncidentPermissionsAndWorkspace(t *testing.T) {
	server, store := newIncidentServer(t)
	rec := incidentRequest(t, server, "viewer-token", "POST", "/api/v1/status-pages/page-1/incidents", map[string]any{"title": "t", "message": "m"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer create = %d, want 403", rec.Code)
	}
	if rec = incidentRequest(t, server, "viewer-token", "POST", "/api/v1/status-pages/page-1/announcements",
		map[string]any{"title": "t", "starts_at": time.Now(), "ends_at": time.Now().Add(time.Hour)}); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer announce = %d, want 403", rec.Code)
	}
	if rec = incidentRequest(t, server, "", "GET", "/api/v1/status-pages/page-1/incidents", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous list = %d, want 401", rec.Code)
	}
	// Another workspace's page is invisible: reads, creates and child writes.
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/v1/status-pages/page-other/incidents"},
		{"POST", "/api/v1/status-pages/page-other/incidents"},
		{"GET", "/api/v1/status-pages/page-other/announcements"},
		{"POST", "/api/v1/status-pages/page-other/announcements"},
	} {
		rec := incidentRequest(t, server, "valid-token", tc.method, tc.path, map[string]any{
			"title": "t", "message": "m", "starts_at": time.Now(), "ends_at": time.Now().Add(time.Hour),
		})
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404", tc.method, tc.path, rec.Code)
		}
	}
	if page, _ := store.GetStatusPageNoCtx("page-other"); len(page.Incidents) != 0 || len(page.Announcements) != 0 {
		t.Fatalf("cross-workspace write landed: %+v", page)
	}
}

// Two editors posting updates at once must both land: each write is a
// read-modify-write of the whole stored page.
func TestStatusPageIncidentConcurrentUpdatesAreNotLost(t *testing.T) {
	server, store := newIncidentServer(t)
	rec := incidentRequest(t, server, "valid-token", "POST", "/api/v1/status-pages/page-1/incidents", map[string]any{"title": "t", "message": "first"})
	inc := decodeIncident(t, rec)
	path := "/api/v1/status-pages/page-1/incidents/" + inc.ID + "/updates"

	const editors, perEditor = 4, 10
	var wg sync.WaitGroup
	for e := 0; e < editors; e++ {
		wg.Add(1)
		go func(e int) {
			defer wg.Done()
			for i := 0; i < perEditor; i++ {
				body, _ := json.Marshal(map[string]any{"message": fmt.Sprintf("editor %d update %d", e, i), "status": "monitoring"})
				req := httptest.NewRequest("POST", path, bytes.NewReader(body))
				req.Header.Set("Authorization", "Bearer valid-token")
				req.Header.Set("Content-Type", "application/json")
				req.RemoteAddr = fmt.Sprintf("192.0.2.%d:1000", e+1)
				w := httptest.NewRecorder()
				server.router.ServeHTTP(w, req)
				if w.Code != http.StatusCreated {
					t.Errorf("update = %d %s", w.Code, w.Body.String())
				}
			}
		}(e)
	}
	// A whole-page PUT racing with the updates must not drop them either.
	wg.Add(1)
	go func() {
		defer wg.Done()
		incidentRequest(t, server, "valid-token", "PUT", "/api/v1/status-pages/page-1", map[string]any{"name": "Acme Renamed", "slug": "acme"})
	}()
	wg.Wait()

	page, _ := store.GetStatusPageNoCtx("page-1")
	if got := len(page.Incidents[0].Updates); got != 1+editors*perEditor {
		t.Fatalf("stored %d updates, want %d", got, 1+editors*perEditor)
	}
	if page.Name != "Acme Renamed" {
		t.Fatalf("PUT lost: %q", page.Name)
	}
}

func TestStatusPageAnnouncementLifecycle(t *testing.T) {
	server, store := newIncidentServer(t)
	base := "/api/v1/status-pages/page-1/announcements"
	start := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)

	if rec := incidentRequest(t, server, "valid-token", "POST", base, map[string]any{"title": "DB upgrade", "starts_at": start, "ends_at": start.Add(-time.Hour)}); rec.Code != http.StatusBadRequest {
		t.Fatalf("end before start = %d, want 400", rec.Code)
	}
	rec := incidentRequest(t, server, "valid-token", "POST", base, map[string]any{
		"title": "DB upgrade", "message": "Short read-only period.", "starts_at": start, "ends_at": start.Add(time.Hour),
		"affected_souls": []string{"soul-b"},
	})
	var a core.StatusAnnouncement
	if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil || rec.Code != http.StatusCreated || a.Phase != core.AnnouncementUpcoming {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	rec = incidentRequest(t, server, "valid-token", "PATCH", base+"/"+a.ID, map[string]any{"starts_at": time.Now().Add(-time.Minute)})
	if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil || rec.Code != http.StatusOK || a.Phase != core.AnnouncementInProgress || a.Title != "DB upgrade" {
		t.Fatalf("patch = %d %s", rec.Code, rec.Body.String())
	}
	if page, _ := store.GetStatusPageNoCtx("page-1"); len(page.Announcements) != 1 || page.Announcements[0].Phase != "" {
		t.Fatalf("stored announcements = %+v (phase must not persist)", page.Announcements)
	}

	// A one-off maintenance window supplies the schedule and the page's souls;
	// a recurring one has no single schedule and is refused.
	store.windows["mw-1"] = &core.MaintenanceWindow{ID: "mw-1", Name: "Network work", WorkspaceID: "default",
		StartTime: start, EndTime: start.Add(30 * time.Minute), SoulIDs: []string{"soul-a", "soul-x"}, Enabled: true}
	store.windows["mw-r"] = &core.MaintenanceWindow{ID: "mw-r", Name: "Nightly", WorkspaceID: "default",
		StartTime: start, EndTime: start.Add(time.Hour), Recurring: "daily", Enabled: true}
	store.windows["mw-other"] = &core.MaintenanceWindow{ID: "mw-other", Name: "Theirs", WorkspaceID: "tenant-b",
		StartTime: start, EndTime: start.Add(time.Hour)}
	rec = incidentRequest(t, server, "valid-token", "POST", base, map[string]any{"maintenance_window_id": "mw-1"})
	if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil || rec.Code != http.StatusCreated ||
		a.Title != "Network work" || !a.StartsAt.Equal(start) || len(a.AffectedSouls) != 1 || a.AffectedSouls[0] != "soul-a" {
		t.Fatalf("linked create = %d %s", rec.Code, rec.Body.String())
	}
	for _, id := range []string{"mw-r", "mw-other", "missing"} {
		if rec := incidentRequest(t, server, "valid-token", "POST", base, map[string]any{"maintenance_window_id": id}); rec.Code != http.StatusBadRequest {
			t.Errorf("link %s = %d, want 400", id, rec.Code)
		}
	}
	if rec = incidentRequest(t, server, "valid-token", "DELETE", base+"/"+a.ID, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", rec.Code)
	}
	rec = incidentRequest(t, server, "viewer-token", "GET", base, nil)
	var list []core.StatusAnnouncement
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list) != 1 {
		t.Fatalf("list = %s", rec.Body.String())
	}
}

// Every posted change is exactly one save, and the save before it is the
// stored page, which is what the notification hook diffs: one save per
// update means one notification per update.
func TestStatusPageIncidentOneSavePerChange(t *testing.T) {
	server, store := newIncidentServer(t)
	rec := incidentRequest(t, server, "valid-token", "POST", "/api/v1/status-pages/page-1/incidents", map[string]any{"title": "t", "message": "m"})
	inc := decodeIncident(t, rec)
	incidentRequest(t, server, "valid-token", "POST", "/api/v1/status-pages/page-1/incidents/"+inc.ID+"/updates", map[string]any{"message": "u", "status": "monitoring"})
	incidentRequest(t, server, "valid-token", "POST", "/api/v1/status-pages/page-1/incidents/"+inc.ID+"/updates", map[string]any{"message": "", "status": "monitoring"})
	if len(store.saves) != 2 {
		t.Fatalf("saves = %d, want 2 (an invalid update must not save)", len(store.saves))
	}
	if n := len(store.saves[1].Incidents[0].Updates); n != 2 {
		t.Fatalf("second save has %d updates", n)
	}
}
