// Package grpcapi provides a gRPC API server for AnubisWatch
package grpcapi

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"

	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	v1 "github.com/AnubisWatch/anubiswatch/internal/grpcapi/v1"
)

// Server implements the gRPC AnubisWatchService
type Server struct {
	v1.UnimplementedAnubisWatchServiceServer
	mu        sync.RWMutex
	grpc      *grpc.Server
	listener  net.Listener
	addr      string
	logger    *slog.Logger
	tlsConfig *tls.Config

	store Store
	probe ProbeEngine
	auth  Authenticator
}

// Store defines the storage operations available to the gRPC server
type Store interface {
	GetSoulNoCtx(id string) (interface{}, error)
	ListSoulsNoCtx(workspace string, offset, limit int) ([]interface{}, error)
	SaveSoulNoCtx(soul interface{}) error
	DeleteSoulNoCtx(id string) error

	ListJudgmentsNoCtx(soulID string, start, end time.Time, limit int) ([]interface{}, error)

	GetChannelNoCtx(id string, workspace string) (interface{}, error)
	ListChannelsNoCtx(workspace string) ([]interface{}, error)
	SaveChannelNoCtx(ch interface{}) error
	DeleteChannelNoCtx(id string, workspace string) error

	GetRuleNoCtx(id string, workspace string) (interface{}, error)
	ListRulesNoCtx(workspace string) ([]interface{}, error)
	SaveRuleNoCtx(rule interface{}) error
	DeleteRuleNoCtx(id string, workspace string) error

	GetJourneyNoCtx(id string) (interface{}, error)
	ListJourneysNoCtx(workspace string, offset, limit int) ([]interface{}, error)
	SaveJourneyNoCtx(j interface{}) error
	DeleteJourneyNoCtx(id string) error
	RunJourneyNoCtx(workspace, journeyID string) (interface{}, error)
	ListJourneyRunsNoCtx(workspace, journeyID string, limit int) ([]interface{}, error)
	GetJourneyRunNoCtx(workspace, journeyID, runID string) (interface{}, error)

	ListEvents(soulID string, limit int) ([]interface{}, error)
}

// ProbeEngine interface for probe operations
type ProbeEngine interface {
	ForceCheck(soulID string) (interface{}, error)
}

// AlertManager interface (reserved for future use)
type AlertManager interface{}

// Authenticator interface for authentication
type Authenticator interface {
	Authenticate(token string) (*api.User, error)
}

// contextKey is used for context values
type contextKey int

const (
	userContextKey contextKey = iota
)

// NewServer creates a new gRPC server with authentication
func NewServer(addr string, store Store, probe ProbeEngine, auth Authenticator, logger *slog.Logger, tlsConfig *tls.Config, enableReflection bool) *Server {
	s := &Server{
		addr:      addr,
		logger:    logger,
		store:     store,
		probe:     probe,
		auth:      auth,
		tlsConfig: tlsConfig,
	}

	// Build gRPC options
	opts := []grpc.ServerOption{
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(s.authInterceptor),
		grpc.ChainStreamInterceptor(s.authStreamInterceptor),
	}
	if tlsConfig != nil {
		opts = append(opts, grpc.Creds(credentials.NewTLS(tlsConfig)))
	}

	// Create gRPC server with OpenTelemetry stats handler and auth interceptor
	grpcServer := grpc.NewServer(opts...)

	s.grpc = grpcServer
	v1.RegisterAnubisWatchServiceServer(s.grpc, s)

	// Register reflection if enabled (VULN-007 fix)
	// Default is false for security; set to true only when needed for debugging
	if enableReflection {
		reflection.Register(s.grpc)
		if logger != nil {
			logger.Debug("gRPC reflection enabled")
		}
	} else if logger != nil {
		logger.Info("gRPC reflection disabled for security")
	}

	return s
}

// Start starts the gRPC server
func (s *Server) Start() error {
	lis, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.addr, err)
	}
	s.listener = lis
	if s.logger != nil {
		s.logger.Info("gRPC server starting", "addr", s.addr)
	}
	go func() {
		if err := s.grpc.Serve(lis); err != nil {
			if s.logger != nil {
				s.logger.Error("gRPC server error", "err", err)
			}
		}
	}()
	return nil
}

// Stop gracefully stops the gRPC server
func (s *Server) Stop() {
	if s.grpc != nil {
		s.grpc.GracefulStop()
	}
}

