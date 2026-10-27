package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/mail"
	"github.com/AnubisWatch/anubiswatch/internal/mail/mailtest"
	"github.com/AnubisWatch/anubiswatch/internal/probe"
)

// resetAuth issues real-looking tokens for one account and accepts them once.
type resetAuth struct {
	mockAuthenticator
	mu     sync.Mutex
	admin  string
	tokens map[string]bool
	reset  bool
}

func (a *resetAuth) RequestPasswordReset(email string) (string, error) {
	if email != a.admin {
		return "", nil
	}
	token := "tok-" + core.GenerateID() + core.GenerateID()
	a.mu.Lock()
	a.tokens[token] = true
	a.mu.Unlock()
	return token, nil
}

func (a *resetAuth) ConfirmPasswordReset(token, newPassword string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.tokens[token] {
		return errors.New("invalid reset token")
	}
	delete(a.tokens, token)
	a.reset = true
	return nil
}

type queueRecorder struct {
	mu   sync.Mutex
	msgs []mail.Message
}

func (q *queueRecorder) Enqueue(msg mail.Message) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.msgs = append(q.msgs, msg)
	return true
}

func (q *queueRecorder) all() []mail.Message {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]mail.Message(nil), q.msgs...)
}

func newResetServer(t *testing.T, logs io.Writer) (*RESTServer, *resetAuth) {
	t.Helper()
	auth := &resetAuth{admin: "admin@example.com", tokens: make(map[string]bool)}
	logger := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	server := NewRESTServer(core.ServerConfig{Port: 8080}, core.AuthConfig{Enabled: core.BoolPtr(true), Type: "local"},
		newMockStorage(), &mockProbeEngine{}, &mockAlertManager{}, auth, &mockClusterManager{}, nil, nil, nil, nil, logger)
	t.Cleanup(server.ws.Stop)
	return server, auth
}

func postJSON(server *RESTServer, path, body, ip string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = ip + ":1234"
	rec := httptest.NewRecorder()
	server.router.ServeHTTP(rec, req)
	return rec
}

var resetLink = regexp.MustCompile(`https://anubis\.example\.com/reset-password\?token=[A-Za-z0-9%_.-]+`)

func TestPasswordResetEmailsWorkingLinkAndNeverLogsToken(t *testing.T) {
	var logs lockedLogBuffer
	server, auth := newResetServer(t, &logs)
	queue := &queueRecorder{}
	server.SetPasswordResetMailer(queue, "https://anubis.example.com/")

	known := postJSON(server, "/api/v1/auth/reset-password", `{"email":"admin@example.com"}`, "192.0.2.1")
	unknown := postJSON(server, "/api/v1/auth/reset-password", `{"email":"nobody@example.com"}`, "192.0.2.2")
	if known.Code != http.StatusOK || unknown.Code != http.StatusOK || known.Body.String() != unknown.Body.String() {
		t.Fatalf("responses differ (enumeration):\n%d %s\n%d %s", known.Code, known.Body, unknown.Code, unknown.Body)
	}
	msgs := queue.all()
	if len(msgs) != 1 || msgs[0].To != "admin@example.com" || msgs[0].Kind != "password_reset" {
		t.Fatalf("queued = %+v", msgs)
	}
	link := resetLink.FindString(msgs[0].Text)
	if link == "" || !strings.Contains(msgs[0].HTML, "/reset-password?token=") {
		t.Fatalf("no reset link in:\n%s", msgs[0].Text)
	}
	u, _ := url.Parse(link)
	token := u.Query().Get("token")

	// The link's token resets the password through the confirm endpoint.
	body, _ := json.Marshal(map[string]string{"token": token, "new_password": "N3w-Passw0rd!x"})
	if rec := postJSON(server, "/api/v1/auth/reset-password/confirm", string(body), "192.0.2.3"); rec.Code != http.StatusOK {
		t.Fatalf("confirm = %d %s", rec.Code, rec.Body)
	}
	if !auth.reset {
		t.Fatal("password was not reset")
	}
	if strings.Contains(logs.String(), token) {
		t.Fatalf("reset token appeared in the log:\n%s", logs.String())
	}
}

