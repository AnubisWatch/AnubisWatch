package statuspage

import (
	"fmt"
	"net/http"
	"testing"
)

// Plus-addressing and case used to give one mailbox a fresh per-address
// budget for every spelling.
func TestSubscriptionAddressLimitCoversPlusTagsAndCase(t *testing.T) {
	h, _, mailer := newSubscriptionHandler(t)
	for i := 0; i < addressConfirmLimit+3; i++ {
		email := fmt.Sprintf("Victim+%d@Example.com", i)
		if w := subscribe(h, email, fmt.Sprintf("192.0.2.%d", i+1)); w.Code != http.StatusAccepted {
			t.Fatalf("request %d = %d", i, w.Code)
		}
	}
	if n := len(mailer.all()); n != addressConfirmLimit {
		t.Fatalf("one mailbox got %d confirmation emails, want %d", n, addressConfirmLimit)
	}

	cases := map[string]string{
		"Ops+deploy@Example.COM": "ops@example.com",
		"ops@example.com":        "ops@example.com",
		"+x@example.com":         "+x@example.com",
		"a+b+c@example.com":      "a@example.com",
	}
	for in, want := range cases {
		if got := mailboxKey(in); got != want {
			t.Errorf("mailboxKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// Distinct addresses from distinct IPs are capped per page per hour.
func TestSubscriptionPageHourlyCap(t *testing.T) {
	h, _, mailer := newSubscriptionHandler(t)
	var last int
	for i := 0; i < pageConfirmLimit+1; i++ {
		last = subscribe(h, fmt.Sprintf("u%d@example.com", i), fmt.Sprintf("10.%d.%d.1", i/250, i%250)).Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("request past the page cap = %d", last)
	}
	if n := len(mailer.all()); n != pageConfirmLimit {
		t.Fatalf("page sent %d confirmation emails, want %d", n, pageConfirmLimit)
	}
}
