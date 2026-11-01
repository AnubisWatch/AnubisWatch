package cluster

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
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
	// failoverTimeout, when positive, replaces the distributor's default when
	// Start creates it. It must be set before Start: the heartbeat loop reads
	// the distributor's value from its own goroutine.
	failoverTimeout time.Duration

	lifecycleMu   sync.Mutex
	mu            sync.RWMutex
	necroConfig   core.NecropolisConfig
	config        core.RaftConfig
	node          *raft.Node
	db            *storage.CobaltDB
	logStore      *storage.CobaltDBLogStore
	snapshotStore raft.SnapshotStore
	stableStore   *storage.CobaltDBStableStore
	fsm           *raft.StorageFSM
	logger        *slog.Logger
	isClustered   bool
	stopped       bool // True when Stop() has been called

	// Distribution
	distributor *Distributor

	// Discovery
	discovery *raft.Discovery

	// Heartbeats keep the distributor's liveness view current (see startHeartbeats)
	heartbeatStop chan struct{}
	heartbeatWg   sync.WaitGroup

	// strategy is necropolis.distribution.strategy, parsed.
	strategy DistributionStrategy

	// live is the replicated set of members that own souls (see
	// LiveMembers), nil until the first one is applied. wasLeader is read and
	// written only by the heartbeat goroutine.
	liveMu    sync.RWMutex
	live      []NodeLoad
	wasLeader bool
	// hadQuorum is the quorum-contact state the heartbeat goroutine last
	// observed (see checkQuorumTransition); written only by that goroutine.
	hadQuorum bool

	// Application hooks (see SetApplyObserver, SetRestoreObserver,
	// SetMembershipObserver), guarded by hookMu.
	hookMu             sync.RWMutex
	applyObserver      func(cmd core.FSMCommand)
	restoreObserver    func()
	membershipObserver func()
}

// liveMembersKey holds the leader's view of which members are alive, as a
// liveMembers record. Every node derives soul ownership from the applied
// value, so they all agree on it; a follower's own liveness view cannot be
// used for that, because followers never hear from each other.
const liveMembersKey = "system/cluster/live-members"

type liveMember struct {
	ID     string `json:"id"`
	Region string `json:"region,omitempty"`
}

type liveMembers struct {
	Members   []liveMember `json:"members"`
	UpdatedAt time.Time    `json:"updated_at"`
}

// Membership errors returned by AddNode and RemoveNode.
var (
	ErrNotClustered = errors.New("clustering is not enabled on this node")
	ErrNodeExists   = errors.New("node is already a cluster member")
	ErrNodeUnknown  = errors.New("node is not a cluster member")
	ErrRemoveLeader = errors.New("the current leader cannot remove itself; remove it from another node after leadership moves")
	// ErrIdentifyFailed wraps a failure to learn a node's ID from its address.
	ErrIdentifyFailed = errors.New("could not identify the node")
)

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
		isClustered: cfg.Enabled || cfg.Raft.Bootstrap || len(cfg.Raft.Peers) > 0 || len(cfg.Raft.Join) > 0,
	}

	// If SingleNode is set, we're effectively clustered (for leader election)
	// but don't need peer connections
	if cfg.SingleNode && !m.isClustered {
		m.logger.Info("SingleNode mode enabled - node will self-elect as leader")
		m.isClustered = true
	}

	// Create Raft storage components
	m.logStore = storage.NewCobaltDBLogStore(db)
	// Snapshots are files beside the WAL, not CobaltDB records: inside the
	// in-memory tree they would double the node's memory.
	snapshots, err := storage.NewFileSnapshotStore(db)
	if err != nil {
		return nil, err
	}
	m.snapshotStore = snapshots
	m.stableStore = storage.NewCobaltDBStableStore(db)

	// Create FSM backed by storage
	m.fsm = raft.NewStorageFSM(db)
	m.fsm.SetLogger(m.logger)
	m.fsm.SetApplyObserver(m.observeApplied)
	m.fsm.SetRestoreObserver(m.observeRestore)

	strategy, known := ParseStrategy(cfg.Distribution.Strategy)
	if !known {
		m.logger.Warn("unsupported necropolis.distribution.strategy; using hash-based ownership",
			"strategy", cfg.Distribution.Strategy)
	}
	m.strategy = strategy

	return m, nil
}

