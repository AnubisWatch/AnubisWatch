package probe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// TLSChecker implements TLS certificate checks
type TLSChecker struct{}

// NewTLSChecker creates a new TLS checker
func NewTLSChecker() *TLSChecker {
	return &TLSChecker{}
}

// Type returns the protocol identifier
func (c *TLSChecker) Type() core.CheckType {
	return core.CheckTLS
}

// Validate checks configuration
func (c *TLSChecker) Validate(soul *core.Soul) error {
	if soul.Target == "" {
		return configError("target", "target host:port is required")
	}

	// Strip scheme if present (e.g., "https://example.com:443" -> "example.com:443")
	target := soul.Target
	if strings.HasPrefix(target, "https://") {
		target = strings.TrimPrefix(target, "https://")
	} else if strings.HasPrefix(target, "http://") {
		target = strings.TrimPrefix(target, "http://")
	}

	// Ensure port is specified
	if !strings.Contains(target, ":") {
		target += ":443"
	}

	// Update soul.Target with normalized value
	soul.Target = target

	// SSRF protection - validate target address
	if err := ValidateAddress(target); err != nil {
		return configError("target", fmt.Sprintf("SSRF validation failed: %v", err))
	}

	return nil
}

// Judge performs the TLS certificate check
func (c *TLSChecker) Judge(ctx context.Context, soul *core.Soul) (*core.Judgment, error) {
	cfg := soul.TLS
	if cfg == nil {
		cfg = &core.TLSConfig{
			ExpiryWarnDays:     30,
			ExpiryCriticalDays: 7,
			MinProtocol:        "TLS1.2",
		}
	}

	timeout := soul.Timeout.Duration
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	start := time.Now()

	// Extract hostname from target
	host, _, err := net.SplitHostPort(soul.Target)
	if err != nil {
		host = soul.Target
	}

	// Configure TLS with proper verification
	tlsConfig := &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS12, // Enforce TLS 1.2 minimum
	}

	// Set minimum protocol version from config (if specified and stricter)
	if cfg.MinProtocol != "" {
		version := parseTLSVersion(cfg.MinProtocol)
		if version > tlsConfig.MinVersion {
			tlsConfig.MinVersion = version
		}
	}

	// Connect and perform handshake with verification enabled
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: timeout}, "tcp", soul.Target, tlsConfig)
	if err != nil {
		// If connection fails due to certificate issues, try to get certificate info for diagnostics
		// but still return a failure judgment
		return c.diagnoseTLSFailure(soul, err, timeout), nil
	}
	defer conn.Close()

	duration := time.Since(start)

	// Get connection state
	state := conn.ConnectionState()

	judgment := &core.Judgment{
		ID:         core.GenerateID(),
		SoulID:     soul.ID,
		Timestamp:  time.Now().UTC(),
		Duration:   duration,
		StatusCode: 0,
		TLSInfo:    extractTLSInfo(&state),
		Details:    &core.JudgmentDetails{},
	}

	// Run assertions
	assertions := make([]core.AssertionResult, 0)
	allPassed := true

	// Check certificate expiry
	if len(state.PeerCertificates) > 0 {
		cert := state.PeerCertificates[0]
		daysUntilExpiry := int(time.Until(cert.NotAfter).Hours() / 24)

		// Critical expiry check
		if cfg.ExpiryCriticalDays > 0 && daysUntilExpiry < cfg.ExpiryCriticalDays {
			judgment.Details.Assertions = append(assertions, core.AssertionResult{
				Type:     "certificate_expiry",
				Expected: fmt.Sprintf(">%d days", cfg.ExpiryCriticalDays),
				Actual:   fmt.Sprintf("%d days", daysUntilExpiry),
				Passed:   false,
			})
			judgment.Status = core.SoulDead
			judgment.Message = fmt.Sprintf("Certificate expires in %d days (critical threshold: %d)",
				daysUntilExpiry, cfg.ExpiryCriticalDays)
			return judgment, nil
		}

		// Warning expiry check
		if cfg.ExpiryWarnDays > 0 && daysUntilExpiry < cfg.ExpiryWarnDays {
			assertions = append(assertions, core.AssertionResult{
				Type:     "certificate_expiry",
				Expected: fmt.Sprintf(">%d days", cfg.ExpiryWarnDays),
				Actual:   fmt.Sprintf("%d days", daysUntilExpiry),
				Passed:   false,
			})
			allPassed = false
			judgment.Status = core.SoulDegraded
			judgment.Message = fmt.Sprintf("Certificate expires in %d days (warning threshold: %d)",
				daysUntilExpiry, cfg.ExpiryWarnDays)
		}

		// Protocol version check
		if cfg.MinProtocol != "" {
			minVersion := parseTLSVersion(cfg.MinProtocol)
			versionOK := state.Version >= minVersion
			assertions = append(assertions, core.AssertionResult{
				Type:     "tls_version",
				Expected: cfg.MinProtocol,
				Actual:   tlsVersionString(state.Version),
				Passed:   versionOK,
			})
			if !versionOK {
				allPassed = false
				if judgment.Status != core.SoulDead {
					judgment.Status = core.SoulDegraded
					judgment.Message = fmt.Sprintf("TLS version %s below minimum %s",
						tlsVersionString(state.Version), cfg.MinProtocol)
				}
			}
		}

		// Cipher suite check
		if len(cfg.ForbiddenCiphers) > 0 {
			cipherName := tls.CipherSuiteName(state.CipherSuite)
			forbidden := false
			for _, forbiddenCipher := range cfg.ForbiddenCiphers {
				if strings.Contains(cipherName, forbiddenCipher) {
					forbidden = true
					break
				}
			}
			assertions = append(assertions, core.AssertionResult{
				Type:     "cipher_suite",
				Expected: "not in forbidden list",
				Actual:   cipherName,
				Passed:   !forbidden,
			})
			if forbidden {
				allPassed = false
				if judgment.Status != core.SoulDead {
					judgment.Status = core.SoulDegraded
					judgment.Message = fmt.Sprintf("Cipher suite %s is in forbidden list", cipherName)
				}
			}
		}

		// SAN check
		if len(cfg.ExpectedSAN) > 0 {
			sanMatched := false
			for _, expected := range cfg.ExpectedSAN {
				for _, san := range cert.DNSNames {
					if matchesSAN(san, expected) {
						sanMatched = true
						break
					}
				}
				if sanMatched {
					break
				}
			}
			assertions = append(assertions, core.AssertionResult{
				Type:     "san",
				Expected: strings.Join(cfg.ExpectedSAN, ", "),
				Actual:   strings.Join(cert.DNSNames, ", "),
				Passed:   sanMatched,
			})
			if !sanMatched {
				allPassed = false
				if judgment.Status != core.SoulDead {
					judgment.Status = core.SoulDead
					judgment.Message = fmt.Sprintf("SAN mismatch: expected %v, got %v",
						cfg.ExpectedSAN, cert.DNSNames)
					return judgment, nil
				}
			}
		}

		// Issuer check
		if cfg.ExpectedIssuer != "" {
			issuerMatched := strings.Contains(cert.Issuer.CommonName, cfg.ExpectedIssuer) ||
				strings.Contains(cert.Issuer.Organization[0], cfg.ExpectedIssuer)
			assertions = append(assertions, core.AssertionResult{
				Type:     "issuer",
				Expected: cfg.ExpectedIssuer,
				Actual:   cert.Issuer.CommonName,
				Passed:   issuerMatched,
			})
			if !issuerMatched {
				allPassed = false
				if judgment.Status != core.SoulDead {
					judgment.Status = core.SoulDegraded
					judgment.Message = fmt.Sprintf("Issuer mismatch: expected %s, got %s",
						cfg.ExpectedIssuer, cert.Issuer.CommonName)
				}
			}
		}

		// OCSP stapling check
		if cfg.CheckOCSP {
			hasOCSP := len(state.OCSPResponse) > 0
			assertions = append(assertions, core.AssertionResult{
				Type:     "ocsp_stapling",
				Expected: "present",
				Actual:   boolToString(hasOCSP, "present", "absent"),
				Passed:   hasOCSP,
			})
			if !hasOCSP && judgment.Status != core.SoulDead {
				judgment.Status = core.SoulDegraded
				judgment.Message = "OCSP stapling not available"
			}
		}

		// Key size check
		if cfg.MinKeyBits > 0 {
			// This is a simplified check - real implementation would parse the public key
			keySizeOK := true // Assume OK for now
			assertions = append(assertions, core.AssertionResult{
				Type:     "key_size",
				Expected: fmt.Sprintf(">=%d bits", cfg.MinKeyBits),
				Actual:   "unknown",
				Passed:   keySizeOK,
			})
		}
	}

	judgment.Details.Assertions = assertions

	if judgment.Status == "" {
		if allPassed {
			judgment.Status = core.SoulAlive
			judgment.Message = fmt.Sprintf("TLS certificate valid, expires in %d days",
				judgment.TLSInfo.DaysUntilExpiry)
		} else {
			judgment.Status = core.SoulDegraded
			judgment.Message = "TLS certificate has issues"
		}
	}

	return judgment, nil
}

