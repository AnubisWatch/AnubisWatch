package probe

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// SMTPChecker implements SMTP health checks
type SMTPChecker struct{}

// NewSMTPChecker creates a new SMTP checker
func NewSMTPChecker() *SMTPChecker {
	return &SMTPChecker{}
}

// Type returns the protocol identifier
func (c *SMTPChecker) Type() core.CheckType {
	return core.CheckSMTP
}

// Validate checks configuration
func (c *SMTPChecker) Validate(soul *core.Soul) error {
	if soul.Target == "" {
		return configError("target", "target host:port is required")
	}
	if _, _, err := net.SplitHostPort(soul.Target); err != nil {
		return configError("target", "target must be in host:port format")
	}
	return nil
}

// Judge performs the SMTP check
func (c *SMTPChecker) Judge(ctx context.Context, soul *core.Soul) (*core.Judgment, error) {
	cfg := soul.SMTP
	if cfg == nil {
		cfg = &core.SMTPConfig{}
	}

	timeout := soul.Timeout.Duration
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	// Connect to SMTP server
	start := time.Now()
	conn, err := net.DialTimeout("tcp", soul.Target, timeout)
	if err != nil {
		return failJudgment(soul, fmt.Errorf("SMTP connection failed: %w", err)), nil
	}
	defer conn.Close()

	// Set deadlines
	conn.SetDeadline(time.Now().Add(timeout))

	reader := bufio.NewReader(conn)
	textReader := textproto.NewReader(reader)

	judgment := &core.Judgment{
		ID:         core.GenerateID(),
		SoulID:     soul.ID,
		Timestamp:  time.Now().UTC(),
		StatusCode: 0,
		Details:    &core.JudgmentDetails{},
	}

	// Read greeting
	line, err := textReader.ReadLine()
	if err != nil {
		return failJudgment(soul, fmt.Errorf("failed to read SMTP greeting: %w", err)), nil
	}

	if !strings.HasPrefix(line, "220") {
		return failJudgment(soul, fmt.Errorf("unexpected SMTP greeting: %s", line)), nil
	}

	// Check banner
	if cfg.BannerContains != "" {
		matched := strings.Contains(strings.ToLower(line), strings.ToLower(cfg.BannerContains))
		judgment.Details.Assertions = append(judgment.Details.Assertions, core.AssertionResult{
			Type:     "banner_match",
			Expected: cfg.BannerContains,
			Actual:   line,
			Passed:   matched,
		})
		if !matched {
			judgment.Status = core.SoulDead
			judgment.Message = fmt.Sprintf("SMTP banner mismatch: expected '%s'", cfg.BannerContains)
			return judgment, nil
		}
	}

	// EHLO/HELO
	ehloDomain := cfg.EHLODomain
	if ehloDomain == "" {
		ehloDomain = "anubiswatch.local"
	}

	fmt.Fprintf(conn, "EHLO %s\r\n", ehloDomain)

	// Read EHLO response (multiline)
	var capabilities []string
	for {
		line, err = textReader.ReadLine()
		if err != nil {
			return failJudgment(soul, fmt.Errorf("failed to read EHLO response: %w", err)), nil
		}
		capabilities = append(capabilities, line)
		if !strings.HasPrefix(line, "250-") {
			break
		}
	}

	if !strings.HasPrefix(line, "250 ") {
		return failJudgment(soul, fmt.Errorf("EHLO failed: %s", line)), nil
	}

	judgment.Details.Capabilities = capabilities

	// STARTTLS if requested
	if cfg.StartTLS {
		hasSTARTTLS := false
		for _, cap := range capabilities {
			if strings.Contains(cap, "STARTTLS") {
				hasSTARTTLS = true
				break
			}
		}

		if !hasSTARTTLS {
			return failJudgment(soul, fmt.Errorf("STARTTLS requested but not advertised")), nil
		}

		fmt.Fprintf(conn, "STARTTLS\r\n")
		line, err = textReader.ReadLine()
		if err != nil {
			return failJudgment(soul, fmt.Errorf("STARTTLS command failed: %w", err)), nil
		}
		if !strings.HasPrefix(line, "220") {
			return failJudgment(soul, fmt.Errorf("STARTTLS rejected: %s", line)), nil
		}

		// Upgrade to TLS
		tlsConn := tls.Client(conn, &tls.Config{
			InsecureSkipVerify: true, // TODO: Make configurable
			ServerName:         ehloDomain,
		})
		if err := tlsConn.Handshake(); err != nil {
			return failJudgment(soul, fmt.Errorf("TLS handshake failed: %w", err)), nil
		}
		conn = tlsConn

		// Extract TLS info
		state := tlsConn.ConnectionState()
		judgment.TLSInfo = &core.TLSInfo{
			Protocol:    fmt.Sprintf("TLS %d.%d", state.Version>>8&0xFF, state.Version&0xFF),
			CipherSuite: tls.CipherSuiteName(state.CipherSuite),
		}
		if len(state.PeerCertificates) > 0 {
			cert := state.PeerCertificates[0]
			judgment.TLSInfo.Issuer = cert.Issuer.CommonName
			judgment.TLSInfo.Subject = cert.Subject.CommonName
			judgment.TLSInfo.NotAfter = cert.NotAfter
			judgment.TLSInfo.DaysUntilExpiry = int(time.Until(cert.NotAfter).Hours() / 24)
		}

		// Re-create reader/writer
		reader = bufio.NewReader(conn)
		textReader = textproto.NewReader(reader)

		// Send EHLO again over TLS
		fmt.Fprintf(conn, "EHLO %s\r\n", ehloDomain)
		for {
			line, err = textReader.ReadLine()
			if err != nil {
				return failJudgment(soul, fmt.Errorf("failed to read EHLO response over TLS: %w", err)), nil
			}
			if !strings.HasPrefix(line, "250-") {
				break
			}
		}
	}

	// AUTH if credentials provided
	if cfg.Auth != nil && cfg.Auth.Username != "" {
		// Try AUTH LOGIN (simplified - just check if AUTH is supported)
		hasAuth := false
		for _, cap := range capabilities {
			if strings.Contains(cap, "AUTH") {
				hasAuth = true
				break
			}
		}

		if !hasAuth {
			return failJudgment(soul, fmt.Errorf("AUTH requested but not advertised")), nil
		}

		// For now, just verify AUTH is available (full implementation would do actual auth)
		// TODO: Implement actual AUTH LOGIN/PLAIN/CRAM-MD5
	}

	duration := time.Since(start)
	judgment.Duration = duration
	judgment.Status = core.SoulAlive
	judgment.Message = fmt.Sprintf("SMTP connection to %s successful in %s", soul.Target, duration.Round(time.Millisecond))

	return judgment, nil
}

