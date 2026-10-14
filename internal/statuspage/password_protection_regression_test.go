package statuspage

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/auth"
	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// A password-protected page used to be unreachable: verifyPagePassword always
// returned false, so no password could ever match. These tests pin the fixed
// behaviour -- the stored hash is compared, and only the right password unlocks
// the page.
//
// The passwords are generated per run rather than written as literals, so no
// credential-shaped string lives in the repository.

const passwordRegressionSlug = "password-regression"

func passwordRegressionSecrets() (correct, wrong string) {
	correct = "pw-" + core.GenerateID() + "-" + core.GenerateID()
	wrong = "pw-" + core.GenerateID() + "-" + core.GenerateID()
	return correct, wrong
}

type passwordRegressionRepo struct{ page *core.StatusPage }

func (r *passwordRegressionRepo) GetStatusPageBySlug(string) (*core.StatusPage, error) {
	return r.page, nil
}

func (r *passwordRegressionRepo) GetStatusPageByDomain(string) (*core.StatusPage, error) {
	return r.page, nil
}

func (r *passwordRegressionRepo) GetSoul(id string) (*core.Soul, error) {
	return &core.Soul{ID: id, Name: sessionRegressionComponent, Type: core.CheckHTTP}, nil
}

func (r *passwordRegressionRepo) GetSoulJudgments(string, int) ([]core.Judgment, error) {
	return nil, nil
}

func (r *passwordRegressionRepo) GetIncidentsByPage(string) ([]core.Incident, error) {
	return nil, nil
}

func (r *passwordRegressionRepo) GetUptimeHistory(string, int) ([]core.UptimeDay, error) {
	return nil, nil
}

func (r *passwordRegressionRepo) SaveSubscription(*core.StatusPageSubscription) error { return nil }

func (r *passwordRegressionRepo) GetSubscriptionsByPage(string) ([]*core.StatusPageSubscription, error) {
	return nil, nil
}

func (r *passwordRegressionRepo) DeleteSubscription(string) error { return nil }

func passwordRegressionPage(hash string) *core.StatusPage {
	return &core.StatusPage{
		ID: "password-regression-page", WorkspaceID: "workspace-1",
		Name: "Password Regression Page", Slug: passwordRegressionSlug,
		Enabled: true, Visibility: core.VisibilityProtected, PasswordHash: hash,
		Souls: []string{"soul-1"},
	}
}

func passwordRegressionRequest(t *testing.T, hash string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()

	handler := NewHandler(&passwordRegressionRepo{page: passwordRegressionPage(hash)})
	req := httptest.NewRequest(http.MethodGet, "/status/"+passwordRegressionSlug, nil)
	if mutate != nil {
		mutate(req)
	}
	rec := httptest.NewRecorder()
	handler.Router().ServeHTTP(rec, req)
	return rec
}

func passwordRegressionHash(t *testing.T, password string) string {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	return hash
}

// TestProtectedPageUnlocksWithTheStoredPassword pins the fixed behaviour through
// both credentials the handler accepts: the ?password= query and the cookie.
func TestProtectedPageUnlocksWithTheStoredPassword(t *testing.T) {
	correct, _ := passwordRegressionSecrets()
	hash := passwordRegressionHash(t, correct)

	for _, tc := range []struct {
		name   string
		mutate func(*http.Request)
	}{
		{"query", func(r *http.Request) {
			q := r.URL.Query()
			q.Set("password", correct)
			r.URL.RawQuery = q.Encode()
		}},
		{"cookie", func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: "anubis_page_pass", Value: correct})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := passwordRegressionRequest(t, hash, tc.mutate)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body: %.120s)", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), sessionRegressionComponent) {
				t.Errorf("the unlocked page does not show its data: %.200s", rec.Body.String())
			}
		})
	}
}

// TestProtectedPageRefusesEverythingElse is the neighbouring control: a wrong
// password, no password, and a page with no stored hash all get the form instead
// of the data.
func TestProtectedPageRefusesEverythingElse(t *testing.T) {
	correct, wrong := passwordRegressionSecrets()
	hash := passwordRegressionHash(t, correct)

	for _, tc := range []struct {
		name   string
		hash   string
		mutate func(*http.Request)
	}{
		{"wrong password", hash, func(r *http.Request) {
			q := r.URL.Query()
			q.Set("password", wrong)
			r.URL.RawQuery = q.Encode()
		}},
		{"no password supplied", hash, nil},
		{"wrong password in the cookie", hash, func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: "anubis_page_pass", Value: wrong})
		}},
		{"no hash stored", "", func(r *http.Request) {
			q := r.URL.Query()
			q.Set("password", correct)
			r.URL.RawQuery = q.Encode()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := passwordRegressionRequest(t, tc.hash, tc.mutate)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (the password form)", rec.Code)
			}
			if strings.Contains(rec.Body.String(), sessionRegressionComponent) {
				t.Errorf("the page served its data without the correct password: %.200s", rec.Body.String())
			}
		})
	}
}

// TestPublicPageNeedsNoPassword keeps the other policy honest.
func TestPublicPageNeedsNoPassword(t *testing.T) {
	handler := NewHandler(&passwordRegressionRepo{page: &core.StatusPage{
		ID: "public-page", WorkspaceID: "workspace-1", Name: "Public", Slug: passwordRegressionSlug,
		Enabled: true, Visibility: core.VisibilityPublic, Souls: []string{"soul-1"},
	}})

	req := httptest.NewRequest(http.MethodGet, "/status/"+passwordRegressionSlug, nil)
	rec := httptest.NewRecorder()
	handler.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), sessionRegressionComponent) {
		t.Errorf("a public page did not serve its data: status=%d body=%.200s", rec.Code, rec.Body.String())
	}
}
