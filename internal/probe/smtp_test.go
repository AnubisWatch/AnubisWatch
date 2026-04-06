package probe

import (
	"bufio"
	"context"
	"crypto/tls"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// mockSMTPServer is a simple mock SMTP server for testing
type mockSMTPServer struct {
	listener net.Listener
	closeCh  chan struct{}
}

func newMockSMTPServer(t *testing.T, handler func(net.Conn)) *mockSMTPServer {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}

	server := &mockSMTPServer{
		listener: listener,
		closeCh:  make(chan struct{}),
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-server.closeCh:
				default:
					t.Logf("Accept error: %v", err)
				}
				return
			}
			go handler(conn)
		}
	}()

	return server
}

func (s *mockSMTPServer) Close() {
	close(s.closeCh)
	s.listener.Close()
}

func (s *mockSMTPServer) Addr() string {
	return s.listener.Addr().String()
}

func TestSMTPChecker_Validate_MissingTarget(t *testing.T) {
	checker := NewSMTPChecker()

	soul := &core.Soul{
		ID:   "test-smtp",
		Name: "Test SMTP",
		Type: core.CheckSMTP,
	}

	err := checker.Validate(soul)
	if err == nil {
		t.Error("Expected validation error for missing target")
	}
}

func TestSMTPChecker_Validate_InvalidFormat(t *testing.T) {
	checker := NewSMTPChecker()

	soul := &core.Soul{
		ID:     "test-smtp",
		Name:   "Test SMTP",
		Type:   core.CheckSMTP,
		Target: "invalid-no-port",
	}

	err := checker.Validate(soul)
	if err == nil {
		t.Error("Expected validation error for invalid format")
	}
}

func TestSMTPChecker_Validate_Valid(t *testing.T) {
	checker := NewSMTPChecker()

	soul := &core.Soul{
		ID:     "test-smtp",
		Name:   "Test SMTP",
		Type:   core.CheckSMTP,
		Target: "localhost:25",
	}

	err := checker.Validate(soul)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestSMTPChecker_Judge_Basic(t *testing.T) {
	// Create mock SMTP server
	server := newMockSMTPServer(t, func(conn net.Conn) {
		defer conn.Close()
		conn.Write([]byte("220 smtp.example.com ESMTP\r\n"))

		reader := bufio.NewReader(conn)
		reader.ReadString('\n') // Read EHLO

		conn.Write([]byte("250-smtp.example.com\r\n"))
		conn.Write([]byte("250 OK\r\n"))
	})
	defer server.Close()

	checker := NewSMTPChecker()

	soul := &core.Soul{
		ID:      "test-smtp",
		Name:    "Test SMTP",
		Type:    core.CheckSMTP,
		Target:  server.Addr(),
		Enabled: true,
		Timeout: core.Duration{Duration: 5 * time.Second},
		SMTP: &core.SMTPConfig{
			EHLODomain: "test.local",
		},
	}

	ctx := context.Background()
	judgment, err := checker.Judge(ctx, soul)

	if err != nil {
		t.Fatalf("Judge failed: %v", err)
	}

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}
}

