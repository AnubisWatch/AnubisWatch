package statuspage

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// A status page's visibility policy must apply wherever the page's data is served,
// not only on the main route. The gate used to live inside ServeHTTP, so the
// widget, badge and feed endpoints published a private or password-protected page
// in full: the same data through a different URL.

func gateRepo() *visibilityGateRepo {
	const soulID = "soul-1"

	return &visibilityGateRepo{
		pages: map[string]*core.StatusPage{
			"private-page": {
				ID: "p1", Slug: "private-page", Name: "Private Page",
				Visibility: core.VisibilityPrivate, Enabled: true, Souls: []string{soulID},
			},
			"protected-page": {
				ID: "p2", Slug: "protected-page", Name: "Protected Page",
				Visibility: core.VisibilityProtected, Enabled: true, Souls: []string{soulID},
			},
			"public-page": {
				ID: "p3", Slug: "public-page", Name: "Public Page",
				Visibility: core.VisibilityPublic, Enabled: true, Souls: []string{soulID},
			},
		},
		souls: map[string]*core.Soul{
			soulID: {ID: soulID, Name: "Internal API", WorkspaceID: "default"},
		},
	}
}

func gateGet(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()

	w := httptest.NewRecorder()
	NewHandler(gateRepo()).Router().ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func TestStatusPageAuxiliaryEndpointsHonourVisibility(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
	}{
		{"widget", "/widget?page="},
		{"badge", "/badge/"},
		{"rss feed", "/status/%s/feed.xml"},
	} {
		for _, slug := range []string{"private-page", "protected-page"} {
			t.Run(tc.name+" refuses "+slug, func(t *testing.T) {
				path := tc.path
				if strings.Contains(path, "%s") {
					path = strings.Replace(path, "%s", slug, 1)
				} else {
					path += slug
				}

				w := gateGet(t, path)
				if w.Code == http.StatusOK {
					t.Errorf("%s served a %s page (status 200): %s",
						tc.name, slug, strings.TrimSpace(firstN(w.Body.String(), 120)))
				}
			})
		}
	}
}

// TestStatusPageAuxiliaryEndpointsStillServePublicPages is the neighbouring
// control: the endpoints must keep working for a public page.
func TestStatusPageAuxiliaryEndpointsStillServePublicPages(t *testing.T) {
	for _, tc := range []struct{ name, path string }{
		{"widget", "/widget?page=public-page"},
		{"badge", "/badge/public-page"},
		{"rss feed", "/status/public-page/feed.xml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := gateGet(t, tc.path)
			if w.Code != http.StatusOK || w.Body.Len() == 0 {
				t.Errorf("%s no longer serves a public page (status %d, %d bytes)", tc.name, w.Code, w.Body.Len())
			}
		})
	}
}

// TestStatusPageMainRouteBehaviourUnchanged pins that extracting the shared policy
// did not change the main route: a public page renders its components, a protected
// page is refused.
func TestStatusPageMainRouteBehaviourUnchanged(t *testing.T) {
	if body := gateGet(t, "/status/public-page").Body.String(); !strings.Contains(body, "Internal API") {
		t.Error("the main route no longer renders a public page's components")
	}
	if body := gateGet(t, "/status/protected-page").Body.String(); strings.Contains(body, "Internal API") {
		t.Error("the main route published a protected page's components")
	}
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// visibilityGateRepo is a minimal Repository covering the three visibilities.
type visibilityGateRepo struct {
	pages map[string]*core.StatusPage
	souls map[string]*core.Soul
}

func (r *visibilityGateRepo) GetStatusPageByDomain(string) (*core.StatusPage, error) {
	return nil, errNoPage
}

func (r *visibilityGateRepo) GetStatusPageBySlug(slug string) (*core.StatusPage, error) {
	if p, ok := r.pages[slug]; ok {
		return p, nil
	}
	return nil, errNoPage
}

func (r *visibilityGateRepo) GetSoul(id string) (*core.Soul, error) {
	if s, ok := r.souls[id]; ok {
		return s, nil
	}
	return nil, errNoPage
}

func (r *visibilityGateRepo) GetSoulJudgments(soulID string, limit int) ([]core.Judgment, error) {
	return []core.Judgment{{
		ID: "j1", SoulID: soulID, Status: core.SoulAlive,
		Duration: 5 * time.Millisecond, Timestamp: time.Now().UTC(),
	}}, nil
}

func (r *visibilityGateRepo) GetIncidentsByPage(string) ([]core.Incident, error) {
	return []core.Incident{}, nil
}

func (r *visibilityGateRepo) GetUptimeHistory(string, int) ([]core.UptimeDay, error) {
	return []core.UptimeDay{}, nil
}

func (r *visibilityGateRepo) SaveSubscription(*core.StatusPageSubscription) error { return nil }

func (r *visibilityGateRepo) GetSubscriptionsByPage(string) ([]*core.StatusPageSubscription, error) {
	return nil, nil
}

func (r *visibilityGateRepo) DeleteSubscription(string) error { return nil }

var errNoPage = noPageError{}

type noPageError struct{}

func (noPageError) Error() string { return "status page not found" }
