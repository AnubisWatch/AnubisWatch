package probe

import (
	"context"
	"encoding/binary"
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

	// DNSSEC validation
	var dnssecAssertion core.AssertionResult
	dnssecRequested := cfg.DNSSECValidate

	if dnssecRequested {
		// Query for RRSIG records to check DNSSEC support
		rrsigRecords, adFlag, err := c.queryDNSSEC(ctx, soul.Target, nameservers[0])
		if err != nil {
			// DNSSEC query failed - domain may not be signed
			dnssecAssertion = core.AssertionResult{
				Type:     "dnssec",
				Expected: "signed",
				Actual:   fmt.Sprintf("query failed: %s", err),
				Passed:   false,
			}
		} else if !adFlag && len(rrsigRecords) == 0 {
			// No DNSSEC signatures found - domain is not signed
			dnssecAssertion = core.AssertionResult{
				Type:     "dnssec",
				Expected: "signed",
				Actual:   "not signed (no RRSIG records)",
				Passed:   false,
			}
		} else if adFlag {
			// AD flag set = server validated the DNSSEC chain
			dnssecAssertion = core.AssertionResult{
				Type:     "dnssec",
				Expected: "signed",
				Actual:   "valid (AD flag set by resolver)",
				Passed:   true,
			}
		} else if len(rrsigRecords) > 0 {
			// RRSIG records present but AD not set - domain is signed but chain couldn't be validated
			dnssecAssertion = core.AssertionResult{
				Type:     "dnssec",
				Expected: "validated",
				Actual:   "signed but not validated (AD not set)",
				Passed:   false,
			}
		}
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
			if dnssecRequested {
				judgment.Details.DNSSECValid = &dnssecAssertion.Passed
				judgment.Details.Assertions = append(judgment.Details.Assertions, dnssecAssertion)
			}
			return judgment, nil
		}
	}

	// DNSSEC validation result
	if dnssecRequested {
		judgment.Details.DNSSECValid = &dnssecAssertion.Passed
		judgment.Details.Assertions = append(judgment.Details.Assertions, dnssecAssertion)
		if !dnssecAssertion.Passed {
			judgment.Status = core.SoulDead
			judgment.Message = fmt.Sprintf("DNSSEC validation failed: %s", dnssecAssertion.Actual)
			return judgment, nil
		}
	}

	judgment.Status = core.SoulAlive
	judgment.Message = fmt.Sprintf("DNS %s resolved to %s in %s",
		recordType, strings.Join(records, ", "), duration.Round(time.Millisecond))
	if dnssecRequested && dnssecAssertion.Passed {
		judgment.Message += " (DNSSEC valid)"
	}

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

// queryDNSSEC queries a nameserver with DNSSEC (DO bit set) and returns
// RRSIG records found and whether the AD (Authenticated Data) flag is set.
func (c *DNSChecker) queryDNSSEC(ctx context.Context, domain, nameserver string) (rrsigRecords []string, adFlag bool, err error) {
	if !strings.Contains(nameserver, ":") {
		nameserver += ":53"
	}

	// Build DNS query with EDNS0 DO bit for the target domain
	// Query for the original record type, not RRSIG, since AD flag is what matters
	msg := buildDNSQueryWithEDNS0(domain, 0x01 /* A record */, true /* DO bit */)

	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "udp", nameserver)
	if err != nil {
		return nil, false, err
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Prefix with 2-byte length for TCP-style, but for UDP just send raw
	_, err = conn.Write(msg)
	if err != nil {
		return nil, false, err
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, false, err
	}

	return parseDNSSECResponse(buf[:n])
}