// SetCommandHandler routes replicated commands for table to h (see
// raft.StorageFSM.SetCommandHandler). Set it before Start.
func (m *Manager) SetCommandHandler(table string, h raft.CommandHandler) {
	m.fsm.SetCommandHandler(table, h)
}

// SetBatchCommandHandler routes runs of replicated commands for table inside
// one batch to h (see raft.StorageFSM.SetBatchCommandHandler). Set it before
// Start.
func (m *Manager) SetBatchCommandHandler(table string, h raft.BatchCommandHandler) {
	m.fsm.SetBatchCommandHandler(table, h)
}

// SetApplyObserver registers fn to see every replicated command after it is
// applied on this node, whichever node proposed it. It runs on the Raft apply
// path and must not block.
func (m *Manager) SetApplyObserver(fn func(cmd core.FSMCommand)) {
	m.hookMu.Lock()
	m.applyObserver = fn
	m.hookMu.Unlock()
}

// SetRestoreObserver registers fn to run after a snapshot replaced this node's
// keyspace, so in-memory state derived from storage can be rebuilt.
func (m *Manager) SetRestoreObserver(fn func()) {
	m.hookMu.Lock()
	m.restoreObserver = fn
	m.hookMu.Unlock()
}

// SetMembershipObserver registers fn to be called when the set of members that
// own souls changes. It must not block.
func (m *Manager) SetMembershipObserver(fn func()) {
	m.hookMu.Lock()
	m.membershipObserver = fn
	m.hookMu.Unlock()
}

func (m *Manager) observeApplied(cmd core.FSMCommand) {
	if cmd.Key == liveMembersKey {
		var record liveMembers
		if cmd.Op == core.FSMSet && json.Unmarshal(cmd.Value, &record) == nil {
			m.setLive(record)
		}
	}
	m.hookMu.RLock()
	fn := m.applyObserver
	m.hookMu.RUnlock()
	if fn != nil {
		fn(cmd)
	}
}

func (m *Manager) observeRestore() {
	m.loadLive()
	m.hookMu.RLock()
	fn := m.restoreObserver
	m.hookMu.RUnlock()
	if fn != nil {
		fn()
	}
}

// loadLive reads the replicated live-member record from storage.
func (m *Manager) loadLive() {
	data, err := m.db.Get(liveMembersKey)
	if err != nil {
		return
	}
	var record liveMembers
	if json.Unmarshal(data, &record) == nil {
		m.setLive(record)
	}
}

func (m *Manager) setLive(record liveMembers) {
	live := make([]NodeLoad, 0, len(record.Members))
	for _, member := range record.Members {
		live = append(live, NodeLoad{NodeID: member.ID, Region: member.Region, Healthy: true})
	}
	m.liveMu.Lock()
	changed := !sameMembers(m.live, live) || m.live == nil
	m.live = live
	m.liveMu.Unlock()
	if changed {
		m.logger.Info("soul-owning members changed", "members", memberIDs(live))
		m.notifyMembership()
	}
}

func (m *Manager) notifyMembership() {
	m.hookMu.RLock()
	fn := m.membershipObserver
	m.hookMu.RUnlock()
	if fn != nil {
		fn()
	}
}

func sameMembers(a, b []NodeLoad) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].NodeID != b[i].NodeID || a[i].Region != b[i].Region {
			return false
		}
	}
	return true
}

func memberIDs(members []NodeLoad) []string {
	ids := make([]string, len(members))
	for i, member := range members {
		ids[i] = member.NodeID
	}
	return ids
}

