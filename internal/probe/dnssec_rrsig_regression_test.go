package probe

// Regression coverage for the DNSSEC RRSIG parser bounds (round
// 2026-10-08-dnssec-rrsig-oob).
//
// queryDNSSEC reads a nameserver's datagram into a 4096-byte buffer and hands
// parseDNSSECResponse the slice buf[:n]. For every response shorter than 4096
// bytes len(msg) < cap(msg), and Go permits slicing a slice up to its capacity —
// so an unchecked msg[a:b] silently reads stale buffer bytes instead of failing.
// At exactly n == 4096 len and cap coincide and the same unchecked slice panics.
//
// The probe engine runs Judge on a bare goroutine with no recover
// (internal/probe/engine.go), so either consequence ends with the monitoring
// process dying on a malformed nameserver reply. A truncated RRSIG record must
// be reported as "not signed", never crash the worker.

import (
	"context"
	"encoding/binary"
	"net"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

const rrsigType = 46

const rrsigQueryHeader = 12 // ID, flags, QDCOUNT, ANCOUNT, NSCOUNT, ARCOUNT

// appendRRSIGAnswer writes a single answer record claiming rdlength bytes of
// RDATA followed by exactly rdataLen bytes of RDATA.
func appendRRSIGAnswer(buf []byte, rdlength uint16, rdata []byte) []byte {
	buf = append(buf, 0xC0, 0x0C) // answer NAME: compression pointer to offset 12
	buf = append(buf, byte(rrsigType>>8), byte(rrsigType))
	buf = append(buf, 0x00, 0x01) // CLASS IN
	buf = append(buf, 0, 0, 0, 0) // TTL
	buf = append(buf, byte(rdlength>>8), byte(rdlength))
	return append(buf, rdata...)
}

// wellFormedRRSIGRDATA returns a complete 18-byte RRSIG fixed field plus a
// signer name, i.e. a record the parser must be able to decode. It returns a
// fresh slice so tests cannot mutate a shared fixture.
func wellFormedRRSIGRDATA() []byte {
	rdata := []byte{
		0x00, 0x01, // type covered: A
		0x08,                   // algorithm
		0x02,                   // labels
		0x00, 0x00, 0x0E, 0x10, // original TTL
		0x00, 0x00, 0x00, 0x00, // expiration
		0x00, 0x00, 0x00, 0x00, // inception
		0x12, 0x34, // key tag
	}
	return append(rdata, 0x07, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 0x03, 'c', 'o', 'm', 0x00)
}

// buildExactCapResponse builds a DNS response with one answer record whose
// declared RDLENGTH may exceed the RDATA actually present, and returns it with
// len == cap so every slice is checked against the bytes really received.
func buildExactCapResponse(t *testing.T, rdlength uint16, rdata []byte) []byte {
	t.Helper()
	buf := make([]byte, 0, rrsigQueryHeader+16+12+len(rdata))
	buf = append(buf, 0xAB, 0xCD)             // ID
	buf = append(buf, 0x81, 0x80)             // QR|RD|RA, AD clear
	buf = append(buf, 0x00, 0x01)             // QDCOUNT
	buf = append(buf, 0x00, 0x01)             // ANCOUNT
	buf = append(buf, 0x00, 0x00, 0x00, 0x00) // NSCOUNT, ARCOUNT
	buf = append(buf, 4, 't', 'e', 's', 't', 7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 0)
	buf = append(buf, 0x00, 0x01) // QTYPE A
	buf = append(buf, 0x00, 0x01) // QCLASS IN
	buf = appendRRSIGAnswer(buf, rdlength, rdata)
	// Pin cap to len: without this the spare capacity reproduces the production
	// "silent over-read" and would hide the panic these tests are guarding.
	return buf[:len(buf):len(buf)]
}

func TestParseDNSSECResponse_TruncatedRRSIGDoesNotReadPastMessage(t *testing.T) {
	// RDLENGTH claims 18 but only 3 RDATA bytes are present. The pre-fix parser
	// read its "rdlength" at rdataStart+8, which is past the end of this
	// message, and panicked on the slice.
	msg := buildExactCapResponse(t, 18, make([]byte, 3))
	records, adFlag, err := parseDNSSECResponse(msg)
	if err != nil {
		t.Fatalf("parseDNSSECResponse: %v", err)
	}
	if adFlag {
		t.Errorf("adFlag = true, want false for an unsigned response")
	}
	if len(records) != 0 {
		t.Errorf("records = %q, want none for a truncated RRSIG", records)
	}
}

func TestParseDNSSECResponse_OneByteRDLENGTHDoesNotPanic(t *testing.T) {
	// RDLENGTH=1 satisfies "rdlength > 0 && offset+rdlength <= len(msg)" while
	// leaving fewer than the 2 bytes the covered-type read needs.
	msg := buildExactCapResponse(t, 1, []byte{0x00})
	if _, _, err := parseDNSSECResponse(msg); err != nil {
		t.Fatalf("parseDNSSECResponse: %v", err)
	}
}

func TestParseDNSSECResponse_WellFormedRRSIGParses(t *testing.T) {
	msg := buildExactCapResponse(t, uint16(len(wellFormedRRSIGRDATA())), wellFormedRRSIGRDATA())
	records, _, err := parseDNSSECResponse(msg)
	if err != nil {
		t.Fatalf("parseDNSSECResponse: %v", err)
	}
	found := false
	for _, rec := range records {
		if strings.Contains(rec, "example.com") {
			found = true
		}
	}
	if !found {
		t.Fatalf("records %q do not mention the signer example.com", records)
	}
}

// buildPaddedRRSIGResponse scales the malformed fixture up to exactly total
// bytes so that len(msg) == cap(msg) == total — the condition under which the
// pre-fix parser panicked in production instead of silently over-reading.
func buildPaddedRRSIGResponse(t *testing.T, total int, rdlength uint16, rdataLen int) []byte {
	t.Helper()
	if total < rrsigQueryHeader+16+12+rdataLen {
		t.Fatalf("fixture error: %d bytes cannot hold the question and record", total)
	}
	msg := make([]byte, total)

	binary.BigEndian.PutUint16(msg[0:2], 0xABCD)
	binary.BigEndian.PutUint16(msg[2:4], 0x8180)
	binary.BigEndian.PutUint16(msg[4:6], 1) // QDCOUNT
	binary.BigEndian.PutUint16(msg[6:8], 1) // ANCOUNT

	// The answer record needs 12 bytes of fixed header plus its RDATA, so the
	// question section must end at exactly this offset.
	questionEnd := total - 12 - rdataLen

	pos := rrsigQueryHeader
	for pos+64 <= questionEnd-1 { // 63-byte labels occupy 64 wire bytes each
		msg[pos] = 63
		for j := 1; j <= 63; j++ {
			msg[pos+j] = 'a'
		}
		pos += 64
	}
	remainder := questionEnd - 1 - pos // one final label plus the root label
	if remainder < 2 {
		t.Fatalf("fixture error: %d bytes left for a label", remainder)
	}
	msg[pos] = byte(remainder - 1)
	for j := 1; j < remainder; j++ {
		msg[pos+j] = 'b'
	}
	pos += remainder
	msg[pos] = 0 // root label
	pos++
	if pos != questionEnd {
		t.Fatalf("fixture error: question ends at %d, want %d", pos, questionEnd)
	}

	base := pos
	msg[base+0] = 0xC0
	msg[base+1] = 0x0C
	binary.BigEndian.PutUint16(msg[base+2:base+4], rrsigType)
	binary.BigEndian.PutUint16(msg[base+4:base+6], 1)
	binary.BigEndian.PutUint32(msg[base+6:base+10], 0)
	binary.BigEndian.PutUint16(msg[base+10:base+12], rdlength)
	for i := 0; i < rdataLen; i++ {
		msg[base+12+i] = 0x00
	}
	// The pre-fix parser treated RDATA bytes [8:10] as its rdlength, so encode
	// the declared rdlength there to reach the deepest unchecked read.
	if rdataLen >= 10 {
		binary.BigEndian.PutUint16(msg[base+12+8:base+12+10], rdlength)
	}
	return msg
}

func TestParseDNSSECResponse_FortyKibiTruncatedRRSIGDoesNotPanic(t *testing.T) {
	// Exactly 4096 bytes: len(msg) == cap(msg), so the slice that used to read
	// stale buffer bytes at shorter lengths panics here instead.
	msg := buildPaddedRRSIGResponse(t, 4096, 18, 10)
	if _, _, err := parseDNSSECResponse(msg); err != nil {
		t.Fatalf("parseDNSSECResponse: %v", err)
	}
}

func TestParseDNSSECResponse_FortyKibiOneByteRDLENGTHDoesNotPanic(t *testing.T) {
	msg := buildPaddedRRSIGResponse(t, 4096, 1, 1)
	if _, _, err := parseDNSSECResponse(msg); err != nil {
		t.Fatalf("parseDNSSECResponse: %v", err)
	}
}

// --- End-to-end: the same malformed datagrams through DNSChecker.Judge ---

// rrsigTestServer answers the DNSSEC probe with a canned response and replies
// NXDOMAIN to every other query (the follow-up resolve()), keeping the test
// free of real network waits.
type rrsigTestServer struct {
	addr string
}

func newRRSIGTestServer(t *testing.T, dnssecResponse []byte) *rrsigTestServer {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	local := conn.LocalAddr().(*net.UDPAddr)
	stop := make(chan struct{})
	go func() {
		buf := make([]byte, 65535)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if n >= 2 && binary.BigEndian.Uint16(buf[0:2]) == 0xABCD {
				if _, err := conn.WriteToUDP(dnssecResponse, from); err != nil {
					return
				}
				continue
			}
			// Echo the query with QR=1 and RCODE=3 so the resolver fails fast
			// instead of waiting out its timeout.
			nx := make([]byte, n)
			copy(nx, buf[:n])
			nx[2] = 0x81 // QR=1, RD=1
			nx[3] = 0x83 // RA=1, RCODE=3
			if _, err := conn.WriteToUDP(nx, from); err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		close(stop)
		_ = conn.Close()
	})
	return &rrsigTestServer{addr: local.String()}
}