// buildDNSQueryWithEDNS0 builds a DNS query message with EDNS0 OPT record.
// Returns the wire-format message.
func buildDNSQueryWithEDNS0(domain string, qtype uint16, doBit bool) []byte {
	msgID := uint16(0xABCD) // Fixed ID for simplicity

	// Header: ID(2) + Flags(2) + QDCOUNT(2) + ANCOUNT(2) + NSCOUNT(2) + ARCOUNT(2)
	header := make([]byte, 12)
	binary.BigEndian.PutUint16(header[0:2], msgID)
	// Flags: RD=1 (recursion desired), QR=0 (query)
	binary.BigEndian.PutUint16(header[2:4], 0x0100)
	binary.BigEndian.PutUint16(header[4:6], 1) // QDCOUNT = 1

	// Question section
	question := encodeDNSName(domain)
	question = append(question, byte(qtype>>8), byte(qtype)) // QTYPE
	question = append(question, 0, 1)                        // QCLASS = IN

	// ARCOUNT will be 1 if EDNS0 is added
	questionLen := len(question)

	if doBit {
		binary.BigEndian.PutUint16(header[10:12], 1) // ARCOUNT = 1 (EDNS0 OPT)
	}

	// EDNS0 OPT record
	// NAME: root (0), TYPE: OPT(41), UDP: 4096, TTL: DO bit in upper 8 bits, RDLEN: 0
	edns0 := []byte{
		0x00,       // NAME: root
		0x00, 0x29, // TYPE: OPT (41)
		0x10, 0x00, // UDP payload: 4096
	}
	if doBit {
		edns0 = append(edns0, 0x80, 0x00, 0x00, 0x00) // DO=1 (0x80 in first TTL byte), RCODE=0, VERSION=0
	} else {
		edns0 = append(edns0, 0x00, 0x00, 0x00, 0x00)
	}
	edns0 = append(edns0, 0x00, 0x00) // RDLEN = 0

	msg := make([]byte, 0, 12+questionLen+len(edns0))
	msg = append(msg, header...)
	msg = append(msg, question...)
	msg = append(msg, edns0...)

	return msg
}

// parseDNSSECResponse parses a DNS response and extracts RRSIG records and AD flag.
func parseDNSSECResponse(msg []byte) (rrsigRecords []string, adFlag bool, err error) {
	if len(msg) < 12 {
		return nil, false, fmt.Errorf("DNS response too short")
	}

	// Parse header
	// flags := binary.BigEndian.Uint16(msg[2:4])
	anCount := int(binary.BigEndian.Uint16(msg[6:8]))
	arCount := int(binary.BigEndian.Uint16(msg[10:12]))

	// AD flag is bit 5 of the first flags byte (flags[3] & 0x20)
	adFlag = (msg[3] & 0x20) != 0

	// Skip to question section
	offset := 12

	// Skip question section (parse to get past it)
	if offset < len(msg) {
		_, qOffset := skipDNSName(msg, offset)
		if qOffset > offset {
			offset = qOffset + 4 // QTYPE(2) + QCLASS(2)
		}
	}

	// Parse answer section - look for RRSIG records (type 46)
	for i := 0; i < anCount && offset < len(msg); i++ {
		nameStart := offset
		_, nameEnd := skipDNSName(msg, offset)
		if nameEnd <= offset {
			break
		}
		offset = nameEnd

		if offset+10 > len(msg) {
			break
		}

		rtype := binary.BigEndian.Uint16(msg[offset : offset+2])
		// rdlength := binary.BigEndian.Uint16(msg[offset+8 : offset+10])
		offset += 10

		if rtype == 46 { // RRSIG
			// Extract the covered type and signer from RRSIG data
			name := extractRRSIGInfo(msg, nameStart, offset)
			if name != "" {
				rrsigRecords = append(rrsigRecords, name)
			}
		}

		// Skip RDATA (we already advanced past the fixed header)
		// Actually we need rdlength - let's re-read it
	}

	// Also check additional section for RRSIG
	_ = arCount // For now, we have what we need from answer section

	// Re-parse answer section properly for RRSIG
	offset = 12
	// Skip question
	if offset < len(msg) {
		_, qOffset := skipDNSName(msg, offset)
		if qOffset > offset {
			offset = qOffset + 4
		}
	}

	for i := 0; i < anCount && offset < len(msg); i++ {
		_, nameEnd := skipDNSName(msg, offset)
		if nameEnd <= offset || nameEnd+10 > len(msg) {
			break
		}
		offset = nameEnd
		rtype := binary.BigEndian.Uint16(msg[offset : offset+2])
		rdlength := int(binary.BigEndian.Uint16(msg[offset+8 : offset+10]))
		offset += 10

		if rtype == 46 && rdlength > 0 && offset+rdlength <= len(msg) {
			// Parse RRSIG RDATA
			typeCovered := binary.BigEndian.Uint16(msg[offset : offset+2])
			// Parse signer name
			_, signerEnd := skipDNSName(msg, offset+18)
			if signerEnd > offset+18 {
				signer := decodeDNSName(msg, offset+18, signerEnd)
				typeName := dnsTypeToString(typeCovered)
				rrsigRecords = append(rrsigRecords, fmt.Sprintf("RRSIG %s signed by %s", typeName, signer))
			}
		}

		offset += rdlength
	}

	return rrsigRecords, adFlag, nil
}

