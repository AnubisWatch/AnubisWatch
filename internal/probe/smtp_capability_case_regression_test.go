package probe

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func smtpCaseStartTLSServer(t *testing.T, keyword string) (string, <-chan error, func()) {
	t.Helper()
	cert := loadTestCert(t)
	result := make(chan error, 1)
	server := newMockSMTPServer(t, func(conn net.Conn) {
		defer conn.Close()
		reader := bufio.NewReader(conn)
		if _, err := io.WriteString(conn, "220 proof.example ESMTP ready\r\n"); err != nil {
			result <- err
			return
		}
		if line, err := reader.ReadString('\n'); err != nil || !strings.HasPrefix(line, "EHLO ") {
			result <- fmt.Errorf("EHLO: line=%q err=%v", line, err)
			return
		}
		if _, err := fmt.Fprintf(conn, "250-proof.example\r\n250 %s\r\n", keyword); err != nil {
			result <- err
			return
		}
		line, err := reader.ReadString('\n')
		if err != nil || strings.TrimSpace(line) != "STARTTLS" {
			result <- fmt.Errorf("STARTTLS: line=%q err=%v", line, err)
			return
		}
		if _, err := io.WriteString(conn, "220 ready for TLS\r\n"); err != nil {
			result <- err
			return
		}
		tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
		if err := tlsConn.Handshake(); err != nil {
			result <- fmt.Errorf("handshake: %w", err)
			return
		}
		reader = bufio.NewReader(tlsConn)
		if line, err := reader.ReadString('\n'); err != nil || !strings.HasPrefix(line, "EHLO ") {
			result <- fmt.Errorf("TLS EHLO: line=%q err=%v", line, err)
			return
		}
		if _, err := io.WriteString(tlsConn, "250 proof.example\r\n"); err != nil {
			result <- err
			return
		}
		_, _ = io.Copy(io.Discard, tlsConn)
		result <- nil
	})
	return server.Addr(), result, server.Close
}

func runSMTPCaseStartTLS(t *testing.T, keyword string) (*core.Judgment, error) {
	t.Helper()
	address, result, closeServer := smtpCaseStartTLSServer(t, keyword)
	defer closeServer()
	judgment, _ := NewSMTPChecker().Judge(context.Background(), &core.Soul{ID: "smtp-case-proof", Name: "SMTP case proof", Type: core.CheckSMTP, Target: address, Timeout: core.Duration{Duration: 3 * time.Second}, SMTP: &core.SMTPConfig{StartTLS: true, InsecureSkipVerify: true}})
	select {
	case err := <-result:
		return judgment, err
	case <-time.After(2 * time.Second):
		return judgment, fmt.Errorf("server did not finish")
	}
}

func TestSMTPStartTLSAdvertisementIsCaseInsensitive(t *testing.T) {
	publishValidatorForTest(t, permissiveSSRFValidator())
	control, controlErr := runSMTPCaseStartTLS(t, "STARTTLS")
	if controlErr != nil || control.Status != core.SoulAlive {
		t.Fatalf("uppercase control failed: status=%s message=%q server=%v", control.Status, control.Message, controlErr)
	}
	judgment, serverErr := runSMTPCaseStartTLS(t, "starttls")
	if judgment.Status != core.SoulAlive {
		t.Fatalf("FAIL: lowercase STARTTLS advertisement was refused: status=%s message=%q server=%v", judgment.Status, judgment.Message, serverErr)
	}
	if serverErr != nil {
		t.Fatalf("server: %v", serverErr)
	}
	t.Log("PASS: SMTP extension keywords are matched case-insensitively")
}

func TestSMTPCapabilityTokensAreCaseInsensitive(t *testing.T) {
	mechanisms, found := smtpCapabilityValues([]string{"250-auth login plain"}, "AUTH")
	if !found || len(mechanisms) != 2 || !strings.EqualFold(mechanisms[0], "LOGIN") || !strings.EqualFold(mechanisms[1], "PLAIN") {
		t.Fatalf("lowercase AUTH capability = found %v values %v", found, mechanisms)
	}
	if _, found := smtpCapabilityValues([]string{"250 XSTARTTLS"}, "STARTTLS"); found {
		t.Fatal("substring lookalike XSTARTTLS was accepted as STARTTLS")
	}
}

// TestSMTPCapabilityEqualsFormIsRecognized covers the Exim/sendmail EHLO form
// "250-AUTH=PLAIN" / "250-AUTH=LOGIN", where the mechanism is attached to the
// capability keyword with '='. Matching the keyword exactly ("AUTH" !=
// "AUTH=PLAIN") made hasAuth false and produced a false outage.
func TestSMTPCapabilityEqualsFormIsRecognized(t *testing.T) {
	for _, tc := range []struct {
		name       string
		capability string
		mechanism  string
	}{
		{name: "AUTH=PLAIN", capability: "250-AUTH=PLAIN", mechanism: "PLAIN"},
		{name: "AUTH=LOGIN", capability: "250-AUTH=LOGIN", mechanism: "LOGIN"},
		{name: "lowercase auth=plain", capability: "250-auth=plain", mechanism: "PLAIN"},
		{name: "AUTH=PLAIN with extra mechanism", capability: "250-AUTH=PLAIN LOGIN", mechanism: "PLAIN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mechanisms, found := smtpCapabilityValues([]string{tc.capability}, "AUTH")
			if !found {
				t.Fatalf("%q was not recognised as the AUTH capability", tc.capability)
			}
			matched := false
			for _, mechanism := range mechanisms {
				if strings.EqualFold(mechanism, tc.mechanism) {
					matched = true
					break
				}
			}
			if !matched {
				t.Fatalf("%q did not expose the %s mechanism, got %v", tc.capability, tc.mechanism, mechanisms)
			}
		})
	}

	// Controls: the RFC list form keeps working and a lookalike keyword is
	// still rejected.
	mechanisms, found := smtpCapabilityValues([]string{"250-AUTH LOGIN PLAIN"}, "AUTH")
	if !found || len(mechanisms) != 2 {
		t.Fatalf("control: RFC '250-AUTH LOGIN PLAIN' = found %v values %v", found, mechanisms)
	}
	if _, found := smtpCapabilityValues([]string{"250 XSTARTTLS"}, "STARTTLS"); found {
		t.Fatal("control: substring lookalike XSTARTTLS was accepted as STARTTLS")
	}
	if _, found := smtpCapabilityValues([]string{"250-AUTHX=PLAIN"}, "AUTH"); found {
		t.Fatal("control: keyword lookalike AUTHX=PLAIN was accepted as AUTH")
	}
}