// IMAPChecker implements IMAP health checks
type IMAPChecker struct{}

// NewIMAPChecker creates a new IMAP checker
func NewIMAPChecker() *IMAPChecker {
	return &IMAPChecker{}
}

// Type returns the protocol identifier
func (c *IMAPChecker) Type() core.CheckType {
	return core.CheckIMAP
}

// Validate checks configuration
func (c *IMAPChecker) Validate(soul *core.Soul) error {
	if soul.Target == "" {
		return configError("target", "target host:port is required")
	}
	if _, _, err := net.SplitHostPort(soul.Target); err != nil {
		return configError("target", "target must be in host:port format")
	}
	return nil
}

// Judge performs the IMAP check
func (c *IMAPChecker) Judge(ctx context.Context, soul *core.Soul) (*core.Judgment, error) {
	cfg := soul.IMAP
	if cfg == nil {
		cfg = &core.IMAPConfig{}
	}

	timeout := soul.Timeout.Duration
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	start := time.Now()
	var conn net.Conn
	var err error

	// Connect (with or without TLS)
	if cfg.TLS {
		conn, err = tls.DialWithDialer(&net.Dialer{Timeout: timeout}, "tcp", soul.Target, &tls.Config{
			InsecureSkipVerify: true,
		})
	} else {
		conn, err = net.DialTimeout("tcp", soul.Target, timeout)
	}

	if err != nil {
		return failJudgment(soul, fmt.Errorf("IMAP connection failed: %w", err)), nil
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(timeout))

	reader := bufio.NewReader(conn)

	judgment := &core.Judgment{
		ID:         core.GenerateID(),
		SoulID:     soul.ID,
		Timestamp:  time.Now().UTC(),
		StatusCode: 0,
		Details:    &core.JudgmentDetails{},
	}

	// Read greeting
	line, err := reader.ReadString('\n')
	if err != nil {
		return failJudgment(soul, fmt.Errorf("failed to read IMAP greeting: %w", err)), nil
	}
	line = strings.TrimSpace(line)

	if !strings.HasPrefix(line, "* OK") {
		return failJudgment(soul, fmt.Errorf("unexpected IMAP greeting: %s", line)), nil
	}

	// CAPABILITY command
	fmt.Fprintf(conn, "A001 CAPABILITY\r\n")

	var capabilities []string
	for {
		line, err = reader.ReadString('\n')
		if err != nil {
			return failJudgment(soul, fmt.Errorf("failed to read CAPABILITY response: %w", err)), nil
		}
		line = strings.TrimSpace(line)
		capabilities = append(capabilities, line)

		if strings.HasPrefix(line, "A001 OK") || strings.HasPrefix(line, "A001 BAD") {
			break
		}
	}

	judgment.Details.Capabilities = capabilities

	// LOGIN if credentials provided
	if cfg.Auth != nil && cfg.Auth.Username != "" {
		fmt.Fprintf(conn, "A002 LOGIN \"%s\" \"%s\"\r\n",
			cfg.Auth.Username, cfg.Auth.Password)

		for {
			line, err = reader.ReadString('\n')
			if err != nil {
				return failJudgment(soul, fmt.Errorf("LOGIN failed: %w", err)), nil
			}
			line = strings.TrimSpace(line)

			if strings.HasPrefix(line, "A002 OK") {
				break
			}
			if strings.HasPrefix(line, "A002 NO") || strings.HasPrefix(line, "A002 BAD") {
				return failJudgment(soul, fmt.Errorf("LOGIN rejected: %s", line)), nil
			}
		}

		// Check mailbox if requested
		if cfg.CheckMailbox != "" {
			fmt.Fprintf(conn, "A003 STATUS \"%s\" (MESSAGES UNSEEN)\r\n", cfg.CheckMailbox)

			for {
				line, err = reader.ReadString('\n')
				if err != nil {
					return failJudgment(soul, fmt.Errorf("STATUS failed: %w", err)), nil
				}
				line = strings.TrimSpace(line)

				if strings.HasPrefix(line, "* STATUS") {
					// Parse message count
					// Example: * STATUS "INBOX" (MESSAGES 42 UNSEEN 5)
					if idx := strings.Index(line, "MESSAGES"); idx != -1 {
						rest := line[idx+9:]
						if endIdx := strings.Index(rest, " "); endIdx != -1 {
							count, _ := strconv.Atoi(rest[:endIdx])
							_ = count
						}
					}
				}

				if strings.HasPrefix(line, "A003 OK") {
					break
				}
				if strings.HasPrefix(line, "A003 NO") || strings.HasPrefix(line, "A003 BAD") {
					return failJudgment(soul, fmt.Errorf("STATUS rejected: %s", line)), nil
				}
			}
		}

		// LOGOUT
		fmt.Fprintf(conn, "A004 LOGOUT\r\n")
	}

	duration := time.Since(start)
	judgment.Duration = duration
	judgment.Status = core.SoulAlive
	judgment.Message = fmt.Sprintf("IMAP connection to %s successful in %s", soul.Target, duration.Round(time.Millisecond))

	return judgment, nil
}
