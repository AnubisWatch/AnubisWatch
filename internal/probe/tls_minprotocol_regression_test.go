package probe

import (
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// A configured TLS protocol floor must be enforceable. parseTLSVersion returns 0
// for anything it does not recognize, and the tls_version assertion then reads
// `state.Version >= 0` -- always true -- so an unrecognized floor silently
// disabled the check while the operator believed it was enforced. Validate now
// refuses it, and the engine (engine.go:423, :534) and journey executor
// (executor.go:377) surface that error to the operator.

func validateTLSMinProtocol(t *testing.T, minProtocol string, tlsConfig *core.TLSConfig) error {
	t.Helper()

	soul := &core.Soul{
		Name:   "tls-minprotocol-soul",
		Type:   core.CheckTLS,
		Target: "1.1.1.1:443",
		TLS:    tlsConfig,
	}
	return (&TLSChecker{}).Validate(soul)
}

func TestTLSValidateRefusesUnparseableMinProtocol(t *testing.T) {
	for _, value := range []string{"TLS 1.2", "TLS9", "banana", "TLS1.4"} {
		t.Run(value, func(t *testing.T) {
			err := validateTLSMinProtocol(t, value, &core.TLSConfig{MinProtocol: value})
			if err == nil {
				t.Fatalf("Validate accepted MinProtocol %q, which resolves to 0 and makes the "+
					"tls_version assertion unable to fail", value)
			}
		})
	}
}

// TestTLSValidateAcceptsSupportedMinProtocols is the neighbouring control: the
// shapes operators actually write keep working, including a bare "1.2", and a
// soul with no TLS block or no floor is unaffected.
func TestTLSValidateAcceptsSupportedMinProtocols(t *testing.T) {
	for _, value := range []string{"TLS1.2", "TLS1.3", "1.2", "1.3", "12", "13", "tls1.3"} {
		t.Run(value, func(t *testing.T) {
			if err := validateTLSMinProtocol(t, value, &core.TLSConfig{MinProtocol: value}); err != nil {
				t.Errorf("Validate rejected the supported MinProtocol %q: %v", value, err)
			}
		})
	}

	if err := validateTLSMinProtocol(t, "", &core.TLSConfig{}); err != nil {
		t.Errorf("Validate rejected an empty MinProtocol: %v", err)
	}
	if err := validateTLSMinProtocol(t, "", nil); err != nil {
		t.Errorf("Validate rejected a soul without a TLS block: %v", err)
	}
}
