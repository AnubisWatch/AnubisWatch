package probe

// Regression coverage for extractRRSIGInfo's RRSIG field offsets (round
// 2026-10-08-rrsig-rdlength-misread).
//
// parseDNSSECResponse's first answer loop advances past the fixed 10-byte
// record header and then calls extractRRSIGInfo(msg, nameStart, rdataStart)
// with rdataStart pointing at the FIRST BYTE OF RDATA. RFC 4034 §3.1 lays
// RRSIG RDATA out as:
//
//	typeCovered(2) algorithm(1) labels(1) originalTTL(4) expiration(4)
//	inception(4) keyTag(2) signerName(variable)
//
// and the record's RDLENGTH octets sit immediately before RDATA, at
// rdataStart-2.
//
// extractRRSIGInfo used to read its "rdlength" at rdataStart+8..+10 — the high
// half of the expiration field — and then base its field offsets at
// rdataStart+10, so it read the covered type from the inception field and the
// signer from 10 bytes past the real signer name. A well-formed record
// therefore produced either an empty result (when the expiration's high octets
// were small enough to fail the rdlength<18 gate) or a record naming the wrong
// covered type and a garbage signer.

import (
	"encoding/binary"
	"strings"
	"testing"
)

// rrsigAnswerFixture is a DNS response carrying exactly one answer record: an
// RRSIG for test.example whose RDATA is complete and well formed.
type rrsigAnswerFixture struct {
	msg        []byte
	nameStart  int // offset of the answer owner name
	rdataStart int // offset of the first RDATA byte
	rdlength   int
	signer     string
}

func buildRRSIGAnswerFixture(expiration uint32) rrsigAnswerFixture {
	signerName := []byte{0x07, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 0x03, 'c', 'o', 'm', 0x00}

	rdata := make([]byte, 0, 18+len(signerName))
	rdata = append(rdata, 0x00, 0x01)             // typeCovered: A
	rdata = append(rdata, 0x08)                   // algorithm
	rdata = append(rdata, 0x02)                   // labels
	rdata = append(rdata, 0x00, 0x00, 0x0E, 0x10) // original TTL = 3600
	rdata = append(rdata,
		byte(expiration>>24), byte(expiration>>16), byte(expiration>>8), byte(expiration))
	rdata = append(rdata, 0x00, 0x00, 0x00, 0x00) // inception
	rdata = append(rdata, 0x12, 0x34)             // key tag
	rdata = append(rdata, signerName...)

	msg := make([]byte, 0, 12+14+4+10+len(rdata))
	msg = append(msg, 0xAB, 0xCD)                         // ID
	msg = append(msg, 0x81, 0x80)                         // QR|RD|RA
	msg = append(msg, 0x00, 0x01)                         // QDCOUNT
	msg = append(msg, 0x00, 0x01)                         // ANCOUNT
	msg = append(msg, 0x00, 0x00, 0x00, 0x00)             // NSCOUNT, ARCOUNT
	msg = append(msg, 4, 't', 'e', 's', 't', 7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 0)
	msg = append(msg, 0x00, 0x01)             // QTYPE A
	msg = append(msg, 0x00, 0x01)             // QCLASS IN

	nameStart := len(msg)
	msg = append(msg, 0xC0, 0x0C)             // answer owner name: pointer to offset 12
	msg = append(msg, 0x00, 46)               // TYPE = RRSIG
	msg = append(msg, 0x00, 0x01)             // CLASS IN
	msg = append(msg, 0x00, 0x00, 0x00, 0x00) // TTL
	msg = append(msg, byte(len(rdata)>>8), byte(len(rdata)))
	msg = append(msg, rdata...)

	return rrsigAnswerFixture{
		msg:        msg,
		nameStart:  nameStart,
		rdataStart: nameStart + 12,
		rdlength:   len(rdata),
		signer:     "example.com",
	}
}

// checkRRSIGFixtureLayout independently walks the fixture the way a correct
// parser would, so a mistake in the fixture cannot masquerade as the defect.
func checkRRSIGFixtureLayout(t *testing.T, f rrsigAnswerFixture) {
	t.Helper()
	if got := int(binary.BigEndian.Uint16(f.msg[f.rdataStart-2 : f.rdataStart])); got != f.rdlength {
		t.Fatalf("fixture error: RDLENGTH at rdataStart-2 = %d, want %d", got, f.rdlength)
	}
	if got := binary.BigEndian.Uint16(f.msg[f.rdataStart : f.rdataStart+2]); got != 1 {
		t.Fatalf("fixture error: typeCovered at rdataStart = %d, want 1 (A)", got)
	}
	signerStart := f.rdataStart + 18
	if f.msg[signerStart] != 0x07 {
		t.Fatalf("fixture error: signer label length at rdataStart+18 = %d, want 7", f.msg[signerStart])
	}
	if !strings.Contains(strings.ToLower(string(f.msg[signerStart:len(f.msg)-1])), "example") {
		t.Fatalf("fixture error: signer bytes %q do not contain example", f.msg[signerStart:len(f.msg)])
	}
}

func TestExtractRRSIGInfoReportsCoveredTypeAndSigner(t *testing.T) {
	for _, tc := range []struct {
		name       string
		expiration uint32
	}{
		{"realistic expiration", 0x6A2B0000},
		{"small expiration", 0x00000000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := buildRRSIGAnswerFixture(tc.expiration)
			checkRRSIGFixtureLayout(t, f)

			got := extractRRSIGInfo(f.msg, f.nameStart, f.rdataStart)
			if got != "RRSIG A by example.com" {
				t.Fatalf("extractRRSIGInfo = %q, want %q", got, "RRSIG A by example.com")
			}
		})
	}
}

func TestExtractRRSIGInfoTruncatedRDATAIsEmpty(t *testing.T) {
	// A record whose RDLENGTH claims more RDATA than the message carries must
	// be rejected, not walked past the end of the buffer.
	f := buildRRSIGAnswerFixture(0x6A2B0000)
	truncated := f.msg[:f.rdataStart+18] // keep the fixed 18 bytes, drop the signer

	if got := extractRRSIGInfo(truncated, f.nameStart, f.rdataStart); got != "" {
		t.Fatalf("extractRRSIGInfo = %q, want empty for RDATA that runs past the message", got)
	}
}

func TestParseDNSSECResponse_ExtractsSignerForBothExpirations(t *testing.T) {
	// The same well-formed record must yield a signer-bearing record whatever
	// its expiration timestamp is; the old code silently produced no record
	// from the first parse pass when the expiration's high octets were small.
	for _, tc := range []struct {
		name       string
		expiration uint32
	}{
		{"realistic expiration", 0x6A2B0000},
		{"small expiration", 0x00000000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := buildRRSIGAnswerFixture(tc.expiration)
			records, _, err := parseDNSSECResponse(f.msg)
			if err != nil {
				t.Fatalf("parseDNSSECResponse: %v", err)
			}
			found := false
			for _, rec := range records {
				if strings.Contains(rec, "example.com") && strings.Contains(rec, "RRSIG A") {
					found = true
				}
			}
			if !found {
				t.Fatalf("records %q do not identify an RRSIG covering A signed by example.com", records)
			}
		})
	}
}