func judgeAgainstRRSIGServer(t *testing.T, srv *rrsigTestServer) *core.Judgment {
	t.Helper()
	checker := NewDNSChecker()
	soul := &core.Soul{
		ID:     "rrsig-regression",
		Name:   "RRSIG regression",
		Type:   core.CheckDNS,
		Target: "rrsig.regression.invalid",
		DNS: &core.DNSConfig{
			RecordType:     "A",
			Nameservers:    []string{srv.addr},
			DNSSECValidate: true,
		},
	}
	judgment, err := checker.Judge(context.Background(), soul)
	if err != nil {
		t.Fatalf("Judge returned error: %v", err)
	}
	if judgment == nil {
		t.Fatalf("Judge returned nil judgment")
	}
	return judgment
}

func TestJudge_TruncatedRRSIGDatagramDoesNotPanic(t *testing.T) {
	srv := newRRSIGTestServer(t, buildPaddedRRSIGResponse(t, 4096, 18, 10))
	judgment := judgeAgainstRRSIGServer(t, srv)
	if judgment.Status != core.SoulAlive && judgment.Status != core.SoulDead && judgment.Status != core.SoulDegraded {
		t.Errorf("judgment status = %q, want a recognised soul status", judgment.Status)
	}
}

func TestJudge_OneByteRDLENGTHDatagramDoesNotPanic(t *testing.T) {
	srv := newRRSIGTestServer(t, buildPaddedRRSIGResponse(t, 4096, 1, 1))
	judgment := judgeAgainstRRSIGServer(t, srv)
	if judgment.Status != core.SoulAlive && judgment.Status != core.SoulDead && judgment.Status != core.SoulDegraded {
		t.Errorf("judgment status = %q, want a recognised soul status", judgment.Status)
	}
}

func TestJudge_WellFormedRRSIGControl(t *testing.T) {
	srv := newRRSIGTestServer(t, buildExactCapResponse(t, uint16(len(wellFormedRRSIGRDATA())), wellFormedRRSIGRDATA()))
	judgment := judgeAgainstRRSIGServer(t, srv)
	if judgment.Status != core.SoulAlive && judgment.Status != core.SoulDead && judgment.Status != core.SoulDegraded {
		t.Errorf("judgment status = %q, want a recognised soul status", judgment.Status)
	}
}
