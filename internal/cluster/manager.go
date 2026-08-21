package cluster

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/raft"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

// buildTLSPeerConfig builds a tls.Config for peer-to-peer TLS from Raft config.
// If no TLS cert/key are configured, it returns nil (plaintext fallback with a warning).
// If RequireClientCert is set, mutual TLS (mTLS) is enabled for peer verification.
func buildTLSPeerConfig(cfg *core.TLSPeerConfig) (*tls.Config, error) {
	if cfg == nil || cfg.CertFile == "" || cfg.KeyFile == "" {
		return nil, nil
	}

	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load peer TLS certificate: %w", err)
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
	}

	// Optionally verify peer certificates with a custom CA
	if cfg.CAFile != "" {
		caBytes, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read TLS CA file: %w", err)
		}
		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caBytes) {
			return nil, fmt.Errorf("failed to parse TLS CA certificates")
		}
		tlsCfg.RootCAs = caPool

		if cfg.VerifyPeers {
			tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
			tlsCfg.ClientCAs = caPool
		} else if cfg.RequireClientCert {
			tlsCfg.ClientAuth = tls.RequireAnyClientCert
		}
	} else if cfg.RequireClientCert || cfg.VerifyPeers {
		// No CA provided but mTLS required — require client certs without verification
		tlsCfg.ClientAuth = tls.RequireAnyClientCert
	}

	return tlsCfg, nil
}

// Manager handles cluster coordination
type Manager struct {
	mu            sync.RWMutex
	necroConfig   core.NecropolisConfig
	config        core.RaftConfig
	node          *raft.Node
	db            *storage.CobaltDB
	logStore      *storage.CobaltDBLogStore
	snapshotStore *storage.CobaltDBSnapshotStore
	stableStore   *storage.CobaltDBStableStore
	fsm           *raft.StorageFSM
	logger        *slog.Logger
	isClustered   bool
	stopped       bool // True when Stop() has been called

	// Distribution
	distributor *Distributor

	// Discovery
	discovery *raft.Discovery
}

// NewManager creates a new cluster manager
func NewManager(cfg core.NecropolisConfig, db *storage.CobaltDB, logger *slog.Logger) (*Manager, error) {
	raftCfg := cfg.Raft
	// K9: propagate the cluster HMAC secret from NecropolisConfig to
	// the Raft config so the discovery layer can use it. The
	// NecropolisConfig is the canonical source (env override, YAML
	// key, CLI flag all flow into it); the RaftConfig mirror exists
	// so the raft package doesn't have to depend on NecropolisConfig.
	raftCfg.ClusterSecret = cfg.ClusterSecret

	m := &Manager{
		necroConfig: cfg,
		config:      raftCfg,
		db:          db,
		logger:      logger.With("component", "cluster"),
		// SingleNode mode: explicit single-node operation (self-elected leader)
		// Standalone mode: no clustering at all
		isClustered: cfg.Enabled || cfg.Raft.Bootstrap || len(cfg.Raft.Peers) > 0,
	}

	// If SingleNode is set, we're effectively clustered (for leader election)
	// but don't need peer connections
	if cfg.SingleNode && !m.isClustered {
		m.logger.Info("SingleNode mode enabled - node will self-elect as leader")
		m.isClustered = true
	}

	// Create Raft storage components
	m.logStore = storage.NewCobaltDBLogStore(db)
	m.snapshotStore = storage.NewCobaltDBSnapshotStore(db)
	m.stableStore = storage.NewCobaltDBStableStore(db)

	// Create FSM backed by storage
	m.fsm = raft.NewStorageFSM(db)

	return m, nil
}

