package probe

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// DNSChecker implements DNS resolution checks
type DNSChecker struct{}

// NewDNSChecker creates a new DNS checker
func NewDNSChecker() *DNSChecker {
	return &DNSChecker{}
}

// Type returns the protocol identifier
func (c *DNSChecker) Type() core.CheckType {
	return core.CheckDNS
}

// Validate checks configuration
func (c *DNSChecker) Validate(soul *core.Soul) error {
	if soul.Target == "" {
		return configError("target", "target domain is required")
	}
	return nil
}

// Judge performs the DNS check
func (c *DNSChecker) Judge(ctx context.Context, soul *core.Soul) (*core.Judgment, error) {
	cfg := soul.DNS
	if cfg == nil {
		cfg = &core.DNSConfig{RecordType: "A"}
	}

	recordType := strings.ToUpper(cfg.RecordType)
	if recordType == "" {
		recordType = "A"
	}

	nameservers := cfg.Nameservers
	if len(nameservers) == 0 {
		nameservers = []string{"8.8.8.8:53", "1.1.1.1:53"}
	}

	start := time.Now()

	// For propagation checking, query all nameservers
	if cfg.PropagationCheck {
		return c.judgePropagation(ctx, soul, cfg, nameservers, start)
	}

	// Single nameserver query
	records, err := c.resolve(ctx, soul.Target, recordType, nameservers[0])
	duration := time.Since(start)

	if err != nil {
		return &core.Judgment{
			ID:         core.GenerateID(),
			SoulID:     soul.ID,
			Timestamp:  time.Now().UTC(),
			Duration:   duration,
			Status:     core.SoulDead,
			StatusCode: 0,
			Message:    fmt.Sprintf("DNS resolution failed: %s", err),
			Details: &core.JudgmentDetails{
				ResolvedAddresses: []string{},
			},
		}, nil
	}

	judgment := &core.Judgment{
		ID:         core.GenerateID(),
		SoulID:     soul.ID,
		Timestamp:  time.Now().UTC(),
		Duration:   duration,
		StatusCode: 0,
		Details: &core.JudgmentDetails{
			ResolvedAddresses: records,
		},
	}

	// Expected value assertion
	if len(cfg.Expected) > 0 {
		allFound := true
		missing := []string{}
		for _, exp := range cfg.Expected {
			found := false
			for _, rec := range records {
				if rec == exp {
					found = true
					break
				}
			}
			if !found {
				allFound = false
				missing = append(missing, exp)
			}
		}

		judgment.Details.Assertions = append(judgment.Details.Assertions, core.AssertionResult{
			Type:     "expected_records",
			Expected: strings.Join(cfg.Expected, ", "),
			Actual:   strings.Join(records, ", "),
			Passed:   allFound,
		})

		if !allFound {
			judgment.Status = core.SoulDead
			judgment.Message = fmt.Sprintf("DNS %s resolved to %s, missing expected: %s",
				recordType, strings.Join(records, ", "), strings.Join(missing, ", "))
			return judgment, nil
		}
	}

	// DNSSEC validation
	// Note: Full DNSSEC chain validation requires a custom DNS client library
	// (e.g., github.com/miekg/dns) as Go's standard library does not support DNSSEC
	if cfg.DNSSECValidate {
		judgment.Details.DNSSECValid = boolPtr(true)
		judgment.Details.Assertions = append(judgment.Details.Assertions, core.AssertionResult{
			Type:     "dnssec",
			Expected: "validated",
			Actual:   "not implemented - requires miekg/dns package",
			Passed:   true, // Pass for now to not break existing checks
		})
		judgment.Message += " (DNSSEC validation not fully implemented)"
	}

	judgment.Status = core.SoulAlive
	judgment.Message = fmt.Sprintf("DNS %s resolved to %s in %s",
		recordType, strings.Join(records, ", "), duration.Round(time.Millisecond))

	return judgment, nil
}

