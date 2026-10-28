package probe

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func hasAuthCredentials(auth *core.AuthCreds) bool {
	return auth != nil && (strings.TrimSpace(auth.Username) != "" || strings.TrimSpace(auth.Password) != "")
}

func quoteIMAPString(value string) (string, error) {
	if strings.ContainsAny(value, "\x00\r\n") {
		return "", fmt.Errorf("IMAP quoted strings cannot contain NUL, CR, or LF")
	}

	var quoted strings.Builder
	quoted.Grow(len(value) + 2)
	quoted.WriteByte('"')
	for i := 0; i < len(value); i++ {
		if value[i] == '"' || value[i] == '\\' {
			quoted.WriteByte('\\')
		}
		quoted.WriteByte(value[i])
	}
	quoted.WriteByte('"')
	return quoted.String(), nil
}

// smtpCapabilityValues extracts the values advertised for an SMTP capability.
//
// Servers advertise the AUTH extension in two shapes: the RFC form
// "250-AUTH LOGIN PLAIN" and the Exim/sendmail form "250-AUTH=PLAIN", where
// the mechanism is attached to the keyword with '='. Both must resolve to the
// same capability keyword so AUTH detection does not depend on the server
// implementation. The keyword itself is still matched exactly (case
// insensitively), so a lookalike such as "250 XSTARTTLS" is not accepted.
func smtpCapabilityValues(capabilities []string, name string) ([]string, bool) {
	var values []string
	found := false
	for _, response := range capabilities {
		if len(response) < 4 || !strings.HasPrefix(response, "250") {
			continue
		}
		fields := strings.Fields(response[4:])
		if len(fields) == 0 {
			continue
		}
		keyword := fields[0]
		params := fields[1:]
		if idx := strings.IndexByte(keyword, '='); idx >= 0 {
			params = append([]string{keyword[idx+1:]}, params...)
			keyword = keyword[:idx]
		}
		if !strings.EqualFold(keyword, name) {
			continue
		}
		found = true
		for _, param := range params {
			if param != "" {
				values = append(values, param)
			}
		}
	}
	return values, found
}

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

	// SSRF protection - validate target address
	if err := ValidateAddress(soul.Target); err != nil {
		return configError("target", fmt.Sprintf("SSRF validation failed: %v", err))
	}
	if soul.SMTP != nil && hasAuthCredentials(soul.SMTP.Auth) && !soul.SMTP.StartTLS {
		return configError("smtp.auth", "credentials require STARTTLS")
	}

	// Security warning for disabled TLS verification
	if soul.SMTP != nil && soul.SMTP.InsecureSkipVerify {
		slog.Warn("SECURITY WARNING: SMTP check has InsecureSkipVerify enabled. TLS certificate verification is disabled. This should only be used for testing, never in production.",
			"soul", soul.Name,
			"soul_id", soul.ID)
	}

	return nil
}