// Helper: convert time to timestamppb
func ts(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

const defaultListLimit = 20

func normalizedListWindow(offset, limit int32) (int, int) {
	normalizedOffset := int(offset)
	if normalizedOffset < 0 {
		normalizedOffset = 0
	}
	normalizedLimit := int(limit)
	if normalizedLimit <= 0 {
		normalizedLimit = defaultListLimit
	}
	return normalizedOffset, normalizedLimit
}

func listFetchLimit(offset, limit int) int {
	return offset + limit + 1
}

func newPagination(total, offset, limit, returned int) *v1.Pagination {
	hasMore := offset+returned < total
	var nextOffset *int32
	if hasMore {
		next := int32(offset + returned)
		nextOffset = &next
	}
	return &v1.Pagination{
		Total:      int32(total),
		Offset:     int32(offset),
		Limit:      int32(limit),
		HasMore:    hasMore,
		NextOffset: nextOffset,
	}
}

func paginate(items []interface{}, offset, limit int) ([]interface{}, *v1.Pagination) {
	total := len(items)
	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	page := items[start:end]
	return page, newPagination(total, offset, limit, len(page))
}

func statusValue(v interface{}) string {
	switch typed := v.(type) {
	case *core.Judgment:
		return string(typed.Status)
	case *core.AlertEvent:
		return string(typed.Status)
	case map[string]interface{}:
		return fmt.Sprintf("%v", typed["status"])
	}
	if hf, ok := v.(interface{ GetStatus() string }); ok {
		return hf.GetStatus()
	}
	return ""
}

func severityValue(v interface{}) string {
	switch typed := v.(type) {
	case *core.AlertEvent:
		return string(typed.Severity)
	case map[string]interface{}:
		return fmt.Sprintf("%v", typed["severity"])
	}
	if hf, ok := v.(interface{ GetSeverity() string }); ok {
		return hf.GetSeverity()
	}
	return ""
}

func timestampValue(v interface{}) time.Time {
	switch typed := v.(type) {
	case *core.Judgment:
		return typed.Timestamp
	case *core.AlertEvent:
		return typed.Timestamp
	}
	if hf, ok := v.(interface{ GetTimestamp() time.Time }); ok {
		return hf.GetTimestamp()
	}
	return time.Time{}
}

func matchesOptionalString(value, expected string) bool {
	return expected == "" || strings.EqualFold(value, expected)
}

// --- PB Conversion: core → protobuf ---

// soulToPB converts a core.Soul to protobuf Soul
func soulToPB(s interface{}) *v1.Soul {
	if soul, ok := s.(*core.Soul); ok {
		return &v1.Soul{
			Id:        soul.ID,
			Name:      soul.Name,
			Type:      string(soul.Type),
			Target:    soul.Target,
			Interval:  int32(soul.Weight.Duration.Seconds()),
			Timeout:   int32(soul.Timeout.Duration.Seconds()),
			Enabled:   soul.Enabled,
			Tags:      soul.Tags,
			Workspace: soul.WorkspaceID,
			CreatedAt: ts(soul.CreatedAt),
			UpdatedAt: ts(soul.UpdatedAt),
		}
	}

	soul, ok := s.(interface {
		GetID() string
		GetName() string
		GetType() string
		GetTarget() string
		GetInterval() time.Duration
		GetTimeout() time.Duration
		GetEnabled() bool
		GetTags() []string
		GetWorkspaceID() string
		GetRegion() string
		GetCreatedAt() time.Time
		GetUpdatedAt() time.Time
		GetHTTP() interface{}
		GetTCP() interface{}
		GetDNS() interface{}
		GetTLS() interface{}
		GetGRPC() interface{}
	})
	if !ok {
		// Try direct type assertion for the concrete type
		type hasFields interface {
			GetID() string
			GetName() string
			GetType() string
			GetTarget() string
			GetWeight() time.Duration
			GetTimeout() time.Duration
			GetEnabled() bool
			GetTags() []string
			GetWorkspaceID() string
			GetRegion() string
			GetCreatedAt() time.Time
			GetUpdatedAt() time.Time
		}
		if hf, ok := s.(hasFields); ok {
			return &v1.Soul{
				Id:        hf.GetID(),
				Name:      hf.GetName(),
				Type:      string(hf.GetType()),
				Target:    hf.GetTarget(),
				Interval:  int32(hf.GetWeight().Seconds()),
				Timeout:   int32(hf.GetTimeout().Seconds()),
				Enabled:   hf.GetEnabled(),
				Tags:      hf.GetTags(),
				Workspace: hf.GetWorkspaceID(),
				CreatedAt: ts(hf.GetCreatedAt()),
				UpdatedAt: ts(hf.GetUpdatedAt()),
			}
		}
		return nil
	}
	return &v1.Soul{
		Id:        soul.GetID(),
		Name:      soul.GetName(),
		Type:      soul.GetType(),
		Target:    soul.GetTarget(),
		Interval:  int32(soul.GetInterval().Seconds()),
		Timeout:   int32(soul.GetTimeout().Seconds()),
		Enabled:   soul.GetEnabled(),
		Tags:      soul.GetTags(),
		Workspace: soul.GetWorkspaceID(),
		CreatedAt: ts(soul.GetCreatedAt()),
		UpdatedAt: ts(soul.GetUpdatedAt()),
	}
}

// judgmentToPB converts a core.Judgment to protobuf Judgment
func judgmentToPB(j interface{}) *v1.Judgment {
	if judgment, ok := j.(*core.Judgment); ok {
		return &v1.Judgment{
			Id:        judgment.ID,
			SoulId:    judgment.SoulID,
			Status:    string(judgment.Status),
			LatencyMs: judgment.Duration.Milliseconds(),
			Message:   judgment.Message,
			Timestamp: ts(judgment.Timestamp),
			NodeId:    judgment.JackalID,
			Region:    judgment.Region,
		}
	}

	type hasFields interface {
		GetID() string
		GetSoulID() string
		GetSoulName() string
		GetStatus() string
		GetDuration() time.Duration
		GetMessage() string
		GetTimestamp() time.Time
		GetJackalID() string
		GetRegion() string
	}
	hf, ok := j.(hasFields)
	if !ok {
		return nil
	}
	return &v1.Judgment{
		Id:        hf.GetID(),
		SoulId:    hf.GetSoulID(),
		SoulName:  hf.GetSoulName(),
		Status:    hf.GetStatus(),
		LatencyMs: hf.GetDuration().Milliseconds(),
		Message:   hf.GetMessage(),
		Timestamp: ts(hf.GetTimestamp()),
		NodeId:    hf.GetJackalID(),
		Region:    hf.GetRegion(),
	}
}

// channelToPB converts a core.AlertChannel to protobuf Channel
func channelToPB(c interface{}) *v1.Channel {
	if channel, ok := c.(*core.AlertChannel); ok {
		strCfg := make(map[string]string, len(channel.Config))
		for k, v := range channel.Config {
			strCfg[k] = fmt.Sprintf("%v", v)
		}
		return &v1.Channel{
			Id:        channel.ID,
			Name:      channel.Name,
			Type:      string(channel.Type),
			Enabled:   channel.Enabled,
			Config:    strCfg,
			Workspace: channel.WorkspaceID,
			CreatedAt: ts(channel.CreatedAt),
		}
	}

	type hasFields interface {
		GetID() string
		GetName() string
		GetType() string
		GetEnabled() bool
		GetConfig() map[string]interface{}
		GetWorkspaceID() string
		GetCreatedAt() time.Time
	}
	hf, ok := c.(hasFields)
	if !ok {
		return nil
	}
	cfg := hf.GetConfig()
	strCfg := make(map[string]string, len(cfg))
	for k, v := range cfg {
		strCfg[k] = fmt.Sprintf("%v", v)
	}
	return &v1.Channel{
		Id:        hf.GetID(),
		Name:      hf.GetName(),
		Type:      hf.GetType(),
		Enabled:   hf.GetEnabled(),
		Config:    strCfg,
		Workspace: hf.GetWorkspaceID(),
		CreatedAt: ts(hf.GetCreatedAt()),
	}
}

// ruleToPB converts a core.AlertRule to protobuf Rule
func ruleToPB(r interface{}) *v1.Rule {
	if rule, ok := r.(*core.AlertRule); ok {
		channelID := ""
		if len(rule.Channels) > 0 {
			channelID = rule.Channels[0]
		}
		return &v1.Rule{
			Id:        rule.ID,
			Name:      rule.Name,
			Enabled:   rule.Enabled,
			ChannelId: channelID,
			Workspace: rule.WorkspaceID,
			CreatedAt: ts(rule.CreatedAt),
		}
	}

	type hasFields interface {
		GetID() string
		GetName() string
		GetEnabled() bool
		GetChannels() []string
		GetWorkspaceID() string
		GetCreatedAt() time.Time
	}
	hf, ok := r.(hasFields)
	if !ok {
		return nil
	}
	channelID := ""
	if ch := hf.GetChannels(); len(ch) > 0 {
		channelID = ch[0]
	}
	return &v1.Rule{
		Id:        hf.GetID(),
		Name:      hf.GetName(),
		Enabled:   hf.GetEnabled(),
		ChannelId: channelID,
		Workspace: hf.GetWorkspaceID(),
		CreatedAt: ts(hf.GetCreatedAt()),
	}
}

// journeyToPB converts a core.JourneyConfig to protobuf Journey
func journeyRunToPB(r interface{}) *v1.JourneyRun {
	if run, ok := r.(*core.JourneyRun); ok {
		var startedAt, completedAt *timestamppb.Timestamp
		if run.StartedAt > 0 {
			startedAt = timestamppb.New(time.UnixMilli(run.StartedAt))
		}
		if run.CompletedAt > 0 {
			completedAt = timestamppb.New(time.UnixMilli(run.CompletedAt))
		}
		steps := make([]*v1.JourneyStepResult, 0, len(run.Steps))
		for _, step := range run.Steps {
			steps = append(steps, &v1.JourneyStepResult{
				Name:       step.Name,
				StepIndex:  int32(step.StepIndex),
				DurationMs: step.Duration,
				Status:     string(step.Status),
				Message:    step.Message,
				Extracted:  step.Extracted,
			})
		}
		return &v1.JourneyRun{
			Id:          run.ID,
			JourneyId:   run.JourneyID,
			Workspace:   run.WorkspaceID,
			JackalId:    run.JackalID,
			Region:      run.Region,
			StartedAt:   startedAt,
			CompletedAt: completedAt,
			DurationMs:  run.Duration,
			Status:      string(run.Status),
			Steps:       steps,
			Variables:   run.Variables,
		}
	}

	type hasStepResultFields interface {
		GetName() string
		GetStepIndex() int
		GetDuration() int64
		GetStatus() string
		GetMessage() string
		GetExtracted() map[string]string
	}
	type hasFields interface {
		GetID() string
		GetJourneyID() string
		GetWorkspaceID() string
		GetJackalID() string
		GetRegion() string
		GetStartedAt() int64
		GetCompletedAt() int64
		GetDuration() int64
		GetStatus() string
		GetSteps() []interface{}
		GetVariables() map[string]string
	}
	hf, ok := r.(hasFields)
	if !ok {
		return nil
	}

	steps := hf.GetSteps()
	pbSteps := make([]*v1.JourneyStepResult, 0, len(steps))
	for _, step := range steps {
		if sf, ok := step.(hasStepResultFields); ok {
			pbSteps = append(pbSteps, &v1.JourneyStepResult{
				Name:       sf.GetName(),
				StepIndex:  int32(sf.GetStepIndex()),
				DurationMs: sf.GetDuration(),
				Status:     sf.GetStatus(),
				Message:    sf.GetMessage(),
				Extracted:  sf.GetExtracted(),
			})
		}
	}

	var startedAt, completedAt *timestamppb.Timestamp
	if hf.GetStartedAt() > 0 {
		startedAt = timestamppb.New(time.UnixMilli(hf.GetStartedAt()))
	}
	if hf.GetCompletedAt() > 0 {
		completedAt = timestamppb.New(time.UnixMilli(hf.GetCompletedAt()))
	}

	return &v1.JourneyRun{
		Id:          hf.GetID(),
		JourneyId:   hf.GetJourneyID(),
		Workspace:   hf.GetWorkspaceID(),
		JackalId:    hf.GetJackalID(),
		Region:      hf.GetRegion(),
		StartedAt:   startedAt,
		CompletedAt: completedAt,
		DurationMs:  hf.GetDuration(),
		Status:      hf.GetStatus(),
		Steps:       pbSteps,
		Variables:   hf.GetVariables(),
	}
}

// journeyToPB converts a journey to protobuf
func journeyToPB(j interface{}) *v1.Journey {
	if journey, ok := j.(*core.JourneyConfig); ok {
		steps := make([]*v1.JourneyStep, 0, len(journey.Steps))
		for _, step := range journey.Steps {
			steps = append(steps, &v1.JourneyStep{
				Name:    step.Name,
				Type:    string(step.Type),
				Target:  step.Target,
				Timeout: int32(step.Timeout.Duration.Seconds()),
			})
		}
		return &v1.Journey{
			Id:          journey.ID,
			Name:        journey.Name,
			Description: journey.Description,
			Interval:    int32(journey.Weight.Duration.Seconds()),
			Enabled:     journey.Enabled,
			Workspace:   journey.WorkspaceID,
			Steps:       steps,
			CreatedAt:   ts(journey.CreatedAt),
		}
	}

	type hasStepFields interface {
		GetName() string
		GetType() string
		GetTarget() string
		GetTimeout() time.Duration
	}
	type hasFields interface {
		GetID() string
		GetName() string
		GetDescription() string
		GetWeight() time.Duration
		GetEnabled() bool
		GetWorkspaceID() string
		GetSteps() []interface{}
		GetCreatedAt() time.Time
	}
	hf, ok := j.(hasFields)
	if !ok {
		return nil
	}
	steps := hf.GetSteps()
	pbSteps := make([]*v1.JourneyStep, 0, len(steps))
	for _, step := range steps {
		if sf, ok := step.(hasStepFields); ok {
			pbSteps = append(pbSteps, &v1.JourneyStep{
				Name:    sf.GetName(),
				Type:    string(sf.GetType()),
				Target:  sf.GetTarget(),
				Timeout: int32(sf.GetTimeout().Seconds()),
			})
		}
	}
	return &v1.Journey{
		Id:          hf.GetID(),
		Name:        hf.GetName(),
		Description: hf.GetDescription(),
		Interval:    int32(hf.GetWeight().Seconds()),
		Enabled:     hf.GetEnabled(),
		Workspace:   hf.GetWorkspaceID(),
		Steps:       pbSteps,
		CreatedAt:   ts(hf.GetCreatedAt()),
	}
}

// --- PB Conversion: protobuf → core (for mutations) ---

func pbToSoulConfig(req *v1.CreateSoulRequest) map[string]interface{} {
	cfg := make(map[string]interface{})
	cfg["name"] = req.Name
	cfg["type"] = req.Type
	cfg["target"] = req.Target
	cfg["interval"] = fmt.Sprintf("%ds", req.Interval)
	cfg["timeout"] = fmt.Sprintf("%ds", req.Timeout)
	cfg["enabled"] = req.Enabled
	cfg["tags"] = req.Tags
	cfg["labels"] = req.Labels
	return cfg
}

func pbToChannelConfig(req *v1.CreateChannelRequest) map[string]interface{} {
	cfg := make(map[string]interface{})
	cfg["name"] = req.Name
	cfg["type"] = req.Type
	cfg["enabled"] = req.Enabled
	// Convert string config back to map
	if req.Config != nil {
		for k, v := range req.Config {
			cfg[k] = v
		}
	}
	cfg["workspace_id"] = req.Workspace
	return cfg
}

func pbToRuleConfig(req *v1.CreateRuleRequest) map[string]interface{} {
	cfg := make(map[string]interface{})
	cfg["name"] = req.Name
	cfg["enabled"] = req.Enabled
	cfg["channels"] = []string{req.ChannelId}
	cfg["workspace_id"] = req.Workspace
	if req.Config != nil {
		for k, v := range req.Config {
			cfg[k] = v
		}
	}
	return cfg
}

func pbToJourneyConfig(req *v1.CreateJourneyRequest) map[string]interface{} {
	cfg := make(map[string]interface{})
	cfg["name"] = req.Name
	cfg["description"] = req.Description
	cfg["interval"] = fmt.Sprintf("%ds", req.Interval)
	cfg["enabled"] = req.Enabled
	cfg["workspace_id"] = req.Workspace
	return cfg
}

func workspaceFromContext(ctx context.Context) (string, error) {
	user, ok := GetUserFromContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "unauthenticated")
	}
	if user.Workspace == "" {
		return "default", nil
	}
	return user.Workspace, nil
}

