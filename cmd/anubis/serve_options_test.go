package main

import (
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestParseServeOptionsBooleanFlags(t *testing.T) {
	opts := parseServeOptions([]string{"anubis", "serve", "--cluster=false", "--bootstrap=false"})

	if !opts.ClusterSet {
		t.Fatal("expected cluster flag to be recorded")
	}
	if opts.Cluster {
		t.Fatal("expected cluster=false to disable cluster")
	}
	if !opts.BootstrapSet {
		t.Fatal("expected bootstrap flag to be recorded")
	}
	if opts.Bootstrap {
		t.Fatal("expected bootstrap=false to disable bootstrap")
	}
}

func TestParseServeOptionsBootstrapTrueEnablesCluster(t *testing.T) {
	opts := parseServeOptions([]string{"anubis", "serve", "--bootstrap=true"})

	if !opts.Bootstrap || !opts.BootstrapSet {
		t.Fatal("expected bootstrap=true to enable bootstrap")
	}
	if !opts.Cluster || !opts.ClusterSet {
		t.Fatal("expected bootstrap=true to enable cluster mode")
	}
}

func TestApplyServerOptionOverridesBootstrapFalse(t *testing.T) {
	cfg := &core.Config{
		Server: core.ServerConfig{Port: 8080},
		Storage: core.StorageConfig{
			Path: t.TempDir(),
		},
		Necropolis: core.NecropolisConfig{
			Enabled: true,
			Raft: core.RaftConfig{
				Bootstrap: true,
			},
		},
	}

	applyServerOptionOverrides(cfg, ServerOptions{
		Cluster:      true,
		ClusterSet:   true,
		Bootstrap:    false,
		BootstrapSet: true,
	})

	if !cfg.Necropolis.Enabled {
		t.Fatal("expected cluster to remain enabled")
	}
	if cfg.Necropolis.Raft.Bootstrap {
		t.Fatal("expected bootstrap=false to override config bootstrap")
	}
}
