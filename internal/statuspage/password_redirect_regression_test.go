package statuspage

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestPasswordRedirectStaysOnOrigin(t *testing.T) {
	password, _ := passwordRegressionSecrets()
	page := passwordRegressionPage(passwordRegressionHash(t, password))
	h := NewHandler(&passwordRegressionRepo{page: page})
	for _, path := range []string{"/status/" + page.Slug, "//outside.invalid/path"} {
		req := httptest.NewRequest(http.MethodPost, "http://status.invalid/", strings.NewReader(url.Values{"password": {password}}.Encode()))
		req.URL.Scheme = ""
		req.URL.Host = ""
		req.URL.Path = path
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusSeeOther {
			t.Fatalf("invalid password control: %d", w.Code)
		}
		location := w.Header().Get("Location")
		parsed, err := url.Parse(location)
		if err != nil {
			t.Fatal(err)
		}
		if path == "/status/"+page.Slug {
			if parsed.Host != "" || parsed.IsAbs() {
				t.Fatal("invalid control")
			}
			fmt.Println("CONTROL EXPECTED: normal redirect stays local; ACTUAL: local")
		}
		fmt.Printf("EXPECTED: origin-relative redirect; ACTUAL: %q\n", location)
		if parsed.Host != "" || parsed.IsAbs() || strings.HasPrefix(location, "//") {
			t.Fatal("PROBLEM CONFIRMED")
		}
	}
	fmt.Println("PROBLEM NOT REPRODUCED")
}