func TestPasswordResetMailPerAddressLimit(t *testing.T) {
	server, _ := newResetServer(t, io.Discard)
	queue := &queueRecorder{}
	server.SetPasswordResetMailer(queue, "https://anubis.example.com")
	for i := 0; i < resetMailLimit+2; i++ {
		if rec := postJSON(server, "/api/v1/auth/reset-password", `{"email":"admin@example.com"}`, fmt.Sprintf("192.0.2.%d", i+1)); rec.Code != http.StatusOK && rec.Code != http.StatusTooManyRequests {
			t.Fatalf("request %d = %d", i, rec.Code)
		}
	}
	if n := len(queue.all()); n > resetMailLimit {
		t.Fatalf("%d reset emails for one address, limit %d", n, resetMailLimit)
	}
	if rec := postJSON(server, "/api/v1/auth/reset-password", `{"email":"a@example.com\r\nBcc: x@example.com"}`, "198.51.100.9"); rec.Code != http.StatusBadRequest {
		t.Fatalf("CRLF address = %d", rec.Code)
	}
}

func TestPasswordResetWithoutMailPointsToCLI(t *testing.T) {
	server, _ := newResetServer(t, io.Discard)
	rec := postJSON(server, "/api/v1/auth/reset-password", `{"email":"admin@example.com"}`, "192.0.2.9")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "anubis admin reset-password") {
		t.Fatalf("without mail = %d %s", rec.Code, rec.Body)
	}
	methods := httptest.NewRecorder()
	server.router.ServeHTTP(methods, httptest.NewRequest(http.MethodGet, "/api/v1/auth/methods", nil))
	if !strings.Contains(methods.Body.String(), `"password_reset":false`) {
		t.Fatalf("auth/methods = %s", methods.Body)
	}
	server.SetPasswordResetMailer(&queueRecorder{}, "https://anubis.example.com")
	methods = httptest.NewRecorder()
	server.router.ServeHTTP(methods, httptest.NewRequest(http.MethodGet, "/api/v1/auth/methods", nil))
	if !strings.Contains(methods.Body.String(), `"password_reset":true`) {
		t.Fatalf("auth/methods with mail = %s", methods.Body)
	}
}

// TestPasswordResetOverFakeSMTP sends the reset email through the real
// mailer and queue to an SMTP server on 127.0.0.1.
func TestPasswordResetOverFakeSMTP(t *testing.T) {
	previous := probe.DefaultValidator.Load()
	t.Cleanup(func() { probe.DefaultValidator.Store(previous) })
	if err := probe.ConfigureDefaultValidator(true, nil); err != nil {
		t.Fatal(err)
	}
	srv := mailtest.NewServer(t)
	mailer, err := mail.New(core.MailConfig{Host: srv.Host(), Port: srv.Port(), From: "anubis@example.com", TLS: core.MailTLSNone})
	if err != nil {
		t.Fatal(err)
	}
	var logs lockedLogBuffer
	queue := mail.NewQueue(mailer, slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})), mail.QueueOptions{})
	defer queue.Close()
	server, auth := newResetServer(t, &logs)
	server.SetPasswordResetMailer(queue, "https://anubis.example.com")

	postJSON(server, "/api/v1/auth/reset-password", `{"email":"admin@example.com"}`, "192.0.2.20")
	msgs := srv.WaitFor(1, 3*time.Second)
	if len(msgs) != 1 || msgs[0].To[0] != "admin@example.com" {
		t.Fatalf("relay got %d messages", len(msgs))
	}
	link := resetLink.FindString(msgs[0].Body())
	if link == "" {
		t.Fatalf("no link in delivered mail:\n%s", msgs[0].Data)
	}
	u, _ := url.Parse(link)
	token := u.Query().Get("token")
	body, _ := json.Marshal(map[string]string{"token": token, "new_password": "N3w-Passw0rd!x"})
	if rec := postJSON(server, "/api/v1/auth/reset-password/confirm", string(body), "192.0.2.21"); rec.Code != http.StatusOK || !auth.reset {
		t.Fatalf("confirm via delivered link = %d", rec.Code)
	}
	if strings.Contains(logs.String(), token) {
		t.Fatal("reset token appeared in the log")
	}
}

// lockedLogBuffer collects log output written from several goroutines (the
// request handler and the mail queue worker) so tests can search it.
type lockedLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
