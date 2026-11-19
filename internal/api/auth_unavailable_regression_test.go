package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// outageError is what a cluster authenticator returns when it cannot prove
// quorum: it says nothing about the credentials.
type outageError struct{}

func (outageError) Error() string         { return "authentication quorum unavailable: no quorum" }
func (outageError) AuthUnavailable() bool { return true }

// outageAuthenticator fails Authenticate and Login with err.
type outageAuthenticator struct {
	mockAuthenticator
	err error
}

func (a *outageAuthenticator) Authenticate(string) (*User, error) { return nil, a.err }
func (a *outageAuthenticator) Login(string, string) (*User, string, error) {
	return nil, "", a.err
}

func outageServer(err error) *RESTServer {
	router := &Router{routes: make(map[string]map[string]Handler)}
	server := &RESTServer{
		config:     core.ServerConfig{Host: "localhost", Port: 8080},
		authConfig: core.AuthConfig{Enabled: core.BoolPtr(true)},
		store:      newMockStorage(),
		router:     router,
		auth:       &outageAuthenticator{err: err},
		logger:     newTestLogger(),
		cluster:    &mockClusterManager{},
	}
	router.Handle("GET", "/api/v1/souls", server.requireAuth(func(ctx *Context) error { return ctx.JSON(http.StatusOK, map[string]string{"ok": "1"}) }))
	router.Handle("POST", "/api/v1/auth/login", server.handleLogin)
	return server
}

// While the cluster cannot prove quorum, a valid session must not be answered
// 401: the dashboard logs the user out on 401. 503 + Retry-After tells it to
// keep the session and retry. A genuinely bad token is still 401.
func TestAuthOutageIsServiceUnavailableNotUnauthorized(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		method     string
		path       string
		body       string
		wantStatus int
		wantRetry  bool
	}{
		{"valid session during an outage", outageError{}, "GET", "/api/v1/souls", "", http.StatusServiceUnavailable, true},
		{"wrapped outage error", errors.Join(errors.New("context"), outageError{}), "GET", "/api/v1/souls", "", http.StatusServiceUnavailable, true},
		{"genuinely invalid token", errors.New("invalid token"), "GET", "/api/v1/souls", "", http.StatusUnauthorized, false},
		{"login during an outage", outageError{}, "POST", "/api/v1/auth/login", `{"email":"a@b.c","password":"x"}`, http.StatusServiceUnavailable, true},
		{"wrong credentials", errors.New("invalid credentials"), "POST", "/api/v1/auth/login", `{"email":"a@b.c","password":"x"}`, http.StatusUnauthorized, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := outageServer(tt.err)
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Authorization", "Bearer some-token")
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			server.router.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d (%s), want %d", w.Code, w.Body.String(), tt.wantStatus)
			}
			if got := w.Header().Get("Retry-After") != ""; got != tt.wantRetry {
				t.Fatalf("Retry-After present = %v, want %v", got, tt.wantRetry)
			}
			if tt.wantStatus == http.StatusServiceUnavailable && w.Header().Get("Set-Cookie") != "" {
				t.Fatal("an outage must not set a session cookie")
			}
		})
	}
}

// Password-management handlers classify some failures by message text. An
// outage message must never be mistaken for a bad token or password: it is 503.
type outagePasswordAuth struct {
	mockAuthenticator
	err error
}

func (a *outagePasswordAuth) ConfirmPasswordReset(string, string) error   { return a.err }
func (a *outagePasswordAuth) ChangePassword(string, string, string) error { return a.err }
func (a *outagePasswordAuth) Logout(string) error                         { return a.err }

type textOutage struct{ msg string }

func (e textOutage) Error() string         { return e.msg }
func (e textOutage) AuthUnavailable() bool { return true }

func TestPasswordHandlersAnswerOutageAs503(t *testing.T) {
	// Messages chosen to collide with the handlers' substring matching.
	msgs := []string{"invalid authentication revision", "current password store unavailable", "expired lease, policy engine down"}
	for _, msg := range msgs {
		server := outageServer(textOutage{msg})
		server.auth = &outagePasswordAuth{err: textOutage{msg}}
		server.router.Handle("POST", "/reset", server.handleConfirmPasswordReset)
		server.router.Handle("POST", "/change", func(ctx *Context) error { ctx.User = &User{ID: "u"}; return server.handleChangePassword(ctx) })
		server.router.Handle("POST", "/logout", server.handleLogout)
		for _, path := range []string{"/reset", "/change", "/logout"} {
			req := httptest.NewRequest("POST", path, strings.NewReader(`{"token":"t","new_password":"N3w-Passw0rd-ok!","current_password":"x"}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			server.router.ServeHTTP(w, req)
			if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") == "" {
				t.Fatalf("%s with %q: status %d (%s), want 503 + Retry-After", path, msg, w.Code, w.Body.String())
			}
		}
	}
}
