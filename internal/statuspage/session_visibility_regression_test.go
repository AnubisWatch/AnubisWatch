package statuspage

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// A private page was refused even for the account that owns it: isAuthenticated
// consults a session validator, and none was ever installed, so validateSession
// returned false unconditionally. These tests pin the fixed behaviour: a session
// the validator accepts for the page's workspace serves the page, and everything
// else still gets the 401 challenge.

const sessionRegressionComponent = "SESSION-REGRESSION-COMPONENT"

type sessionRegressionRepo struct{ page *core.StatusPage }

func (r *sessionRegressionRepo) GetStatusPageBySlug(string) (*core.StatusPage, error) {
	return r.page, nil
}

func (r *sessionRegressionRepo) GetStatusPageByDomain(string) (*core.StatusPage, error) {
	return r.page, nil
}

func (r *sessionRegressionRepo) GetSoul(id string) (*core.Soul, error) {
	return &core.Soul{ID: id, Name: sessionRegressionComponent, Type: core.CheckHTTP}, nil
}

func (r *sessionRegressionRepo) GetSoulJudgments(string, int) ([]core.Judgment, error) {
	return nil, nil
}

func (r *sessionRegressionRepo) GetIncidentsByPage(string) ([]core.Incident, error) {
	return nil, nil
}

func (r *sessionRegressionRepo) GetUptimeHistory(string, int) ([]core.UptimeDay, error) {
	return nil, nil
}

func (r *sessionRegressionRepo) SaveSubscription(*core.StatusPageSubscription) error { return nil }

func (r *sessionRegressionRepo) GetSubscriptionsByPage(string) ([]*core.StatusPageSubscription, error) {
	return nil, nil
}

func (r *sessionRegressionRepo) DeleteSubscription(string) error { return nil }

// sessionRegressionValidator accepts one token for one workspace.
type sessionRegressionValidator struct {
	token     string
	workspace string
}

func (v sessionRegressionValidator) ValidateStatusPageSession(token, workspaceID string) bool {
	return token == v.token && workspaceID == v.workspace
}

func sessionRegressionPage(visibility core.PageVisibility) *core.StatusPage {
	return &core.StatusPage{
		ID: "session-regression-page", WorkspaceID: "workspace-1",
		Name: "Session Regression Page", Slug: "session-regression",
		Enabled: true, Visibility: visibility, Souls: []string{"soul-1"},
	}
}

func sessionRegressionRequest(t *testing.T, visibility core.PageVisibility, validator SessionValidator, mutate func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()

	handler := NewHandler(&sessionRegressionRepo{page: sessionRegressionPage(visibility)})
	if validator != nil {
		handler.SetSessionValidator(validator)
	}

	req := httptest.NewRequest(http.MethodGet, "/status/session-regression", nil)
	if mutate != nil {
		mutate(req)
	}
	rec := httptest.NewRecorder()
	handler.Router().ServeHTTP(rec, req)
	return rec
}

// TestPrivatePageServesASessionTheValidatorAccepts pins the fixed behaviour: the
// owner's session sees the page instead of the 401 challenge.
func TestPrivatePageServesASessionTheValidatorAccepts(t *testing.T) {
	validator := sessionRegressionValidator{token: "valid-session", workspace: "workspace-1"}
	rec := sessionRegressionRequest(t, core.VisibilityPrivate, validator, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: "anubis_session", Value: "valid-session"})
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a session the validator accepts (body: %.120s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), sessionRegressionComponent) {
		t.Errorf("the page body does not contain its component: %.200s", rec.Body.String())
	}
}

// TestPrivatePageStillRefusesEverythingElse is the neighbouring control: no
// session, an unknown session, a session for another workspace, and no validator
// configured all keep the challenge.
func TestPrivatePageStillRefusesEverythingElse(t *testing.T) {
	validator := sessionRegressionValidator{token: "valid-session", workspace: "workspace-1"}

	for _, tc := range []struct {
		name      string
		validator SessionValidator
		mutate    func(*http.Request)
	}{
		{"no session at all", validator, nil},
		{"unknown session", validator, func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: "anubis_session", Value: "other-session"})
		}},
		{"session for another workspace", sessionRegressionValidator{token: "valid-session", workspace: "workspace-2"}, func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: "anubis_session", Value: "valid-session"})
		}},
		{"no validator configured", nil, func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: "anubis_session", Value: "valid-session"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := sessionRegressionRequest(t, core.VisibilityPrivate, tc.validator, tc.mutate)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401 (body: %.120s)", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestPublicAndProtectedPagesAreUnaffected keeps the other visibility policies
// honest: public pages need no session, and a password-protected page still asks
// for the password rather than serving its data.
func TestPublicAndProtectedPagesAreUnaffected(t *testing.T) {
	public := sessionRegressionRequest(t, core.VisibilityPublic, nil, nil)
	if public.Code != http.StatusOK {
		t.Errorf("public page status = %d, want 200", public.Code)
	}

	protected := sessionRegressionRequest(t, core.VisibilityProtected, nil, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: "anubis_session", Value: "valid-session"})
	})
	if protected.Code != http.StatusOK {
		t.Fatalf("protected page status = %d, want 200 (the password form)", protected.Code)
	}
	if strings.Contains(protected.Body.String(), sessionRegressionComponent) {
		t.Errorf("a password-protected page served its data to a session without the password: %.200s",
			protected.Body.String())
	}
}