// Start initializes and starts the Raft node
func (m *Manager) Start(ctx context.Context) error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

	var cleanupNode *raft.Node
	var cleanupDiscovery *raft.Discovery
	// Rollback runs after m.mu is released so discovery callbacks can finish.
	defer func() {
		if cleanupDiscovery != nil {
			if err := cleanupDiscovery.Stop(); err != nil {
				m.logger.Warn("failed to roll back discovery", "err", err)
			}
		}
		if cleanupNode != nil {
			if err := cleanupNode.Stop(); err != nil {
				m.logger.Warn("failed to roll back Raft node", "err", err)
			}
		}
	}()

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isClustered {
		m.logger.Info("running in standalone mode")
		return nil
	}

	if m.node != nil && !m.stopped {
		return fmt.Errorf("cluster manager already running")
	}

	m.logger.Info("starting Raft node", "node_id", m.config.NodeID, "bind_addr", m.config.BindAddr)

	// Build TLS config for peer-to-peer communication (nil if TLS not configured)
	tlsCfg, err := buildTLSPeerConfig(m.config.TLS)
	if err != nil {
		return fmt.Errorf("failed to build TLS config: %w", err)
	}
	if tlsCfg != nil {
		m.logger.Info("Raft peer TLS enabled", "min_version", "TLS 1.2", "verify_peers", m.config.TLS.VerifyPeers, "client_auth", tlsCfg.ClientAuth.String())
	} else if m.necroConfig.ClusterSecret != "" {
		m.logger.Warn("Raft inter-node auth uses only pre-shared key (no TLS); specify tls.cert_file to enable mTLS")
	}
	if m.necroConfig.ClusterSecret == "" {
		m.logger.Warn("necropolis.cluster_secret is not set: cluster traffic is unauthenticated, and anyone who can reach the Raft port can vote, replicate entries or change membership; set the same cluster_secret on every node",
			"event", "cluster_unauthenticated", "bind_addr", m.config.BindAddr)
	} else {
		m.logger.Info("Raft RPCs are signed with the cluster secret")
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

	// Start Raft node (this also starts the transport)
	if err := node.Start(); err != nil {
		return fmt.Errorf("failed to start Raft node: %w", err)
	}
	cleanupNode = node

	m.logger.Info("Raft node started")

	// Initialize and start discovery service
	if cfg := m.necroConfig.Discovery; cfg.Mode != "" && cfg.Mode != "manual" {
		disc, err := raft.NewDiscovery(m.config, m.logger)
		if err != nil {
			return fmt.Errorf("failed to create discovery service: %w", err)
		} else {
			cleanupDiscovery = disc
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

	// Initialize distributor. Its rebalance loop is not started: soul ownership
	// is recomputed deterministically by Rebuild on every node (see
	// PlanSouls), and a loop moving souls in one node's table would make that
	// node disagree with the rest. The distributor's heartbeat bookkeeping is
	// what the leader uses to decide which members are alive.
	strategy := m.strategy
	m.distributor = NewDistributor(m.config.NodeID, m.config.Region, strategy, m.logger)
	m.distributor.failoverTimeout = m.defaultFailoverTimeout()

	// Register self
	m.distributor.RegisterNode(m.config.NodeID, m.config.Region)

	// Register peers
	for _, peer := range node.GetPeers() {
		m.distributor.RegisterNode(peer.ID, peer.Region)
	}

	m.node = node
	m.stopped = false
	cleanupNode = nil
	cleanupDiscovery = nil

	// Keep the distributor's view of liveness current. The distributor only records
	// a heartbeat when a node is registered or its load is updated, and nothing in
	// production does either after startup: without this every node is stamped at
	// Start and goes stale after failoverTimeout, so no soul could ever be assigned.
	m.startHeartbeats()

	// Pick up the live-member record a previous run applied, so ownership is
	// right before this run's leader publishes a fresh one.
	m.loadLive()

	m.logger.Info("Cluster distributor started", "strategy", strategy.String(),
		"failover_timeout", m.distributor.failoverTimeout)
	return nil
}

// defaultFailoverTimeout is how long a member may go unheard before the leader
// stops counting it as a soul owner: failoverTimeout when set, else five
// election timeouts -- long enough not to flap on one slow round trip, short
// enough that a dead node's souls are not left unchecked for long.
func (m *Manager) defaultFailoverTimeout() time.Duration {
	if m.failoverTimeout > 0 {
		return m.failoverTimeout
	}
	timeout := 5 * m.config.ElectionTimeout.Duration
	if timeout < time.Second {
		timeout = time.Second
	}
	return timeout
}

// heartbeatInterval is how often node heartbeats are refreshed: a third of the
// failover timeout, so two refreshes can be missed before a node is called dead. It
// is read per tick so a changed timeout takes effect immediately.
func (m *Manager) heartbeatInterval() time.Duration {
	interval := 10 * time.Second
	if m.distributor != nil && m.distributor.failoverTimeout > 0 {
		interval = m.distributor.failoverTimeout / 3
	}
	if interval < 10*time.Millisecond {
		interval = 10 * time.Millisecond
	}
	return interval
}

func (m *Manager) startHeartbeats() {
	m.heartbeatStop = make(chan struct{})
	m.heartbeatWg.Add(1)
	go func() {
		defer m.heartbeatWg.Done()
		timer := time.NewTimer(m.heartbeatInterval())
		defer timer.Stop()
		// Quorum contact is watched more often than heartbeats are refreshed:
		// a node cut off from the majority must stop owning souls well before
		// the majority's failover timeout hands them to another member.
		quorumTicker := time.NewTicker(m.quorumCheckInterval())
		defer quorumTicker.Stop()
		for {
			select {
			case <-m.heartbeatStop:
				return
			case <-quorumTicker.C:
				m.checkQuorumTransition()
			case <-timer.C:
				m.refreshNodeHeartbeats()
				m.publishLiveMembers()
				m.checkQuorumTransition()
				timer.Reset(m.heartbeatInterval())
			}
		}
	}()
}

// quorumCheckInterval is how often quorum contact is re-evaluated: half an
// election timeout, bounded to [10ms, 1s].
func (m *Manager) quorumCheckInterval() time.Duration {
	interval := m.config.ElectionTimeout.Duration / 2
	if interval <= 0 || interval > time.Second {
		interval = time.Second
	}
	if interval < 10*time.Millisecond {
		interval = 10 * time.Millisecond
	}
	return interval
}

// checkQuorumTransition re-plans ownership when this node gains or loses
// contact with a working majority. Called only from the heartbeat goroutine.
func (m *Manager) checkQuorumTransition() {
	has := m.HasQuorum()
	if has == m.hadQuorum {
		return
	}
	m.hadQuorum = has
	if has {
		m.logger.Info("in contact with a cluster majority again; resuming soul ownership")
	} else {
		m.logger.Warn("lost contact with a cluster majority; releasing soul ownership until it is restored")
	}
	m.notifyMembership()
}

func (m *Manager) stopHeartbeats() {
	if m.heartbeatStop == nil {
		return
	}
	close(m.heartbeatStop)
	m.heartbeatWg.Wait()
	m.heartbeatStop = nil
}

// refreshNodeHeartbeats marks this node and every peer Raft reports as connected as
// alive. A peer that stops responding is not refreshed, so it goes stale and its
// souls are failed over to a node that is still up.
func (m *Manager) refreshNodeHeartbeats() {
	m.mu.RLock()
	distributor, node, stopped := m.distributor, m.node, m.stopped
	m.mu.RUnlock()

	if distributor == nil || stopped {
		return
	}

	distributor.Heartbeat(m.config.NodeID)
	if node == nil {
		return
	}
	for _, peer := range node.GetState().Peers {
		if peer.IsConnected {
			distributor.Heartbeat(peer.ID)
		}
	}
}

// membershipNodes returns this node and every Raft peer as distributor
// entries, sorted by ID.
func (m *Manager) membershipNodes(node *raft.Node) []NodeLoad {
	nodes := []NodeLoad{{NodeID: m.config.NodeID, Region: firstNonEmpty(m.config.Region, m.necroConfig.Region)}}
	if node != nil {
		for _, peer := range node.GetPeers() {
			if peer.ID != m.config.NodeID {
				nodes = append(nodes, NodeLoad{NodeID: peer.ID, Region: peer.Region})
			}
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].NodeID < nodes[j].NodeID })
	return nodes
}

// publishLiveMembers runs on the leader's heartbeat tick. It keeps the
// distributor's entries in step with Raft membership, decides which members are
// alive (heard from within failoverTimeout), and replicates that set when it
// differs from the applied one. A follower does nothing: it only consumes the
// replicated set.
func (m *Manager) publishLiveMembers() {
	m.mu.RLock()
	distributor, node, stopped := m.distributor, m.node, m.stopped
	m.mu.RUnlock()
	if distributor == nil || node == nil || stopped || !node.IsLeader() || !node.HasQuorumContact() {
		m.wasLeader = false
		return
	}

	members := m.membershipNodes(node)
	known := distributor.GetLoadDistribution()
	inMembership := make(map[string]bool, len(members))
	for _, member := range members {
		inMembership[member.NodeID] = true
		if _, ok := known[member.NodeID]; !ok {
			distributor.RegisterNode(member.NodeID, member.Region)
		}
	}
	for nodeID := range known {
		if !inMembership[nodeID] {
			distributor.UnregisterNode(nodeID)
		}
	}
	if !m.wasLeader {
		// A new leader has only a follower's liveness view, in which every
		// other follower looks silent. Start everyone with a fresh heartbeat
		// rather than failing them over on the first tick.
		for _, member := range members {
			distributor.Heartbeat(member.NodeID)
		}
		m.wasLeader = true
	}

	loads := distributor.GetLoadDistribution()
	record := liveMembers{UpdatedAt: time.Now().UTC()}
	live := make([]NodeLoad, 0, len(members))
	for _, member := range members {
		load, ok := loads[member.NodeID]
		if member.NodeID != m.config.NodeID && (!ok || distributor.nodeStale(load)) {
			continue
		}
		record.Members = append(record.Members, liveMember{ID: member.NodeID, Region: member.Region})
		live = append(live, NodeLoad{NodeID: member.NodeID, Region: member.Region})
	}

	m.liveMu.RLock()
	unchanged := m.live != nil && sameMembers(m.live, live)
	m.liveMu.RUnlock()
	if unchanged {
		return
	}

	data, err := json.Marshal(record)
	if err != nil {
		return
	}
	if _, _, err := m.ApplyMutation(core.FSMCommand{Op: core.FSMSet, Key: liveMembersKey, Value: data}, 5*time.Second); err != nil {
		m.logger.Warn("failed to publish live cluster members", "err", err)
	}
}

// LiveMembers returns the members that currently own souls, sorted by ID: the
// replicated set the leader last published, or -- before one exists -- every
// member of the Raft configuration. Without a running Raft node it is this node
// alone.
func (m *Manager) LiveMembers() []NodeLoad {
	m.mu.RLock()
	node := m.node
	m.mu.RUnlock()
	if node == nil {
		return []NodeLoad{{NodeID: m.localID(), Region: firstNonEmpty(m.config.Region, m.necroConfig.Region)}}
	}
	if !node.HasQuorumContact() {
		// Cut off from the majority (or not a member): own nothing. The
		// majority side hands this node's souls to a live member after its
		// failover timeout, and running them here too would duplicate checks
		// and alerts. Ownership comes back when contact does.
		return nil
	}
	m.liveMu.RLock()
	live := append([]NodeLoad(nil), m.live...)
	m.liveMu.RUnlock()
	if len(live) > 0 {
		return live
	}
	if node.AwaitingJoin() {
		// Not a member yet: own nothing rather than everything.
		return nil
	}
	return m.membershipNodes(node)
}

// localID is this node's ID in ownership plans.
func (m *Manager) localID() string {
	return firstNonEmpty(m.config.NodeID, m.necroConfig.NodeName, "standalone")
}

// NodeID returns this node's ID as it appears in ownership plans.
func (m *Manager) NodeID() string {
	return m.localID()
}

// PlanSouls computes which member owns each soul (soul ID -> node ID) under
// the configured strategy over LiveMembers, and records the plan in the
// distributor. Every node computes the same plan from the same replicated
// souls and members.
func (m *Manager) PlanSouls(souls []*core.Soul) map[string]string {
	members := m.LiveMembers()
	m.mu.RLock()
	distributor := m.distributor
	m.mu.RUnlock()
	if distributor == nil {
		return PlanOwnership(m.strategy, souls, members)
	}
	return distributor.Rebuild(souls, members)
}

// Redundancy is how many distinct members probe each soul
// (necropolis.distribution.redundancy), at least 1.
func (m *Manager) Redundancy() int {
	if n := m.necroConfig.Distribution.Redundancy; n > 1 {
		return n
	}
	return 1
}

// PlanSoulOwners is PlanSouls with redundancy: soul ID -> the members that
// probe it, primary first. The primary is the owner PlanSouls returns, so the
// distributor's table (and everything that reads it) is unchanged. At
// redundancy 1 every set holds just the primary.
func (m *Manager) PlanSoulOwners(souls []*core.Soul) map[string][]string {
	members := m.LiveMembers()
	m.PlanSouls(souls)
	return PlanOwnerSets(m.strategy, souls, members, m.Redundancy())
}

// PlanJourneys computes which member runs each journey (journey ID -> node
// ID). Journeys are planned exactly like souls -- the same strategy over the
// same replicated LiveMembers, keyed by journey ID -- so every node agrees on
// one runner per journey. It does not touch the distributor's soul table.
func (m *Manager) PlanJourneys(journeys []*core.JourneyConfig) map[string]string {
	keys := make([]*core.Soul, 0, len(journeys))
	for _, j := range journeys {
		if j != nil && j.ID != "" {
			keys = append(keys, &core.Soul{ID: j.ID})
		}
	}
	return PlanOwnership(m.strategy, keys, m.LiveMembers())
}

// Running reports whether the Raft node is up, i.e. whether replicated writes
// can be submitted at all.
func (m *Manager) Running() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.node != nil && !m.stopped
}