// Start initializes and starts the Raft node
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isClustered {
		m.logger.Info("running in standalone mode")
		return nil
	}

	m.logger.Info("starting Raft node", "node_id", m.config.NodeID, "bind_addr", m.config.BindAddr)

	// Build TLS config for peer-to-peer communication (nil if TLS not configured)
	tlsCfg, err := buildTLSPeerConfig(m.config.TLS)
	if err != nil {
		return fmt.Errorf("failed to build TLS config: %w", err)
	}
	if tlsCfg != nil {
		m.logger.Info("Raft peer TLS enabled", "min_version", "TLS 1.2", "verify_peers", m.config.TLS.VerifyPeers)
	} else if m.necroConfig.ClusterSecret != "" {
		m.logger.Warn("Raft inter-node auth uses only pre-shared key (no TLS); specify tls.cert_file to enable mTLS")
	}

	// Create TCP transport with optional TLS
	transport, err := raft.NewTCPTransport(m.config.BindAddr, m.config.AdvertiseAddr, tlsCfg, m.logger)
	if err != nil {
		return fmt.Errorf("failed to create transport: %w", err)
	}

	// Create Raft node with durable stable store for term/vote/etc.
	node, err := raft.NewNodeWithStableStore(m.config, m.logStore, m.stableStore, m.snapshotStore, m.fsm, m.logger)
	if err != nil {
		return fmt.Errorf("failed to create Raft node: %w", err)
	}

	// Set the transport on the node
	node.SetTransport(transport)
	m.node = node

	// Start Raft node (this also starts the transport)
	if err := node.Start(); err != nil {
		return fmt.Errorf("failed to start Raft node: %w", err)
	}

	m.logger.Info("Raft node started")

	// Initialize and start discovery service
	if cfg := m.necroConfig.Discovery; cfg.Mode != "" && cfg.Mode != "manual" {
		disc, err := raft.NewDiscovery(m.config, m.logger)
		if err != nil {
			return fmt.Errorf("failed to create discovery service: %w", err)
		} else {
			// Wire peer discovery callbacks to Raft node
			// Use atomic stopped flag to avoid race with Stop()
			disc.RegisterPeerCallback(
				func(peer core.RaftPeer) {
					m.mu.RLock()
					defer m.mu.RUnlock()
					// Check both node and stopped flag under read lock
					if !m.stopped && m.node != nil {
						m.logger.Info("peer discovered", "id", peer.ID, "addr", peer.Address)
						if err := m.node.AddPeer(peer); err != nil {
							m.logger.Warn("failed to add discovered peer", "id", peer.ID, "err", err)
						}
					}
				},
				func(nodeID string) {
					m.mu.RLock()
					defer m.mu.RUnlock()
					// Check both node and stopped flag under read lock
					if !m.stopped && m.node != nil {
						m.logger.Info("peer lost", "id", nodeID)
						if err := m.node.RemovePeer(nodeID); err != nil {
							m.logger.Warn("failed to remove lost peer", "id", nodeID, "err", err)
						}
					}
				},
			)

			if err := disc.Start(); err != nil {
				return fmt.Errorf("failed to start discovery service: %w", err)
			}
			m.discovery = disc
			m.logger.Info("auto-discovery started", "mode", cfg.Mode)
		}
	}

	// Initialize distributor
	strategy := StrategyLoadBased
	m.distributor = NewDistributor(m.config.NodeID, m.config.Region, strategy, m.logger)

	// Register self
	m.distributor.RegisterNode(m.config.NodeID, m.config.Region)

	// Register peers
	for _, peer := range m.config.Peers {
		m.distributor.RegisterNode(peer.ID, peer.Region)
	}

	// Start distributor
	m.distributor.Start()

	m.logger.Info("Cluster distributor started", "strategy", strategy.String())
	return nil
}

// Stop gracefully shuts down the Raft node
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	// Mark as stopped to prevent callbacks from accessing node
	m.stopped = true
	if m.node != nil {
		m.logger.Info("stopping Raft node")
		if err := m.node.Stop(); err != nil {
			m.logger.Warn("failed to stop Raft node", "err", err)
		}
	}
	m.mu.Unlock()

	// Stop other components outside of main lock to avoid deadlock
	if m.distributor != nil {
		m.logger.Info("stopping distributor")
		m.distributor.Stop()
	}

	if m.discovery != nil {
		m.logger.Info("stopping discovery")
		if err := m.discovery.Stop(); err != nil {
			m.logger.Warn("failed to stop discovery", "err", err)
		}
	}

	return nil
}

// ApplyMutation submits an FSM command through Raft consensus. Returns
// ErrNotLeader if this is not the leader and the cluster supports forwarding,
// or nil when the command is committed and applied successfully. A nil node
// (standalone mode) returns ErrNotLeader so callers can fall back to a
// local write.
func (m *Manager) ApplyMutation(cmd core.FSMCommand, timeout time.Duration) (uint64, uint64, error) {
	m.mu.RLock()
	node := m.node
	m.mu.RUnlock()

	if node == nil {
		return 0, 0, &core.RaftError{Code: core.ErrNotLeader, Message: "standalone node: write locally"}
	}
	if node.State() != core.StateLeader {
		return 0, 0, &core.RaftError{
			Code:    core.ErrNotLeader,
			Message: "not leader",
			NodeID:  node.Leader(),
		}
	}
	index, term, _, err := node.Apply(cmd, timeout)
	return index, term, err
}

// IsLeader returns true if this node is the Raft leader
func (m *Manager) IsLeader() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.node == nil {
		return false
	}

	return m.node.State() == core.StateLeader
}

// Leader returns the current leader's ID
func (m *Manager) Leader() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.node == nil {
		return ""
	}

	return m.node.Leader()
}

// IsClustered returns true if cluster mode is enabled
func (m *Manager) IsClustered() bool {
	return m.isClustered
}

// GetStatus returns cluster status information
func (m *Manager) GetStatus() *ClusterStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := &ClusterStatus{
		IsClustered: m.isClustered,
		NodeID:      m.config.NodeID,
	}

	if m.node != nil {
		status.State = m.node.State().String()
		status.Leader = m.node.Leader()
		status.Term = m.node.CurrentTerm()
		status.PeerCount = len(m.node.Peers())
		status.CommitIndex = m.node.CommitIndex()
	}

	return status
}

// GetDiscoveredPeers returns peers discovered via mDNS/gossip
func (m *Manager) GetDiscoveredPeers() []raft.DiscoveredPeer {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.discovery == nil {
		return nil
	}
	return m.discovery.GetPeers()
}

// ClusterStatus contains cluster status information
type ClusterStatus struct {
	IsClustered bool   `json:"is_clustered"`
	NodeID      string `json:"node_id"`
	State       string `json:"state,omitempty"`
	Leader      string `json:"leader,omitempty"`
	Term        uint64 `json:"term,omitempty"`
	PeerCount   int    `json:"peer_count,omitempty"`
	CommitIndex uint64 `json:"commit_index,omitempty"`
}
