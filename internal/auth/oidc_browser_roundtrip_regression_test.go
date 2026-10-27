package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// fakeOIDCProvider is a minimal OpenID provider: discovery, JWKS, a token
// endpoint that enforces PKCE (S256), and userinfo. The test plays the
// browser and the authorization endpoint: it reads the authorization request
// and hands the code back to the callback, as the IdP's redirect would.
type fakeOIDCProvider struct {
	t      *testing.T
	server *httptest.Server
	priv   *rsa.PrivateKey

	mu    sync.Mutex
	codes map[string]fakeAuthorization // code -> the request it answers
}

type fakeAuthorization struct {
	nonce, challenge, method string
}

func newFakeOIDCProvider(t *testing.T) *fakeOIDCProvider {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &fakeOIDCProvider{t: t, priv: priv, codes: make(map[string]fakeAuthorization)}
	p.server = httptest.NewServer(http.HandlerFunc(p.serve))
	t.Cleanup(p.server.Close)
	return p
}

func (p *fakeOIDCProvider) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		_ = json.NewEncoder(w).Encode(oidcConfig{
			Issuer:      p.server.URL,
			AuthURL:     p.server.URL + "/authorize",
			TokenURL:    p.server.URL + "/token",
			UserInfoURL: p.server.URL + "/userinfo",
			JWKSURI:     p.server.URL + "/jwks",
		})
	case "/jwks":
		_ = json.NewEncoder(w).Encode(createTestJWK(p.priv))
	case "/token":
		if err := r.ParseForm(); err != nil {
			http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
			return
		}
		p.mu.Lock()
		authz, ok := p.codes[r.PostForm.Get("code")]
		delete(p.codes, r.PostForm.Get("code"))
		p.mu.Unlock()
		verifier := r.PostForm.Get("code_verifier")
		if !ok || authz.method != "S256" || verifier == "" || pkceChallengeS256(verifier) != authz.challenge {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
			return
		}
		idToken := createTestJWT(p.t, p.priv, map[string]interface{}{
			"iss":            p.server.URL,
			"aud":            "anubis-client",
			"sub":            "sso-user-1",
			"nonce":          authz.nonce,
			"email":          "sso.user@example.com",
			"email_verified": true,
			"name":           "SSO User",
			"exp":            time.Now().Add(time.Hour).Unix(),
		})
		_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: "provider-access", IDToken: idToken, TokenType: "Bearer"})
	case "/userinfo":
		w.WriteHeader(http.StatusUnauthorized)
	default:
		http.NotFound(w, r)
	}
}

// authorize plays the authorization endpoint for the request in loginURL and
// returns the code and state the IdP would redirect back with.
func (p *fakeOIDCProvider) authorize(loginURL string) (code, state string) {
	p.t.Helper()
	u, err := url.Parse(loginURL)
	if err != nil {
		p.t.Fatal(err)
	}
	if !strings.HasPrefix(loginURL, p.server.URL+"/authorize?") {
		p.t.Fatalf("login redirected to %q, not the provider", loginURL)
	}
	q := u.Query()
	code = "code-" + q.Get("nonce")[:8]
	p.mu.Lock()
	p.codes[code] = fakeAuthorization{nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), method: q.Get("code_challenge_method")}
	p.mu.Unlock()
	return code, q.Get("state")
}

func newOIDCRoundTripServer(t *testing.T, issuer string, trustedProxies []string) http.Handler {
	t.Helper()
	authn := newTestOIDCAuth(t, core.OIDCAuth{
		Issuer:       issuer,
		ClientID:     "anubis-client",
		ClientSecret: "client-secret",
		RedirectURL:  "https://anubis.example/api/v1/auth/oidc/callback",
	}, "", "admin@example.com", "TestPass1234!")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := api.NewRESTServer(core.ServerConfig{Host: "127.0.0.1", Port: 8080, TrustedProxies: trustedProxies},
		core.AuthConfig{Enabled: core.BoolPtr(true), Type: "oidc"}, nil, nil, nil, authn, nil, nil, nil, nil, nil, logger)
	t.Cleanup(func() { _ = srv.Stop(t.Context()) })
	return srv.Handler()
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}

