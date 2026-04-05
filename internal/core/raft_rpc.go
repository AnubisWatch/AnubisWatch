package core

// Raft RPC Request/Response types
// These messages flow between the Jackals (nodes) in the Necropolis (cluster)

// PreVoteRequest is sent by candidates to check if they would win an election
// This prevents disruptive elections from candidates with stale logs
type PreVoteRequest struct {
	Term         uint64 `json:"term"`
	CandidateID  string `json:"candidate_id"`
	LastLogIndex uint64 `json:"last_log_index"`
	LastLogTerm  uint64 `json:"last_log_term"`
}

// PreVoteResponse is the response to a PreVote RPC
type PreVoteResponse struct {
	Term        uint64 `json:"term"`
	VoteGranted bool   `json:"vote_granted"`
	Reason      string `json:"reason,omitempty"`
}

// RequestVoteRequest is sent by candidates to gather votes
type RequestVoteRequest struct {
	Term         uint64 `json:"term"`
	CandidateID  string `json:"candidate_id"`
	LastLogIndex uint64 `json:"last_log_index"`
	LastLogTerm  uint64 `json:"last_log_term"`
	// Pre-vote extension: carry over pre-vote term
	PreVoteTerm uint64 `json:"pre_vote_term,omitempty"`
}

// RequestVoteResponse is the response to a RequestVote RPC
type RequestVoteResponse struct {
	Term        uint64 `json:"term"`
	VoteGranted bool   `json:"vote_granted"`
	Reason      string `json:"reason,omitempty"`
}

// AppendEntriesRequest is sent by the leader to replicate log entries
type AppendEntriesRequest struct {
	Term         uint64         `json:"term"`
	LeaderID     string         `json:"leader_id"`
	PrevLogIndex uint64         `json:"prev_log_index"`
	PrevLogTerm  uint64         `json:"prev_log_term"`
	Entries      []RaftLogEntry `json:"entries"`
	LeaderCommit uint64         `json:"leader_commit"`
}

// AppendEntriesResponse is the response to an AppendEntries RPC
type AppendEntriesResponse struct {
	Term       uint64 `json:"term"`
	Success    bool   `json:"success"`
	MatchIndex uint64 `json:"match_index"`
	// Used for log inconsistency optimization
	ConflictTerm  uint64 `json:"conflict_term,omitempty"`
	ConflictIndex uint64 `json:"conflict_index,omitempty"`
}

// InstallSnapshotRequest is sent by the leader to transfer a snapshot
type InstallSnapshotRequest struct {
	Term              uint64 `json:"term"`
	LeaderID          string `json:"leader_id"`
	LastIncludedIndex uint64 `json:"last_included_index"`
	LastIncludedTerm  uint64 `json:"last_included_term"`
	Offset            uint64 `json:"offset"`
	Data              []byte `json:"data"`
	Done              bool   `json:"done"`
}

// InstallSnapshotResponse is the response to an InstallSnapshot RPC
type InstallSnapshotResponse struct {
	Term    uint64 `json:"term"`
	Success bool   `json:"success"`
}

// TimeoutNowRequest is used to force a node to start an election immediately
// Used during leadership transfers
type TimeoutNowRequest struct {
	Term     uint64 `json:"term"`
	LeaderID string `json:"leader_id"`
}

// TimeoutNowResponse is the response to a TimeoutNow RPC
type TimeoutNowResponse struct {
	Term    uint64 `json:"term"`
	Started bool   `json:"started"`
}

// PeerInfoRequest requests information about a peer
type PeerInfoRequest struct {
	NodeID string `json:"node_id"`
}

// PeerInfoResponse returns peer information
type PeerInfoResponse struct {
	Info         RaftPeerInfo `json:"info"`
	ClusterState ClusterState `json:"cluster_state"`
}

// JoinRequest is sent by a node wanting to join the cluster
type JoinRequest struct {
	NodeID                 string           `json:"node_id"`
	Address                string           `json:"address"`
	Region                 string           `json:"region"`
	Role                   RaftRole         `json:"role"`
	Capabilities           NodeCapabilities `json:"capabilities"`
	Version                string           `json:"version"`
	PrevConfigurationIndex uint64           `json:"prev_configuration_index"`
}

// JoinResponse is the response to a JoinRequest
type JoinResponse struct {
	Success       bool       `json:"success"`
	LeaderID      string     `json:"leader_id,omitempty"`
	LeaderAddress string     `json:"leader_address,omitempty"`
	Peers         []RaftPeer `json:"peers,omitempty"`
	Error         string     `json:"error,omitempty"`
	Term          uint64     `json:"term"`
}

// LeaveRequest is sent by a node leaving the cluster
type LeaveRequest struct {
	NodeID string `json:"node_id"`
	Force  bool   `json:"force"` // Force removal even if not graceful
}

// LeaveResponse is the response to a LeaveRequest
type LeaveResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// PromoteRequest promotes a non-voting node to voter
type PromoteRequest struct {
	NodeID string `json:"node_id"`
}

// PromoteResponse is the response to a PromoteRequest
type PromoteResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// DemoteRequest demotes a voting node to non-voter
type DemoteRequest struct {
	NodeID string `json:"node_id"`
}

// DemoteResponse is the response to a DemoteRequest
type DemoteResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// GetConfigurationRequest requests the current cluster configuration
type GetConfigurationRequest struct{}

// GetConfigurationResponse returns the cluster configuration
type GetConfigurationResponse struct {
	Index   uint64       `json:"index"`
	Servers []RaftServer `json:"servers"`
}

// RaftServer represents a server in the configuration
type RaftServer struct {
	ID       string   `json:"id"`
	Address  string   `json:"address"`
	Suffrage RaftRole `json:"suffrage"` // voter or nonvoter
	Region   string   `json:"region"`
}

