package probe

import (
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func tcpCancellationServer(t *testing.T, afterPayload func(), response string) (string, <-chan error) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	done := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			done <- acceptErr
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(12 * time.Second))

		payload := make([]byte, len("PING"))
		if _, readErr := io.ReadFull(conn, payload); readErr != nil {
			done <- fmt.Errorf("read payload: %w", readErr)
			return
		}
		if string(payload) != "PING" {
			done <- fmt.Errorf("payload = %q, want PING", payload)
			return
		}
		if afterPayload != nil {
			afterPayload()
		}
		if response != "" {
			if _, writeErr := io.WriteString(conn, response); writeErr != nil {
				done <- fmt.Errorf("write response: %w", writeErr)
				return
			}
		}
		_, _ = io.Copy(io.Discard, conn)
		done <- nil
	}()
	return listener.Addr().String(), done
}

func tcpCancellationSoul(target string) *core.Soul {
	return &core.Soul{
		ID:      "tcp-cancel-regression",
		Name:    "TCP cancellation regression",
		Type:    core.CheckTCP,
		Target:  target,
		Enabled: true,
		Timeout: core.Duration{Duration: 10 * time.Second},
		TCP: &core.TCPConfig{
			Send:        "PING",
			BannerMatch: "ready",
		},
	}
}

func waitForTCPCancellationServer(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("server: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not finish after checker returned")
	}
}

func TestTCPBannerReadHonorsContextCancellation(t *testing.T) {
	publishValidatorForTest(t, permissiveSSRFValidator())
	checker := NewTCPChecker()

	controlAddr, controlDone := tcpCancellationServer(t, nil, "ready\n")
	controlStart := time.Now()
	control, _ := checker.Judge(context.Background(), tcpCancellationSoul(controlAddr))
	controlElapsed := time.Since(controlStart)
	waitForTCPCancellationServer(t, controlDone)
	if control.Status != core.SoulAlive || controlElapsed >= time.Second {
		t.Fatalf("control failed: status=%s elapsed=%s message=%q", control.Status, controlElapsed, control.Message)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancelAddr, cancelDone := tcpCancellationServer(t, cancel, "")
	start := time.Now()
	judgment, _ := checker.Judge(ctx, tcpCancellationSoul(cancelAddr))
	elapsed := time.Since(start)
	waitForTCPCancellationServer(t, cancelDone)
	if elapsed >= time.Second {
		t.Fatalf("TCP banner read ignored caller cancellation for %s; status=%s message=%q", elapsed, judgment.Status, judgment.Message)
	}
}