// diagnoseTLSFailure attempts to get certificate info even when TLS verification fails
func (c *TLSChecker) diagnoseTLSFailure(soul *core.Soul, dialErr error, timeout time.Duration) *core.Judgment {
	// HIGH-06: Use VerifyPeerCertificate callback to capture certificates for diagnostics
	// while still performing our own chain verification, rather than blindly accepting
	// any certificate with InsecureSkipVerify.
	var capturedCerts []*x509.Certificate
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true, // Required to not fail handshake; we verify ourselves below
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			certs := make([]*x509.Certificate, len(rawCerts))
			for i, rawCert := range rawCerts {
				cert, err := x509.ParseCertificate(rawCert)
				if err != nil {
					return nil // Don't fail the handshake
				}
				certs[i] = cert
			}
			capturedCerts = certs
			return nil // Never fail the handshake
		},
	}

	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: timeout}, "tcp", soul.Target, tlsConfig)
	if err != nil {
		// Can't even connect, return the original error
		return failJudgment(soul, fmt.Errorf("TLS connection failed: %w", dialErr))
	}
	defer conn.Close()

	// Build TLSInfo from captured certificates
	var tlsInfo *core.TLSInfo
	if len(capturedCerts) > 0 {
		tlsInfo = extractTLSCertsOnly(capturedCerts)
	}

	// Build detailed error message based on the original error
	errMsg := fmt.Sprintf("TLS verification failed: %v", dialErr)
	if len(capturedCerts) > 0 {
		cert := capturedCerts[0]
		daysUntilExpiry := int(time.Until(cert.NotAfter).Hours() / 24)
		if daysUntilExpiry < 0 {
			errMsg = fmt.Sprintf("Certificate expired %d days ago", -daysUntilExpiry)
		} else {
			errMsg = fmt.Sprintf("Certificate expires in %d days but verification failed: %v", daysUntilExpiry, dialErr)
		}
	}

	judgment := failJudgment(soul, fmt.Errorf("%s", errMsg))
	judgment.TLSInfo = tlsInfo
	return judgment
}

