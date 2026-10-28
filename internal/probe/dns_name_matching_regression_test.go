package probe

import (
	"context"
	"net"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"golang.org/x/net/dns/dnsmessage"
)

func dnsNameMatchingServer(t *testing.T, answer string) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	go func() {
		buf := make([]byte, 1500)
		n, addr, readErr := conn.ReadFrom(buf)
		if readErr != nil {
			return
		}
		var parser dnsmessage.Parser
		header, parseErr := parser.Start(buf[:n])
		if parseErr != nil {
			return
		}
		question, parseErr := parser.Question()
		if parseErr != nil {
			return
		}
		builder := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: header.ID, Response: true, Authoritative: true, RecursionAvailable: true})
		builder.EnableCompression()
		if builder.StartQuestions() != nil || builder.Question(question) != nil || builder.StartAnswers() != nil {
			return
		}
		ns, nameErr := dnsmessage.NewName(answer)
		if nameErr != nil {
			return
		}
		if builder.NSResource(dnsmessage.ResourceHeader{Name: question.Name, Class: dnsmessage.ClassINET, TTL: 60}, dnsmessage.NSResource{NS: ns}) != nil {
			return
		}
		response, finishErr := builder.Finish()
		if finishErr == nil {
			_, _ = conn.WriteTo(response, addr)
		}
	}()
	return conn.LocalAddr().String()
}

func dnsNameMatchingSoul(nameservers []string, expected string, propagation bool) *core.Soul {
	return &core.Soul{
		ID:      "dns-name-regression",
		Name:    "DNS name matching regression",
		Type:    core.CheckDNS,
		Target:  "example.test",
		Enabled: true,
		DNS: &core.DNSConfig{
			RecordType:           "NS",
			Nameservers:          nameservers,
			Expected:             []string{expected},
			PropagationCheck:     propagation,
			PropagationThreshold: 100,
		},
	}
}

// dnsTypedMatchingServer answers a single DNS query with the answer built by
// the given callback, which receives the builder and the echoed question.
func dnsTypedMatchingServer(t *testing.T, answer func(*dnsmessage.Builder, dnsmessage.Question) error) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	go func() {
		buf := make([]byte, 1500)
		n, addr, readErr := conn.ReadFrom(buf)
		if readErr != nil {
			return
		}
		var parser dnsmessage.Parser
		header, parseErr := parser.Start(buf[:n])
		if parseErr != nil {
			return
		}
		question, parseErr := parser.Question()
		if parseErr != nil {
			return
		}
		builder := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: header.ID, Response: true, Authoritative: true, RecursionAvailable: true})
		builder.EnableCompression()
		if builder.StartQuestions() != nil || builder.Question(question) != nil || builder.StartAnswers() != nil {
			return
		}
		if answer(&builder, question) != nil {
			return
		}
		response, finishErr := builder.Finish()
		if finishErr == nil {
			_, _ = conn.WriteTo(response, addr)
		}
	}()
	return conn.LocalAddr().String()
}

// dnsMXMatchingServer answers an MX query for the canonical host "mail.example.com.".
func dnsMXMatchingServer(t *testing.T) string {
	t.Helper()
	return dnsTypedMatchingServer(t, func(builder *dnsmessage.Builder, question dnsmessage.Question) error {
		mx, err := dnsmessage.NewName("mail.example.com.")
		if err != nil {
			return err
		}
		return builder.MXResource(
			dnsmessage.ResourceHeader{Name: question.Name, Class: dnsmessage.ClassINET, TTL: 60},
			dnsmessage.MXResource{Pref: 10, MX: mx},
		)
	})
}

// dnsSRVMatchingServer answers an SRV query for the canonical target "mail.example.com.".
func dnsSRVMatchingServer(t *testing.T) string {
	t.Helper()
	return dnsTypedMatchingServer(t, func(builder *dnsmessage.Builder, question dnsmessage.Question) error {
		target, err := dnsmessage.NewName("mail.example.com.")
		if err != nil {
			return err
		}
		return builder.SRVResource(
			dnsmessage.ResourceHeader{Name: question.Name, Class: dnsmessage.ClassINET, TTL: 60},
			dnsmessage.SRVResource{Priority: 10, Weight: 5, Port: 587, Target: target},
		)
	})
}

func dnsTypedNameMatchingSoul(recordType, target string, nameservers []string, expected string) *core.Soul {
	return &core.Soul{
		ID:      "dns-name-regression-" + recordType,
		Name:    "DNS name matching regression",
		Type:    core.CheckDNS,
		Target:  target,
		Enabled: true,
		DNS: &core.DNSConfig{
			RecordType:  recordType,
			Nameservers: nameservers,
			Expected:    []string{expected},
		},
	}
}