const browserAccept = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"

// TestOIDCBrowserRoundTrip drives the whole SSO flow the way a browser does.
//
// Before the fix the flow cookies were SameSite=Strict, which browsers drop
// from the IdP's cross-site redirect back to the callback ("missing nonce
// cookie"), the callback answered a browser with raw JSON instead of landing
// on the dashboard, and the authorization request carried no PKCE challenge.
func TestOIDCBrowserRoundTrip(t *testing.T) {
	provider := newFakeOIDCProvider(t)
	handler := newOIDCRoundTripServer(t, provider.server.URL, nil)

	// 1. The login page's "Sign in with SSO" link.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/login", nil)
	req.Header.Set("Accept", browserAccept)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("login: status %d body %s", rec.Code, rec.Body.String())
	}
	loginURL := rec.Header().Get("Location")
	q, _ := url.ParseQuery(strings.SplitN(loginURL, "?", 2)[1])
	if q.Get("code_challenge_method") != "S256" || len(q.Get("code_challenge")) != 43 {
		t.Fatalf("authorization request lacks an S256 PKCE challenge: %s", loginURL)
	}
	if q.Get("code_verifier") != "" {
		t.Fatal("the PKCE verifier must never leave the server")
	}
	flow := rec.Result().Cookies()
	for _, name := range []string{"oidc_state", "oidc_nonce"} {
		c := findCookie(flow, name)
		if c == nil || c.Value == "" {
			t.Fatalf("login did not set %s", name)
		}
		// Lax: sent on the IdP's cross-site top-level GET back to the callback.
		if c.SameSite != http.SameSiteLaxMode || !c.HttpOnly || c.MaxAge <= 0 || c.MaxAge > 600 {
			t.Errorf("%s cookie policy = %+v, want HttpOnly SameSite=Lax short-lived", name, c)
		}
		if c.Secure {
			t.Errorf("%s is Secure on a plain-HTTP request; the browser would drop it", name)
		}
		if !strings.HasPrefix("/api/v1/auth/oidc/callback", c.Path) {
			t.Errorf("%s path %q does not cover the callback", name, c.Path)
		}
	}

	// 2. The IdP authenticates the user and redirects back.
	code, state := provider.authorize(loginURL)

	// 3. The browser follows that redirect, carrying the Lax cookies.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(state), nil)
	req.Header.Set("Accept", browserAccept)
	for _, c := range flow {
		req.AddCookie(c)
	}
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("callback: status %d location %q body %s; want 303 to the dashboard", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	result := rec.Result().Cookies()
	session := findCookie(result, "auth_token")
	if session == nil || session.Value == "" || !session.HttpOnly || session.Path != "/" {
		t.Fatalf("callback did not set the dashboard session cookie: %+v", session)
	}
	for _, name := range []string{"oidc_state", "oidc_nonce"} {
		if c := findCookie(result, name); c == nil || c.MaxAge >= 0 {
			t.Errorf("callback did not expire %s: %+v", name, c)
		}
	}

	// 4. The dashboard detects the session the way useAuth does.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.AddCookie(session)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "sso.user@example.com") {
		t.Fatalf("/auth/me with the SSO session: %d %s", rec.Code, rec.Body.String())
	}

	// 5. The flow is single-use: replaying the callback lands on the login
	// page with an error instead of minting a second session.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(state), nil)
	req.Header.Set("Accept", browserAccept)
	for _, c := range flow {
		req.AddCookie(c)
	}
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/login?") || findCookie(rec.Result().Cookies(), "auth_token") != nil {
		t.Fatalf("replayed callback: status %d location %q", rec.Code, rec.Header().Get("Location"))
	}
}

