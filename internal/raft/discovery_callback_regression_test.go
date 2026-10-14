package raft

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type regressionDiscoveryCallbackHandler struct{ entered, release chan struct{} }

func (h *regressionDiscoveryCallbackHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *regressionDiscoveryCallbackHandler) Handle(_ context.Context, record slog.Record) error {
	if record.Message == "Received gossip" {
		close(h.entered)
		<-h.release
	}
	return nil
}
func (h *regressionDiscoveryCallbackHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *regressionDiscoveryCallbackHandler) WithGroup(string) slog.Handler      { return h }

func regressionDiscoveryCallbackPair(first, second func()) {
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

func TestDiscoveryCallbackRegistrationSynchronization(t *testing.T) {
	for _, mode := range []string{"gossip", "mDNS"} {
		t.Run("concurrent_registration_"+mode, func(t *testing.T) {
			const iterations = 32
			delivered := make(chan core.RaftPeer, iterations)
			callback := func(peer core.RaftPeer) { delivered <- peer }
			d := &Discovery{nodeID: "self", knownPeers: make(map[string]*DiscoveredPeer), logger: newTestDiscoveryLogger()}
			d.RegisterPeerCallback(callback, nil)
			regressionDiscoveryCallbackPair(func() {
				for i := 0; i < iterations; i++ {
					d.RegisterPeerCallback(callback, nil)
				}
			}, func() {
				for i := 0; i < iterations; i++ {
					id := fmt.Sprintf("peer-%d", i)
					if mode == "gossip" {
						d.handleGossip(&GossipMessage{NodeID: id, Address: "127.0.0.1:7001"})
					} else {
						d.handleMDNSDiscovery(&MDNSService{Host: "127.0.0.1", Port: 7001, TXT: []string{"id=" + id}})
					}
				}
			})
			notified := make(map[string]bool)
			for i := 0; i < iterations; i++ {
				peer := <-delivered
				if notified[peer.ID] {
					t.Fatalf("duplicate notification: %s", peer.ID)
				}
				notified[peer.ID] = true
			}
			if len(d.GetPeers()) != iterations || len(notified) != iterations {
				t.Fatal("missing peer or notification")
			}
		})
	}
	t.Run("replace_callback_before_suspended_gossip_resumes", func(t *testing.T) {
		gate := &regressionDiscoveryCallbackHandler{entered: make(chan struct{}), release: make(chan struct{})}
		delivered := make(chan string, 1)
		d := &Discovery{nodeID: "self", knownPeers: make(map[string]*DiscoveredPeer), logger: slog.New(gate)}
		d.RegisterPeerCallback(func(core.RaftPeer) { delivered <- "old" }, nil)
		done := make(chan struct{})
		go func() { defer close(done); d.handleGossip(&GossipMessage{NodeID: "peer", Address: "127.0.0.1:7001"}) }()
		<-gate.entered
		d.RegisterPeerCallback(func(core.RaftPeer) { delivered <- "new" }, nil)
		close(gate.release)
		<-done
		if got := <-delivered; got != "new" {
			t.Fatalf("callback owner: got %s, want new", got)
		}
	})
	t.Run("nil_callbacks", func(t *testing.T) {
		d := &Discovery{nodeID: "self", knownPeers: make(map[string]*DiscoveredPeer), logger: newTestDiscoveryLogger()}
		d.RegisterPeerCallback(nil, nil)
		d.handleGossip(&GossipMessage{NodeID: "peer", Address: "127.0.0.1:7001"})
		if len(d.GetPeers()) != 1 || d.onPeerDiscovered != nil || d.onPeerLost != nil {
			t.Fatal("nil callback control failed")
		}
	})
	t.Run("callback_can_unregister_itself", func(t *testing.T) {
		d := &Discovery{nodeID: "self", knownPeers: make(map[string]*DiscoveredPeer), logger: newTestDiscoveryLogger()}
		done := make(chan struct{})
		d.RegisterPeerCallback(func(core.RaftPeer) { d.GetPeers(); d.RegisterPeerCallback(nil, nil); close(done) }, nil)
		d.handleGossip(&GossipMessage{NodeID: "peer", Address: "127.0.0.1:7001"})
		<-done
		if d.onPeerDiscovered != nil || d.onPeerLost != nil {
			t.Fatal("callback did not unregister")
		}
	})
}