// AddNode adds a voting member through a replicated single-server
// configuration change. It may be called on any node: a follower forwards the
// change to the leader. Only one change can be in flight at a time.
func (m *Manager) AddNode(peer core.RaftPeer) error {
	m.mu.RLock()
	node := m.node
	m.mu.RUnlock()
	if node == nil {
		return ErrNotClustered
	}
	if peer.ID == "" || peer.Address == "" {
		return fmt.Errorf("node id and address are required")
	}
	if peer.Role == "" {
		peer.Role = core.RoleVoter
	}
	if peer.ID == m.config.NodeID {
		return ErrNodeExists
	}
	for _, existing := range node.GetPeers() {
		if existing.ID == peer.ID {
			return ErrNodeExists
		}
	}
	return node.AddPeerOrForward(peer, 40*time.Second)
}

// AddNodeByAddress adds the node listening on the Raft address addr as a
// voter, learning its real node ID (and, when region is empty, its region)
// by asking it. It is the summon flow: the new node only has to be running,
// typically with raft.join so it waits instead of electing itself.
func (m *Manager) AddNodeByAddress(addr, region string) (core.RaftPeer, error) {
	m.mu.RLock()
	node := m.node
	m.mu.RUnlock()
	if node == nil {
		return core.RaftPeer{}, ErrNotClustered
	}
	identity, err := node.IdentifyPeer(addr, 10*time.Second)
	if err != nil {
		return core.RaftPeer{}, fmt.Errorf("%w at %s: %v", ErrIdentifyFailed, addr, err)
	}
	peer := core.RaftPeer{ID: identity.ID, Address: addr, Region: firstNonEmpty(region, identity.Region), Role: core.RoleVoter}
	return peer, m.AddNode(peer)
}