// TestOIDCCallbackAPIClientsAndBinding covers the non-browser contract and
// the browser binding of the flow.
func TestOIDCCallbackAPIClientsAndBinding(t *testing.T) {
	provider := newFakeOIDCProvider(t)
	handler := newOIDCRoundTripServer(t, provider.server.URL, []string{"10.0.0.1"})

	start := func(t *testing.T, mutate func(*http.Request)) (flow []*http.Cookie, code, state string) {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/login", nil)
		if mutate != nil {
			mutate(req)
		}
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusFound {
			t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
		}
		code, state = provider.authorize(rec.Header().Get("Location"))
		return rec.Result().Cookies(), code, state
	}
	callback := func(flow []*http.Cookie, code, state, accept string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(state), nil)
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		for _, c := range flow {
			req.AddCookie(c)
		}
		handler.ServeHTTP(rec, req)
		return rec
	}

	t.Run("API client gets JSON", func(t *testing.T) {
		flow, code, state := start(t, nil)
		rec := callback(flow, code, state, "application/json")
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "sso.user@example.com") {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), findCookie(rec.Result().Cookies(), "auth_token").Value) {
			t.Fatal("session token leaked into the JSON body")
		}
	})

	t.Run("flow cookies are Secure behind a trusted HTTPS proxy", func(t *testing.T) {
		flow, _, _ := start(t, func(r *http.Request) {
			r.RemoteAddr = "10.0.0.1:4000"
			r.Header.Set("X-Forwarded-Proto", "https")
		})
		for _, name := range []string{"oidc_state", "oidc_nonce"} {
			if c := findCookie(flow, name); c == nil || !c.Secure || c.SameSite != http.SameSiteLaxMode {
				t.Errorf("%s = %+v, want Secure SameSite=Lax", name, c)
			}
		}
	})

	t.Run("X-Forwarded-Proto from an untrusted peer is ignored", func(t *testing.T) {
		flow, _, _ := start(t, func(r *http.Request) {
			r.RemoteAddr = "198.51.100.7:4000"
			r.Header.Set("X-Forwarded-Proto", "https")
		})
		if c := findCookie(flow, "oidc_state"); c == nil || c.Secure {
			t.Errorf("oidc_state = %+v, want non-Secure for an untrusted peer", c)
		}
	})

	t.Run("callback without the flow cookies is refused", func(t *testing.T) {
		_, code, state := start(t, nil)
		rec := callback(nil, code, state, "application/json")
		if rec.Code != http.StatusBadRequest || findCookie(rec.Result().Cookies(), "auth_token") != nil {
			t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("state from another browser's flow is refused", func(t *testing.T) {
		victimFlow, _, _ := start(t, nil)
		_, attackerCode, attackerState := start(t, nil)
		rec := callback(victimFlow, attackerCode, attackerState, browserAccept)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") == "/" || findCookie(rec.Result().Cookies(), "auth_token") != nil {
			t.Fatalf("login CSRF accepted: status %d location %q", rec.Code, rec.Header().Get("Location"))
		}
	})

	t.Run("provider error sends the browser to the login page", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?error=access_denied&error_description=nope", nil)
		req.Header.Set("Accept", browserAccept)
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login?sso_error=1" {
			t.Fatalf("status %d location %q", rec.Code, rec.Header().Get("Location"))
		}
	})
}

// TestOIDCTokenExchangeSendsPKCEVerifier pins the authenticator half: the
// verifier stored with the login state is what the token request proves.
func TestOIDCTokenExchangeSendsPKCEVerifier(t *testing.T) {
	provider := newFakeOIDCProvider(t)
	a := newTestOIDCAuth(t, core.OIDCAuth{
		Issuer:      provider.server.URL,
		ClientID:    "anubis-client",
		RedirectURL: "https://anubis.example/api/v1/auth/oidc/callback",
	}, "", "admin@example.com", "TestPass1234!")

	loginURL, state, nonce, err := a.OIDCLoginURL()
	if err != nil {
		t.Fatal(err)
	}
	code, _ := provider.authorize(loginURL)

	// Without the verifier the provider refuses the code.
	if _, err := a.exchangeCode(code); err == nil {
		t.Fatal("token endpoint accepted a code without its PKCE verifier")
	}
	code, _ = provider.authorize(loginURL)
	if _, _, err := a.OIDCCallback(code, state, nonce); err != nil {
		t.Fatalf("OIDCCallback with PKCE: %v", err)
	}
}
