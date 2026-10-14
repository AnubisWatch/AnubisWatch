package probe

// Regression coverage for encodeDNSName's label-length octet (round
// 2026-10-08-dns-label-length-desync).
//
// encodeDNSName writes a 6-bit length octet before each label. It used to emit
// byte(len(part)&0x3f) while appending len(part) bytes untruncated, so a label
// longer than 63 bytes declared a shorter length than the bytes that followed:
// a 70-byte label declared 6, and a 64-byte label declared 0x00 — the root
// terminator — which cut the name off in mid-message.
//
// buildDNSQueryWithEDNS0 sends that buffer straight to the configured
// nameserver for the DNSSEC probe, so an over-long label produced a query no
// server could parse. Every reader of the buffer (findNameEnd, skipDNSName,
// decodeDNSNameDepth) trusts the declared length and would walk into the middle
// of the label rather than onto the next length octet.

import (
	"strings"
	"testing"
)

// wireName is the result of walking a wire-format name the way a DNS parser
// does: read a length octet, consume that many label bytes, repeat until the
// root octet (0x00).
type wireName struct {
	labels   []string
	consumed int
	complete bool
}

func walkWireName(msg []byte) wireName {
	var w wireName
	for w.consumed < len(msg) {
		l := int(msg[w.consumed])
		if l == 0 {
			w.consumed++ // root label
			w.complete = true
			return w
		}
		if w.consumed+1+l > len(msg) {
			return w // length octet promises more bytes than the buffer holds
		}
		w.labels = append(w.labels, string(msg[w.consumed+1:w.consumed+1+l]))
		w.consumed += 1 + l
	}
	return w
}

func TestEncodeDNSName_WireNameIsWalkable(t *testing.T) {
	const name = "proof.example.com"
	msg := encodeDNSName(name)

	w := walkWireName(msg)
	if !w.complete {
		t.Fatalf("name %q does not terminate on its root octet (consumed %d of %d bytes)",
			name, w.consumed, len(msg))
	}
	if w.consumed != len(msg) {
		t.Fatalf("name %q consumed %d bytes, buffer holds %d", name, w.consumed, len(msg))
	}
	want := []string{"proof", "example", "com"}
	if len(w.labels) != len(want) {
		t.Fatalf("name %q decoded %d labels (%q), want %d", name, len(w.labels), w.labels, len(want))
	}
	for i := range want {
		if w.labels[i] != want[i] {
			t.Fatalf("label %d = %q, want %q", i, w.labels[i], want[i])
		}
	}
}

func TestEncodeDNSName_LongLabelStaysWalkable(t *testing.T) {
	long := strings.Repeat("a", 70) // 70 & 0x3f == 6, the old desync
	name := long + ".example.com"
	msg := encodeDNSName(name)

	w := walkWireName(msg)
	if !w.complete {
		t.Fatalf("encoded name for %q is not walkable — the parser left the buffer after %d of %d bytes",
			name, w.consumed, len(msg))
	}
	if w.consumed != len(msg) {
		t.Fatalf("encoded name consumed %d bytes, buffer holds %d", w.consumed, len(msg))
	}
	if len(w.labels) == 0 {
		t.Fatalf("no labels decoded")
	}
	if got := w.labels[0]; got != long[:maxDNSLabelLength] {
		t.Fatalf("first label is %d bytes, want the first %d bytes of the input label",
			len(got), maxDNSLabelLength)
	}
}

func TestEncodeDNSName_DeclaredLengthMatchesEmittedLabel(t *testing.T) {
	for _, n := range []int{1, 62, 63, 64, 70, 128, 200} {
		label := strings.Repeat("a", n)
		msg := encodeDNSName(label + ".com")

		declared := int(msg[0])
		if declared == 0 {
			t.Fatalf("label of %d bytes encoded with a 0x00 length octet, which terminates the name", n)
		}
		if declared > maxDNSLabelLength {
			t.Fatalf("label of %d bytes declared length %d, which the 6-bit field cannot express", n, declared)
		}
		if n <= maxDNSLabelLength {
			if declared != n {
				t.Fatalf("label of %d bytes declared length %d, want %d", n, declared, n)
			}
			continue
		}
		if declared != maxDNSLabelLength {
			t.Fatalf("label of %d bytes declared length %d, want it clamped to %d",
				n, declared, maxDNSLabelLength)
		}
	}
}

func TestEncodeDNSName_TruncationKeepsRootTerminator(t *testing.T) {
	// A single over-long label with no other labels must still end on the
	// root octet, not on truncated label data.
	msg := encodeDNSName(strings.Repeat("b", 200))
	w := walkWireName(msg)
	if !w.complete {
		t.Fatalf("single over-long label does not terminate on its root octet (consumed %d of %d bytes)",
			w.consumed, len(msg))
	}
	if w.consumed != len(msg) {
		t.Fatalf("consumed %d bytes, buffer holds %d", w.consumed, len(msg))
	}
	if len(w.labels) != 1 || len(w.labels[0]) != maxDNSLabelLength {
		t.Fatalf("decoded %q, want one %d-byte label", w.labels, maxDNSLabelLength)
	}
}
