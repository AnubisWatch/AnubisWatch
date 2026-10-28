package statuspage

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestProtectedPagePasswordTransport(t *testing.T) {
	password, _ := passwordRegressionSecrets()
	hash := passwordRegressionHash(t, password)
	page := passwordRegressionPage(hash)
	handler := NewHandler(&passwordRegressionRepo{page: page})
	path := "/status/" + passwordRegressionSlug
	form := httptest.NewRecorder()
	handler.ServeHTTP(form, httptest.NewRequest("GET", path, nil))
	if !strings.Contains(form.Body.String(), `method="POST"`) {
		t.Error("password form must submit POST")
	}
	query := httptest.NewRecorder()
	handler.ServeHTTP(query, httptest.NewRequest("GET", path+"?password="+url.QueryEscape(password), nil))
	if strings.Contains(query.Body.String(), sessionRegressionComponent) {
		t.Error("GET password authenticated")
	}
	request := httptest.NewRequest("POST", path, strings.NewReader(url.Values{"password": {password}}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post := httptest.NewRecorder()
	handler.ServeHTTP(post, request)
	if post.Code != http.StatusSeeOther {
		t.Fatalf("EXPECTED POST redirect=303; ACTUAL=%d", post.Code)
	}
	cookies := post.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing authentication cookie")
	}
	cookie := cookies[0]
	if strings.Contains(cookie.Value, password) || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe cookie")
	}
	for _, bad := range []string{password, cookie.Value + "tamper", pagePasswordTicket(page, time.Now().Add(-time.Second))} {
		req := httptest.NewRequest("GET", path, nil)
		req.AddCookie(&http.Cookie{Name: cookie.Name, Value: bad})
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		if strings.Contains(out.Body.String(), sessionRegressionComponent) {
			t.Fatal("invalid cookie authenticated")
		}
	}
	second := *page
	second.ID = "other-page"
	req := httptest.NewRequest("GET", path, nil)
	req.AddCookie(cookie)
	if handler.checkPasswordProtection(req, &second) {
		t.Fatal("cookie authenticated another page")
	}
}
