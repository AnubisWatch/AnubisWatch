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