func resourceWorkspace(v interface{}) string {
	switch resource := v.(type) {
	case *core.Soul:
		return resource.WorkspaceID
	case *core.Judgment:
		return resource.WorkspaceID
	case *core.AlertChannel:
		return resource.WorkspaceID
	case *core.AlertRule:
		return resource.WorkspaceID
	case *core.JourneyConfig:
		return resource.WorkspaceID
	case *core.AlertEvent:
		return resource.WorkspaceID
	case map[string]interface{}:
		if ws, ok := resource["workspace_id"].(string); ok {
			return ws
		}
	}
	if hf, ok := v.(interface{ GetWorkspaceID() string }); ok {
		return hf.GetWorkspaceID()
	}
	return ""
}

func resourceID(v interface{}) string {
	switch resource := v.(type) {
	case *core.Soul:
		return resource.ID
	case *core.Judgment:
		return resource.ID
	case *core.AlertEvent:
		return resource.ID
	case map[string]interface{}:
		if id, ok := resource["id"].(string); ok {
			return id
		}
	}
	if hf, ok := v.(interface{ GetID() string }); ok {
		return hf.GetID()
	}
	return ""
}

func ensureResourceWorkspace(resource interface{}, workspace, name string) error {
	if ws := resourceWorkspace(resource); ws != "" && ws != workspace {
		return status.Errorf(codes.PermissionDenied, "access denied: %s belongs to another workspace", name)
	}
	return nil
}