// smtpEqualsFormAuthServer serves a STARTTLS session whose TLS EHLO response
// advertises the AUTH extension in the "250-AUTH=<mechanism>" form, then
// completes an AUTH PLAIN exchange.
func smtpEqualsFormAuthServer(t *testing.T, mechanism string) (string, <-chan error, func()) {
	t.Helper()
	cert := loadTestCert(t)
	result := make(chan error, 1)
	server := newMockSMTPServer(t, func(conn net.Conn) {
		defer conn.Close()
		reader := bufio.NewReader(conn)
		if _, err := io.WriteString(conn, "220 proof.example ESMTP ready\r\n"); err != nil {
			result <- err
			return
		}
		if line, err := reader.ReadString('\n'); err != nil || !strings.HasPrefix(line, "EHLO ") {
			result <- fmt.Errorf("EHLO: line=%q err=%v", line, err)
			return
		}
		if _, err := io.WriteString(conn, "250-proof.example\r\n250 STARTTLS\r\n"); err != nil {
			result <- err
			return
		}
		if line, err := reader.ReadString('\n'); err != nil || strings.TrimSpace(line) != "STARTTLS" {
			result <- fmt.Errorf("STARTTLS: line=%q err=%v", line, err)
			return
		}
		if _, err := io.WriteString(conn, "220 ready for TLS\r\n"); err != nil {
			result <- err
			return
		}
		tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
		if err := tlsConn.Handshake(); err != nil {
			result <- fmt.Errorf("handshake: %w", err)
			return
		}
		reader = bufio.NewReader(tlsConn)
		if line, err := reader.ReadString('\n'); err != nil || !strings.HasPrefix(line, "EHLO ") {
			result <- fmt.Errorf("TLS EHLO: line=%q err=%v", line, err)
			return
		}
		if _, err := fmt.Fprintf(tlsConn, "250-proof.example\r\n250-AUTH=%s\r\n250 OK\r\n", mechanism); err != nil {
			result <- err
			return
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			result <- fmt.Errorf("AUTH: %w", err)
			return
		}
		command := strings.TrimSpace(line)
		if !strings.HasPrefix(command, "AUTH PLAIN ") {
			result <- fmt.Errorf("AUTH: unexpected command %q", command)
			return
		}
		payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(command, "AUTH PLAIN "))
		if err != nil {
			result <- fmt.Errorf("AUTH decode: %w", err)
			return
		}
		if string(payload) != "\x00user\x00pass" {
			result <- fmt.Errorf("AUTH payload: %q", payload)
			return
		}
		if _, err := io.WriteString(tlsConn, "235 2.7.0 Authentication successful\r\n"); err != nil {
			result <- err
			return
		}
		result <- nil
	})
	return server.Addr(), result, server.Close
}

// TestSMTPAuthEqualsFormAdvertisesAndAuthenticates is the end-to-end
// regression: a server that advertises "250-AUTH=PLAIN" must be recognised as
// offering AUTH, and the probe must authenticate instead of reporting
// "AUTH requested but not advertised".
func TestSMTPAuthEqualsFormAdvertisesAndAuthenticates(t *testing.T) {
	publishValidatorForTest(t, permissiveSSRFValidator())

	address, result, closeServer := smtpEqualsFormAuthServer(t, "PLAIN")
	defer closeServer()

	judgment, _ := NewSMTPChecker().Judge(context.Background(), &core.Soul{
		ID:      "smtp-equals-form-proof",
		Name:    "SMTP equals form proof",
		Type:    core.CheckSMTP,
		Target:  address,
		Timeout: core.Duration{Duration: 5 * time.Second},
		SMTP: &core.SMTPConfig{
			StartTLS:           true,
			InsecureSkipVerify: true,
			Auth:               &core.AuthCreds{Username: "user", Password: "pass"},
		},
	})

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("server error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not finish")
	}

	if judgment.Status != core.SoulAlive {
		t.Fatalf("AUTH=PLAIN advertisement was refused: status=%s message=%q", judgment.Status, judgment.Message)
	}
	authenticated := false
	for _, assertion := range judgment.Details.Assertions {
		if assertion.Type == "auth_plain" && assertion.Passed {
			authenticated = true
		}
	}
	if !authenticated {
		t.Fatalf("expected a passing auth_plain assertion, got %v", judgment.Details.Assertions)
	}
}
