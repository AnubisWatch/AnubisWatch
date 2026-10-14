package raft

import "testing"

func regressionGossipPeerFixture() (*Discovery, *DiscoveredPeer, GossipMessage) {
	peer := &DiscoveredPeer{ID: "peer", Address: "127.0.0.1:7001"}
	d := &Discovery{nodeID: "self", knownPeers: map[string]*DiscoveredPeer{"peer": peer}, logger: newTestDiscoveryLogger()}
	return d, peer, GossipMessage{Type: "gossip", NodeID: "self"}
}

// Both real operations start only after both goroutines reach the barrier.
func regressionGossipPeerPair(first, second func()) {
	ready := make(chan struct{}, 2)
	start := make(chan struct{})
	firstDone := make(chan struct{})
	secondDone := make(chan struct{})
	go func() { defer close(firstDone); ready <- struct{}{}; <-start; first() }()
	go func() { defer close(secondDone); ready <- struct{}{}; <-start; second() }()
	<-ready
	<-ready
	close(start)
	<-firstDone
	<-secondDone
}

func TestDiscoveryGossipPeerSynchronization(t *testing.T) {
	t.Run("sequential_control_and_owned_snapshot", func(t *testing.T) {
		d, peer, msg := regressionGossipPeerFixture()
		snapshot := d.GetPeers()
		d.sendGossip(peer, msg)
		d.sendGossip(peer, msg)
		got := d.GetPeers()
		if len(got) != 1 || got[0].GossipCount != 2 || got[0].LastGossip.IsZero() {
			t.Fatalf("sequential sends: %+v", got)
		}
		if snapshot[0].GossipCount != 0 || !snapshot[0].LastGossip.IsZero() {
			t.Fatalf("old snapshot changed: %+v", snapshot)
		}
	})
	t.Run("concurrent_snapshot_and_send", func(t *testing.T) {
		d, _, msg := regressionGossipPeerFixture()
		selected := d.selectGossipPeers(1)
		if len(selected) != 1 {
			t.Fatal("expected one selected peer")
		}
		const iterations = 32
		snapshots := make(chan []DiscoveredPeer, iterations)
		regressionGossipPeerPair(func() {
			for i := 0; i < iterations; i++ {
				snapshots <- d.GetPeers()
			}
		}, func() {
			for i := 0; i < iterations; i++ {
				d.sendGossip(selected[0], msg)
			}
		})
		close(snapshots)
		for snapshot := range snapshots {
			if len(snapshot) != 1 || snapshot[0].GossipCount < 0 || snapshot[0].GossipCount > iterations {
				t.Fatalf("snapshot: %+v", snapshot)
			}
		}
		if got := d.GetPeers()[0].GossipCount; got != iterations {
			t.Fatalf("count: got %d, want %d", got, iterations)
		}
	})
	t.Run("concurrent_mDNS_address_update_and_send", func(t *testing.T) {
		d, _, msg := regressionGossipPeerFixture()
		selected := d.selectGossipPeers(1)
		const iterations = 32
		regressionGossipPeerPair(func() {
			for i := 0; i < iterations; i++ {
				d.sendGossip(selected[0], msg)
			}
		}, func() {
			for i := 0; i < iterations; i++ {
				d.handleMDNSDiscovery(&MDNSService{Host: "127.0.0.1", Port: 7002 + i, TXT: []string{"id=peer"}})
			}
		})
		got := d.GetPeers()[0]
		if got.Address != "127.0.0.1:7033" || got.GossipCount != iterations {
			t.Fatalf("updated peer: %+v", got)
		}
	})
	t.Run("concurrent_senders", func(t *testing.T) {
		d, peer, msg := regressionGossipPeerFixture()
		send := func() {
			for i := 0; i < 32; i++ {
				d.sendGossip(peer, msg)
			}
		}
		regressionGossipPeerPair(send, send)
		if got := d.GetPeers()[0].GossipCount; got != 64 {
			t.Fatalf("count: got %d, want 64", got)
		}
	})
	t.Run("stale_selection_finishes_after_replacement", func(t *testing.T) {
		d, oldPeer, msg := regressionGossipPeerFixture()
		selected := d.selectGossipPeers(1)
		ready := make(chan struct{})
		release := make(chan struct{})
		done := make(chan struct{})
		go func() { defer close(done); close(ready); <-release; d.sendGossip(selected[0], msg) }()
		<-ready
		replacement := &DiscoveredPeer{ID: oldPeer.ID, Address: oldPeer.Address}
		d.peersMu.Lock()
		d.knownPeers[replacement.ID] = replacement
		d.peersMu.Unlock()
		close(release)
		<-done
		got := d.GetPeers()[0]
		if got.GossipCount != 0 || !got.LastGossip.IsZero() {
			t.Fatalf("stale send changed replacement: %+v", got)
		}
		if oldPeer.GossipCount != 1 {
			t.Fatalf("stale send did not finish: count=%d", oldPeer.GossipCount)
		}
	})
	t.Run("invalid_address_does_not_change_tracking", func(t *testing.T) {
		d, peer, msg := regressionGossipPeerFixture()
		peer.Address = "["
		d.sendGossip(peer, msg)
		got := d.GetPeers()[0]
		if got.GossipCount != 0 || !got.LastGossip.IsZero() {
			t.Fatalf("invalid address changed tracking: %+v", got)
		}
	})
	t.Run("empty_peers", func(t *testing.T) {
		d := &Discovery{nodeID: "self", knownPeers: make(map[string]*DiscoveredPeer)}
		if got := d.selectGossipPeers(1); len(got) != 0 {
			t.Fatalf("selected from empty peers: %+v", got)
		}
		if got := d.GetPeers(); len(got) != 0 {
			t.Fatalf("snapshot of empty peers: %+v", got)
		}
	})
}