func (s *Server) ensureSoulAccess(soulID, workspace string) error {
	if soulID == "" {
		return nil
	}
	soul, err := s.store.GetSoulNoCtx(soulID)
	if err != nil || soul == nil {
		return nil
	}
	return ensureResourceWorkspace(soul, workspace, "soul")
}

func applyChannelConfig(config map[string]interface{}, updates map[string]string) {
	if config == nil {
		return
	}
	allowedConfigFields := map[string]bool{
		"webhook_url":     true,
		"channel":         true,
		"bot_token":       true,
		"chat_id":         true,
		"api_key":         true,
		"region":          true,
		"integration_key": true,
		"server":          true,
		"topic":           true,
		"to":              true,
		"from":            true,
		"subject":         true,
		"smtp_host":       true,
		"smtp_port":       true,
		"username":        true,
		"password":        true,
		"use_tls":         true,
		"template":        true,
		"headers":         true,
		"secret":          true,
		"method":          true,
		"url":             true,
	}
	for k, v := range updates {
		if allowedConfigFields[k] {
			config[k] = v
		}
	}
}

func applyRuleConfig(m map[string]interface{}, updates map[string]string) {
	allowedConfigKeys := map[string]bool{
		"channel_ids":        true,
		"cooldown":           true,
		"severity":           true,
		"notification_delay": true,
		"recovery_delay":     true,
		"aggregation_window": true,
	}
	for k, v := range updates {
		if allowedConfigKeys[k] {
			m[k] = v
		}
	}
}

func applyRuleCoreConfig(rule *core.AlertRule, updates map[string]string) {
	for k, v := range updates {
		switch k {
		case "severity":
			rule.Severity = core.Severity(v)
		case "channel_ids":
			if v != "" {
				rule.Channels = strings.Split(v, ",")
			}
		case "cooldown":
			if d, err := time.ParseDuration(v); err == nil {
				rule.Cooldown = core.Duration{Duration: d}
			}
		}
	}
}

func applyJourneyUpdates(journey *core.JourneyConfig, req *v1.UpdateJourneyRequest) {
	if req.Name != nil {
		journey.Name = *req.Name
	}
	if req.Description != nil {
		journey.Description = *req.Description
	}
	if req.Interval != nil {
		journey.Weight = core.Duration{Duration: time.Duration(*req.Interval) * time.Second}
	}
	if req.Enabled != nil {
		journey.Enabled = *req.Enabled
	}
	journey.UpdatedAt = time.Now()
}

func applySoulUpdates(soul *core.Soul, req *v1.UpdateSoulRequest) {
	if req.Name != nil {
		soul.Name = *req.Name
	}
	if req.Target != nil {
		soul.Target = *req.Target
	}
	if req.Interval != nil {
		soul.Weight = core.Duration{Duration: time.Duration(*req.Interval) * time.Second}
	}
	if req.Timeout != nil {
		soul.Timeout = core.Duration{Duration: time.Duration(*req.Timeout) * time.Second}
	}
	if req.Enabled != nil {
		soul.Enabled = *req.Enabled
	}
	if req.Tags != nil {
		soul.Tags = req.Tags
	}
	soul.UpdatedAt = time.Now()
}

func applyChannelUpdates(channel *core.AlertChannel, req *v1.UpdateChannelRequest) {
	if req.Name != nil {
		channel.Name = *req.Name
	}
	if req.Enabled != nil {
		channel.Enabled = *req.Enabled
	}
	if req.Config != nil {
		if channel.Config == nil {
			channel.Config = make(map[string]interface{})
		}
		applyChannelConfig(channel.Config, req.Config)
	}
	channel.UpdatedAt = time.Now()
}

func applyRuleUpdates(rule *core.AlertRule, req *v1.UpdateRuleRequest) {
	if req.Name != nil {
		rule.Name = *req.Name
	}
	if req.Enabled != nil {
		rule.Enabled = *req.Enabled
	}
	if req.Config != nil {
		applyRuleCoreConfig(rule, req.Config)
	}
}

func legacyMapUpdates(req *v1.UpdateSoulRequest) map[string]interface{} {
	updates := make(map[string]interface{})
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Target != nil {
		updates["target"] = *req.Target
	}
	if req.Interval != nil {
		updates["interval"] = fmt.Sprintf("%ds", *req.Interval)
	}
	if req.Timeout != nil {
		updates["timeout"] = fmt.Sprintf("%ds", *req.Timeout)
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if req.Tags != nil {
		updates["tags"] = req.Tags
	}
	if req.Labels != nil {
		updates["labels"] = req.Labels
	}
	return updates
}

func applyJourneyMapUpdates(m map[string]interface{}, req *v1.UpdateJourneyRequest) {
	if req.Name != nil {
		m["name"] = *req.Name
	}
	if req.Description != nil {
		m["description"] = *req.Description
	}
	if req.Interval != nil {
		m["interval"] = fmt.Sprintf("%ds", *req.Interval)
	}
	if req.Enabled != nil {
		m["enabled"] = *req.Enabled
	}
}

func ensureJourneyWorkspace(j interface{}, workspace string) error {
	return ensureResourceWorkspace(j, workspace, "journey")
}

// --- Soul RPCs ---

func (s *Server) ListSouls(ctx context.Context, req *v1.ListSoulsRequest) (*v1.ListSoulsResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Use authenticated user's workspace, not client-supplied
	user, ok := GetUserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}
	workspace := user.Workspace
	if workspace == "" {
		workspace = "default"
	}

	offset, limit := normalizedListWindow(req.Offset, req.Limit)

	souls, err := s.store.ListSoulsNoCtx(workspace, offset, limit+1)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list souls: %v", err)
	}
	hasMore := len(souls) > limit
	if len(souls) > limit {
		souls = souls[:limit]
	}

	pbSouls := make([]*v1.Soul, 0, len(souls))
	for _, soul := range souls {
		if pb := soulToPB(soul); pb != nil {
			pbSouls = append(pbSouls, pb)
		}
	}

	total := offset + len(pbSouls)
	if hasMore {
		total++
	}
	pagination := newPagination(total, offset, limit, len(pbSouls))
	if hasMore {
		pagination.HasMore = true
		next := int32(offset + len(pbSouls))
		pagination.NextOffset = &next
	}

	return &v1.ListSoulsResponse{
		Souls:      pbSouls,
		Pagination: pagination,
	}, nil
}