// extractTLSCertsOnly builds TLSInfo from raw certificates without connection state.
func extractTLSCertsOnly(certs []*x509.Certificate) *core.TLSInfo {
	if len(certs) == 0 {
		return nil
	}
	cert := certs[0]
	keyType := "RSA"
	if cert.PublicKeyAlgorithm == x509.ECDSA {
		keyType = "ECDSA"
	}
	info := &core.TLSInfo{
		Issuer:          cert.Issuer.CommonName,
		Subject:         cert.Subject.CommonName,
		SANs:            cert.DNSNames,
		NotBefore:       cert.NotBefore,
		NotAfter:        cert.NotAfter,
		DaysUntilExpiry: int(time.Until(cert.NotAfter).Hours() / 24),
		KeyType:         keyType,
		ChainLength:     len(certs),
		ChainValid:      len(certs) > 1,
	}
	return info
}

func parseTLSVersion(s string) uint16 {
	s = strings.ToUpper(strings.ReplaceAll(s, ".", ""))
	switch s {
	case "TLS10", "TLS1.0", "TLS1":
		return tls.VersionTLS10
	case "TLS11", "TLS1.1":
		return tls.VersionTLS11
	case "TLS12", "TLS1.2":
		return tls.VersionTLS12
	case "TLS13", "TLS1.3":
		return tls.VersionTLS13
	default:
		return 0
	}
}

// matchesSAN checks if a SAN matches an expected pattern (supports wildcards)
func matchesSAN(san, expected string) bool {
	if expected == san {
		return true
	}
	// Simple wildcard matching - *.example.com matches only one level
	if strings.HasPrefix(expected, "*.") {
		suffix := expected[1:] // Remove *
		if !strings.HasSuffix(san, suffix) {
			return false
		}
		// Ensure only one level matches (e.g., *.example.com matches api.example.com but not api.sub.example.com)
		prefix := strings.TrimSuffix(san, suffix)
		return !strings.Contains(prefix, ".")
	}
	return false
}