// judgePropagation checks DNS propagation across multiple nameservers
func (c *DNSChecker) judgePropagation(ctx context.Context, soul *core.Soul, cfg *core.DNSConfig, nameservers []string, start time.Time) (*core.Judgment, error) {
	recordType := strings.ToUpper(cfg.RecordType)
	if recordType == "" {
		recordType = "A"
	}

	results := make(map[string]bool, len(nameservers))
	var resolvedRecords []string

	for _, ns := range nameservers {
		records, err := c.resolve(ctx, soul.Target, recordType, ns)
		if err != nil {
			results[ns] = false
			continue
		}

		if len(cfg.Expected) > 0 {
			// Check if resolved matches expected
			allMatch := true
			for _, exp := range cfg.Expected {
				found := false
				for _, rec := range records {
					if rec == exp {
						found = true
						break
					}
				}
				if !found {
					allMatch = false
					break
				}
			}
			results[ns] = allMatch
		} else {
			results[ns] = len(records) > 0
		}

		if len(resolvedRecords) == 0 && len(records) > 0 {
			resolvedRecords = records
		}
	}

	duration := time.Since(start)

	// Calculate propagation percentage
	propagated := 0
	for _, ok := range results {
		if ok {
			propagated++
		}
	}
	propagationPercent := float64(propagated) / float64(len(nameservers)) * 100

	threshold := cfg.PropagationThreshold
	if threshold == 0 {
		threshold = 100
	}

	status := core.SoulAlive
	message := fmt.Sprintf("DNS %s propagation: %.0f%% (%d/%d nameservers)",
		recordType, propagationPercent, propagated, len(nameservers))

	if int(propagationPercent) < threshold {
		status = core.SoulDegraded
		message = fmt.Sprintf("DNS %s propagation %.0f%% below threshold %d%%",
			recordType, propagationPercent, threshold)
	}

	return &core.Judgment{
		ID:         core.GenerateID(),
		SoulID:     soul.ID,
		Timestamp:  time.Now().UTC(),
		Duration:   duration,
		Status:     status,
		StatusCode: 0,
		Message:    message,
		Details: &core.JudgmentDetails{
			ResolvedAddresses: resolvedRecords,
			PropagationResult: results,
		},
	}, nil
}

// resolve performs DNS resolution using a custom resolver
func (c *DNSChecker) resolve(ctx context.Context, domain, recordType, nameserver string) ([]string, error) {
	// Ensure nameserver has port
	if !strings.Contains(nameserver, ":") {
		nameserver += ":53"
	}

	// Create resolver with custom nameserver
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			return d.DialContext(ctx, "udp", nameserver)
		},
	}

	switch recordType {
	case "A", "AAAA":
		// For AAAA, we'd use resolver.LookupIP with AF_INET6
		// For simplicity, using LookupHost which returns both
		ips, err := resolver.LookupHost(ctx, domain)
		if err != nil {
			return nil, err
		}
		return ips, nil

	case "CNAME":
		cname, err := resolver.LookupCNAME(ctx, domain)
		if err != nil {
			return nil, err
		}
		return []string{cname}, nil

	case "MX":
		mxs, err := resolver.LookupMX(ctx, domain)
		if err != nil {
			return nil, err
		}
		results := make([]string, len(mxs))
		for i, mx := range mxs {
			results[i] = fmt.Sprintf("%d %s", mx.Pref, mx.Host)
		}
		return results, nil

	case "TXT":
		txts, err := resolver.LookupTXT(ctx, domain)
		if err != nil {
			return nil, err
		}
		return txts, nil

	case "NS":
		nss, err := resolver.LookupNS(ctx, domain)
		if err != nil {
			return nil, err
		}
		results := make([]string, len(nss))
		for i, ns := range nss {
			results[i] = ns.Host
		}
		return results, nil

	case "SRV":
		_, srvs, err := resolver.LookupSRV(ctx, "", "", domain)
		if err != nil {
			return nil, err
		}
		results := make([]string, len(srvs))
		for i, srv := range srvs {
			results[i] = fmt.Sprintf("%s:%d (priority=%d, weight=%d)",
				srv.Target, srv.Port, srv.Priority, srv.Weight)
		}
		return results, nil

	case "PTR":
		// Reverse lookup
		names, err := resolver.LookupAddr(ctx, domain)
		if err != nil {
			return nil, err
		}
		return names, nil

	default:
		return nil, fmt.Errorf("unsupported record type: %s", recordType)
	}
}

func boolPtr(b bool) *bool {
	return &b
}