func (s *Server) GetSoul(ctx context.Context, req *v1.GetSoulRequest) (*v1.Soul, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Verify authenticated user has access to this soul's workspace
	user, ok := GetUserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}

	soul, err := s.store.GetSoulNoCtx(req.Id)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "soul not found: %s", req.Id)
	}
	if s, ok := soul.(*core.Soul); ok && s.WorkspaceID != "" && s.WorkspaceID != user.Workspace {
		return nil, status.Error(codes.PermissionDenied, "access denied: soul belongs to another workspace")
	}
	if pb := soulToPB(soul); pb != nil {
		return pb, nil
	}
	return nil, status.Errorf(codes.Internal, "failed to convert soul")
}

func (s *Server) CreateSoul(ctx context.Context, req *v1.CreateSoulRequest) (*v1.Soul, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Use authenticated user's workspace
	user, ok := GetUserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}

	soulData := pbToSoulConfig(req)
	soulData["workspace_id"] = user.Workspace
	if soulData["workspace_id"] == "" {
		soulData["workspace_id"] = "default"
	}
	if err := s.store.SaveSoulNoCtx(soulData); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create soul: %v", err)
	}

	// Return the created soul by its name (unique within workspace)
	// List souls sorted by creation time descending and return the first one
	// This is a workaround since SaveSoulNoCtx doesn't return the ID
	souls, err := s.store.ListSoulsNoCtx(user.Workspace, 0, 1)
	if err != nil || len(souls) == 0 {
		return nil, status.Errorf(codes.Internal, "soul created but could not be retrieved")
	}

	// Get the first soul returned - it should be the most recently created
	if pb := soulToPB(souls[0]); pb != nil {
		return pb, nil
	}
	return nil, status.Errorf(codes.Internal, "failed to convert created soul")
}

func (s *Server) UpdateSoul(ctx context.Context, req *v1.UpdateSoulRequest) (*v1.Soul, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Verify authenticated user has access to this soul's workspace
	user, ok := GetUserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}

	// Get existing soul first
	existing, err := s.store.GetSoulNoCtx(req.Id)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "soul not found: %s", req.Id)
	}
	if s, ok := existing.(*core.Soul); ok && s.WorkspaceID != "" && s.WorkspaceID != user.Workspace {
		return nil, status.Error(codes.PermissionDenied, "access denied: soul belongs to another workspace")
	}

	switch current := existing.(type) {
	case *core.Soul:
		applySoulUpdates(current, req)
		if err := s.store.SaveSoulNoCtx(current); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to update soul: %v", err)
		}
	case map[string]interface{}:
		updates := legacyMapUpdates(req)
		for k, v := range updates {
			current[k] = v
		}
		if err := s.store.SaveSoulNoCtx(current); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to update soul: %v", err)
		}
	}

	existing, _ = s.store.GetSoulNoCtx(req.Id)
	if pb := soulToPB(existing); pb != nil {
		return pb, nil
	}
	return nil, status.Errorf(codes.Internal, "failed to convert updated soul")
}

func (s *Server) DeleteSoul(ctx context.Context, req *v1.DeleteSoulRequest) (*emptypb.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Verify authenticated user has access to this soul's workspace
	user, ok := GetUserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}
	existing, err := s.store.GetSoulNoCtx(req.Id)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "soul not found: %s", req.Id)
	}
	if s, ok := existing.(*core.Soul); ok && s.WorkspaceID != "" && s.WorkspaceID != user.Workspace {
		return nil, status.Error(codes.PermissionDenied, "access denied: soul belongs to another workspace")
	}
	if err := s.store.DeleteSoulNoCtx(req.Id); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete soul: %v", err)
	}
	return &emptypb.Empty{}, nil
}

// --- Judgment RPCs ---

func (s *Server) ListJudgments(ctx context.Context, req *v1.ListJudgmentsRequest) (*v1.ListJudgmentsResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user, ok := GetUserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}

	offset, limit := normalizedListWindow(req.Offset, req.Limit)

	var start, end time.Time
	if req.Since != nil {
		start = req.Since.AsTime()
	}
	if req.Until != nil {
		end = req.Until.AsTime()
	}

	soulID := ""
	if req.SoulId != nil {
		soulID = *req.SoulId
	}

	// If soulID specified, verify it belongs to caller's workspace (IDOR protection).
	// If soul does not exist, allow the call to proceed � ListJudgments will return empty.
	if soulID != "" {
		soul, err := s.store.GetSoulNoCtx(soulID)
		if err == nil && soul != nil {
			if s, ok := soul.(*core.Soul); ok && s.WorkspaceID != "" && s.WorkspaceID != user.Workspace {
				return nil, status.Error(codes.PermissionDenied, "access denied: soul belongs to another workspace")
			}
		}
	}

	fetchLimit := listFetchLimit(offset, limit)
	if req.GetStatus() != "" || soulID == "" {
		fetchLimit = 0
	}
	var judgments []interface{}
	if soulID == "" {
		souls, err := s.store.ListSoulsNoCtx(user.Workspace, 0, 0)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to list souls for judgments: %v", err)
		}
		for _, soul := range souls {
			id := resourceID(soul)
			if id == "" {
				continue
			}
			soulJudgments, err := s.store.ListJudgmentsNoCtx(id, start, end, fetchLimit)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to list judgments: %v", err)
			}
			judgments = append(judgments, soulJudgments...)
		}
	} else {
		var err error
		judgments, err = s.store.ListJudgmentsNoCtx(soulID, start, end, fetchLimit)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to list judgments: %v", err)
		}
	}

	filtered := make([]interface{}, 0, len(judgments))
	for _, j := range judgments {
		if matchesOptionalString(statusValue(j), req.GetStatus()) {
			filtered = append(filtered, j)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return timestampValue(filtered[i]).After(timestampValue(filtered[j]))
	})
	page, pagination := paginate(filtered, offset, limit)

	pbJudgments := make([]*v1.Judgment, 0, len(page))
	for _, j := range page {
		if pb := judgmentToPB(j); pb != nil {
			pbJudgments = append(pbJudgments, pb)
		}
	}

	return &v1.ListJudgmentsResponse{
		Judgments:  pbJudgments,
		Pagination: pagination,
	}, nil
}

func (s *Server) GetSoulJudgments(ctx context.Context, req *v1.GetSoulJudgmentsRequest) (*v1.ListJudgmentsResponse, error) {
	return s.ListJudgments(ctx, &v1.ListJudgmentsRequest{
		Offset: req.Offset,
		Limit:  req.Limit,
		SoulId: &req.SoulId,
	})
}

func (s *Server) JudgeSoul(ctx context.Context, req *v1.JudgeSoulRequest) (*v1.Judgment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	workspace, err := workspaceFromContext(ctx)
	if err != nil {
		return nil, err
	}
	soul, err := s.store.GetSoulNoCtx(req.SoulId)
	if err != nil || soul == nil {
		return nil, status.Errorf(codes.NotFound, "soul not found: %s", req.SoulId)
	}
	if err := ensureResourceWorkspace(soul, workspace, "soul"); err != nil {
		return nil, err
	}

	result, err := s.probe.ForceCheck(req.SoulId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to judge soul: %v", err)
	}
	if pb := judgmentToPB(result); pb != nil {
		return pb, nil
	}
	return nil, status.Errorf(codes.Internal, "failed to convert judgment")
}