// TestDNSMXAndSRVExpectedUseDNSEquality proves that MX and SRV expectations are
// compared with DNS-name equality: a resolver returning the canonical form
// "mail.example.com." must satisfy an expectation of "mail.example.com".
// Before the fix these record types fell through to the exact-match branch and
// produced a false outage.
func TestDNSMXAndSRVExpectedUseDNSEquality(t *testing.T) {
	publishValidatorForTest(t, permissiveSSRFValidator())
	checker := NewDNSChecker()

	for _, tc := range []struct {
		name       string
		recordType string
		target     string
		server     string
		expected   string
	}{
		{name: "MX canonical trailing dot", recordType: "MX", target: "example.test", server: dnsMXMatchingServer(t), expected: "mail.example.com"},
		{name: "MX different case", recordType: "MX", target: "example.test", server: dnsMXMatchingServer(t), expected: "MAIL.EXAMPLE.COM"},
		{name: "SRV canonical trailing dot", recordType: "SRV", target: "_sip._tcp.example.test", server: dnsSRVMatchingServer(t), expected: "mail.example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			judgment, _ := checker.Judge(context.Background(), dnsTypedNameMatchingSoul(tc.recordType, tc.target, []string{tc.server}, tc.expected))
			if judgment.Status != core.SoulAlive {
				t.Fatalf("status=%s message=%q records=%v", judgment.Status, judgment.Message, judgment.Details.ResolvedAddresses)
			}
		})
	}
}

// TestDNSRecordMatchingUsesNameEqualityForMXAndSRV pins the record-value
// comparison rules for MX and SRV, and keeps the non-name record types exact.
func TestDNSRecordMatchingUsesNameEqualityForMXAndSRV(t *testing.T) {
	for _, tc := range []struct {
		recordType string
		actual     string
		expected   string
	}{
		{recordType: "MX", actual: "10 mail.example.com.", expected: "mail.example.com"},
		{recordType: "MX", actual: "10 MAIL.Example.COM.", expected: "mail.example.com"},
		{recordType: "MX", actual: "10 mail.example.com.", expected: "10 mail.example.com."},
		{recordType: "SRV", actual: "mail.example.com.:587 (priority=10, weight=5)", expected: "mail.example.com"},
		{recordType: "SRV", actual: "10 5 587 mail.example.com.", expected: "mail.example.com"},
		{recordType: "SRV", actual: "mail.example.com.:587 (priority=10, weight=5)", expected: "mail.example.com.:587 (priority=10, weight=5)"},
	} {
		if !dnsRecordMatches(tc.recordType, tc.actual, tc.expected) {
			t.Fatalf("%s %q should match expectation %q with DNS-name equality", tc.recordType, tc.actual, tc.expected)
		}
	}
	for _, tc := range []struct {
		recordType string
		actual     string
		expected   string
	}{
		{recordType: "MX", actual: "10 mail.example.com.", expected: "mail.other.example"},
		{recordType: "MX", actual: "10 mail.example.com.", expected: "20 mail.example.com"},
		{recordType: "SRV", actual: "mail.example.com.:587 (priority=10, weight=5)", expected: "mail.other.example"},
		{recordType: "SRV", actual: "mail.example.com.:587 (priority=10, weight=5)", expected: "mail.example.com.:588 (priority=10, weight=5)"},
		{recordType: "SRV", actual: "10 5 587 mail.example.com.", expected: "10 5 588 mail.example.com."},
	} {
		if dnsRecordMatches(tc.recordType, tc.actual, tc.expected) {
			t.Fatalf("%s %q unexpectedly matched expectation %q", tc.recordType, tc.actual, tc.expected)
		}
	}
}

func TestDNSExpectedHostnameRecordsUseDNSEquality(t *testing.T) {
	publishValidatorForTest(t, permissiveSSRFValidator())
	checker := NewDNSChecker()

	for _, tc := range []struct {
		name     string
		answer   string
		expected string
	}{
		{name: "exact control", answer: "Ns1.Example.", expected: "Ns1.Example."},
		{name: "case and root dot", answer: "Ns1.Example.", expected: "ns1.example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			judgment, _ := checker.Judge(context.Background(), dnsNameMatchingSoul([]string{dnsNameMatchingServer(t, tc.answer)}, tc.expected, false))
			if judgment.Status != core.SoulAlive {
				t.Fatalf("status=%s message=%q records=%v", judgment.Status, judgment.Message, judgment.Details.ResolvedAddresses)
			}
		})
	}
}

func TestDNSPropagationUsesDNSNameEquality(t *testing.T) {
	publishValidatorForTest(t, permissiveSSRFValidator())
	servers := []string{dnsNameMatchingServer(t, "Ns1.Example."), dnsNameMatchingServer(t, "NS1.EXAMPLE.")}
	judgment, _ := NewDNSChecker().Judge(context.Background(), dnsNameMatchingSoul(servers, "ns1.example", true))
	if judgment.Status != core.SoulAlive {
		t.Fatalf("status=%s message=%q propagation=%v", judgment.Status, judgment.Message, judgment.Details.PropagationResult)
	}
	for _, server := range servers {
		if !judgment.Details.PropagationResult[server] {
			t.Fatalf("nameserver %s was not counted as propagated: %v", server, judgment.Details.PropagationResult)
		}
	}
}

func TestDNSRecordMatchingKeepsNonHostnameValuesExact(t *testing.T) {
	for _, recordType := range []string{"CNAME", "NS", "PTR"} {
		if !dnsRecordMatches(recordType, "Host.Example.", "host.example") {
			t.Fatalf("%s hostname values should use DNS equality", recordType)
		}
	}
	for _, tc := range []struct{ recordType, actual, expected string }{
		{recordType: "TXT", actual: "Value", expected: "value"},
		{recordType: "A", actual: "192.0.2.1", expected: "192.0.2.01"},
	} {
		if dnsRecordMatches(tc.recordType, tc.actual, tc.expected) {
			t.Fatalf("%s values unexpectedly matched: %q and %q", tc.recordType, tc.actual, tc.expected)
		}
	}
}
