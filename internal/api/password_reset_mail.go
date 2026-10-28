package api

import (
	"html"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/mail"
)

// MailQueue queues an email for asynchronous delivery.
type MailQueue interface {
	Enqueue(msg mail.Message) bool
}

// Reset emails per address per window. Further requests get the same answer
// and send nothing, so the limit is not observable.
const (
	resetMailLimit  = 3
	resetMailWindow = time.Hour
)

type resetMailer struct {
	queue     MailQueue
	publicURL string

	mu   sync.Mutex
	sent map[string][]time.Time
}

// SetPasswordResetMailer makes POST /api/v1/auth/reset-password email a link
// to the dashboard reset page at publicURL. Without it the endpoint keeps
// pointing operators to `anubis admin reset-password`.
func (s *RESTServer) SetPasswordResetMailer(queue MailQueue, publicURL string) {
	publicURL = strings.TrimRight(strings.TrimSpace(publicURL), "/")
	if queue == nil || publicURL == "" {
		s.resetMail = nil
		return
	}
	s.resetMail = &resetMailer{queue: queue, publicURL: publicURL, sent: make(map[string][]time.Time)}
}

func (m *resetMailer) allow(email string, now time.Time) bool {
	key := strings.ToLower(strings.TrimSpace(email))
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sent) > 10000 {
		for k, times := range m.sent {
			if len(times) == 0 || now.Sub(times[len(times)-1]) > resetMailWindow {
				delete(m.sent, k)
			}
		}
	}
	recent := m.sent[key][:0]
	for _, t := range m.sent[key] {
		if now.Sub(t) < resetMailWindow {
			recent = append(recent, t)
		}
	}
	if len(recent) >= resetMailLimit {
		m.sent[key] = recent
		return false
	}
	m.sent[key] = append(recent, now)
	return true
}

func (m *resetMailer) message(to, token string) mail.Message {
	link := m.publicURL + "/reset-password?" + url.Values{"token": {token}}.Encode()
	return mail.Message{
		To:      to,
		Subject: "Reset your AnubisWatch password",
		Kind:    "password_reset",
		Text: "A password reset was requested for this AnubisWatch account.\n\n" +
			"Choose a new password within one hour:\n" + link + "\n\n" +
			"If you did not ask for this, ignore this email; your password stays unchanged.",
		HTML: `<!DOCTYPE html><html><body style="font-family:Arial,sans-serif;line-height:1.6;color:#333">` +
			`<p>A password reset was requested for this AnubisWatch account.</p>` +
			`<p><a href="` + html.EscapeString(link) + `">Choose a new password</a> within one hour.</p>` +
			`<p>If you did not ask for this, ignore this email; your password stays unchanged.</p></body></html>`,
	}
}

// resetRecipientAuth is implemented by authenticators that report the
// account's stored address with a reset token.
type resetRecipientAuth interface {
	RequestPasswordResetTo(email string) (token, recipient string, err error)
}

// requestPasswordReset issues a token and names where to send it: the stored
// address of the account, never the address as typed (which only matches it
// in canonical form).
func (s *RESTServer) requestPasswordReset(email string) (string, string, error) {
	if r, ok := s.auth.(resetRecipientAuth); ok {
		return r.RequestPasswordResetTo(email)
	}
	token, err := s.auth.RequestPasswordReset(email)
	return token, email, err
}

const resetMailSentMessage = "if the email belongs to an account, a password reset link has been sent to it"

// requestPasswordResetByEmail issues a reset token and queues it to the
// address. The answer and its timing do not depend on whether the address
// belongs to an account: the token lookup equalises its own timing and the
// email is sent asynchronously. The token is never logged.
func (s *RESTServer) requestPasswordResetByEmail(ctx *Context, email string) error {
	email = strings.TrimSpace(email)
	if email == "" || len(email) > 254 || strings.ContainsAny(email, "\r\n") {
		return ctx.Error(http.StatusBadRequest, "a valid email is required")
	}
	if s.resetMail.allow(email, time.Now()) {
		token, recipient, err := s.requestPasswordReset(email)
		if err != nil {
			s.logger.Error("password reset request failed", "error", err)
			return ctx.Error(http.StatusInternalServerError, "failed to request password reset")
		}
		if token != "" && !s.resetMail.queue.Enqueue(s.resetMail.message(recipient, token)) {
			s.logger.Warn("password reset email could not be queued")
		}
	}
	return ctx.JSON(http.StatusOK, map[string]string{"message": resetMailSentMessage})
}
