package statuspage

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestStatusPageVisibilityCacheMatrix(t *testing.T) {
	for _, visibility := range []core.PageVisibility{core.VisibilityPublic, core.VisibilityPrivate, core.VisibilityProtected} {
		for _, path := range []string{"/status/session-regression", "/status/session-regression?format=json", "/badge/session-regression", "/widget?page=session-regression", "/status/session-regression/feed.xml"} {
			for _, authenticated := range []bool{false, true} {
				name := string(visibility) + path
				if authenticated {
					name += "/authenticated"
				}
				t.Run(name, func(t *testing.T) {
					page := sessionRegressionPage(visibility)
					page.PasswordHash = "fixture-page-hash"
					h := NewHandler(&sessionRegressionRepo{page: page}).SetSessionValidator(sessionRegressionValidator{token: "fixture", workspace: page.WorkspaceID})
					r := httptest.NewRequest(http.MethodGet, path, nil)
					if authenticated {
						r.AddCookie(&http.Cookie{Name: "auth_token", Value: "fixture"})
						r.AddCookie(&http.Cookie{Name: "anubis_page_pass", Value: pagePasswordTicket(page, time.Now().Add(time.Hour))})
					}
					w := httptest.NewRecorder()
					h.Router().ServeHTTP(w, r)
					cache := w.Header().Get("Cache-Control")
					if visibility == core.VisibilityPublic {
						if !strings.Contains(cache, "public") {
							t.Fatalf("public control cache=%q", cache)
						}
					} else if cache != "private, no-store" {
						t.Fatalf("protected response cache=%q", cache)
					}
					if (authenticated || visibility == core.VisibilityPublic) && w.Code != http.StatusOK {
						t.Fatalf("authorized status=%d body=%s", w.Code, w.Body.String())
					}
					if !authenticated && visibility != core.VisibilityPublic && strings.Contains(w.Body.String(), sessionRegressionComponent) {
						t.Fatal("private component in anonymous response")
					}
				})
			}
		}
	}
}

func TestProtectedPagePasswordPostIsNotCacheable(t *testing.T) {
	page := sessionRegressionPage(core.VisibilityProtected)
	h := NewHandler(&sessionRegressionRepo{page: page})
	r := httptest.NewRequest(http.MethodPost, "/status/session-regression", strings.NewReader("password=wrong"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if got := w.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("password form cache=%q", got)
	}
}

func TestPrivatePageLoginCookieAndLegacyPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cookies []*http.Cookie
		want    int
	}{
		{"login", []*http.Cookie{{Name: "auth_token", Value: "fixture"}}, http.StatusOK},
		{"legacy", []*http.Cookie{{Name: "anubis_session", Value: "fixture"}}, http.StatusOK},
		{"invalid login overrides legacy", []*http.Cookie{{Name: "auth_token", Value: "invalid"}, {Name: "anubis_session", Value: "fixture"}}, http.StatusUnauthorized},
		{"wrong workspace", []*http.Cookie{{Name: "auth_token", Value: "fixture"}}, http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace := "workspace-1"
			if tc.name == "wrong workspace" {
				workspace = "workspace-2"
			}
			w := sessionRegressionRequest(t, core.VisibilityPrivate, sessionRegressionValidator{token: "fixture", workspace: workspace}, func(r *http.Request) {
				for _, cookie := range tc.cookies {
					r.AddCookie(cookie)
				}
			})
			if w.Code != tc.want {
				t.Fatalf("status=%d want=%d", w.Code, tc.want)
			}
		})
	}
}
