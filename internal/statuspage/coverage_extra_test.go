package statuspage

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type coverageRepo struct {
	page        *core.StatusPage
	domainErr   error
	slugErr     error
	soulErr     error
	judgeErr    error
	uptimeErr   error
	incidentErr error
	statuses    map[string]core.SoulStatus
	incidents   []core.Incident
}

func (r *coverageRepo) GetStatusPageByDomain(string) (*core.StatusPage, error) {
	if r.domainErr != nil {
		return nil, r.domainErr
	}
	return r.page, nil
}
func (r *coverageRepo) GetStatusPageBySlug(string) (*core.StatusPage, error) {
	if r.slugErr != nil {
		return nil, r.slugErr
	}
	return r.page, nil
}
func (r *coverageRepo) GetSoul(id string) (*core.Soul, error) {
	if r.soulErr != nil {
		return nil, r.soulErr
	}
	return &core.Soul{ID: id, Name: "Soul " + id}, nil
}
func (r *coverageRepo) GetSoulJudgments(id string, _ int) ([]core.Judgment, error) {
	if r.judgeErr != nil {
		return nil, r.judgeErr
	}
	status, ok := r.statuses[id]
	if !ok {
		return nil, nil
	}
	return []core.Judgment{{ID: "j-" + id, SoulID: id, Status: status, Timestamp: time.Now()}}, nil
}
func (r *coverageRepo) GetIncidentsByPage(string) ([]core.Incident, error) {
	if r.incidentErr != nil {
		return nil, r.incidentErr
	}
	return r.incidents, nil
}
func (r *coverageRepo) GetUptimeHistory(string, int) ([]core.UptimeDay, error) {
	if r.uptimeErr != nil {
		return nil, r.uptimeErr
	}
	return []core.UptimeDay{{Uptime: 50}}, nil
}
func (*coverageRepo) SaveSubscription(*core.StatusPageSubscription) error { return nil }
func (*coverageRepo) GetSubscriptionsByPage(string) ([]*core.StatusPageSubscription, error) {
	return nil, nil
}
func (*coverageRepo) DeleteSubscription(string) error { return nil }

func coveragePage(statuses map[string]core.SoulStatus) (*core.StatusPage, *coverageRepo) {
	ids := make([]string, 0, len(statuses))
	for id := range statuses {
		ids = append(ids, id)
	}
	page := &core.StatusPage{ID: "p", Name: "Page", CustomDomain: "status.example", Visibility: core.VisibilityPublic, Souls: ids, UptimeDays: 7}
	return page, &coverageRepo{page: page, statuses: statuses}
}

func TestCoverageServeHTTPFallbacksAndVisibility(t *testing.T) {
	page, repo := coveragePage(map[string]core.SoulStatus{"a": core.SoulAlive})
	repo.domainErr = errors.New("missing")
	h := NewHandler(repo)
	for _, tc := range []struct {
		name, path string
		setup      func(*http.Request, *coverageRepo, *core.StatusPage)
		code       int
	}{
		{"missing slug", "/", nil, http.StatusNotFound},
		{"missing page", "/status/nope", func(_ *http.Request, r *coverageRepo, _ *core.StatusPage) { r.slugErr = errors.New("missing") }, http.StatusNotFound},
		{"private", "/status/p", func(_ *http.Request, r *coverageRepo, p *core.StatusPage) {
			r.slugErr = nil
			p.Visibility = core.VisibilityPrivate
		}, http.StatusUnauthorized},
		{"private cookie", "/status/p", func(q *http.Request, r *coverageRepo, p *core.StatusPage) {
			r.slugErr = nil
			p.Visibility = core.VisibilityPrivate
			q.AddCookie(&http.Cookie{Name: "anubis_session", Value: "x"})
		}, http.StatusUnauthorized},
		{"private api key", "/status/p", func(q *http.Request, r *coverageRepo, p *core.StatusPage) {
			r.slugErr = nil
			p.Visibility = core.VisibilityPrivate
			q.Header.Set("X-API-Key", "x")
		}, http.StatusUnauthorized},
		{"protected", "/status/p?password=x", func(_ *http.Request, r *coverageRepo, p *core.StatusPage) {
			r.slugErr = nil
			p.Visibility = core.VisibilityProtected
		}, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page.Visibility = core.VisibilityPublic
			repo.slugErr = nil
			req := httptest.NewRequest("GET", tc.path, nil)
			if tc.setup != nil {
				tc.setup(req, repo, page)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tc.code {
				t.Fatalf("code=%d body=%q", w.Code, w.Body.String())
			}
		})
	}
}

func TestCoverageUptimeErrorAndCanServeEmpty(t *testing.T) {
	page, repo := coveragePage(map[string]core.SoulStatus{"a": core.SoulAlive})
	repo.uptimeErr = errors.New("no history")
	h := NewHandler(repo)
	if h.CanServeHost("   ") {
		t.Fatal("empty host served")
	}
	data, err := h.buildStatusPageData(page)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Souls) != 1 || data.Souls[0].UptimePercent != 100 {
		t.Fatalf("data=%+v", data.Souls)
	}
	if len(data.Uptime.Days) != 0 {
		t.Fatalf("uptime=%+v", data.Uptime)
	}
}