// RemoveNode removes a member through a replicated single-server
// configuration change, forwarding to the leader from a follower.
func (m *Manager) RemoveNode(nodeID string) error {
	m.mu.RLock()
	node := m.node
	m.mu.RUnlock()
	if node == nil {
		return ErrNotClustered
	}
	// Raft removes a member through the leader, which cannot remove itself
	// (there is no leadership transfer); refuse up front rather than after a
	// forward round trip.
	if nodeID == node.Leader() {
		return ErrRemoveLeader
	}
	known := nodeID == m.config.NodeID
	for _, existing := range node.GetPeers() {
		if existing.ID == nodeID {
			known = true
		}
	}
	if !known {
		return ErrNodeUnknown
	}
	return node.RemovePeerOrForward(nodeID, 40*time.Second)
}

// Stop gracefully shuts down the Raft node
func (m *Manager) Stop(ctx context.Context) error {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()

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

	// Stop the heartbeat producer before the distributor it feeds.
	m.stopHeartbeats()

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

// ApplyMutation submits an FSM command through Raft consensus from any node.
// On a follower the command is forwarded to the leader over the Raft
// transport, and the call returns once this node has applied it, so a read
// here sees the write. With no reachable leader it returns a RaftError with
// ErrNoLeader. A nil node (standalone mode) returns ErrNotLeader so callers
// can fall back to a local write.
func (m *Manager) ApplyMutation(cmd core.FSMCommand, timeout time.Duration) (uint64, uint64, error) {
	m.mu.RLock()
	node := m.node
	m.mu.RUnlock()

	if node == nil {
		return 0, 0, &core.RaftError{Code: core.ErrNotLeader, Message: "standalone node: write locally"}
	}
	if cmd.Origin == "" {
		cmd.Origin = m.config.NodeID
	}
	return node.ApplyOrForward(cmd, timeout)
}

// IsLeader returns true if this node is the Raft leader and a voter majority
// has acknowledged it within the last election timeout. A leader cut off from
// the majority reports false even before check-quorum steps it down, so
// leader-only decisions fail closed.
func (m *Manager) IsLeader() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.node == nil {
		return false
	}

	return m.node.State() == core.StateLeader && m.node.HasQuorumContact()
}