// Judge performs the SMTP check
func (c *SMTPChecker) Judge(ctx context.Context, soul *core.Soul) (*core.Judgment, error) {
	cfg := soul.SMTP
	if cfg == nil {
		cfg = &core.SMTPConfig{}
	}
	if hasAuthCredentials(cfg.Auth) && !cfg.StartTLS {
		return failJudgment(soul, fmt.Errorf("SMTP credentials require STARTTLS")), nil
	}

	timeout := soul.Timeout.Duration
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	opCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Connect to SMTP server with SSRF DNS-rebinding protection.
	// WrapDialerContext re-resolves the hostname and dials the validated
	// literal IP, closing the TOCTOU window between config validation
	// and connection time.
	start := time.Now()
	dialCtx := DefaultValidator.Load().WrapDialerContext((&net.Dialer{Timeout: timeout}).DialContext)
	conn, err := dialCtx(opCtx, "tcp", soul.Target)
	if err != nil {
		return failJudgment(soul, fmt.Errorf("SMTP connection failed: %w", err)), nil
	}
	defer conn.Close()
	stopDeadline, deadlineErr := bindConnDeadline(opCtx, conn, timeout)
	if deadlineErr != nil {
		return failJudgment(soul, fmt.Errorf("SMTP deadline setup failed: %w", deadlineErr)), nil
	}
	defer stopDeadline()
	// Bound the total peer data read: textproto/ReadString have no line or
	// reply-size limit, so a hostile server could stream unbounded data.

	reader := bufio.NewReader(io.LimitReader(conn, maxReadSize))
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
	// Consume the complete greeting before reading the EHLO response, keeping
	// the raw reply codes available for existing banner assertions.
	greeting := []string{line}
	for strings.HasPrefix(line, "220-") {
		line, err = textReader.ReadLine()
		if err != nil {
			return failJudgment(soul, fmt.Errorf("failed to read SMTP greeting: %w", err)), nil
		}
		if !strings.HasPrefix(line, "220") {
			return failJudgment(soul, fmt.Errorf("unexpected SMTP greeting: %s", line)), nil
		}
		greeting = append(greeting, line)
	}
	line = strings.Join(greeting, "\n")

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
	tlsServerName, _, splitErr := net.SplitHostPort(soul.Target)
	if splitErr != nil {
		return failJudgment(soul, fmt.Errorf("invalid SMTP target: %w", splitErr)), nil
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
		if _, hasSTARTTLS := smtpCapabilityValues(capabilities, "STARTTLS"); !hasSTARTTLS {
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

		// Upgrade to TLS. Certificate identity is the connection target, not the
		// client-controlled EHLO identity.
		tlsConfig := &tls.Config{
			InsecureSkipVerify: cfg.InsecureSkipVerify, // Default: false (secure)
			ServerName:         tlsServerName,
			MinVersion:         tls.VersionTLS12,
		}
		tlsConn := tls.Client(conn, tlsConfig)
		if err := tlsConn.HandshakeContext(opCtx); err != nil {
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
		reader = bufio.NewReader(io.LimitReader(conn, maxReadSize))
		textReader = textproto.NewReader(reader)

		// Send EHLO again over TLS and use only capabilities advertised on the
		// protected channel for subsequent AUTH mechanism selection.
		capabilities = capabilities[:0]
		fmt.Fprintf(conn, "EHLO %s\r\n", ehloDomain)
		for {
			line, err = textReader.ReadLine()
			if err != nil {
				return failJudgment(soul, fmt.Errorf("failed to read EHLO response over TLS: %w", err)), nil
			}
			capabilities = append(capabilities, line)
			if !strings.HasPrefix(line, "250-") {
				break
			}
		}
		if !strings.HasPrefix(line, "250 ") {
			return failJudgment(soul, fmt.Errorf("EHLO over TLS failed: %s", line)), nil
		}
		judgment.Details.Capabilities = capabilities
	}

	// AUTH if credentials provided
	if cfg.Auth != nil && cfg.Auth.Username != "" {
		authMechanisms, hasAuth := smtpCapabilityValues(capabilities, "AUTH")
		supportsLogin := false
		supportsPlain := false
		for _, mechanism := range authMechanisms {
			if strings.EqualFold(mechanism, "LOGIN") {
				supportsLogin = true
			}
			if strings.EqualFold(mechanism, "PLAIN") {
				supportsPlain = true
			}
		}

		if !hasAuth {
			return failJudgment(soul, fmt.Errorf("AUTH requested but not advertised")), nil
		}

		// Prefer LOGIN, fall back to PLAIN
		if supportsLogin {
			// AUTH LOGIN: server sends 334 with base64 username prompt
			fmt.Fprintf(conn, "AUTH LOGIN\r\n")
			line, err = textReader.ReadLine()
			if err != nil {
				return failJudgment(soul, fmt.Errorf("AUTH LOGIN command failed: %w", err)), nil
			}
			if !strings.HasPrefix(line, "334") {
				return failJudgment(soul, fmt.Errorf("AUTH LOGIN rejected: %s", line)), nil
			}

			// Send base64-encoded username
			usernameB64 := base64.StdEncoding.EncodeToString([]byte(cfg.Auth.Username))
			fmt.Fprintf(conn, "%s\r\n", usernameB64)
			line, err = textReader.ReadLine()
			if err != nil {
				return failJudgment(soul, fmt.Errorf("failed to read username response: %w", err)), nil
			}
			if !strings.HasPrefix(line, "334") {
				return failJudgment(soul, fmt.Errorf("username rejected: %s", line)), nil
			}

			// Send base64-encoded password
			passwordB64 := base64.StdEncoding.EncodeToString([]byte(cfg.Auth.Password))
			fmt.Fprintf(conn, "%s\r\n", passwordB64)
			line, err = textReader.ReadLine()
			if err != nil {
				return failJudgment(soul, fmt.Errorf("failed to read auth result: %w", err)), nil
			}
			if !strings.HasPrefix(line, "235") {
				return failJudgment(soul, fmt.Errorf("authentication failed: %s", line)), nil
			}

			judgment.Details.Assertions = append(judgment.Details.Assertions, core.AssertionResult{
				Type:     "auth_login",
				Expected: "success",
				Actual:   "authenticated",
				Passed:   true,
			})
		} else if supportsPlain {
			// AUTH PLAIN: send credentials in single command
			// Format: "AUTH PLAIN \0username\0password" (base64 encoded)
			authString := fmt.Sprintf("\000%s\000%s", cfg.Auth.Username, cfg.Auth.Password)
			authB64 := base64.StdEncoding.EncodeToString([]byte(authString))
			fmt.Fprintf(conn, "AUTH PLAIN %s\r\n", authB64)
			line, err = textReader.ReadLine()
			if err != nil {
				return failJudgment(soul, fmt.Errorf("AUTH PLAIN command failed: %w", err)), nil
			}
			if !strings.HasPrefix(line, "235") {
				return failJudgment(soul, fmt.Errorf("AUTH PLAIN rejected: %s", line)), nil
			}

			judgment.Details.Assertions = append(judgment.Details.Assertions, core.AssertionResult{
				Type:     "auth_plain",
				Expected: "success",
				Actual:   "authenticated",
				Passed:   true,
			})
		} else {
			// AUTH available but mechanism unknown
			return failJudgment(soul, fmt.Errorf("AUTH supported but no known mechanism (need LOGIN or PLAIN)")), nil
		}
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

	// SSRF protection - validate target address
	if err := ValidateAddress(soul.Target); err != nil {
		return configError("target", fmt.Sprintf("SSRF validation failed: %v", err))
	}
	if soul.IMAP != nil && hasAuthCredentials(soul.IMAP.Auth) && !soul.IMAP.TLS {
		return configError("imap.auth", "credentials require TLS")
	}
	if soul.IMAP != nil && soul.IMAP.Auth != nil {
		if _, err := quoteIMAPString(soul.IMAP.Auth.Username); err != nil {
			return configError("imap.auth.username", err.Error())
		}
		if _, err := quoteIMAPString(soul.IMAP.Auth.Password); err != nil {
			return configError("imap.auth.password", err.Error())
		}
	}

	// Security warning for disabled TLS verification
	if soul.IMAP != nil && soul.IMAP.InsecureSkipVerify {
		slog.Warn("SECURITY WARNING: IMAP check has InsecureSkipVerify enabled. TLS certificate verification is disabled. This should only be used for testing, never in production.",
			"soul", soul.Name,
			"soul_id", soul.ID)
	}

	return nil
}

// Judge performs the IMAP check
func (c *IMAPChecker) Judge(ctx context.Context, soul *core.Soul) (*core.Judgment, error) {
	cfg := soul.IMAP
	if cfg == nil {
		cfg = &core.IMAPConfig{}
	}
	if hasAuthCredentials(cfg.Auth) && !cfg.TLS {
		return failJudgment(soul, fmt.Errorf("IMAP credentials require TLS")), nil
	}
	quotedUsername, quotedPassword := "", ""
	if cfg.Auth != nil && cfg.Auth.Username != "" {
		var quoteErr error
		quotedUsername, quoteErr = quoteIMAPString(cfg.Auth.Username)
		if quoteErr != nil {
			return failJudgment(soul, fmt.Errorf("invalid IMAP username: %w", quoteErr)), nil
		}
		quotedPassword, quoteErr = quoteIMAPString(cfg.Auth.Password)
		if quoteErr != nil {
			return failJudgment(soul, fmt.Errorf("invalid IMAP password: %w", quoteErr)), nil
		}
	}

	timeout := soul.Timeout.Duration
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	opCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	var conn net.Conn
	var err error

	// SSRF protection: resolve hostname, validate all IPs against the
	// blocklist, and dial the pinned literal IP so an attacker that
	// changes DNS between Validate() and Judge() cannot redirect the
	// connection to an internal/metadata address.
	if cfg.TLS {
		host, _, splitErr := net.SplitHostPort(soul.Target)
		if splitErr != nil {
			host = soul.Target
		}
		pinnedDial := DefaultValidator.Load().WrapDialerContext((&net.Dialer{Timeout: timeout}).DialContext)
		rawConn, dialErr := pinnedDial(opCtx, "tcp", soul.Target)
		if dialErr != nil {
			return failJudgment(soul, fmt.Errorf("IMAP connection failed: %w", dialErr)), nil
		}
		conn = tls.Client(rawConn, &tls.Config{
			ServerName:         host,
			InsecureSkipVerify: cfg.InsecureSkipVerify, // #nosec G402 -- see K7 gate
			MinVersion:         tls.VersionTLS12,
		})
		if err := conn.(*tls.Conn).HandshakeContext(opCtx); err != nil {
			rawConn.Close()
			return failJudgment(soul, fmt.Errorf("IMAP TLS handshake failed: %w", err)), nil
		}
	} else {
		dialCtx := DefaultValidator.Load().WrapDialerContext((&net.Dialer{Timeout: timeout}).DialContext)
		conn, err = dialCtx(opCtx, "tcp", soul.Target)
	}

	if err != nil {
		return failJudgment(soul, fmt.Errorf("IMAP connection failed: %w", err)), nil
	}
	defer conn.Close()
	stopDeadline, deadlineErr := bindConnDeadline(opCtx, conn, timeout)
	if deadlineErr != nil {
		return failJudgment(soul, fmt.Errorf("IMAP deadline setup failed: %w", deadlineErr)), nil
	}
	defer stopDeadline()

	reader := bufio.NewReader(io.LimitReader(conn, maxReadSize))

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

		if strings.HasPrefix(line, "A001 OK") {
			break
		}
		if strings.HasPrefix(line, "A001 NO") || strings.HasPrefix(line, "A001 BAD") {
			return failJudgment(soul, fmt.Errorf("CAPABILITY rejected: %s", line)), nil
		}
	}

	judgment.Details.Capabilities = capabilities

	// LOGIN if credentials provided
	if cfg.Auth != nil && cfg.Auth.Username != "" {
		fmt.Fprintf(conn, "A002 LOGIN %s %s\r\n", quotedUsername, quotedPassword)

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
