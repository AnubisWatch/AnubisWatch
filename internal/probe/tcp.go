package probe

import (
	"bufio"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// TCPChecker implements TCP health checks
type TCPChecker struct{}

// NewTCPChecker creates a new TCP checker
func NewTCPChecker() *TCPChecker {
	return &TCPChecker{}
}

// Type returns the protocol identifier
func (c *TCPChecker) Type() core.CheckType {
	return core.CheckTCP
}

// Validate checks configuration
func (c *TCPChecker) Validate(soul *core.Soul) error {
	if soul.Target == "" {
		return configError("target", "target host:port is required")
	}
	if _, _, err := net.SplitHostPort(soul.Target); err != nil {
		return configError("target", "target must be in host:port format")
	}
	return nil
}

// Judge performs the TCP check
func (c *TCPChecker) Judge(ctx context.Context, soul *core.Soul) (*core.Judgment, error) {
	cfg := soul.TCP
	if cfg == nil {
		cfg = &core.TCPConfig{}
	}

	timeout := soul.Timeout.Duration
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	// Resolve address
	dialer := net.Dialer{Timeout: timeout}
	start := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", soul.Target)
	duration := time.Since(start)

	if err != nil {
		return &core.Judgment{
			ID:         core.GenerateID(),
			SoulID:     soul.ID,
			Timestamp:  time.Now().UTC(),
			Duration:   duration,
			Status:     core.SoulDead,
			StatusCode: 0,
			Message:    fmt.Sprintf("TCP connection failed: %s", err),
			Details:    &core.JudgmentDetails{},
		}, nil
	}
	defer conn.Close()

	// Build judgment
	judgment := &core.Judgment{
		ID:         core.GenerateID(),
		SoulID:     soul.ID,
		Timestamp:  time.Now().UTC(),
		Duration:   duration,
		StatusCode: 0,
		Details:    &core.JudgmentDetails{},
	}

	// Banner grab / send-expect
	if cfg.BannerMatch != "" || cfg.ExpectRegex != "" || cfg.Send != "" {
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		reader := bufio.NewReader(conn)

		// Send payload if configured
		if cfg.Send != "" {
			conn.Write([]byte(cfg.Send))
		}

		// Read response
		banner, err := reader.ReadString('\n')
		if err != nil && banner == "" {
			// Try reading without delimiter (limited to maxBannerSize)
			buf := make([]byte, maxBannerSize)
			n, _ := io.ReadFull(reader, buf[:])
			banner = string(buf[:n])
		}

		judgment.Details.Banner = strings.TrimSpace(banner)

		// Banner match assertion
		if cfg.BannerMatch != "" {
			matched := strings.Contains(strings.ToLower(banner), strings.ToLower(cfg.BannerMatch))
			judgment.Details.Assertions = append(judgment.Details.Assertions, core.AssertionResult{
				Type:     "banner_match",
				Expected: cfg.BannerMatch,
				Actual:   truncateString(banner, 200),
				Passed:   matched,
			})
			if !matched {
				judgment.Status = core.SoulDead
				judgment.Message = fmt.Sprintf("TCP connect OK, banner mismatch: expected '%s', got '%s'",
					cfg.BannerMatch, truncateString(banner, 100))
				return judgment, nil
			}
		}

		// Regex assertion
		if cfg.ExpectRegex != "" {
			re, err := regexp.Compile(cfg.ExpectRegex)
			matched := err == nil && re.MatchString(banner)
			judgment.Details.Assertions = append(judgment.Details.Assertions, core.AssertionResult{
				Type:     "expect_regex",
				Expected: cfg.ExpectRegex,
				Actual:   truncateString(banner, 200),
				Passed:   matched,
			})
			if !matched {
				judgment.Status = core.SoulDead
				judgment.Message = "TCP connect OK, response did not match expected pattern"
				return judgment, nil
			}
		}
	}

	judgment.Status = core.SoulAlive
	judgment.Message = fmt.Sprintf("TCP connect to %s in %s", soul.Target, duration.Round(time.Millisecond))
	return judgment, nil
}

// UDPChecker implements UDP health checks
type UDPChecker struct{}

// NewUDPChecker creates a new UDP checker
func NewUDPChecker() *UDPChecker {
	return &UDPChecker{}
}

// Type returns the protocol identifier
func (c *UDPChecker) Type() core.CheckType {
	return core.CheckUDP
}

// Validate checks configuration
func (c *UDPChecker) Validate(soul *core.Soul) error {
	if soul.Target == "" {
		return configError("target", "target host:port is required")
	}
	if _, _, err := net.SplitHostPort(soul.Target); err != nil {
		return configError("target", "target must be in host:port format")
	}
	return nil
}

// Judge performs the UDP check
func (c *UDPChecker) Judge(ctx context.Context, soul *core.Soul) (*core.Judgment, error) {
	cfg := soul.UDP
	if cfg == nil {
		cfg = &core.UDPConfig{}
	}

	timeout := soul.Timeout.Duration
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	// Resolve address
	addr, err := net.ResolveUDPAddr("udp", soul.Target)
	if err != nil {
		return failJudgment(soul, fmt.Errorf("failed to resolve address: %w", err)), nil
	}

	// Create UDP connection
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return failJudgment(soul, fmt.Errorf("failed to create UDP socket: %w", err)), nil
	}
	defer conn.Close()

	// Set deadline
	conn.SetDeadline(time.Now().Add(timeout))

	// Build payload
	var payload []byte
	if cfg.SendHex != "" {
		payload, err = hex.DecodeString(strings.ReplaceAll(cfg.SendHex, " ", ""))
		if err != nil {
			return failJudgment(soul, fmt.Errorf("invalid hex payload: %w", err)), nil
		}
	}

	// Send packet
	start := time.Now()
	if len(payload) > 0 {
		_, err = conn.Write(payload)
		if err != nil {
			return failJudgment(soul, fmt.Errorf("failed to send UDP packet: %w", err)), nil
		}
	}

	// Wait for response (limited to maxBannerSize)
	buf := make([]byte, maxBannerSize)
	n, err := conn.Read(buf)
	duration := time.Since(start)

	if err != nil {
		return &core.Judgment{
			ID:         core.GenerateID(),
			SoulID:     soul.ID,
			Timestamp:  time.Now().UTC(),
			Duration:   duration,
			Status:     core.SoulDead,
			StatusCode: 0,
			Message:    fmt.Sprintf("UDP no response: %s", err),
			Details:    &core.JudgmentDetails{},
		}, nil
	}

	response := string(buf[:n])

	// Check expected content
	if cfg.ExpectContains != "" {
		if !strings.Contains(response, cfg.ExpectContains) {
			return &core.Judgment{
				ID:         core.GenerateID(),
				SoulID:     soul.ID,
				Timestamp:  time.Now().UTC(),
				Duration:   duration,
				Status:     core.SoulDead,
				StatusCode: 0,
				Message:    fmt.Sprintf("UDP response does not contain expected: '%s'", cfg.ExpectContains),
				Details: &core.JudgmentDetails{
					Banner: truncateString(response, 200),
				},
			}, nil
		}
	}

	return &core.Judgment{
		ID:         core.GenerateID(),
		SoulID:     soul.ID,
		Timestamp:  time.Now().UTC(),
		Duration:   duration,
		Status:     core.SoulAlive,
		StatusCode: 0,
		Message:    fmt.Sprintf("UDP response received in %s", duration.Round(time.Millisecond)),
		Details: &core.JudgmentDetails{
			Banner: truncateString(response, 200),
		},
	}, nil
}
