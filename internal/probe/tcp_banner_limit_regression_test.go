package probe

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func tcpBannerServer(t *testing.T, payload string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		_, _ = io.WriteString(conn, payload)
	}()
	return listener.Addr().String()
}

func TestTCPBannerIsBoundedByMaxBannerSize(t *testing.T) {
	publishValidatorForTest(t, permissiveSSRFValidator())
	checker := NewTCPChecker()
	soul := func(target string) *core.Soul {
		return &core.Soul{
			ID: "tcp-banner-limit", Type: core.CheckTCP, Target: target, Enabled: true,
			Timeout: core.Duration{Duration: 5 * time.Second},
			TCP:     &core.TCPConfig{BannerMatch: "ready"},
		}
	}

	// Control: a normal banner is returned whole.
	control, _ := checker.Judge(context.Background(), soul(tcpBannerServer(t, "ready\n")))
	if control.Status != core.SoulAlive || control.Details.Banner != "ready" {
		t.Fatalf("control failed: status=%s banner=%q msg=%q", control.Status, control.Details.Banner, control.Message)
	}

	for name, payload := range map[string]string{
		"no newline":   "ready" + strings.Repeat("a", 4*maxBannerSize),
		"late newline": "ready" + strings.Repeat("a", 4*maxBannerSize) + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			j, _ := checker.Judge(context.Background(), soul(tcpBannerServer(t, payload)))
			if len(j.Details.Banner) > maxBannerSize {
				t.Fatalf("banner retained %d bytes, limit is %d", len(j.Details.Banner), maxBannerSize)
			}
			if j.Status != core.SoulAlive {
				t.Fatalf("status=%s msg=%q, want alive (banner prefix contains 'ready')", j.Status, j.Message)
			}
		})
	}
}