// --- Verdict RPCs ---

func (s *Server) ListVerdicts(ctx context.Context, req *v1.ListVerdictsRequest) (*v1.ListVerdictsResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	workspace, err := workspaceFromContext(ctx)
	if err != nil {
		return nil, err
	}

	// Verdicts come from alert events. List recent events.
	offset, limit := normalizedListWindow(req.Offset, req.Limit)

	soulID := ""
	if req.SoulId != nil {
		soulID = *req.SoulId
	}
	if err := s.ensureSoulAccess(soulID, workspace); err != nil {
		return nil, err
	}

	fetchLimit := listFetchLimit(offset, limit)
	if req.GetStatus() != "" || req.GetSeverity() != "" {
		fetchLimit = 0
	}
	events, err := s.store.ListEvents(soulID, fetchLimit)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list verdicts: %v", err)
	}

	filtered := make([]interface{}, 0, len(events))
	for _, e := range events {
		if ws := resourceWorkspace(e); ws != "" && ws != workspace {
			continue
		}
		if !matchesOptionalString(statusValue(e), req.GetStatus()) {
			continue
		}
		if !matchesOptionalString(severityValue(e), req.GetSeverity()) {
			continue
		}
		filtered = append(filtered, e)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return timestampValue(filtered[i]).After(timestampValue(filtered[j]))
	})
	page, pagination := paginate(filtered, offset, limit)

	pbVerdicts := make([]*v1.Verdict, 0, len(page))
	for _, e := range page {
		if pb := eventToVerdict(e); pb != nil {
			pbVerdicts = append(pbVerdicts, pb)
		}
	}

	return &v1.ListVerdictsResponse{
		Verdicts:   pbVerdicts,
		Pagination: pagination,
	}, nil
}

func eventToVerdict(e interface{}) *v1.Verdict {
	if event, ok := e.(*core.AlertEvent); ok {
		status := "firing"
		if event.Resolved {
			status = "resolved"
		} else if event.Acknowledged {
			status = "acknowledged"
		}
		return &v1.Verdict{
			Id:       event.ID,
			SoulId:   event.SoulID,
			SoulName: event.SoulName,
			RuleId:   event.ChannelID,
			Status:   status,
			Severity: string(event.Severity),
			Message:  event.Message,
			FiredAt:  ts(event.Timestamp),
		}
	}

	type hasFields interface {
		GetID() string
		GetSoulID() string
		GetSoulName() string
		GetChannelID() string
		GetStatus() string
		GetSeverity() string
		GetMessage() string
		GetTimestamp() time.Time
		GetResolved() bool
		GetAcknowledged() bool
	}
	hf, ok := e.(hasFields)
	if !ok {
		return nil
	}
	status := "firing"
	if hf.GetResolved() {
		status = "resolved"
	} else if hf.GetAcknowledged() {
		status = "acknowledged"
	}
	return &v1.Verdict{
		Id:       hf.GetID(),
		SoulId:   hf.GetSoulID(),
		SoulName: hf.GetSoulName(),
		RuleId:   hf.GetChannelID(),
		Status:   status,
		Severity: hf.GetSeverity(),
		Message:  hf.GetMessage(),
		FiredAt:  ts(hf.GetTimestamp()),
	}
}

// --- Channel RPCs ---

func (s *Server) ListChannels(ctx context.Context, req *v1.ListChannelsRequest) (*v1.ListChannelsResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	workspace, err := workspaceFromContext(ctx)
	if err != nil {
		return nil, err
	}
	offset, limit := normalizedListWindow(req.Offset, req.Limit)

	channels, err := s.store.ListChannelsNoCtx(workspace)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list channels: %v", err)
	}
	page, pagination := paginate(channels, offset, limit)

	pbChannels := make([]*v1.Channel, 0, len(page))
	for _, ch := range page {
		if pb := channelToPB(ch); pb != nil {
			pbChannels = append(pbChannels, pb)
		}
	}

	return &v1.ListChannelsResponse{
		Channels:   pbChannels,
		Pagination: pagination,
	}, nil
}

func (s *Server) GetChannel(ctx context.Context, req *v1.GetChannelRequest) (*v1.Channel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user, ok := GetUserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}

	ch, err := s.store.GetChannelNoCtx(req.Id, user.Workspace)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "channel not found: %s", req.Id)
	}
	if pb := channelToPB(ch); pb != nil {
		return pb, nil
	}
	return nil, status.Errorf(codes.Internal, "failed to convert channel")
}

func (s *Server) CreateChannel(ctx context.Context, req *v1.CreateChannelRequest) (*v1.Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspace, err := workspaceFromContext(ctx)
	if err != nil {
		return nil, err
	}

	channelData := pbToChannelConfig(req)
	channelData["workspace_id"] = workspace
	if err := s.store.SaveChannelNoCtx(channelData); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create channel: %v", err)
	}

	channels, _ := s.store.ListChannelsNoCtx(workspace)
	if len(channels) > 0 {
		if pb := channelToPB(channels[len(channels)-1]); pb != nil {
			return pb, nil
		}
	}
	return nil, status.Errorf(codes.Internal, "channel created but could not be retrieved")
}

func (s *Server) UpdateChannel(ctx context.Context, req *v1.UpdateChannelRequest) (*v1.Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspace, err := workspaceFromContext(ctx)
	if err != nil {
		return nil, err
	}

	existing, err := s.store.GetChannelNoCtx(req.Id, workspace)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "channel not found: %s", req.Id)
	}
	if err := ensureResourceWorkspace(existing, workspace, "channel"); err != nil {
		return nil, err
	}

	switch current := existing.(type) {
	case *core.AlertChannel:
		applyChannelUpdates(current, req)
		if err := s.store.SaveChannelNoCtx(current); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to update channel: %v", err)
		}
	case map[string]interface{}:
		if req.Name != nil {
			current["name"] = *req.Name
		}
		if req.Enabled != nil {
			current["enabled"] = *req.Enabled
		}
		if req.Config != nil {
			applyChannelConfig(current, req.Config)
		}
		if err := s.store.SaveChannelNoCtx(current); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to update channel: %v", err)
		}
	}

	existing, _ = s.store.GetChannelNoCtx(req.Id, workspace)
	if pb := channelToPB(existing); pb != nil {
		return pb, nil
	}
	return nil, status.Errorf(codes.Internal, "failed to convert updated channel")
}

func (s *Server) DeleteChannel(ctx context.Context, req *v1.DeleteChannelRequest) (*emptypb.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspace, err := workspaceFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.store.GetChannelNoCtx(req.Id, workspace); err != nil {
		return nil, status.Errorf(codes.NotFound, "channel not found: %s", req.Id)
	}
	if err := s.store.DeleteChannelNoCtx(req.Id, workspace); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete channel: %v", err)
	}
	return &emptypb.Empty{}, nil
}

// --- Rule RPCs ---