// extractRRSIGInfo extracts human-readable info from an RRSIG record
func extractRRSIGInfo(msg []byte, nameStart int, rdataStart int) string {
	if nameStart+10 > len(msg) {
		return ""
	}
	_, nameEnd := skipDNSName(msg, nameStart)
	if nameEnd <= nameStart || nameEnd+10 > len(msg) {
		return ""
	}
	// rdlength at rdataStart
	rdlength := int(binary.BigEndian.Uint16(msg[rdataStart+8 : rdataStart+10]))
	if rdlength < 18 {
		return ""
	}
	offset := rdataStart + 10
	typeCovered := binary.BigEndian.Uint16(msg[offset : offset+2])
	_, signerEnd := skipDNSName(msg, offset+18)
	if signerEnd > offset+18 {
		signer := decodeDNSName(msg, offset+18, signerEnd)
		return fmt.Sprintf("RRSIG %s by %s", dnsTypeToString(typeCovered), signer)
	}
	return ""
}

// dnsTypeToString converts a DNS type code to a human-readable string
func dnsTypeToString(t uint16) string {
	switch t {
	case 1:
		return "A"
	case 2:
		return "NS"
	case 5:
		return "CNAME"
	case 6:
		return "SOA"
	case 12:
		return "PTR"
	case 15:
		return "MX"
	case 16:
		return "TXT"
	case 28:
		return "AAAA"
	case 33:
		return "SRV"
	case 43:
		return "DS"
	case 46:
		return "RRSIG"
	case 47:
		return "NSEC"
	case 48:
		return "DNSKEY"
	default:
		return fmt.Sprintf("TYPE%d", t)
	}
}

// skipDNSName skips over a DNS-compressed name starting at offset.
// Returns the end offset of the name.
func skipDNSName(msg []byte, offset int) (int, int) {
	start := offset
	for offset < len(msg) {
		if msg[offset] == 0 {
			return start, offset + 1
		}
		if msg[offset]&0xC0 == 0xC0 {
			// Compression pointer
			return start, offset + 2
		}
		labelLen := int(msg[offset])
		offset += 1 + labelLen
	}
	return start, offset
}

// decodeDNSName decodes a DNS name from msg[start:end] into a human-readable string
func decodeDNSName(msg []byte, start, end int) string {
	var parts []string
	offset := start
	for offset < end {
		if offset >= len(msg) {
			break
		}
		if msg[offset] == 0 {
			break
		}
		if msg[offset]&0xC0 == 0xC0 {
			// Follow compression pointer
			ptr := int(binary.BigEndian.Uint16(msg[offset:offset+2])) & 0x3FFF
			if ptr < len(msg) {
				part := decodeDNSName(msg, ptr, findNameEnd(msg, ptr))
				if part != "" {
					parts = append(parts, part)
				}
			}
			break
		}
		labelLen := int(msg[offset])
		offset++
		if offset+labelLen > len(msg) {
			break
		}
		parts = append(parts, string(msg[offset:offset+labelLen]))
		offset += labelLen
	}
	return strings.Join(parts, ".")
}

// findNameEnd finds the end of a DNS name starting at offset
func findNameEnd(msg []byte, offset int) int {
	for offset < len(msg) {
		if msg[offset] == 0 {
			return offset + 1
		}
		if msg[offset]&0xC0 == 0xC0 {
			return offset + 2
		}
		labelLen := int(msg[offset])
		offset += 1 + labelLen
	}
	return offset
}

// encodeDNSName encodes a domain name into DNS wire format
func encodeDNSName(name string) []byte {
	name = strings.TrimSuffix(name, ".")
	parts := strings.Split(name, ".")
	var result []byte
	for _, part := range parts {
		result = append(result, byte(len(part)))
		result = append(result, part...)
	}
	result = append(result, 0) // Root label
	return result
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
