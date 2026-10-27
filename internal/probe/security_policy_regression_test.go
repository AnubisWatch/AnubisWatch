package probe

import (
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"testing"
)

func TestSharedSecurityPolicyClonesEveryInsecureConfig(t *testing.T) {
	soul := &core.Soul{HTTP: &core.HTTPConfig{InsecureSkipVerify: true}, SMTP: &core.SMTPConfig{InsecureSkipVerify: true}, IMAP: &core.IMAPConfig{InsecureSkipVerify: true}, GRPC: &core.GRPCConfig{InsecureSkipVerify: true}, WebSocket: &core.WebSocketConfig{InsecureSkipVerify: true}}
	got := ApplySecurityPolicy(soul, false, nil)
	if got == soul || got.HTTP.InsecureSkipVerify || got.SMTP.InsecureSkipVerify || got.IMAP.InsecureSkipVerify || got.GRPC.InsecureSkipVerify || got.WebSocket.InsecureSkipVerify {
		t.Fatal("insecure TLS escaped shared policy")
	}
	if !soul.HTTP.InsecureSkipVerify || !soul.SMTP.InsecureSkipVerify || !soul.IMAP.InsecureSkipVerify || !soul.GRPC.InsecureSkipVerify || !soul.WebSocket.InsecureSkipVerify {
		t.Fatal("caller state mutated")
	}
	if ApplySecurityPolicy(soul, true, nil) != soul {
		t.Fatal("master override not honored")
	}
	if ApplySecurityPolicy(nil, false, nil) != nil {
		t.Fatal("nil soul changed")
	}
	secure := &core.Soul{HTTP: &core.HTTPConfig{}}
	if ApplySecurityPolicy(secure, false, nil) != secure {
		t.Fatal("secure control needlessly copied")
	}
}
