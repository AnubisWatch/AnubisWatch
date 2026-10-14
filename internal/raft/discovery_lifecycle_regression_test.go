package raft

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type regressionGossipLifecycleHandler struct{ handle func(slog.Record) }

func (h *regressionGossipLifecycleHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *regressionGossipLifecycleHandler) Handle(_ context.Context, r slog.Record) error {
	h.handle(r)
	return nil
}
func (h *regressionGossipLifecycleHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *regressionGossipLifecycleHandler) WithGroup(string) slog.Handler      { return h }

func regressionGossipLifecycleDiscovery(t *testing.T, logger *slog.Logger) *Discovery {
	t.Helper()
	d, err := NewDiscovery(core.RaftConfig{NodeID: "self", AdvertiseAddr: "127.0.0.1:7000", ClusterSecret: "audit-fixture-secret"}, logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Stop(); err != nil {
			t.Error(err)
		}
	})
	return d
}

func regressionGossipLifecycleSnapshot(d *Discovery) map[string]DiscoveredPeer {
	result := make(map[string]DiscoveredPeer)
	for _, peer := range d.GetPeers() {
		result[peer.ID] = peer
	}
	return result
}

func TestDiscoveryGossipDoesNotPublishAfterStop(t *testing.T) {
	t.Run("active_control", func(t *testing.T) {
		d := regressionGossipLifecycleDiscovery(t, newTestDiscoveryLogger())
		delivered := make(chan core.RaftPeer, 1)
		d.RegisterPeerCallback(func(peer core.RaftPeer) { delivered <- peer }, nil)
		d.handleGossip(&GossipMessage{NodeID: "peer", Address: "127.0.0.1:7001"})
		got := <-delivered
		if got.ID != "peer" || len(d.GetPeers()) != 1 {
			t.Fatalf("active discovery: %s", got.ID)
		}
	})
	fixed := time.Unix(1700000000, 0)
	cases := []struct {
		name    string
		seeds   []DiscoveredPeer
		message *GossipMessage
	}{
		{name: "new_sender", message: &GossipMessage{NodeID: "peer", Address: "127.0.0.1:7001"}},
		{name: "existing_sender", seeds: []DiscoveredPeer{{ID: "peer", Address: "127.0.0.1:7001", LastSeen: fixed}}, message: &GossipMessage{NodeID: "peer", Address: "127.0.0.1:7001"}},
		{name: "embedded_updates", seeds: []DiscoveredPeer{{ID: "sender", Address: "127.0.0.1:7001", LastSeen: fixed}, {ID: "known", Address: "127.0.0.1:7002", LastSeen: fixed}}, message: &GossipMessage{NodeID: "sender", Address: "127.0.0.1:7001", Peers: []GossipPeerInfo{{ID: "known", Address: "127.0.0.1:7003", LastSeen: fixed.Unix() + 1}, {ID: "learned", Address: "127.0.0.1:7004", LastSeen: fixed.Unix() + 1}}}},
	}
	for _, tc := range cases {
		for _, repeatStop := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s_repeat_stop_%t", tc.name, repeatStop), func(t *testing.T) {
				entered := make(chan struct{})
				release := make(chan struct{})
				var once sync.Once
				handler := &regressionGossipLifecycleHandler{handle: func(record slog.Record) {
					if record.Message == "Received gossip" {
						once.Do(func() { close(entered); <-release })
					}
				}}
				d := regressionGossipLifecycleDiscovery(t, slog.New(handler))
				for _, seed := range tc.seeds {
					copyPeer := seed
					d.knownPeers[seed.ID] = &copyPeer
				}
				expected := regressionGossipLifecycleSnapshot(d)
				done := make(chan struct{})
				go func() { defer close(done); d.handleGossip(tc.message) }()
				<-entered
				stopErr := d.Stop()
				if repeatStop && stopErr == nil {
					stopErr = d.Stop()
				}
				close(release)
				<-done
				if stopErr != nil {
					t.Fatal(stopErr)
				}
				// Repeated post-stop arrivals must also leave the retained state alone.
				for i := 0; i < 3; i++ {
					d.handleGossip(tc.message)
				}
				actual := regressionGossipLifecycleSnapshot(d)
				if !reflect.DeepEqual(actual, expected) {
					t.Fatalf("stale gossip changed stopped state: got %+v, want %+v", actual, expected)
				}
			})
		}
	}
	t.Run("cancellation_before_embedded_merge", func(t *testing.T) {
		var d *Discovery
		handler := &regressionGossipLifecycleHandler{handle: func(record slog.Record) {
			if record.Message == "New peer discovered via gossip" {
				d.cancel()
			}
		}}
		d = regressionGossipLifecycleDiscovery(t, slog.New(handler))
		d.handleGossip(&GossipMessage{NodeID: "sender", Address: "127.0.0.1:7001", Peers: []GossipPeerInfo{{ID: "learned", Address: "127.0.0.1:7002"}}})
		actual := regressionGossipLifecycleSnapshot(d)
		if d.ctx.Err() != context.Canceled || len(actual) != 1 || actual["sender"].ID != "sender" {
			t.Fatalf("merge after cancellation: %+v", actual)
		}
	})
	t.Run("nil_context_fixture_control", func(t *testing.T) {
		d := &Discovery{nodeID: "self", knownPeers: make(map[string]*DiscoveredPeer), logger: newTestDiscoveryLogger()}
		d.handleGossip(&GossipMessage{NodeID: "peer", Address: "127.0.0.1:7001"})
		if len(d.GetPeers()) != 1 {
			t.Fatal("context-less fixture must remain usable")
		}
	})
}