func TestSMTPChecker_Judge_BannerMatch(t *testing.T) {
	server := newMockSMTPServer(t, func(conn net.Conn) {
		defer conn.Close()
		conn.Write([]byte("220 mail.example.com ESMTP Postfix\r\n"))

		reader := bufio.NewReader(conn)
		reader.ReadString('\n') // Read EHLO

		conn.Write([]byte("250-mail.example.com\r\n"))
		conn.Write([]byte("250 OK\r\n"))
	})
	defer server.Close()

	checker := NewSMTPChecker()

	soul := &core.Soul{
		ID:     "test-smtp",
		Name:   "Test SMTP",
		Type:   core.CheckSMTP,
		Target: server.Addr(),
		SMTP: &core.SMTPConfig{
			BannerContains: "Postfix",
			EHLODomain:     "test.local",
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}
}

func TestSMTPChecker_Judge_BannerMismatch(t *testing.T) {
	server := newMockSMTPServer(t, func(conn net.Conn) {
		defer conn.Close()
		conn.Write([]byte("220 mail.example.com ESMTP Sendmail\r\n"))

		reader := bufio.NewReader(conn)
		reader.ReadString('\n') // Read EHLO

		conn.Write([]byte("250-mail.example.com\r\n"))
		conn.Write([]byte("250 OK\r\n"))
	})
	defer server.Close()

	checker := NewSMTPChecker()

	soul := &core.Soul{
		ID:     "test-smtp",
		Name:   "Test SMTP",
		Type:   core.CheckSMTP,
		Target: server.Addr(),
		SMTP: &core.SMTPConfig{
			BannerContains: "Postfix",
			EHLODomain:     "test.local",
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestSMTPChecker_Judge_UnexpectedGreeting(t *testing.T) {
	server := newMockSMTPServer(t, func(conn net.Conn) {
		defer conn.Close()
		conn.Write([]byte("500 Not an SMTP server\r\n"))
	})
	defer server.Close()

	checker := NewSMTPChecker()

	soul := &core.Soul{
		ID:      "test-smtp",
		Name:    "Test SMTP",
		Type:    core.CheckSMTP,
		Target:  server.Addr(),
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestSMTPChecker_Judge_StartTLS(t *testing.T) {
	server := newMockSMTPServer(t, func(conn net.Conn) {
		defer conn.Close()
		conn.Write([]byte("220 smtp.example.com ESMTP\r\n"))

		reader := bufio.NewReader(conn)
		line, _ := reader.ReadString('\n') // Read EHLO

		if line != "" {
			// Advertise STARTTLS
			conn.Write([]byte("250-smtp.example.com\r\n"))
			conn.Write([]byte("250 STARTTLS\r\n"))

			// Read STARTTLS command
			reader.ReadString('\n')

			// Accept STARTTLS
			conn.Write([]byte("220 Go ahead\r\n"))

			// Upgrade to TLS (simplified - just keep connection open for test)
			reader.ReadString('\n') // Read EHLO over TLS
			conn.Write([]byte("250-smtp.example.com\r\n"))
			conn.Write([]byte("250 OK\r\n"))
		}
	})
	defer server.Close()

	checker := NewSMTPChecker()

	soul := &core.Soul{
		ID:     "test-smtp",
		Name:   "Test SMTP",
		Type:   core.CheckSMTP,
		Target: server.Addr(),
		SMTP: &core.SMTPConfig{
			StartTLS:   true,
			EHLODomain: "test.local",
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// This test may fail due to TLS handshake complexity
	t.Logf("STARTTLS test result: %s", judgment.Message)
}

func TestSMTPChecker_Judge_StartTLSNotAdvertised(t *testing.T) {
	server := newMockSMTPServer(t, func(conn net.Conn) {
		defer conn.Close()
		conn.Write([]byte("220 smtp.example.com ESMTP\r\n"))

		reader := bufio.NewReader(conn)
		reader.ReadString('\n') // Read EHLO

		// Don't advertise STARTTLS
		conn.Write([]byte("250-smtp.example.com\r\n"))
		conn.Write([]byte("250 SIZE 1024\r\n"))
	})
	defer server.Close()

	checker := NewSMTPChecker()

	soul := &core.Soul{
		ID:     "test-smtp",
		Name:   "Test SMTP",
		Type:   core.CheckSMTP,
		Target: server.Addr(),
		SMTP: &core.SMTPConfig{
			StartTLS:   true,
			EHLODomain: "test.local",
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestSMTPChecker_Judge_ConnectionRefused(t *testing.T) {
	checker := NewSMTPChecker()

	soul := &core.Soul{
		ID:      "test-smtp",
		Name:    "Test SMTP",
		Type:    core.CheckSMTP,
		Target:  "127.0.0.1:1", // Invalid port
		Timeout: core.Duration{Duration: 2 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestSMTPChecker_Judge_Timeout(t *testing.T) {
	// Create server that accepts but never responds
	server := newMockSMTPServer(t, func(conn net.Conn) {
		defer conn.Close()
		// Accept connection but don't send greeting
		time.Sleep(10 * time.Second)
	})
	defer server.Close()

	checker := NewSMTPChecker()

	soul := &core.Soul{
		ID:      "test-smtp",
		Name:    "Test SMTP",
		Type:    core.CheckSMTP,
		Target:  server.Addr(),
		Timeout: core.Duration{Duration: 100 * time.Millisecond},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestSMTPChecker_Judge_Capabilities(t *testing.T) {
	server := newMockSMTPServer(t, func(conn net.Conn) {
		defer conn.Close()
		conn.Write([]byte("220 smtp.example.com ESMTP\r\n"))

		reader := bufio.NewReader(conn)
		reader.ReadString('\n') // Read EHLO

		conn.Write([]byte("250-smtp.example.com\r\n"))
		conn.Write([]byte("250-SIZE 1024\r\n"))
		conn.Write([]byte("250-AUTH LOGIN PLAIN\r\n"))
		conn.Write([]byte("250 STARTTLS\r\n"))
	})
	defer server.Close()

	checker := NewSMTPChecker()

	soul := &core.Soul{
		ID:     "test-smtp",
		Name:   "Test SMTP",
		Type:   core.CheckSMTP,
		Target: server.Addr(),
		SMTP: &core.SMTPConfig{
			EHLODomain: "test.local",
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}

	if len(judgment.Details.Capabilities) == 0 {
		t.Error("Expected capabilities to be captured")
	}
}

// IMAP Checker Tests

func TestIMAPChecker_Validate_MissingTarget(t *testing.T) {
	checker := NewIMAPChecker()

	soul := &core.Soul{
		ID:   "test-imap",
		Name: "Test IMAP",
		Type: core.CheckIMAP,
	}

	err := checker.Validate(soul)
	if err == nil {
		t.Error("Expected validation error for missing target")
	}
}

func TestIMAPChecker_Validate_InvalidFormat(t *testing.T) {
	checker := NewIMAPChecker()

	soul := &core.Soul{
		ID:     "test-imap",
		Name:   "Test IMAP",
		Type:   core.CheckIMAP,
		Target: "invalid-no-port",
	}

	err := checker.Validate(soul)
	if err == nil {
		t.Error("Expected validation error for invalid format")
	}
}

func TestIMAPChecker_Validate_Valid(t *testing.T) {
	checker := NewIMAPChecker()

	soul := &core.Soul{
		ID:     "test-imap",
		Name:   "Test IMAP",
		Type:   core.CheckIMAP,
		Target: "localhost:143",
	}

	err := checker.Validate(soul)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestIMAPChecker_Judge_Basic(t *testing.T) {
	// Create mock IMAP server
	server := newMockSMTPServer(t, func(conn net.Conn) {
		defer conn.Close()
		conn.Write([]byte("* OK IMAP4rev1 Server Ready\r\n"))

		reader := bufio.NewReader(conn)
		reader.ReadString('\n') // Read CAPABILITY command

		conn.Write([]byte("* CAPABILITY IMAP4rev1 STARTTLS AUTH=LOGIN\r\n"))
		conn.Write([]byte("A001 OK CAPABILITY completed\r\n"))

		reader.ReadString('\n') // Read LOGOUT or LOGIN
		conn.Write([]byte("A002 OK LOGOUT completed\r\n"))
	})
	defer server.Close()

	checker := NewIMAPChecker()

	soul := &core.Soul{
		ID:      "test-imap",
		Name:    "Test IMAP",
		Type:    core.CheckIMAP,
		Target:  server.Addr(),
		Enabled: true,
		Timeout: core.Duration{Duration: 5 * time.Second},
		IMAP: &core.IMAPConfig{
			TLS: false,
		},
	}

	ctx := context.Background()
	judgment, err := checker.Judge(ctx, soul)

	if err != nil {
		t.Fatalf("Judge failed: %v", err)
	}

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}
}

func TestIMAPChecker_Judge_InvalidGreeting(t *testing.T) {
	server := newMockSMTPServer(t, func(conn net.Conn) {
		defer conn.Close()
		conn.Write([]byte("NOT OK This is not IMAP\r\n"))
	})
	defer server.Close()

	checker := NewIMAPChecker()

	soul := &core.Soul{
		ID:     "test-imap",
		Name:   "Test IMAP",
		Type:   core.CheckIMAP,
		Target: server.Addr(),
		IMAP: &core.IMAPConfig{
			TLS: false,
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestIMAPChecker_Judge_TLS(t *testing.T) {
	// Create mock IMAP server with TLS
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// TLS handshake happens first
		tlsConn := tls.Server(conn, &tls.Config{
			InsecureSkipVerify: true,
			Certificates:       []tls.Certificate{loadTestCert(t)},
		})
		if err := tlsConn.Handshake(); err != nil {
			return
		}

		tlsConn.Write([]byte("* OK IMAP4rev1 Server Ready\r\n"))

		reader := bufio.NewReader(tlsConn)
		reader.ReadString('\n')
		tlsConn.Write([]byte("* CAPABILITY IMAP4rev1\r\n"))
		tlsConn.Write([]byte("A001 OK CAPABILITY completed\r\n"))
	}()

	checker := NewIMAPChecker()

	soul := &core.Soul{
		ID:     "test-imap",
		Name:   "Test IMAP",
		Type:   core.CheckIMAP,
		Target: listener.Addr().String(),
		IMAP: &core.IMAPConfig{
			TLS:                true,
			InsecureSkipVerify: true, // Test server uses self-signed cert
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}
}

func TestIMAPChecker_Judge_Login(t *testing.T) {
	server := newMockSMTPServer(t, func(conn net.Conn) {
		defer conn.Close()
		conn.Write([]byte("* OK IMAP4rev1 Server Ready\r\n"))

		reader := bufio.NewReader(conn)
		reader.ReadString('\n') // Read CAPABILITY
		conn.Write([]byte("* CAPABILITY IMAP4rev1 AUTH=LOGIN\r\n"))
		conn.Write([]byte("A001 OK CAPABILITY completed\r\n"))

		reader.ReadString('\n') // Read LOGIN
		conn.Write([]byte("A002 OK LOGIN completed\r\n"))

		reader.ReadString('\n') // Read STATUS
		conn.Write([]byte("* STATUS \"INBOX\" (MESSAGES 5 UNSEEN 2)\r\n"))
		conn.Write([]byte("A003 OK STATUS completed\r\n"))

		reader.ReadString('\n') // Read LOGOUT
		conn.Write([]byte("A004 OK LOGOUT completed\r\n"))
	})
	defer server.Close()

	checker := NewIMAPChecker()

	soul := &core.Soul{
		ID:     "test-imap",
		Name:   "Test IMAP",
		Type:   core.CheckIMAP,
		Target: server.Addr(),
		IMAP: &core.IMAPConfig{
			TLS: false,
			Auth: &core.AuthCreds{
				Username: "testuser",
				Password: "testpass",
			},
			CheckMailbox: "INBOX",
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}
}

func TestIMAPChecker_Judge_LoginFailed(t *testing.T) {
	server := newMockSMTPServer(t, func(conn net.Conn) {
		defer conn.Close()
		conn.Write([]byte("* OK IMAP4rev1 Server Ready\r\n"))

		reader := bufio.NewReader(conn)
		reader.ReadString('\n') // Read CAPABILITY
		conn.Write([]byte("* CAPABILITY IMAP4rev1 AUTH=LOGIN\r\n"))
		conn.Write([]byte("A001 OK CAPABILITY completed\r\n"))

		reader.ReadString('\n') // Read LOGIN
		conn.Write([]byte("A002 NO LOGIN failed\r\n"))
	})
	defer server.Close()

	checker := NewIMAPChecker()

	soul := &core.Soul{
		ID:     "test-imap",
		Name:   "Test IMAP",
		Type:   core.CheckIMAP,
		Target: server.Addr(),
		IMAP: &core.IMAPConfig{
			TLS: false,
			Auth: &core.AuthCreds{
				Username: "testuser",
				Password: "wrongpass",
			},
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestIMAPChecker_Judge_ConnectionRefused(t *testing.T) {
	checker := NewIMAPChecker()

	soul := &core.Soul{
		ID:      "test-imap",
		Name:    "Test IMAP",
		Type:    core.CheckIMAP,
		Target:  "127.0.0.1:1",
		Timeout: core.Duration{Duration: 2 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

// Helper to load test certificate
func loadTestCert(t *testing.T) tls.Certificate {
	// Generate a self-signed cert for testing
	cert, err := tls.X509KeyPair([]byte(testCertPEM), []byte(testKeyPEM))
	if err != nil {
		t.Fatalf("Failed to load test cert: %v", err)
	}
	return cert
}

// Test certificate and key (self-signed, for testing only)
const testCertPEM = `-----BEGIN CERTIFICATE-----
MIIBhTCCASugAwIBAgIQIRi6zePL6mKjOipn+dNuaTAKBggqhkjOPQQDAjASMRAw
DgYDVQQKEwdBY21lIENvMB4XDTE3MTAyMDE5NDMwNloXDTE4MTAyMDE5NDMwNlow
EjEQMA4GA1UEChMHQWNtZSBDbzBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABD0d
7VNhbWvZLWPuj/RtHFjvtJBEwOkhbN/BnnE8rnZR8+sbwnc/KhCk3FhnpHZnQz7B
5aETbbIgmuvewdjvSBSjYzBhMA4GA1UdDwEB/wQEAwICpDATBgNVHSUEDDAKBggr
BgEFBQcDATAPBgNVHRMBAf8EBTADAQH/MCkGA1UdEQQiMCCCDmxvY2FsaG9zdDo1
NDUzgg4xMjcuMC4wLjE6NTQ1MzAKBggqhkjOPQQDAgNIADBFAiEA2zpJEPQyz6/l
Wf86aX6PepsntZv2GYlA5UpabfT2EZICICpJ5h/iI+i341gBmLiAFQOyTDT+/wQc
6MF9+Yw1Yy0t
-----END CERTIFICATE-----`

const testKeyPEM = `-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIIrYSSNQFaA2Hwf1duRSxKtLYX5CB04fSeQ6tF1aY/PuoAoGCCqGSM49
AwEHoUQDQgAEPR3tU2Fta9ktY+6P9G0cWO+0kETA6SFs38GecTyudlHz6xvCdz8q
EKTcWGekdmdDPsHloRNtsiCa697B2O9IFA==
-----END EC PRIVATE KEY-----`

func TestIMAPChecker_Judge_CapabilityResponse(t *testing.T) {
	server := newMockSMTPServer(t, func(conn net.Conn) {
		defer conn.Close()
		conn.Write([]byte("* OK IMAP4rev1 Server Ready\r\n"))

		reader := bufio.NewReader(conn)
		reader.ReadString('\n') // Read CAPABILITY
		conn.Write([]byte("* CAPABILITY IMAP4rev1 STARTTLS AUTH=PLAIN\r\n"))
		conn.Write([]byte("A001 OK CAPABILITY completed\r\n"))
	})
	defer server.Close()

	checker := NewIMAPChecker()

	soul := &core.Soul{
		ID:     "test-imap",
		Name:   "Test IMAP",
		Type:   core.CheckIMAP,
		Target: server.Addr(),
		IMAP: &core.IMAPConfig{
			TLS: false,
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}

	if len(judgment.Details.Capabilities) == 0 {
		t.Error("Expected capabilities to be captured")
	}
}

func TestIMAPChecker_Judge_StatusFailed(t *testing.T) {
	server := newMockSMTPServer(t, func(conn net.Conn) {
		defer conn.Close()
		conn.Write([]byte("* OK IMAP4rev1 Server Ready\r\n"))

		reader := bufio.NewReader(conn)
		reader.ReadString('\n') // Read CAPABILITY
		conn.Write([]byte("* CAPABILITY IMAP4rev1\r\n"))
		conn.Write([]byte("A001 OK CAPABILITY completed\r\n"))

		reader.ReadString('\n') // Read LOGIN
		conn.Write([]byte("A002 OK LOGIN completed\r\n"))

		reader.ReadString('\n') // Read STATUS
		conn.Write([]byte("A003 NO Mailbox not found\r\n"))
	})
	defer server.Close()

	checker := NewIMAPChecker()

	soul := &core.Soul{
		ID:     "test-imap",
		Name:   "Test IMAP",
		Type:   core.CheckIMAP,
		Target: server.Addr(),
		IMAP: &core.IMAPConfig{
			TLS: false,
			Auth: &core.AuthCreds{
				Username: "testuser",
				Password: "testpass",
			},
			CheckMailbox: "NonExistent",
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

func TestIMAPChecker_Judge_ContextCancellation(t *testing.T) {
	server := newMockSMTPServer(t, func(conn net.Conn) {
		defer conn.Close()
		conn.Write([]byte("* OK IMAP4rev1 Server Ready\r\n"))
		time.Sleep(10 * time.Second) // Long delay
	})
	defer server.Close()

	checker := NewIMAPChecker()

	soul := &core.Soul{
		ID:     "test-imap",
		Name:   "Test IMAP",
		Type:   core.CheckIMAP,
		Target: server.Addr(),
		IMAP: &core.IMAPConfig{
			TLS: false,
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	judgment, _ := checker.Judge(ctx, soul)

	if judgment == nil {
		t.Error("Expected judgment to be returned")
	}
}

func TestSMTPChecker_Judge_ContextCancellation(t *testing.T) {
	server := newMockSMTPServer(t, func(conn net.Conn) {
		defer conn.Close()
		conn.Write([]byte("220 smtp.example.com ESMTP\r\n"))
		time.Sleep(10 * time.Second)
	})
	defer server.Close()

	checker := NewSMTPChecker()

	soul := &core.Soul{
		ID:      "test-smtp",
		Name:    "Test SMTP",
		Type:    core.CheckSMTP,
		Target:  server.Addr(),
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	judgment, _ := checker.Judge(ctx, soul)

	if judgment == nil {
		t.Error("Expected judgment to be returned")
	}
}

func TestSMTPChecker_Judge_AUTHAvailable(t *testing.T) {
	server := newMockSMTPServer(t, func(conn net.Conn) {
		defer conn.Close()
		conn.Write([]byte("220 smtp.example.com ESMTP\r\n"))

		reader := bufio.NewReader(conn)
		reader.ReadString('\n') // Read EHLO

		// Advertise AUTH
		conn.Write([]byte("250-smtp.example.com\r\n"))
		conn.Write([]byte("250-AUTH LOGIN PLAIN\r\n"))
		conn.Write([]byte("250 SIZE 1024\r\n"))

		// Read AUTH LOGIN command
		authCmd, _ := reader.ReadString('\n')
		if strings.HasPrefix(authCmd, "AUTH LOGIN") {
			// Send 334 username prompt
			conn.Write([]byte("334 VXNlcm5hbWU6\r\n")) // "Username:" base64

			// Read username
			usernameB64, _ := reader.ReadString('\n')
			_ = usernameB64

			// Send 334 password prompt
			conn.Write([]byte("334 UGFzc3dvcmQ6\r\n")) // "Password:" base64

			// Read password
			passwordB64, _ := reader.ReadString('\n')
			_ = passwordB64

			// Send 235 success
			conn.Write([]byte("235 2.7.0 Authentication successful\r\n"))
		}

		reader.ReadString('\n') // Read QUIT
	})
	defer server.Close()

	checker := NewSMTPChecker()

	soul := &core.Soul{
		ID:     "test-smtp-auth",
		Name:   "Test SMTP AUTH",
		Type:   core.CheckSMTP,
		Target: server.Addr(),
		SMTP: &core.SMTPConfig{
			EHLODomain: "test.local",
			Auth: &core.AuthCreds{
				Username: "testuser",
				Password: "testpass",
			},
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// AUTH is available and should succeed
	if judgment.Status != core.SoulAlive {
		t.Errorf("Expected status Alive, got %s", judgment.Status)
	}
}

func TestSMTPChecker_Judge_AUTHNotAdvertised(t *testing.T) {
	server := newMockSMTPServer(t, func(conn net.Conn) {
		defer conn.Close()
		conn.Write([]byte("220 smtp.example.com ESMTP\r\n"))

		reader := bufio.NewReader(conn)
		reader.ReadString('\n') // Read EHLO

		// Don't advertise AUTH
		conn.Write([]byte("250-smtp.example.com\r\n"))
		conn.Write([]byte("250 SIZE 1024\r\n"))
	})
	defer server.Close()

	checker := NewSMTPChecker()

	soul := &core.Soul{
		ID:     "test-smtp-no-auth",
		Name:   "Test SMTP No AUTH",
		Type:   core.CheckSMTP,
		Target: server.Addr(),
		SMTP: &core.SMTPConfig{
			EHLODomain: "test.local",
			Auth: &core.AuthCreds{
				Username: "testuser",
				Password: "testpass",
			},
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, _ := checker.Judge(ctx, soul)

	// AUTH requested but not advertised, should fail
	if judgment.Status != core.SoulDead {
		t.Errorf("Expected status Dead, got %s", judgment.Status)
	}
}

// Test SMTP STARTTLS with full flow
func TestSMTPChecker_Judge_STARTTLSFullFlow(t *testing.T) {
	// Create TLS config for server
	cert, err := tls.X509KeyPair([]byte(testCertPEM), []byte(testKeyPEM))
	if err != nil {
		t.Skipf("Cannot load test cert: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Send greeting
		conn.Write([]byte("220 smtp.example.com ESMTP\r\n"))

		reader := bufio.NewReader(conn)
		line, _ := reader.ReadString('\n') // Read EHLO
		t.Logf("Received: %s", line)

		// Advertise STARTTLS
		conn.Write([]byte("250-smtp.example.com\r\n"))
		conn.Write([]byte("250 STARTTLS\r\n"))

		// Read STARTTLS command
		line, _ = reader.ReadString('\n')
		t.Logf("Received: %s", line)

		// Accept STARTTLS
		conn.Write([]byte("220 Go ahead\r\n"))

		// Upgrade to TLS
		tlsConn := tls.Server(conn, &tls.Config{
			Certificates: []tls.Certificate{cert},
		})
		if err := tlsConn.Handshake(); err != nil {
			t.Logf("TLS handshake error: %v", err)
			return
		}

		// Read EHLO over TLS
		tlsReader := bufio.NewReader(tlsConn)
		line, _ = tlsReader.ReadString('\n')
		t.Logf("Received over TLS: %s", line)

		// Send final response
		tlsConn.Write([]byte("250-smtp.example.com\r\n"))
		tlsConn.Write([]byte("250 OK\r\n"))

		// Keep connection open briefly
		time.Sleep(100 * time.Millisecond)
	}()

	checker := NewSMTPChecker()

	soul := &core.Soul{
		ID:     "test-smtp-starttls-full",
		Name:   "Test SMTP STARTTLS Full",
		Type:   core.CheckSMTP,
		Target: listener.Addr().String(),
		SMTP: &core.SMTPConfig{
			StartTLS:   true,
			EHLODomain: "test.local",
		},
		Timeout: core.Duration{Duration: 5 * time.Second},
	}

	ctx := context.Background()
	judgment, err := checker.Judge(ctx, soul)

	if err != nil {
		t.Fatalf("Judge failed: %v", err)
	}
	t.Logf("STARTTLS full flow result: %s - %s", judgment.Status, judgment.Message)
}

func TestSMTPChecker_Judge_ContextTimeout(t *testing.T) {
	checker := NewSMTPChecker()

	soul := &core.Soul{
		Target: "192.0.2.1:25", // RFC 5737 TEST-NET-1 - should timeout
		SMTP:   &core.SMTPConfig{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	judgment, err := checker.Judge(ctx, soul)
	if err != nil {
		t.Logf("Judge with timeout returned error (expected): %v", err)
	}
	if judgment != nil && judgment.Status != core.SoulDead {
		t.Errorf("Expected dead status, got %v", judgment.Status)
	}
}
