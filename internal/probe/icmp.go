package probe

import (
	"context"
	"fmt"
	"math"
	"net"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// ICMPChecker implements ICMP ping checks
type ICMPChecker struct{}

// NewICMPChecker creates a new ICMP checker
func NewICMPChecker() *ICMPChecker {
	return &ICMPChecker{}
}

// Type returns the protocol identifier
func (c *ICMPChecker) Type() core.CheckType {
	return core.CheckICMP
}

// Validate checks configuration
func (c *ICMPChecker) Validate(soul *core.Soul) error {
	if soul.Target == "" {
		return configError("target", "target host is required")
	}
	return nil
}

// Judge performs the ICMP ping check
func (c *ICMPChecker) Judge(ctx context.Context, soul *core.Soul) (*core.Judgment, error) {
	cfg := soul.ICMP
	if cfg == nil {
		cfg = &core.ICMPConfig{
			Count:    3,
			Interval: core.Duration{Duration: 200 * time.Millisecond},
		}
	}

	count := cfg.Count
	if count == 0 {
		count = 3
	}
	interval := cfg.Interval.Duration
	if interval == 0 {
		interval = 200 * time.Millisecond
	}
	timeout := soul.Timeout.Duration
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	// Resolve target
	addr, err := net.ResolveIPAddr("ip", soul.Target)
	if err != nil {
		return failJudgment(soul, fmt.Errorf("DNS resolution failed: %w", err)), nil
	}

	isIPv6 := addr.IP.To4() == nil
	var network string
	var icmpType icmp.Type

	if isIPv6 {
		network = "ip6:ipv6-icmp"
		icmpType = ipv6.ICMPTypeEchoRequest
	} else {
		network = "ip4:icmp"
		icmpType = ipv4.ICMPTypeEcho
	}

	// Use unprivileged mode if not privileged (UDP instead of raw sockets)
	if !cfg.Privileged {
		if isIPv6 {
			network = "udp6"
		} else {
			network = "udp4"
		}
	}

	// Listen for ICMP packets
	conn, err := icmp.ListenPacket(network, "")
	if err != nil {
		return failJudgment(soul, fmt.Errorf("ICMP listen failed: %w", err)), nil
	}
	defer conn.Close()

	var latencies []float64
	sent := 0
	received := 0

	for i := 0; i < count; i++ {
		select {
		case <-ctx.Done():
			return failJudgment(soul, ctx.Err()), nil
		default:
		}

		// Build ICMP Echo Request
		msg := icmp.Message{
			Type: icmpType,
			Code: 0,
			Body: &icmp.Echo{
				ID:   i,
				Seq:  i,
				Data: []byte("AnubisWatch"),
			},
		}

		msgBytes, err := msg.Marshal(nil)
		if err != nil {
			continue
		}

		start := time.Now()
		sent++

		// Determine destination address
		var dst net.Addr
		if cfg.Privileged {
			dst = addr
		} else {
			if isIPv6 {
				dst = &net.UDPAddr{IP: addr.IP, Zone: addr.Zone}
			} else {
				dst = &net.UDPAddr{IP: addr.IP}
			}
		}

		// Send packet
		if _, err := conn.WriteTo(msgBytes, dst); err != nil {
			continue
		}

		// Wait for reply
		conn.SetReadDeadline(time.Now().Add(timeout))
		reply := make([]byte, 1500)
		n, peer, err := conn.ReadFrom(reply)
		duration := time.Since(start)

		if err != nil {
			// Timeout or error = packet lost
			continue
		}

		// Parse reply
		var parseType icmp.Type
		if isIPv6 {
			parseType = ipv6.ICMPTypeEchoReply
		} else {
			parseType = ipv4.ICMPTypeEchoReply
		}

		parsed, err := icmp.ParseMessage(parseType.Protocol(), reply[:n])
		if err != nil {
			continue
		}

		// Verify it's an echo reply
		if parsed.Type == parseType {
			received++
			latencies = append(latencies, float64(duration.Microseconds())/1000.0)
			_ = peer // Could log peer for debugging
		}

		// Wait between pings (except last)
		if i < count-1 {
			time.Sleep(interval)
		}
	}

	// Calculate statistics
	packetLoss := float64(sent-received) / float64(sent) * 100

	var minLat, maxLat, avgLat, jitter float64
	if len(latencies) > 0 {
		minLat = latencies[0]
		maxLat = latencies[0]
		sum := 0.0
		for _, l := range latencies {
			sum += l
			if l < minLat {
				minLat = l
			}
			if l > maxLat {
				maxLat = l
			}
		}
		avgLat = sum / float64(len(latencies))

		// Calculate jitter (mean deviation between consecutive packets)
		if len(latencies) > 1 {
			var devSum float64
			for i := 1; i < len(latencies); i++ {
				devSum += math.Abs(latencies[i] - latencies[i-1])
			}
			jitter = devSum / float64(len(latencies)-1)
		}
	}

	// Determine status
	status := core.SoulAlive
	message := fmt.Sprintf("ICMP: %d/%d packets received, %.1f%% loss, avg %.2fms",
		received, sent, packetLoss, avgLat)

	// Check failure conditions
	if received == 0 {
		status = core.SoulDead
		message = fmt.Sprintf("ICMP: all %d packets lost - host unreachable", sent)
	} else if cfg.MaxLossPercent > 0 && packetLoss > cfg.MaxLossPercent {
		status = core.SoulDegraded
		message = fmt.Sprintf("ICMP: %.1f%% packet loss exceeds threshold %.1f%%", packetLoss, cfg.MaxLossPercent)
	} else if cfg.Feather.Duration > 0 && time.Duration(avgLat)*time.Millisecond > cfg.Feather.Duration {
		status = core.SoulDegraded
		message = fmt.Sprintf("ICMP: avg latency %.2fms exceeds feather %s", avgLat, cfg.Feather.Duration)
	}

	return &core.Judgment{
		ID:         core.GenerateID(),
		SoulID:     soul.ID,
		Timestamp:  time.Now().UTC(),
		Duration:   time.Duration(avgLat) * time.Millisecond,
		Status:     status,
		StatusCode: 0,
		Message:    message,
		Details: &core.JudgmentDetails{
			PacketsSent:     sent,
			PacketsReceived: received,
			PacketLoss:      packetLoss,
			MinLatency:      minLat,
			AvgLatency:      avgLat,
			MaxLatency:      maxLat,
			Jitter:          jitter,
		},
	}, nil
}
