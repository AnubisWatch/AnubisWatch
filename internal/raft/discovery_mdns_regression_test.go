package raft

import (
	"fmt"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestDiscoveryMDNSAdvertisesConfiguredPort(t *testing.T) {
	cases := []struct {
		name, advertise string
		port            int
	}{
		{"default", "127.0.0.1:7946", 7946},
		{"custom_advertise_differs_from_bind", "127.0.0.1:7000", 7000},
		{"IPv6_port", "[::1]:7001", 7001},
		{"hostname_port", "fixture.invalid:7002", 7002},
		{"minimum_port", "127.0.0.1:1", 1},
		{"maximum_port", "127.0.0.1:65535", 65535},
		{"empty_fallback", "", 7946},
		{"malformed_fallback", "bad-address", 7946},
		{"nonnumeric_fallback", "127.0.0.1:bad", 7946},
		{"zero_fallback", "127.0.0.1:0", 7946},
		{"negative_fallback", "127.0.0.1:-1", 7946},
		{"overflow_fallback", "127.0.0.1:65536", 7946},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := NewDiscovery(core.RaftConfig{NodeID: "advertiser", BindAddr: "0.0.0.0:7946", AdvertiseAddr: tc.advertise, ClusterSecret: "audit-fixture-secret"}, newTestDiscoveryLogger())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := d.Stop(); err != nil {
					t.Error(err)
				}
			})
			if d.mdnsServer.port != tc.port {
				t.Fatalf("advertised port: got %d, want %d", d.mdnsServer.port, tc.port)
			}
			receiver := &Discovery{nodeID: "receiver", knownPeers: make(map[string]*DiscoveredPeer), logger: newTestDiscoveryLogger()}
			receiver.handleMDNSDiscovery(&MDNSService{Host: "127.0.0.1", Port: d.mdnsServer.port, TXT: d.mdnsServer.txt})
			peers := receiver.GetPeers()
			if len(peers) != 1 {
				t.Fatalf("expected one peer, got %d", len(peers))
			}
			expected := fmt.Sprintf("127.0.0.1:%d", tc.port)
			if peers[0].Address != expected {
				t.Fatalf("discovered endpoint: got %s, want %s", peers[0].Address, expected)
			}
		})
	}
}
