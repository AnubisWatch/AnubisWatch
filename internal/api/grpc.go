package api

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// GRPCServer implements the gRPC API
// The sacred protocols of the ancient temples
type GRPCServer struct {
	mu       sync.RWMutex
	config   core.ServerConfig
	listener net.Listener
	logger   *slog.Logger

	// Services
	soulService    SoulService
	judgmentService JudgmentService
	alertService   AlertService
	clusterService ClusterService
}

// SoulService handles soul operations
type SoulService interface {
	ListSouls(ctx context.Context, req *ListSoulsRequest) (*ListSoulsResponse, error)
	GetSoul(ctx context.Context, req *GetSoulRequest) (*SoulResponse, error)
	CreateSoul(ctx context.Context, req *CreateSoulRequest) (*SoulResponse, error)
	UpdateSoul(ctx context.Context, req *UpdateSoulRequest) (*SoulResponse, error)
	DeleteSoul(ctx context.Context, req *DeleteSoulRequest) (*DeleteResponse, error)
	ForceCheck(ctx context.Context, req *ForceCheckRequest) (*JudgmentResponse, error)
}

// JudgmentService handles judgment operations
type JudgmentService interface {
	GetJudgment(ctx context.Context, req *GetJudgmentRequest) (*JudgmentResponse, error)
	ListJudgments(ctx context.Context, req *ListJudgmentsRequest) (*ListJudgmentsResponse, error)
}

// AlertService handles alert operations
type AlertService interface {
	ListChannels(ctx context.Context, req *ListChannelsRequest) (*ListChannelsResponse, error)
	CreateChannel(ctx context.Context, req *CreateChannelRequest) (*ChannelResponse, error)
	DeleteChannel(ctx context.Context, req *DeleteChannelRequest) (*DeleteResponse, error)
	ListRules(ctx context.Context, req *ListRulesRequest) (*ListRulesResponse, error)
	CreateRule(ctx context.Context, req *CreateRuleRequest) (*RuleResponse, error)
	DeleteRule(ctx context.Context, req *DeleteRuleRequest) (*DeleteResponse, error)
}

// ClusterService handles cluster operations
type ClusterService interface {
	GetStatus(ctx context.Context, req *GetClusterStatusRequest) (*ClusterStatusResponse, error)
	GetPeers(ctx context.Context, req *GetClusterPeersRequest) (*ClusterPeersResponse, error)
	Join(ctx context.Context, req *JoinClusterRequest) (*JoinClusterResponse, error)
	Leave(ctx context.Context, req *LeaveClusterRequest) (*LeaveClusterResponse, error)
}

// Request/Response types

type ListSoulsRequest struct {
	Workspace string `json:"workspace"`
	Offset    int32  `json:"offset"`
	Limit     int32  `json:"limit"`
}

type ListSoulsResponse struct {
	Souls []*core.Soul `json:"souls"`
	Total int32        `json:"total"`
}

type GetSoulRequest struct {
	ID string `json:"id"`
}

type SoulResponse struct {
	Soul *core.Soul `json:"soul"`
}

type CreateSoulRequest struct {
	Soul *core.Soul `json:"soul"`
}

type UpdateSoulRequest struct {
	ID   string     `json:"id"`
	Soul *core.Soul `json:"soul"`
}

type DeleteSoulRequest struct {
	ID string `json:"id"`
}

type DeleteResponse struct {
	Success bool `json:"success"`
}

type ForceCheckRequest struct {
	SoulID string `json:"soul_id"`
}

type GetJudgmentRequest struct {
	ID string `json:"id"`
}

type JudgmentResponse struct {
	Judgment *core.Judgment `json:"judgment"`
}

type ListJudgmentsRequest struct {
	SoulID string    `json:"soul_id"`
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	Limit  int32     `json:"limit"`
}

type ListJudgmentsResponse struct {
	Judgments []*core.Judgment `json:"judgments"`
}

