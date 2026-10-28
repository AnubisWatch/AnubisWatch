package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// The authorization request is a URL: every parameter value must be
// query-escaped. A redirect_uri that carries its own query string (or a client
// ID with reserved characters) was spliced in raw, so the provider saw the
// extra "&k=v" pieces as parameters of the authorization request and the
// redirect_uri it matched against its registration was truncated.
func TestOIDCLoginURLEscapesParameterValues(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(oidcConfig{Issuer: server.URL, AuthURL: server.URL + "/auth", TokenURL: server.URL + "/token"})
	}))
	defer server.Close()

	login := func(clientID, redirect string) url.Values {
		t.Helper()
		a := newTestOIDCAuth(t, core.OIDCAuth{Issuer: server.URL, ClientID: clientID, RedirectURL: redirect}, "", "", "")
		raw, _, _, err := a.OIDCLoginURL()
		if err != nil {
			t.Fatalf("OIDCLoginURL: %v", err)
		}
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		return u.Query()
	}

	// Control: a plain callback URL round-trips unchanged.
	q := login("test-client", "http://localhost:8080/callback")
	if q.Get("redirect_uri") != "http://localhost:8080/callback" || q.Get("client_id") != "test-client" {
		t.Fatalf("control: redirect_uri=%q client_id=%q", q.Get("redirect_uri"), q.Get("client_id"))
	}

	const redirect = "https://app.example.com/cb?tenant=a&mode=b"
	const clientID = "client id&x=1"
	q = login(clientID, redirect)
	if got := q.Get("redirect_uri"); got != redirect {
		t.Errorf("redirect_uri = %q, want %q", got, redirect)
	}
	if got := q.Get("client_id"); got != clientID {
		t.Errorf("client_id = %q, want %q", got, clientID)
	}
	for _, injected := range []string{"tenant", "mode", "x"} {
		if q.Has(injected) {
			t.Errorf("value leaked into the authorization request as parameter %q", injected)
		}
	}
	if q.Get("response_type") != "code" || q.Get("scope") != "openid profile email" || q.Get("state") == "" || q.Get("nonce") == "" {
		t.Errorf("standard parameters damaged: %v", q)
	}
}
