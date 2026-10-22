package probe

import (
	"net"
	"os"
	"testing"
)

// The probe checkers in this package dial local test servers through the
// process-wide SSRF validator, so the package runs under a policy that allows
// private targets. That policy is process-wide state: a test that changes the
// environment or publishes a stricter validator without restoring it changes
// the meaning of every test that runs after it.
//
// The tests below pin the invariants that keep the package order-independent:
// the package-wide policy is intact, a strict unit validator stays strict
// whatever the environment says, and a published validator is restored.

// TestPackageWideTestPolicyIsIntact is the control for the whole package: the
// fixtures that dial local test servers need private targets to be allowed.
// A test that leaked an environment change or a stricter published validator
// breaks every fixture that runs after it, which only shows up under a
// shuffled order.
func TestPackageWideTestPolicyIsIntact(t *testing.T) {
	if got := os.Getenv("ANUBIS_SSRF_ALLOW_PRIVATE"); got != "1" {
		t.Fatalf("ANUBIS_SSRF_ALLOW_PRIVATE = %q, want %q: a test leaked an environment change", got, "1")
	}
	if !DefaultValidator.Load().AllowPrivate {
		t.Fatal("published DefaultValidator denies private targets: a test leaked a stricter validator")
	}
}

// TestStrictValidatorIgnoresAmbientAllowPrivate is the regression for the
// order-dependent failure: the ambient policy allows private targets, so a
// strict unit validator that derives its policy from the environment would
// silently allow them.
func TestStrictValidatorIgnoresAmbientAllowPrivate(t *testing.T) {
	t.Setenv("ANUBIS_SSRF_ALLOW_PRIVATE", "1")

	v := strictSSRFValidator()
	if v.AllowPrivate {
		t.Fatal("strict validator allows private targets")
	}
	for _, ipStr := range []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "::1"} {
		if !v.isBlockedIP(net.ParseIP(ipStr)) {
			t.Errorf("strict validator allowed private address %s", ipStr)
		}
	}
}

// TestPublishedValidatorIsRestored checks that a test which publishes a
// stricter validator does not leak it into the tests that run after it.
func TestPublishedValidatorIsRestored(t *testing.T) {
	t.Run("publishes a strict validator", func(t *testing.T) {
		publishValidatorForTest(t, strictSSRFValidator())
		if DefaultValidator.Load().AllowPrivate {
			t.Fatal("strict validator was not published")
		}
		if err := ValidateAddress("127.0.0.1:80"); err == nil {
			t.Fatal("published strict validator allowed a loopback address")
		}
	})

	// The cleanup registered by publishValidatorForTest runs when the
	// subtest ends, so the package-wide policy has to be back in place.
	if !DefaultValidator.Load().AllowPrivate {
		t.Fatal("published validator was not restored after the subtest")
	}
	if err := ValidateAddress("127.0.0.1:80"); err != nil {
		t.Fatalf("package-wide policy was not restored: %v", err)
	}
}
