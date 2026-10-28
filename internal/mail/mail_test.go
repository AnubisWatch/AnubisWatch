package mail

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/smtp"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/mail/mailtest"
	"github.com/AnubisWatch/anubiswatch/internal/probe"
)

func TestMain(m *testing.M) {
	// The fake relay listens on 127.0.0.1, which the SSRF policy blocks by
	// default; allow private targets as the alert tests do.
	os.Setenv("ANUBIS_SSRF_ALLOW_PRIVATE", "1")
	probe.ResetDefaultForTest()
	os.Exit(m.Run())
}

func testMailer(t *testing.T, srv *mailtest.Server) *Mailer {
	t.Helper()
	m, err := New(core.MailConfig{
		Host: srv.Host(),
		Port: srv.Port(),
		From: "AnubisWatch <status@example.com>",
		TLS:  core.MailTLSNone,
	})
	if err != nil || m == nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

func TestNewReturnsNilWithoutHost(t *testing.T) {
	m, err := New(core.MailConfig{})
	if m != nil || err != nil {
		t.Fatalf("New(empty) = %v, %v; want nil, nil", m, err)
	}
	if err := m.Send(context.Background(), Message{To: "a@example.com"}); err == nil {
		t.Fatal("nil mailer sent a message")
	}
}

func TestSendDeliversMultipartMessage(t *testing.T) {
	srv := mailtest.NewServer(t)
	m := testMailer(t, srv)
	err := m.Send(context.Background(), Message{
		To:      "user@example.com",
		Subject: "Hello — status",
		Text:    "Plain body https://status.example.com/x?token=abc",
		HTML:    "<p>HTML body</p>",
		Headers: map[string]string{"List-Unsubscribe": "<https://status.example.com/u>"},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	msgs := srv.WaitFor(1, 2*time.Second)
	if len(msgs) != 1 {
		t.Fatalf("got %d messages", len(msgs))
	}
	got := msgs[0]
	if got.From != "status@example.com" || len(got.To) != 1 || got.To[0] != "user@example.com" {
		t.Fatalf("envelope = %q -> %v", got.From, got.To)
	}
	for _, h := range []string{"Date", "Message-ID", "MIME-Version", "Subject", "List-Unsubscribe"} {
		if got.Header(h) == "" {
			t.Errorf("missing header %s", h)
		}
	}
	if !strings.Contains(got.Header("Content-Type"), "multipart/alternative") {
		t.Errorf("Content-Type = %q", got.Header("Content-Type"))
	}
	body := got.Body()
	if !strings.Contains(body, "https://status.example.com/x?token=abc") || !strings.Contains(body, "<p>HTML body</p>") {
		t.Errorf("body lacks a part:\n%s", body)
	}
}

func TestHeaderInjectionRejected(t *testing.T) {
	srv := mailtest.NewServer(t)
	m := testMailer(t, srv)
	cases := []Message{
		{To: "victim@example.com\r\nBcc: attacker@example.com", Subject: "x", Text: "x"},
		{To: "victim@example.com", Subject: "x\r\nBcc: attacker@example.com", Text: "x"},
		{To: "victim@example.com", Subject: "x", Text: "x", Headers: map[string]string{"List-Unsubscribe": "<x>\r\nBcc: a@example.com"}},
		{To: "victim@example.com", Subject: "x", Text: "x", Headers: map[string]string{"Bad Name": "v"}},
	}
	for i, msg := range cases {
		if err := m.Send(context.Background(), msg); err == nil {
			t.Errorf("case %d: injected message was sent", i)
		}
	}
	if n := len(srv.Messages()); n != 0 {
		t.Fatalf("%d messages reached the relay", n)
	}
	if _, err := New(core.MailConfig{Host: "x", From: "a@example.com\r\nBcc: b@example.com"}); err == nil {
		t.Fatal("New accepted a From with a line break")
	}
}

func TestSendRefusesBlockedRelay(t *testing.T) {
	previous := probe.DefaultValidator.Load()
	t.Cleanup(func() { probe.DefaultValidator.Store(previous) })
	t.Setenv("ANUBIS_SSRF_ALLOW_PRIVATE", "")
	if err := probe.ConfigureDefaultValidator(false, nil); err != nil {
		t.Fatal(err)
	}
	srv := mailtest.NewServer(t)
	m := testMailer(t, srv)
	err := m.Send(context.Background(), Message{To: "a@example.com", Subject: "x", Text: "x"})
	if err == nil {
		t.Fatal("send to a loopback relay succeeded under the default SSRF policy")
	}
	if len(srv.Messages()) != 0 {
		t.Fatal("blocked relay received a message")
	}
}

type flakySender struct {
	fails int
	calls chan Message
}

func (f *flakySender) Send(ctx context.Context, msg Message) error {
	f.calls <- msg
	if f.fails > 0 {
		f.fails--
		return errors.New("transient")
	}
	return nil
}

func TestQueueRetriesThenDelivers(t *testing.T) {
	srv := mailtest.NewServer(t)
	srv.FailNext(2)
	m := testMailer(t, srv)
	q := NewQueue(m, slog.New(slog.NewTextHandler(io.Discard, nil)), QueueOptions{RetryDelays: []time.Duration{10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond}})
	defer q.Close()
	if !q.Enqueue(Message{To: "a@example.com", Subject: "s", Text: "t", Kind: "test"}) {
		t.Fatal("Enqueue refused")
	}
	if msgs := srv.WaitFor(1, 3*time.Second); len(msgs) != 1 {
		t.Fatalf("got %d messages after retries", len(msgs))
	}
}

func TestQueueGivesUpAfterRetries(t *testing.T) {
	f := &flakySender{fails: 100, calls: make(chan Message, 10)}
	q := NewQueue(f, slog.New(slog.NewTextHandler(io.Discard, nil)), QueueOptions{RetryDelays: []time.Duration{time.Millisecond}})
	defer q.Close()
	q.Enqueue(Message{To: "a@example.com"})
	for i := 0; i < 2; i++ {
		select {
		case <-f.calls:
		case <-time.After(2 * time.Second):
			t.Fatalf("attempt %d never happened", i+1)
		}
	}
	select {
	case <-f.calls:
		t.Fatal("queue retried past its retry budget")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestQueueClosedRefuses(t *testing.T) {
	q := NewQueue(&flakySender{calls: make(chan Message, 1)}, nil, QueueOptions{})
	q.Close()
	q.Close()
	if q.Enqueue(Message{To: "a@example.com"}) {
		t.Fatal("closed queue accepted a message")
	}
}

// plainAuth keeps net/smtp.PlainAuth's guards: no credentials over an
// unencrypted non-local connection, and none for a host other than the
// configured one.
func TestPlainAuthGuards(t *testing.T) {
	password := "pw-" + core.GenerateID()
	auth := &plainAuth{username: "alerts@example.com", password: password, host: "smtp.example.com"}

	if _, resp, err := auth.Start(&smtp.ServerInfo{Name: "smtp.example.com", TLS: false}); err == nil || strings.Contains(string(resp), password) {
		t.Fatalf("Start() accepted an unencrypted connection (err %v)", err)
	}
	if _, resp, err := auth.Start(&smtp.ServerInfo{Name: "attacker.example", TLS: true}); err == nil || strings.Contains(string(resp), password) {
		t.Fatalf("Start() accepted another host (err %v)", err)
	}
	mech, resp, err := auth.Start(&smtp.ServerInfo{Name: "smtp.example.com", TLS: true})
	if err != nil || mech != "PLAIN" || !strings.Contains(string(resp), password) {
		t.Fatalf("Start() over TLS = %q, %v", mech, err)
	}
	for _, name := range []string{"localhost", "127.0.0.1", "::1"} {
		local := &plainAuth{username: "u", password: password, host: name}
		if _, _, err := local.Start(&smtp.ServerInfo{Name: name}); err != nil {
			t.Errorf("Start() refused local relay %q: %v", name, err)
		}
	}
	withIdentity := &plainAuth{identity: "id", username: "u", password: "p", host: "h"}
	if _, resp, _ := withIdentity.Start(nil); string(resp) != "id\x00u\x00p" {
		t.Errorf("PLAIN response = %q", resp)
	}
	if next, err := withIdentity.Next(nil, true); next != nil || err != nil {
		t.Errorf("Next() = %v, %v", next, err)
	}
}

func TestSendRawValidation(t *testing.T) {
	if err := SendRaw(context.Background(), Server{}, "a@example.com", nil, nil); err == nil {
		t.Fatal("empty host accepted")
	}
	if err := SendRaw(context.Background(), Server{Host: "h", Port: 25}, "a@example.com\r\n", nil, nil); err == nil {
		t.Fatal("CRLF sender accepted")
	}
}