// ApplyCommandRequest applies a command through Raft
type ApplyCommandRequest struct {
	Command FSMCommand `json:"command"`
	Timeout Duration   `json:"timeout"`
}

// ApplyCommandResponse is the response to an ApplyCommand
type ApplyCommandResponse struct {
	Index   uint64 `json:"index"`
	Term    uint64 `json:"term"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Data    []byte `json:"data,omitempty"`
}

// BarrierRequest creates a barrier for read consistency
type BarrierRequest struct {
	Timeout Duration `json:"timeout"`
}

// BarrierResponse is the response to a BarrierRequest
type BarrierResponse struct {
	Index   uint64 `json:"index"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// VerifyLeaderRequest verifies if the current node is still the leader
type VerifyLeaderRequest struct{}

// VerifyLeaderResponse is the response to a VerifyLeaderRequest
type VerifyLeaderResponse struct {
	IsLeader bool   `json:"is_leader"`
	LeaderID string `json:"leader_id,omitempty"`
	Term     uint64 `json:"term"`
}

// AddVoterRequest adds a voter to the cluster
type AddVoterRequest struct {
	ID        string `json:"id"`
	Address   string `json:"address"`
	PrevIndex uint64 `json:"prev_index"`
}

// AddVoterResponse is the response to an AddVoterRequest
type AddVoterResponse struct {
	Index uint64 `json:"index"`
}

// RemoveServerRequest removes a server from the cluster
type RemoveServerRequest struct {
	ID        string `json:"id"`
	PrevIndex uint64 `json:"prev_index"`
}

// RemoveServerResponse is the response to a RemoveServerRequest
type RemoveServerResponse struct {
	Index uint64 `json:"index"`
}

// StatsRequest requests Raft statistics
type StatsRequest struct{}

// StatsResponse returns Raft statistics
type StatsResponse struct {
	State                    string       `json:"state"`
	Term                     uint64       `json:"term"`
	LastLogIndex             uint64       `json:"last_log_index"`
	LastLogTerm              uint64       `json:"last_log_term"`
	CommitIndex              uint64       `json:"commit_index"`
	AppliedIndex             uint64       `json:"applied_index"`
	FSMIndex                 uint64       `json:"fsm_index"`
	LastSnapshotIndex        uint64       `json:"last_snapshot_index"`
	LastSnapshotTerm         uint64       `json:"last_snapshot_term"`
	LatestConfigurationIndex uint64       `json:"latest_configuration_index"`
	LatestConfiguration      []RaftServer `json:"latest_configuration"`
	LastContact              int64        `json:"last_contact"` // milliseconds
	NumPeers                 int          `json:"num_peers"`
	LastAppliedTime          int64        `json:"last_applied_time"` // milliseconds
	Stats                    ClusterStats `json:"stats"`
}

// LeadershipTransferRequest requests a leadership transfer
type LeadershipTransferRequest struct {
	TargetServerID string `json:"target_server_id,omitempty"`
}

// LeadershipTransferResponse is the response to a LeadershipTransferRequest
type LeadershipTransferResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// HeartbeatRequest is a lightweight heartbeat for health checks
type HeartbeatRequest struct {
	NodeID    string `json:"node_id"`
	LeaderID  string `json:"leader_id"`
	Term      uint64 `json:"term"`
	Timestamp int64  `json:"timestamp"`
}

// HeartbeatResponse is the response to a HeartbeatRequest
type HeartbeatResponse struct {
	NodeID    string `json:"node_id"`
	Term      uint64 `json:"term"`
	IsLeader  bool   `json:"is_leader"`
	LeaderID  string `json:"leader_id"`
	Timestamp int64  `json:"timestamp"`
}

// DiscoveryRequest is sent during service discovery
type DiscoveryRequest struct {
	NodeID       string           `json:"node_id"`
	Region       string           `json:"region"`
	Address      string           `json:"address"`
	Capabilities NodeCapabilities `json:"capabilities"`
	Version      string           `json:"version"`
}

// DiscoveryResponse is the response to a DiscoveryRequest
type DiscoveryResponse struct {
	KnownPeers []RaftPeer `json:"known_peers"`
	LeaderID   string     `json:"leader_id,omitempty"`
	ClusterID  string     `json:"cluster_id"`
}

// DistributionUpdateRequest updates soul distribution
type DistributionUpdateRequest struct {
	Plan     DistributionPlan `json:"plan"`
	Revision uint64           `json:"revision"`
}

// DistributionUpdateResponse is the response to a DistributionUpdateRequest
type DistributionUpdateResponse struct {
	Success         bool   `json:"success"`
	Accepted        bool   `json:"accepted"` // If false, client should fetch new plan
	CurrentRevision uint64 `json:"current_revision,omitempty"`
	Error           string `json:"error,omitempty"`
}

// GetDistributionRequest requests the current distribution plan
type GetDistributionRequest struct{}

// GetDistributionResponse returns the current distribution plan
type GetDistributionResponse struct {
	Plan DistributionPlan `json:"plan"`
}

// NodeHealthReport is sent periodically by nodes to report health
type NodeHealthReport struct {
	NodeID       string  `json:"node_id"`
	Timestamp    int64   `json:"timestamp"`
	LoadAvg      float64 `json:"load_avg"`
	MemoryUsage  float64 `json:"memory_usage"`
	DiskUsage    float64 `json:"disk_usage"`
	ActiveSouls  int     `json:"active_souls"`
	CheckRate    float64 `json:"check_rate"` // actual checks/sec
	FailedChecks int     `json:"failed_checks"`
	Region       string  `json:"region"`
}
