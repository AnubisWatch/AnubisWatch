package main

import (
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestSingleModeClearsConfiguredConsensus(t *testing.T) {
	for _, mode := range []string{"join", "single-node", "all", "standalone"} {
		t.Run(mode, func(t *testing.T) {
			var cfg core.Config
			if mode == "join" || mode == "all" {
				cfg.Necropolis.Raft.Join = []string{"127.0.0.1:17946"}
			}
			if mode == "single-node" || mode == "all" {
				cfg.Necropolis.SingleNode = true
			}
			if mode == "all" {
				cfg.Necropolis.Enabled = true
				cfg.Necropolis.Raft.Bootstrap = true
				cfg.Necropolis.Raft.Peers = []core.RaftPeer{{ID: "peer", Address: "127.0.0.1:17947"}}
			}
			for attempt := 0; attempt < 2; attempt++ {
				applyServerOptionOverrides(&cfg, ServerOptions{SingleNode: true})
				if cfg.Necropolis.Enabled || cfg.Necropolis.SingleNode || cfg.Necropolis.Raft.Bootstrap || len(cfg.Necropolis.Raft.Peers) != 0 || len(cfg.Necropolis.Raft.Join) != 0 {
					t.Fatalf("--single retained configured consensus: %+v", cfg.Necropolis)
				}
			}
		})
	}
}

func TestSingleModePreservesExplicitClusterOverride(t *testing.T) {
	var cfg core.Config
	cfg.Necropolis.Raft.Join = []string{"old-peer:7946"}
	applyEnvOverrides(&cfg, ServerOptions{SingleNode: true, ClusterSet: true, Cluster: true, JoinAddrs: []string{"new-peer:7946"}})
	if !cfg.Necropolis.Enabled || len(cfg.Necropolis.Raft.Join) != 1 || cfg.Necropolis.Raft.Join[0] != "new-peer:7946" {
		t.Fatalf("explicit cluster flags should apply after --single clears saved config: %+v", cfg.Necropolis)
	}
}