// HasQuorum reports whether this node is in contact with a working cluster
// majority (raft.Node.HasQuorumContact). Without a Raft node (standalone) it
// is always true. Work that must run on exactly one node should stop while it
// is false.
func (m *Manager) HasQuorum() bool {
	m.mu.RLock()
	node := m.node
	m.mu.RUnlock()
	if node == nil {
		return true
	}
	return node.HasQuorumContact()
}

// VerifyLeader confirms with a fresh round of acknowledgements that this node
// is still the leader of a voter majority (raft.Node.VerifyLeader). Use it
// before acting on leadership where a stale leader would do harm.
func (m *Manager) VerifyLeader(timeout time.Duration) error {
	m.mu.RLock()
	node := m.node
	m.mu.RUnlock()
	if node == nil {
		return &core.RaftError{Code: core.ErrNotLeader, Message: "standalone node"}
	}
	return node.VerifyLeader(timeout)
}

// ReadBarrier makes the next local read linearizable: it returns once this
// node has applied every write committed before the call. A follower asks the
// leader for its read index; no log entry is written
// (raft.Node.ReadBarrierOrForward). raft.ErrReadIndexUnsupported reports a
// leader too old to answer.
func (m *Manager) ReadBarrier(timeout time.Duration) (uint64, error) {
	m.mu.RLock()
	node := m.node
	m.mu.RUnlock()
	if node == nil {
		return 0, &core.RaftError{Code: core.ErrNotLeader, Message: "standalone node"}
	}
	return node.ReadBarrierOrForward(timeout)
}

