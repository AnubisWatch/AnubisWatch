package alert

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// fakeSMTP accepts one plain-text session (no STARTTLS, no AUTH) and returns
// the DATA payload.
func fakeSMTP(t *testing.T) (port int, data <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	out := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		r := bufio.NewReader(conn)
		write := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
		write("220 fake ESMTP")
		var body strings.Builder
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if inData {
				if line == ".\r\n" {
					inData = false
					out <- body.String()
					write("250 queued")
					continue
				}
				body.WriteString(line)
				continue
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				write("250 fake")
			case strings.HasPrefix(cmd, "MAIL"), strings.HasPrefix(cmd, "RCPT"):
				write("250 ok")
			case cmd == "DATA":
				inData = true
				write("354 go ahead")
			case cmd == "QUIT":
				write("221 bye")
				return
			default:
				write("250 ok")
			}
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, out
}

// Mail used to go out without From/Date headers (spam-foldered or rejected)
// and a soul name containing CR/LF could inject headers.
func TestEmailSendWritesStandardHeaders(t *testing.T) {
	port, data := fakeSMTP(t)
	d := &EmailDispatcher{logger: newTestLogger()}
	event := &core.AlertEvent{
		SoulName: "api\r\nBcc: victim@example.com",
		Status:   core.SoulDead,
		Severity: core.SeverityCritical,
	}
	channel := &core.AlertChannel{Config: map[string]any{
		"smtp_host": "127.0.0.1",
		"smtp_port": float64(port),
		"from":      "alerts@example.com",
		"to":        []any{"ops@example.com"},
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Send(ctx, event, channel); err != nil {
		t.Fatalf("Send: %v", err)
	}
	msg := <-data
	headers := msg[:strings.Index(msg, "\r\n\r\n")]
	for _, want := range []string{"From: alerts@example.com", "To: ops@example.com", "Date: ", "Message-ID: <", "MIME-Version: 1.0"} {
		if !strings.Contains(headers, want) {
			t.Errorf("missing header %q in:\n%s", want, headers)
		}
	}
	for _, line := range strings.Split(headers, "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), "bcc:") {
			t.Fatalf("header injection via soul name: %q", line)
		}
	}
}

func TestEmailSendRespectsContextCancellation(t *testing.T) {
	// A listener that accepts but never speaks would hang smtp.SendMail forever.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	d := &EmailDispatcher{logger: newTestLogger()}
	channel := &core.AlertChannel{Config: map[string]any{
		"smtp_host": "127.0.0.1",
		"smtp_port": float64(ln.Addr().(*net.TCPAddr).Port),
		"to":        []any{"ops@example.com"},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := d.Send(ctx, &core.AlertEvent{SoulName: "x"}, channel); err == nil {
		t.Fatal("expected error from silent server")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("Send ignored context deadline (took %v)", time.Since(start))
	}
}