func (s *Server) ListRules(ctx context.Context, req *v1.ListRulesRequest) (*v1.ListRulesResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	workspace, err := workspaceFromContext(ctx)
	if err != nil {
		return nil, err
	}
	offset, limit := normalizedListWindow(req.Offset, req.Limit)

	rules, err := s.store.ListRulesNoCtx(workspace)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list rules: %v", err)
	}
	page, pagination := paginate(rules, offset, limit)

	pbRules := make([]*v1.Rule, 0, len(page))
	for _, r := range page {
		if pb := ruleToPB(r); pb != nil {
			pbRules = append(pbRules, pb)
		}
	}

	return &v1.ListRulesResponse{
		Rules:      pbRules,
		Pagination: pagination,
	}, nil
}

func (s *Server) GetRule(ctx context.Context, req *v1.GetRuleRequest) (*v1.Rule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user, ok := GetUserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}

	r, err := s.store.GetRuleNoCtx(req.Id, user.Workspace)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "rule not found: %s", req.Id)
	}
	// Verify rule belongs to caller's workspace (IDOR protection)
	if rule, ok := r.(*core.AlertRule); ok && rule.WorkspaceID != "" && rule.WorkspaceID != user.Workspace {
		return nil, status.Error(codes.PermissionDenied, "access denied: rule belongs to another workspace")
	}
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "rule not found: %s", req.Id)
	}
	if pb := ruleToPB(r); pb != nil {
		return pb, nil
	}
	return nil, status.Errorf(codes.Internal, "failed to convert rule")
}

func (s *Server) CreateRule(ctx context.Context, req *v1.CreateRuleRequest) (*v1.Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspace, err := workspaceFromContext(ctx)
	if err != nil {
		return nil, err
	}

	ruleData := pbToRuleConfig(req)
	ruleData["workspace_id"] = workspace
	if err := s.store.SaveRuleNoCtx(ruleData); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create rule: %v", err)
	}

	rules, _ := s.store.ListRulesNoCtx(workspace)
	if len(rules) > 0 {
		if pb := ruleToPB(rules[len(rules)-1]); pb != nil {
			return pb, nil
		}
	}
	return nil, status.Errorf(codes.Internal, "rule created but could not be retrieved")
}

func (s *Server) UpdateRule(ctx context.Context, req *v1.UpdateRuleRequest) (*v1.Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := GetUserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}

	existing, err := s.store.GetRuleNoCtx(req.Id, user.Workspace)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "rule not found: %s", req.Id)
	}

	if err := ensureResourceWorkspace(existing, user.Workspace, "rule"); err != nil {
		return nil, err
	}

	switch current := existing.(type) {
	case *core.AlertRule:
		applyRuleUpdates(current, req)
		if err := s.store.SaveRuleNoCtx(current); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to update rule: %v", err)
		}
	case map[string]interface{}:
		if req.Name != nil {
			current["name"] = *req.Name
		}
		if req.Enabled != nil {
			current["enabled"] = *req.Enabled
		}
		if req.Config != nil {
			applyRuleConfig(current, req.Config)
		}
		if err := s.store.SaveRuleNoCtx(current); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to update rule: %v", err)
		}
	}

	existing, _ = s.store.GetRuleNoCtx(req.Id, user.Workspace)
	if pb := ruleToPB(existing); pb != nil {
		return pb, nil
	}
	return nil, status.Errorf(codes.Internal, "failed to convert updated rule")
}

func (s *Server) DeleteRule(ctx context.Context, req *v1.DeleteRuleRequest) (*emptypb.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspace, err := workspaceFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.store.GetRuleNoCtx(req.Id, workspace); err != nil {
		return nil, status.Errorf(codes.NotFound, "rule not found: %s", req.Id)
	}
	if err := s.store.DeleteRuleNoCtx(req.Id, workspace); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete rule: %v", err)
	}
	return &emptypb.Empty{}, nil
}

// --- Journey RPCs ---

func (s *Server) ListJourneys(ctx context.Context, req *v1.ListJourneysRequest) (*v1.ListJourneysResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	workspace, err := workspaceFromContext(ctx)
	if err != nil {
		return nil, err
	}
	offset, limit := normalizedListWindow(req.Offset, req.Limit)

	journeys, err := s.store.ListJourneysNoCtx(workspace, 0, 0)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list journeys: %v", err)
	}
	page, pagination := paginate(journeys, offset, limit)

	pbJourneys := make([]*v1.Journey, 0, len(page))
	for _, j := range page {
		if pb := journeyToPB(j); pb != nil {
			pbJourneys = append(pbJourneys, pb)
		}
	}

	return &v1.ListJourneysResponse{
		Journeys:   pbJourneys,
		Pagination: pagination,
	}, nil
}

func (s *Server) GetJourney(ctx context.Context, req *v1.GetJourneyRequest) (*v1.Journey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	workspace, err := workspaceFromContext(ctx)
	if err != nil {
		return nil, err
	}

	j, err := s.store.GetJourneyNoCtx(req.Id)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "journey not found: %s", req.Id)
	}
	if err := ensureJourneyWorkspace(j, workspace); err != nil {
		return nil, err
	}
	if pb := journeyToPB(j); pb != nil {
		return pb, nil
	}
	return nil, status.Errorf(codes.Internal, "failed to convert journey")
}

func (s *Server) CreateJourney(ctx context.Context, req *v1.CreateJourneyRequest) (*v1.Journey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspace, err := workspaceFromContext(ctx)
	if err != nil {
		return nil, err
	}

	journeyData := pbToJourneyConfig(req)
	journeyData["workspace_id"] = workspace
	if err := s.store.SaveJourneyNoCtx(journeyData); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create journey: %v", err)
	}

	journeys, _ := s.store.ListJourneysNoCtx(workspace, 0, 1)
	if len(journeys) > 0 {
		if pb := journeyToPB(journeys[0]); pb != nil {
			return pb, nil
		}
	}
	return nil, status.Errorf(codes.Internal, "journey created but could not be retrieved")
}

func (s *Server) UpdateJourney(ctx context.Context, req *v1.UpdateJourneyRequest) (*v1.Journey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	workspace, err := workspaceFromContext(ctx)
	if err != nil {
		return nil, err
	}

	existing, err := s.store.GetJourneyNoCtx(req.Id)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "journey not found: %s", req.Id)
	}
	if err := ensureJourneyWorkspace(existing, workspace); err != nil {
		return nil, err
	}

	switch current := existing.(type) {
	case *core.JourneyConfig:
		applyJourneyUpdates(current, req)
		if err := s.store.SaveJourneyNoCtx(current); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to update journey: %v", err)
		}
	case map[string]interface{}:
		applyJourneyMapUpdates(current, req)
		if err := s.store.SaveJourneyNoCtx(current); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to update journey: %v", err)
		}
	}

	existing, _ = s.store.GetJourneyNoCtx(req.Id)
	if pb := journeyToPB(existing); pb != nil {
		return pb, nil
	}
	return nil, status.Errorf(codes.Internal, "failed to convert updated journey")
}