// TableReadBarrier is ReadBarrier for a read of only the state that commands
// of table write; on a follower behind on other writes it returns sooner.
func (m *Manager) TableReadBarrier(table string, timeout time.Duration) (uint64, error) {
	m.mu.RLock()
	node := m.node
	m.mu.RUnlock()
	if node == nil {
		return 0, &core.RaftError{Code: core.ErrNotLeader, Message: "standalone node"}
	}
	return node.TableReadBarrierOrForward(table, timeout)
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

// Member describes one node of the cluster as this node sees it.
type Member struct {
	ID          string
	Name        string
	Address     string
	Region      string
	State       string // leader, follower, candidate, unreachable, unknown
	IsLeader    bool
	IsSelf      bool
	LastContact time.Time // zero when this node has never heard from it
}

// Members returns this node followed by every Raft peer it knows about.
// Without clustering it returns this node alone as the leader, since a
// standalone node is the sole authority over its data.
func (m *Manager) Members() []Member {
	m.mu.RLock()
	node := m.node
	m.mu.RUnlock()

	self := Member{
		ID:      m.config.NodeID,
		Name:    m.necroConfig.NodeName,
		Address: firstNonEmpty(m.config.AdvertiseAddr, m.config.BindAddr, m.necroConfig.AdvertiseAddr, m.necroConfig.BindAddr),
		Region:  firstNonEmpty(m.config.Region, m.necroConfig.Region),
		IsSelf:  true,
	}
	if self.ID == "" {
		self.ID = firstNonEmpty(self.Name, "standalone")
	}
	if self.Name == "" {
		self.Name = self.ID
	}

	if node == nil {
		if m.isClustered {
			self.State = "unknown"
		} else {
			self.State = "leader"
			self.IsLeader = true
			self.LastContact = time.Now().UTC()
		}
		return []Member{self}
	}

	state := node.GetState()
	self.State = state.State.String()
	self.IsLeader = state.State == core.StateLeader
	self.LastContact = time.Now().UTC()

	// Only the leader talks to every peer. A follower never hears from the
	// other followers, so their connection state says nothing; it uses the
	// leader's replicated live-member list instead (as ownership does).
	var liveByLeader map[string]bool
	if !self.IsLeader {
		m.liveMu.RLock()
		if len(m.live) > 0 {
			liveByLeader = make(map[string]bool, len(m.live))
			for _, l := range m.live {
				liveByLeader[l.NodeID] = true
			}
		}
		m.liveMu.RUnlock()
	}

	members := make([]Member, 0, len(state.Peers)+1)
	members = append(members, self)
	peers := append([]core.RaftPeerInfo(nil), state.Peers...)
	sort.Slice(peers, func(i, j int) bool { return peers[i].ID < peers[j].ID })
	for _, peer := range peers {
		if peer.ID == self.ID {
			continue
		}
		member := Member{
			ID:          peer.ID,
			Name:        peer.ID,
			Address:     peer.Address,
			Region:      peer.Region,
			IsLeader:    peer.ID == state.LeaderID,
			LastContact: peer.LastContact,
		}
		switch {
		case member.IsLeader:
			member.State = string(core.StateLeader)
		case peer.IsConnected:
			member.State = string(core.StateFollower)
		case liveByLeader != nil && liveByLeader[peer.ID]:
			member.State = string(core.StateFollower)
		case liveByLeader != nil:
			member.State = "unreachable"
		case !self.IsLeader:
			// No live list yet: this follower cannot tell.
			member.State = "unknown"
		default:
			member.State = "unreachable"
		}
		members = append(members, member)
	}
	return members
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
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
