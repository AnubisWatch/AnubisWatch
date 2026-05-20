package main

import (
	"os"
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

func TestParseServeOptions_AllFlags(t *testing.T) {
	opts := parseServeOptions([]string{
		"anubis", "serve",
		"--single",
		"--cluster=true",
		"--join=192.168.1.1:7001",
		"--node-name=test-node",
		"--region=us-east",
		"--bind=0.0.0.0:7000",
		"--advertise-addr=10.0.0.1:7000",
	})

	if !opts.SingleNode {
		t.Error("expected SingleNode to be true")
	}
	if !opts.Cluster {
		t.Error("expected Cluster to be true")
	}
	if !opts.ClusterSet {
		t.Error("expected ClusterSet to be true")
	}
	if len(opts.JoinAddrs) != 1 || opts.JoinAddrs[0] != "192.168.1.1:7001" {
		t.Errorf("expected JoinAddrs [192.168.1.1:7001], got %v", opts.JoinAddrs)
	}
	if opts.NodeName != "test-node" {
		t.Errorf("expected NodeName test-node, got %s", opts.NodeName)
	}
	if opts.Region != "us-east" {
		t.Errorf("expected Region us-east, got %s", opts.Region)
	}
	if opts.BindAddr != "0.0.0.0:7000" {
		t.Errorf("expected BindAddr 0.0.0.0:7000, got %s", opts.BindAddr)
	}
	if opts.AdvertiseAddr != "10.0.0.1:7000" {
		t.Errorf("expected AdvertiseAddr 10.0.0.1:7000, got %s", opts.AdvertiseAddr)
	}
}

func TestParseServeOptions_JoinFlag(t *testing.T) {
	opts := parseServeOptions([]string{
		"anubis", "serve",
		"--join", "192.168.1.2:7001",
	})

	if !opts.Cluster {
		t.Error("expected Cluster to be true")
	}
	if !opts.ClusterSet {
		t.Error("expected ClusterSet to be true")
	}
	if len(opts.JoinAddrs) != 1 || opts.JoinAddrs[0] != "192.168.1.2:7001" {
		t.Errorf("expected JoinAddrs [192.168.1.2:7001], got %v", opts.JoinAddrs)
	}
}

func TestParseServeOptions_MultipleJoinFlags(t *testing.T) {
	opts := parseServeOptions([]string{
		"anubis", "serve",
		"--join=192.168.1.1:7001",
		"--join=192.168.1.2:7002",
	})

	if len(opts.JoinAddrs) != 2 {
		t.Errorf("expected 2 JoinAddrs, got %d", len(opts.JoinAddrs))
	}
}

func TestParseServeOptions_NodeIdFlag(t *testing.T) {
	opts := parseServeOptions([]string{
		"anubis", "serve",
		"--node-id=custom-node-id",
	})

	if opts.NodeName != "custom-node-id" {
		t.Errorf("expected NodeName custom-node-id, got %s", opts.NodeName)
	}
}

func TestParseServeOptions_BindAddrFlags(t *testing.T) {
	// Test --bind flag with separate value
	opts := parseServeOptions([]string{
		"anubis", "serve",
		"--bind", "127.0.0.1:7000",
	})

	if opts.BindAddr != "127.0.0.1:7000" {
		t.Errorf("expected BindAddr 127.0.0.1:7000, got %s", opts.BindAddr)
	}

	// Test --bind-addr= flag
	opts2 := parseServeOptions([]string{
		"anubis", "serve",
		"--bind-addr=127.0.0.1:7001",
	})

	if opts2.BindAddr != "127.0.0.1:7001" {
		t.Errorf("expected BindAddr 127.0.0.1:7001, got %s", opts2.BindAddr)
	}
}

func TestParseServeOptions_AdvertiseAddrFlags(t *testing.T) {
	// Test --advertise-addr flag with separate value
	opts := parseServeOptions([]string{
		"anubis", "serve",
		"--advertise-addr", "10.0.0.1:7000",
	})

	if opts.AdvertiseAddr != "10.0.0.1:7000" {
		t.Errorf("expected AdvertiseAddr 10.0.0.1:7000, got %s", opts.AdvertiseAddr)
	}

	// Test --advertise-addr= flag
	opts2 := parseServeOptions([]string{
		"anubis", "serve",
		"--advertise-addr=10.0.0.2:7001",
	})

	if opts2.AdvertiseAddr != "10.0.0.2:7001" {
		t.Errorf("expected AdvertiseAddr 10.0.0.2:7001, got %s", opts2.AdvertiseAddr)
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

func TestApplyServerOptionOverrides_EnvVars(t *testing.T) {
	// Set environment variables
	os.Setenv("ANUBIS_HTTP_PORT", "9090")
	os.Setenv("ANUBIS_NODE_ID", "env-node-id")
	defer func() {
		os.Unsetenv("ANUBIS_HTTP_PORT")
		os.Unsetenv("ANUBIS_NODE_ID")
	}()

	cfg := &core.Config{
		Server: core.ServerConfig{Port: 8080},
		Storage: core.StorageConfig{
			Path: t.TempDir(),
		},
	}

	opts := ServerOptions{}

	applyServerOptionOverrides(cfg, opts)

	if cfg.Server.Port != 9090 {
		t.Errorf("expected Port 9090 from env var, got %d", cfg.Server.Port)
	}
}

func TestApplyServerOptionOverrides_EnvVarsEmptyOpts(t *testing.T) {
	os.Setenv("ANUBIS_NODE_ID", "env-node-id")
	os.Setenv("ANUBIS_BIND_ADDR", "192.168.1.1:7000")
	defer func() {
		os.Unsetenv("ANUBIS_NODE_ID")
		os.Unsetenv("ANUBIS_BIND_ADDR")
	}()

	cfg := &core.Config{
		Server: core.ServerConfig{Port: 8080},
		Storage: core.StorageConfig{
			Path: t.TempDir(),
		},
	}

	opts := ServerOptions{}

	applyServerOptionOverrides(cfg, opts)

	if cfg.Necropolis.NodeName != "env-node-id" {
		t.Errorf("expected NodeName env-node-id, got %s", cfg.Necropolis.NodeName)
	}
}

func TestApplyServerOptionOverrides_EnvRaftPort(t *testing.T) {
	os.Setenv("ANUBIS_RAFT_PORT", "8000")
	defer func() {
		os.Unsetenv("ANUBIS_RAFT_PORT")
	}()

	cfg := &core.Config{
		Server: core.ServerConfig{Port: 8080},
		Storage: core.StorageConfig{
			Path: t.TempDir(),
		},
	}

	opts := ServerOptions{}

	applyServerOptionOverrides(cfg, opts)

	if cfg.Necropolis.BindAddr != "0.0.0.0:8000" {
		t.Errorf("expected BindAddr 0.0.0.0:8000, got %s", cfg.Necropolis.BindAddr)
	}
}
