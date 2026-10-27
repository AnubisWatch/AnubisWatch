package probe

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func smtpOversizeGreeting(t *testing.T, greeting string) *core.Judgment {
	t.Helper()
	server := newMockSMTPServer(t, func(conn net.Conn) {
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		_, _ = io.WriteString(conn, greeting)
		_, _ = io.Copy(io.Discard, bufio.NewReader(conn))
	})
	t.Cleanup(server.Close)
	judgment, _ := NewSMTPChecker().Judge(context.Background(), &core.Soul{
		ID: "smtp-limit", Type: core.CheckSMTP, Target: server.Addr(),
		Timeout: core.Duration{Duration: 5 * time.Second},
		SMTP:    &core.SMTPConfig{BannerContains: "nomatch"},
	})
	return judgment
}

func retainedSMTPBytes(j *core.Judgment) int {
	n := len(j.Message)
	if j.Details != nil {
		for _, a := range j.Details.Assertions {
			n += len(a.Actual)
		}
	}
	return n
}

func TestSMTPGreetingIsBoundedByMaxReadSize(t *testing.T) {
	publishValidatorForTest(t, permissiveSSRFValidator())

	// Control: an ordinary greeting is still read and asserted.
	control := smtpOversizeGreeting(t, "220 ready\r\n")
	if control.Status != core.SoulDead || len(control.Details.Assertions) != 1 || control.Details.Assertions[0].Actual != "220 ready" {
		t.Fatalf("control failed: status=%s msg=%q", control.Status, control.Message)
	}

	for name, greeting := range map[string]string{
		"single huge line":   "220 ready" + strings.Repeat("a", 4*maxReadSize) + "\r\n",
		"long multiline 220": strings.Repeat("220-ready "+strings.Repeat("a", 1000)+"\r\n", 4*maxReadSize/1000) + "220 end\r\n",
	} {
		t.Run(name, func(t *testing.T) {
			j := smtpOversizeGreeting(t, greeting)
			if got := retainedSMTPBytes(j); got > maxReadSize {
				t.Fatalf("judgment retained %d bytes of peer data, limit is %d", got, maxReadSize)
			}
			if j.Status == core.SoulAlive {
				t.Fatalf("oversize greeting must not yield alive")
			}
		})
	}
}