type ListChannelsRequest struct {
	Workspace string `json:"workspace"`
}

type ListChannelsResponse struct {
	Channels []*core.AlertChannel `json:"channels"`
}

type CreateChannelRequest struct {
	Channel *core.AlertChannel `json:"channel"`
}

type ChannelResponse struct {
	Channel *core.AlertChannel `json:"channel"`
}

type DeleteChannelRequest struct {
	ID string `json:"id"`
}

type ListRulesRequest struct {
	Workspace string `json:"workspace"`
}

type ListRulesResponse struct {
	Rules []*core.AlertRule `json:"rules"`
}

type CreateRuleRequest struct {
	Rule *core.AlertRule `json:"rule"`
}

type RuleResponse struct {
	Rule *core.AlertRule `json:"rule"`
}

type DeleteRuleRequest struct {
	ID string `json:"id"`
}

type GetClusterStatusRequest struct{}

type ClusterStatusResponse struct {
	Status    string             `json:"status"`
	Role      string             `json:"role"`
	Term      uint64             `json:"term"`
	LeaderID  string             `json:"leader_id"`
	NodeID    string             `json:"node_id"`
	PeerCount int                `json:"peer_count"`
	Stats     core.ClusterStats  `json:"stats"`
}

type GetClusterPeersRequest struct{}

type ClusterPeersResponse struct {
	Peers []PeerInfo `json:"peers"`
}

type PeerInfo struct {
	ID       string    `json:"id"`
	Address  string    `json:"address"`
	Region   string    `json:"region"`
	State    string    `json:"state"`
	LastSeen time.Time `json:"last_seen"`
}

type JoinClusterRequest struct {
	NodeID   string `json:"node_id"`
	Address  string `json:"address"`
	Region   string `json:"region"`
}

type JoinClusterResponse struct {
	Success bool   `json:"success"`
	Peers   []PeerInfo `json:"peers"`
}

type LeaveClusterRequest struct {
	NodeID string `json:"node_id"`
}

type LeaveClusterResponse struct {
	Success bool `json:"success"`
}

// NewGRPCServer creates a new gRPC server
func NewGRPCServer(config core.ServerConfig, logger *slog.Logger) *GRPCServer {
	return &GRPCServer{
		config: config,
		logger: logger.With("component", "grpc_server"),
	}
}

// RegisterServices registers service implementations
func (s *GRPCServer) RegisterServices(
	soul SoulService,
	judgment JudgmentService,
	alert AlertService,
	cluster ClusterService,
) {
	s.soulService = soul
	s.judgmentService = judgment
	s.alertService = alert
	s.clusterService = cluster
}

// Start starts the gRPC server
func (s *GRPCServer) Start() error {
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port+1) // Use port+1 for gRPC
	if addr == ":1" {
		addr = ":9090"
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	s.listener = listener
	s.logger.Info("gRPC server starting", "addr", addr)

	// In a real implementation, this would use google.golang.org/grpc
	// For now, we just accept connections and log them
	go s.serve()

	return nil
}

// serve handles incoming connections
func (s *GRPCServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.isStopped() {
				return
			}
			s.logger.Error("Accept error", "error", err)
			continue
		}

		go s.handleConnection(conn)
	}
}

// handleConnection handles a single gRPC connection
func (s *GRPCServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	// In a real implementation, this would use gRPC framing
	// For now, just log the connection
	s.logger.Debug("gRPC connection accepted", "remote", conn.RemoteAddr())

	// TODO: Implement actual gRPC protocol handling
	// This would require the full gRPC library
}

// Stop stops the gRPC server
func (s *GRPCServer) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

// isStopped checks if the server is stopped
func (s *GRPCServer) isStopped() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listener == nil
}

// GetStats returns gRPC server statistics
func (s *GRPCServer) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"active_connections": 0,
		"total_requests":     0,
	}
}