func TestCoverageBadgeAndWidgetPalettes(t *testing.T) {
	cases := []struct {
		name     string
		statuses map[string]core.SoulStatus
		want     string
	}{
		{"operational", map[string]core.SoulStatus{"a": core.SoulAlive}, "22c55e"},
		{"degraded", map[string]core.SoulStatus{"a": core.SoulDegraded}, "f59e0b"},
		{"major", map[string]core.SoulStatus{"a": core.SoulDead}, "ef4444"},
		{"empty is operational", map[string]core.SoulStatus{}, "22c55e"},
		{"unknown soul detail falls back to operational", map[string]core.SoulStatus{"a": core.SoulUnknown}, "22c55e"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, repo := coveragePage(tc.statuses)
			h := NewHandler(repo)
			for _, path := range []string{"/badge/p?format=json", "/widget?page=p&style=detailed"} {
				req := httptest.NewRequest("GET", path, nil)
				w := httptest.NewRecorder()
				if strings.HasPrefix(path, "/badge") {
					h.BadgeHandler(w, req)
				} else {
					h.WidgetHandler(w, req)
				}
				if !strings.Contains(w.Body.String(), tc.want) {
					t.Fatalf("body=%q", w.Body.String())
				}
			}
		})
	}
}

func TestCoverageBadgeWidgetSoulErrors(t *testing.T) {
	page, repo := coveragePage(map[string]core.SoulStatus{"a": core.SoulAlive})
	_ = page
	h := NewHandler(repo)
	for _, fail := range []string{"soul", "judgment"} {
		repo.soulErr = nil
		repo.judgeErr = nil
		if fail == "soul" {
			repo.soulErr = errors.New("x")
		} else {
			repo.judgeErr = errors.New("x")
		}
		for _, path := range []string{"/badge/p", "/widget?page=p"} {
			req := httptest.NewRequest("GET", path, nil)
			w := httptest.NewRecorder()
			if strings.HasPrefix(path, "/badge") {
				h.BadgeHandler(w, req)
			} else {
				h.WidgetHandler(w, req)
			}
			if w.Code != http.StatusOK {
				t.Fatalf("%s %s code=%d", fail, path, w.Code)
			}
		}
	}
}

func TestCoverageRSSIncidentBranches(t *testing.T) {
	_, repo := coveragePage(nil)
	repo.incidents = []core.Incident{{ID: "i", SoulID: "a", Status: core.IncidentOpen, StartedAt: time.Now()}}
	h := NewHandler(repo)
	for _, soulErr := range []bool{false, true} {
		if soulErr {
			repo.soulErr = errors.New("x")
		} else {
			repo.soulErr = nil
		}
		req := httptest.NewRequest("GET", "/status/p/feed.xml", nil)
		w := httptest.NewRecorder()
		h.RSSFeedHandler(w, req)
		if !strings.Contains(w.Body.String(), "<item>") {
			t.Fatal("missing item")
		}
	}
	repo.incidentErr = errors.New("x")
	req := httptest.NewRequest("GET", "/status/p/feed.xml", nil)
	w := httptest.NewRecorder()
	h.RSSFeedHandler(w, req)
	if strings.Contains(w.Body.String(), "<item>") {
		t.Fatal("unexpected item")
	}
}

func TestSanitizeCSSCoverage(t *testing.T) {
	if sanitizeCSS("") != "" {
		t.Fatal("empty changed")
	}
	if got := sanitizeCSS("body { color: red }"); got == "" {
		t.Fatal("safe CSS removed")
	}
	for _, bad := range []string{"</STYLE>", "expression(x)", "behavior(x)", "-moz-binding:x", "javascript:x", "data:x", "vbscript:x", "@import x"} {
		if got := sanitizeCSS(bad); got != "" {
			t.Fatalf("%q survived", bad)
		}
	}
}

func TestBuildStatusPageDataFnInjection(t *testing.T) {
	old := buildStatusPageDataFn
	buildStatusPageDataFn = func(_ *Handler, _ *core.StatusPage) (*core.StatusPageData, error) {
		return nil, errors.New("injected build error")
	}
	t.Cleanup(func() { buildStatusPageDataFn = old })

	req := httptest.NewRequest("GET", "/status/p", nil)
	w := httptest.NewRecorder()
	h := &Handler{repository: &mockRepository{}}
	h.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestStatusColorFromStatus(t *testing.T) {
	cases := []struct{ status, want string }{
		{"operational", "22c55e"},
		{"degraded", "f59e0b"},
		{"down", "ef4444"},
		{"major_outage", "ef4444"},
		{"unknown", "6b7280"},
		{"", "6b7280"},
	}
	for _, tc := range cases {
		if got := statusColorFromStatus(tc.status); got != tc.want {
			t.Errorf("statusColorFromStatus(%q) = %q, want %q", tc.status, got, tc.want)
		}
	}
}