func (s *Server) DeleteJourney(ctx context.Context, req *v1.DeleteJourneyRequest) (*emptypb.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	workspace, err := workspaceFromContext(ctx)
	if err != nil {
		return nil, err
	}
	existing, err := s.store.GetJourneyNoCtx(req.Id)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "journey not found: %s", req.Id)
	}
	if err := ensureJourneyWorkspace(existing, workspace); err != nil {
		return nil, err
	}
	if err := s.store.DeleteJourneyNoCtx(req.Id); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete journey: %v", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *Server) RunJourney(ctx context.Context, req *v1.RunJourneyRequest) (*v1.RunJourneyResponse, error) {
	workspace, err := workspaceFromContext(ctx)
	if err != nil {
		return nil, err
	}

	journey, err := s.store.GetJourneyNoCtx(req.Id)
	if err != nil || journey == nil {
		return nil, status.Errorf(codes.NotFound, "journey not found: %s", req.Id)
	}
	if err := ensureJourneyWorkspace(journey, workspace); err != nil {
		return nil, err
	}

	run, err := s.store.RunJourneyNoCtx(workspace, req.Id)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to run journey: %v", err)
	}
	pbRun := journeyRunToPB(run)
	if pbRun == nil {
		return nil, status.Errorf(codes.Internal, "failed to convert journey run")
	}

	return &v1.RunJourneyResponse{
		JourneyId: req.Id,
		Status:    pbRun.Status,
		Message:   fmt.Sprintf("Journey execution completed with status %s", pbRun.Status),
	}, nil
}

func (s *Server) ListJourneyRuns(ctx context.Context, req *v1.ListJourneyRunsRequest) (*v1.ListJourneyRunsResponse, error) {
	workspace, err := workspaceFromContext(ctx)
	if err != nil {
		return nil, err
	}

	limit := int(req.Limit)
	if limit <= 0 {
		limit = 20
	}

	runsIface, err := s.store.ListJourneyRunsNoCtx(workspace, req.JourneyId, limit)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list journey runs: %v", err)
	}

	runs := make([]*v1.JourneyRun, 0, len(runsIface))
	for _, r := range runsIface {
		if pb := journeyRunToPB(r); pb != nil {
			runs = append(runs, pb)
		}
	}

	return &v1.ListJourneyRunsResponse{
		Runs:  runs,
		Total: int32(len(runs)),
	}, nil
}

func (s *Server) GetJourneyRun(ctx context.Context, req *v1.GetJourneyRunRequest) (*v1.JourneyRun, error) {
	workspace, err := workspaceFromContext(ctx)
	if err != nil {
		return nil, err
	}

	run, err := s.store.GetJourneyRunNoCtx(workspace, req.JourneyId, req.RunId)
	if err != nil || run == nil {
		return nil, status.Errorf(codes.NotFound, "journey run not found: %s", req.RunId)
	}

	if pb := journeyRunToPB(run); pb != nil {
		return pb, nil
	}
	return nil, status.Errorf(codes.Internal, "failed to convert journey run")
}

// --- Cluster RPCs ---

func (s *Server) GetClusterStatus(ctx context.Context, req *emptypb.Empty) (*v1.ClusterStatus, error) {
	return &v1.ClusterStatus{
		Clustered: false,
		IsLeader:  true,
		NodeId:    "single-node",
		NodeCount: 1,
	}, nil
}

// --- Streaming RPCs ---

func (s *Server) StreamJudgments(req *v1.StreamRequest, stream v1.AnubisWatchService_StreamJudgmentsServer) error {
	workspace, err := workspaceFromContext(stream.Context())
	if err != nil {
		return err
	}

	// Poll-based streaming: check for new judgments every second
	soulID := ""
	if req.SoulId != nil {
		soulID = *req.SoulId
	}
	if err := s.ensureSoulAccess(soulID, workspace); err != nil {
		return err
	}

	seen := make(map[string]bool)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stream.Context().Done():
			return nil
		case <-ticker.C:
			s.mu.RLock()
			judgments, err := s.store.ListJudgmentsNoCtx(soulID, time.Now().Add(-5*time.Minute), time.Now(), 50)
			s.mu.RUnlock()

			if err != nil {
				continue
			}

			for _, j := range judgments {
				if ws := resourceWorkspace(j); ws != "" && ws != workspace {
					continue
				}
				if id := resourceID(j); id != "" {
					if !seen[id] {
						seen[id] = true
						if pb := judgmentToPB(j); pb != nil {
							if err := stream.Send(pb); err != nil {
								return err
							}
						}
					}
				}
			}
		}
	}
}

func (s *Server) StreamVerdicts(req *v1.StreamRequest, stream v1.AnubisWatchService_StreamVerdictsServer) error {
	workspace, err := workspaceFromContext(stream.Context())
	if err != nil {
		return err
	}

	// Poll-based streaming: check for new alert events every second
	soulID := ""
	if req.SoulId != nil {
		soulID = *req.SoulId
	}
	if err := s.ensureSoulAccess(soulID, workspace); err != nil {
		return err
	}

	seen := make(map[string]bool)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stream.Context().Done():
			return nil
		case <-ticker.C:
			s.mu.RLock()
			events, err := s.store.ListEvents(soulID, 50)
			s.mu.RUnlock()

			if err != nil {
				continue
			}

			for _, e := range events {
				if ws := resourceWorkspace(e); ws != "" && ws != workspace {
					continue
				}
				if id := resourceID(e); id != "" {
					if !seen[id] {
						seen[id] = true
						if pb := eventToVerdict(e); pb != nil {
							if err := stream.Send(pb); err != nil {
								return err
							}
						}
					}
				}
			}
		}
	}
}

// authInterceptor is a gRPC unary interceptor for authentication
func (s *Server) authInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	// Extract token from metadata
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing metadata")
	}

	// Get authorization header
	authHeader := md.Get("authorization")
	if len(authHeader) == 0 {
		return nil, status.Error(codes.Unauthenticated, "missing authorization header")
	}

	// Extract token (Bearer token)
	token := authHeader[0]
	if strings.HasPrefix(token, "Bearer ") {
		token = strings.TrimPrefix(token, "Bearer ")
	}

	// Validate token
	user, err := s.auth.Authenticate(token)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("gRPC authentication failed", "error", err, "method", info.FullMethod)
		}
		return nil, status.Error(codes.Unauthenticated, "invalid token")
	}

	// Add user to context
	ctx = context.WithValue(ctx, userContextKey, user)

	return handler(ctx, req)
}

// authStreamInterceptor is a gRPC stream interceptor for authentication
func (s *Server) authStreamInterceptor(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	// Extract token from metadata
	ctx := ss.Context()
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "missing metadata")
	}

	// Get authorization header
	authHeader := md.Get("authorization")
	if len(authHeader) == 0 {
		return status.Error(codes.Unauthenticated, "missing authorization header")
	}

	// Extract token (Bearer token)
	token := authHeader[0]
	if strings.HasPrefix(token, "Bearer ") {
		token = strings.TrimPrefix(token, "Bearer ")
	}

	// Validate token
	user, err := s.auth.Authenticate(token)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("gRPC stream authentication failed", "error", err, "method", info.FullMethod)
		}
		return status.Error(codes.Unauthenticated, "invalid token")
	}

	// Add user to context
	ctx = context.WithValue(ctx, userContextKey, user)

	// Wrap the stream with the new context
	wrapped := &wrappedStream{ServerStream: ss, ctx: ctx}

	return handler(srv, wrapped)
}

// wrappedStream wraps a grpc.ServerStream with a new context
type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context {
	return w.ctx
}

// GetUserFromContext retrieves the authenticated user from the context
func GetUserFromContext(ctx context.Context) (*api.User, bool) {
	user, ok := ctx.Value(userContextKey).(*api.User)
	return user, ok
}
