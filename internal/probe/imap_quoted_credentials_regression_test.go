package probe

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type imapQuotedLoginResult struct {
	line string
	err  error
}

func runIMAPQuotedLogin(t *testing.T, username, password, expectedLine string) (*core.Judgment, imapQuotedLoginResult) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	cert := loadTestCert(t)
	resultCh := make(chan imapQuotedLoginResult, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			resultCh <- imapQuotedLoginResult{err: acceptErr}
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

		tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}})
		if handshakeErr := tlsConn.Handshake(); handshakeErr != nil {
			resultCh <- imapQuotedLoginResult{err: handshakeErr}
			return
		}
		reader := bufio.NewReader(tlsConn)
		if _, writeErr := fmt.Fprint(tlsConn, "* OK IMAP4rev1 ready\r\n"); writeErr != nil {
			resultCh <- imapQuotedLoginResult{err: writeErr}
			return
		}
		if _, readErr := reader.ReadString('\n'); readErr != nil {
			resultCh <- imapQuotedLoginResult{err: readErr}
			return
		}
		if _, writeErr := fmt.Fprint(tlsConn, "* CAPABILITY IMAP4rev1 AUTH=LOGIN\r\nA001 OK CAPABILITY completed\r\n"); writeErr != nil {
			resultCh <- imapQuotedLoginResult{err: writeErr}
			return
		}
		loginLine, readErr := reader.ReadString('\n')
		if readErr != nil {
			resultCh <- imapQuotedLoginResult{err: readErr}
			return
		}
		if loginLine == expectedLine {
			_, _ = fmt.Fprint(tlsConn, "A002 OK LOGIN completed\r\n")
			_, _ = reader.ReadString('\n') // LOGOUT
		} else {
			_, _ = fmt.Fprint(tlsConn, "A002 NO malformed LOGIN\r\n")
		}
		resultCh <- imapQuotedLoginResult{line: loginLine}
	}()

	soul := &core.Soul{
		ID:      "imap-quoted-regression",
		Name:    "IMAP quoted credentials regression",
		Type:    core.CheckIMAP,
		Target:  listener.Addr().String(),
		Enabled: true,
		Timeout: core.Duration{Duration: 5 * time.Second},
		IMAP: &core.IMAPConfig{
			TLS:                true,
			InsecureSkipVerify: true,
			Auth:               &core.AuthCreds{Username: username, Password: password},
		},
	}
	checker := NewIMAPChecker()
	if validateErr := checker.Validate(soul); validateErr != nil {
		t.Fatalf("Validate: %v", validateErr)
	}
	judgment, _ := checker.Judge(context.Background(), soul)
	serverResult := <-resultCh
	if serverResult.err != nil {
		t.Fatalf("server: %v", serverResult.err)
	}
	return judgment, serverResult
}

func TestIMAPLoginEscapesQuotedCredentials(t *testing.T) {
	publishValidatorForTest(t, permissiveSSRFValidator())

	tests := []struct {
		name         string
		username     string
		password     string
		expectedLine string
	}{
		{name: "simple control", username: "simple-user", password: "simple-pass", expectedLine: `A002 LOGIN "simple-user" "simple-pass"` + "\r\n"},
		{name: "quote", username: `user"name`, password: `pass"word`, expectedLine: `A002 LOGIN "user\"name" "pass\"word"` + "\r\n"},
		{name: "backslash", username: `domain\user`, password: `pa\ss`, expectedLine: `A002 LOGIN "domain\\user" "pa\\ss"` + "\r\n"},
		{name: "combined", username: `domain\"user`, password: `pa\"ss`, expectedLine: `A002 LOGIN "domain\\\"user" "pa\\\"ss"` + "\r\n"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			judgment, server := runIMAPQuotedLogin(t, tc.username, tc.password, tc.expectedLine)
			if judgment.Status != core.SoulAlive {
				t.Fatalf("status=%s message=%q line=%q", judgment.Status, judgment.Message, server.line)
			}
			if server.line != tc.expectedLine {
				t.Fatalf("LOGIN line = %q, want %q", server.line, tc.expectedLine)
			}
		})
	}
}

func TestIMAPCredentialsRejectCommandBoundaries(t *testing.T) {
	publishValidatorForTest(t, permissiveSSRFValidator())
	checker := NewIMAPChecker()

	for _, tc := range []struct {
		name     string
		username string
		password string
	}{
		{name: "carriage return", username: "user\rX", password: "pass"},
		{name: "line feed", username: "user", password: "pass\nX"},
		{name: "nul", username: "user\x00X", password: "pass"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			soul := &core.Soul{
				Target: "127.0.0.1:993",
				IMAP: &core.IMAPConfig{
					TLS:  true,
					Auth: &core.AuthCreds{Username: tc.username, Password: tc.password},
				},
			}
			err := checker.Validate(soul)
			if err == nil || !strings.Contains(err.Error(), "cannot contain") {
				t.Fatalf("Validate error = %v, want quoted-string rejection", err)
			}
		})
	}
}
