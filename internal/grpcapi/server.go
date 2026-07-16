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

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

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

// Store defines the storage operations available to the gRPC server.
// Every method uses concrete *core.* types — no interface{} fallbacks.
type Store interface {
	GetSoulNoCtx(id string) (*core.Soul, error)
	ListSoulsNoCtx(workspace string, offset, limit int) ([]*core.Soul, error)
	SaveSoulNoCtx(soul *core.Soul) error
	DeleteSoulNoCtx(id string) error

	ListJudgmentsNoCtx(soulID string, start, end time.Time, limit int) ([]*core.Judgment, error)

	GetChannelNoCtx(id string, workspace string) (*core.AlertChannel, error)
	ListChannelsNoCtx(workspace string) ([]*core.AlertChannel, error)
	SaveChannelNoCtx(ch *core.AlertChannel) error
	DeleteChannelNoCtx(id string, workspace string) error

	GetRuleNoCtx(id string, workspace string) (*core.AlertRule, error)
	ListRulesNoCtx(workspace string) ([]*core.AlertRule, error)
	SaveRuleNoCtx(rule *core.AlertRule) error
	DeleteRuleNoCtx(id string, workspace string) error

	GetJourneyNoCtx(id string) (*core.JourneyConfig, error)
	ListJourneysNoCtx(workspace string, offset, limit int) ([]*core.JourneyConfig, error)
	SaveJourneyNoCtx(j *core.JourneyConfig) error
	DeleteJourneyNoCtx(id string) error
	RunJourneyNoCtx(workspace, journeyID string) (*core.JourneyRun, error)
	ListJourneyRunsNoCtx(workspace, journeyID string, limit int) ([]*core.JourneyRun, error)
	GetJourneyRunNoCtx(workspace, journeyID, runID string) (*core.JourneyRun, error)

	ListEvents(soulID string, limit int) ([]*core.AlertEvent, error)
}

// ProbeEngine interface for probe operations
type ProbeEngine interface {
	ForceCheck(soulID string) (*core.Judgment, error)
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

// Stop gracefully stops the gRPC server, blocking until in-flight RPCs finish.
func (s *Server) Stop() {
	if s.grpc != nil {
		s.grpc.GracefulStop()
	}
}

// StopWithContext gracefully stops the server but bounds the drain by ctx: if
// the deadline elapses (e.g. a long-lived streaming RPC won't return), it force
// stops so shutdown cannot hang past the process's SIGTERM grace period.
func (s *Server) StopWithContext(ctx context.Context) {
	if s.grpc == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		s.grpc.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		s.grpc.Stop() // force-close connections; unblocks GracefulStop goroutine
		<-done
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

func matchesSoulFilters(soul *core.Soul, req *v1.ListSoulsRequest) bool {
	if req.GetType() != "" && !strings.EqualFold(string(soul.Type), req.GetType()) {
		return false
	}
	if req.GetTag() != "" {
		matched := false
		for _, tag := range soul.Tags {
			if strings.EqualFold(tag, req.GetTag()) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func paginateSouls(items []*core.Soul, offset, limit int) ([]*core.Soul, *v1.Pagination) {
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

func paginateJudgments(items []*core.Judgment, offset, limit int) ([]*core.Judgment, *v1.Pagination) {
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

// --- PB Conversion: core → protobuf ---

// soulToPB converts a core.Soul to protobuf Soul
func soulToPB(soul *core.Soul) *v1.Soul {
	if soul == nil {
		return nil
	}
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

// judgmentToPB converts a core.Judgment to protobuf Judgment
func judgmentToPB(j *core.Judgment) *v1.Judgment {
	if j == nil {
		return nil
	}
	return &v1.Judgment{
		Id:        j.ID,
		SoulId:    j.SoulID,
		Status:    string(j.Status),
		LatencyMs: j.Duration.Milliseconds(),
		Message:   j.Message,
		Timestamp: ts(j.Timestamp),
		NodeId:    j.JackalID,
		Region:    j.Region,
	}
}

// channelToPB converts a core.AlertChannel to protobuf Channel
func channelToPB(ch *core.AlertChannel) *v1.Channel {
	if ch == nil {
		return nil
	}
	strCfg := make(map[string]string, len(ch.Config))
	for k, v := range ch.Config {
		strCfg[k] = fmt.Sprintf("%v", v)
	}
	return &v1.Channel{
		Id:        ch.ID,
		Name:      ch.Name,
		Type:      string(ch.Type),
		Enabled:   ch.Enabled,
		Config:    strCfg,
		Workspace: ch.WorkspaceID,
		CreatedAt: ts(ch.CreatedAt),
	}
}

// ruleToPB converts a core.AlertRule to protobuf Rule
func ruleToPB(rule *core.AlertRule) *v1.Rule {
	if rule == nil {
		return nil
	}
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

// journeyRunToPB converts a core.JourneyRun to protobuf JourneyRun
func journeyRunToPB(run *core.JourneyRun) *v1.JourneyRun {
	if run == nil {
		return nil
	}
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

// journeyToPB converts a core.JourneyConfig to protobuf Journey
func journeyToPB(j *core.JourneyConfig) *v1.Journey {
	if j == nil {
		return nil
	}
	steps := make([]*v1.JourneyStep, 0, len(j.Steps))
	for _, step := range j.Steps {
		steps = append(steps, &v1.JourneyStep{
			Name:    step.Name,
			Type:    string(step.Type),
			Target:  step.Target,
			Timeout: int32(step.Timeout.Duration.Seconds()),
		})
	}
	return &v1.Journey{
		Id:          j.ID,
		Name:        j.Name,
		Description: j.Description,
		Interval:    int32(j.Weight.Duration.Seconds()),
		Enabled:     j.Enabled,
		Workspace:   j.WorkspaceID,
		Steps:       steps,
		CreatedAt:   ts(j.CreatedAt),
	}
}

// --- PB Conversion: core → protobuf (for alert events) ---

// eventToVerdict converts a core.AlertEvent to protobuf Verdict
func eventToVerdict(event *core.AlertEvent) *v1.Verdict {
	if event == nil {
		return nil
	}
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

// --- PB Conversion: protobuf → core (for mutations) ---

func pbToSoulConfig(req *v1.CreateSoulRequest) *core.Soul {
	return &core.Soul{
		ID:       core.GenerateID(),
		Name:     req.Name,
		Type:     core.CheckType(req.Type),
		Target:   req.Target,
		Weight:   core.Duration{Duration: time.Duration(req.Interval) * time.Second},
		Timeout:  core.Duration{Duration: time.Duration(req.Timeout) * time.Second},
		Enabled:  req.Enabled,
		Tags:     req.Tags,
	}
}

func pbToChannelConfig(req *v1.CreateChannelRequest) *core.AlertChannel {
	channelType := core.AlertChannelType(req.Type)
	now := time.Now()
	ch := &core.AlertChannel{
		ID:        core.GenerateID(),
		Name:      req.Name,
		Type:      channelType,
		Enabled:   req.Enabled,
		Config:    make(map[string]interface{}),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if req.Config != nil {
		for k, v := range req.Config {
			ch.Config[k] = v
		}
	}
	return ch
}

func pbToRuleConfig(req *v1.CreateRuleRequest) *core.AlertRule {
	now := time.Now()
	return &core.AlertRule{
		ID:        core.GenerateID(),
		Name:      req.Name,
		Enabled:   req.Enabled,
		Channels:  []string{req.ChannelId},
		CreatedAt: now,
	}
}

func pbToJourneyConfig(req *v1.CreateJourneyRequest) *core.JourneyConfig {
	now := time.Now()
	return &core.JourneyConfig{
		ID:          core.GenerateID(),
		Name:        req.Name,
		Description: req.Description,
		Weight:      core.Duration{Duration: time.Duration(req.Interval) * time.Second},
		Enabled:     req.Enabled,
		CreatedAt:   now,
	}
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

func (s *Server) checkPermission(ctx context.Context, permission string) (*api.User, error) {
	user, ok := GetUserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}
	role := core.MemberRole(user.Role)
	if !role.Can(permission) {
		return nil, status.Errorf(codes.PermissionDenied, "role %q lacks permission %q", user.Role, permission)
	}
	return user, nil
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

	user, err := s.checkPermission(ctx, "souls:read")
	if err != nil {
		return nil, err
	}
	workspace := user.Workspace
	if workspace == "" {
		workspace = "default"
	}

	offset, limit := normalizedListWindow(req.Offset, req.Limit)

	// Type and tag are declared request filters, so they must be applied before
	// pagination; filtering a storage-sized page would otherwise skip matches
	// and produce incorrect totals/next offsets.
	if req.GetType() != "" || req.GetTag() != "" {
		allSouls, err := s.store.ListSoulsNoCtx(workspace, 0, 0)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to list souls: %v", err)
		}
		filtered := make([]*core.Soul, 0, len(allSouls))
		for _, soul := range allSouls {
			if matchesSoulFilters(soul, req) {
				filtered = append(filtered, soul)
			}
		}
		page, pagination := paginateSouls(filtered, offset, limit)
		pbSouls := make([]*v1.Soul, 0, len(page))
		for _, soul := range page {
			if pb := soulToPB(soul); pb != nil {
				pbSouls = append(pbSouls, pb)
			}
		}
		return &v1.ListSoulsResponse{Souls: pbSouls, Pagination: pagination}, nil
	}

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

	user, err := s.checkPermission(ctx, "souls:read")
	if err != nil {
		return nil, err
	}

	soul, err := s.store.GetSoulNoCtx(req.Id)
	if err != nil || soul == nil {
		return nil, status.Errorf(codes.NotFound, "soul not found: %s", req.Id)
	}
	if soul.WorkspaceID != "" && soul.WorkspaceID != user.Workspace {
		return nil, status.Error(codes.PermissionDenied, "access denied: soul belongs to another workspace")
	}
	return soulToPB(soul), nil
}

func (s *Server) CreateSoul(ctx context.Context, req *v1.CreateSoulRequest) (*v1.Soul, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, err := s.checkPermission(ctx, "souls:*")
	if err != nil {
		return nil, err
	}

	soul := pbToSoulConfig(req)
	soul.WorkspaceID = user.Workspace
	if soul.WorkspaceID == "" {
		soul.WorkspaceID = "default"
	}

	if err := s.store.SaveSoulNoCtx(soul); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create soul: %v", err)
	}

	// Retrieve the created soul directly using the generated ID
	created, err := s.store.GetSoulNoCtx(soul.ID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "soul created but could not be retrieved: %v", err)
	}
	return soulToPB(created), nil
}

func (s *Server) UpdateSoul(ctx context.Context, req *v1.UpdateSoulRequest) (*v1.Soul, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, err := s.checkPermission(ctx, "souls:*")
	if err != nil {
		return nil, err
	}

	// Get existing soul first
	existing, err := s.store.GetSoulNoCtx(req.Id)
	if err != nil || existing == nil {
		return nil, status.Errorf(codes.NotFound, "soul not found: %s", req.Id)
	}
	if existing.WorkspaceID != "" && existing.WorkspaceID != user.Workspace {
		return nil, status.Error(codes.PermissionDenied, "access denied: soul belongs to another workspace")
	}

	applySoulUpdates(existing, req)
	if err := s.store.SaveSoulNoCtx(existing); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update soul: %v", err)
	}

	updated, _ := s.store.GetSoulNoCtx(req.Id)
	return soulToPB(updated), nil
}

func (s *Server) DeleteSoul(ctx context.Context, req *v1.DeleteSoulRequest) (*emptypb.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, err := s.checkPermission(ctx, "souls:*")
	if err != nil {
		return nil, err
	}
	existing, err := s.store.GetSoulNoCtx(req.Id)
	if err != nil || existing == nil {
		return nil, status.Errorf(codes.NotFound, "soul not found: %s", req.Id)
	}
	if existing.WorkspaceID != "" && existing.WorkspaceID != user.Workspace {
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

	user, err := s.checkPermission(ctx, "judgments:read")
	if err != nil {
		return nil, err
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
	if soulID != "" {
		soul, err := s.store.GetSoulNoCtx(soulID)
		if err == nil && soul != nil {
			if soul.WorkspaceID != "" && soul.WorkspaceID != user.Workspace {
				return nil, status.Error(codes.PermissionDenied, "access denied: soul belongs to another workspace")
			}
		}
	}

	fetchLimit := listFetchLimit(offset, limit)
	if req.GetStatus() != "" || soulID == "" {
		fetchLimit = 0
	}
	var judgments []*core.Judgment
	if soulID == "" {
		souls, err := s.store.ListSoulsNoCtx(user.Workspace, 0, 0)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to list souls for judgments: %v", err)
		}
		for _, soul := range souls {
			soulJudgments, err := s.store.ListJudgmentsNoCtx(soul.ID, start, end, fetchLimit)
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

	filtered := make([]*core.Judgment, 0, len(judgments))
	for _, j := range judgments {
		if matchesOptionalString(string(j.Status), req.GetStatus()) {
			filtered = append(filtered, j)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].Timestamp.After(filtered[j].Timestamp)
	})
	page, pagination := paginateJudgments(filtered, offset, limit)

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

	user, err := s.checkPermission(ctx, "souls:*")
	if err != nil {
		return nil, err
	}
	workspace := user.Workspace
	if workspace == "" {
		workspace = "default"
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

	user, err := s.checkPermission(ctx, "souls:read")
	if err != nil {
		return nil, err
	}
	workspace := user.Workspace
	if workspace == "" {
		workspace = "default"
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

	filtered := make([]*core.AlertEvent, 0, len(events))
	for _, e := range events {
		if ws := e.WorkspaceID; ws != "" && ws != workspace {
			continue
		}
		if !matchesOptionalString(string(e.Status), req.GetStatus()) {
			continue
		}
		if !matchesOptionalString(string(e.Severity), req.GetSeverity()) {
			continue
		}
		filtered = append(filtered, e)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].Timestamp.After(filtered[j].Timestamp)
	})
	// Inline pagination for AlertEvents
	total := len(filtered)
	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	page := filtered[start:end]
	pagination := newPagination(total, offset, limit, len(page))

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

// --- Channel RPCs ---

func (s *Server) ListChannels(ctx context.Context, req *v1.ListChannelsRequest) (*v1.ListChannelsResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user, err := s.checkPermission(ctx, "channels:read")
	if err != nil {
		return nil, err
	}
	workspace := user.Workspace
	if workspace == "" {
		workspace = "default"
	}
	offset, limit := normalizedListWindow(req.Offset, req.Limit)

	channels, err := s.store.ListChannelsNoCtx(workspace)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list channels: %v", err)
	}
	// Inline pagination for channels
	total := len(channels)
	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	page := channels[start:end]
	pagination := newPagination(total, offset, limit, len(page))

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

	user, err := s.checkPermission(ctx, "channels:read")
	if err != nil {
		return nil, err
	}

	ch, err := s.store.GetChannelNoCtx(req.Id, user.Workspace)
	if err != nil || ch == nil {
		return nil, status.Errorf(codes.NotFound, "channel not found: %s", req.Id)
	}
	return channelToPB(ch), nil
}

func (s *Server) CreateChannel(ctx context.Context, req *v1.CreateChannelRequest) (*v1.Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, err := s.checkPermission(ctx, "channels:*")
	if err != nil {
		return nil, err
	}
	workspace := user.Workspace
	if workspace == "" {
		workspace = "default"
	}

	ch := pbToChannelConfig(req)
	ch.WorkspaceID = workspace

	if err := s.store.SaveChannelNoCtx(ch); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create channel: %v", err)
	}

	created, err := s.store.GetChannelNoCtx(ch.ID, workspace)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "channel created but could not be retrieved: %v", err)
	}
	return channelToPB(created), nil
}

func (s *Server) UpdateChannel(ctx context.Context, req *v1.UpdateChannelRequest) (*v1.Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, err := s.checkPermission(ctx, "channels:*")
	if err != nil {
		return nil, err
	}
	workspace := user.Workspace
	if workspace == "" {
		workspace = "default"
	}

	existing, err := s.store.GetChannelNoCtx(req.Id, workspace)
	if err != nil || existing == nil {
		return nil, status.Errorf(codes.NotFound, "channel not found: %s", req.Id)
	}

	applyChannelUpdates(existing, req)
	if err := s.store.SaveChannelNoCtx(existing); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update channel: %v", err)
	}

	updated, _ := s.store.GetChannelNoCtx(req.Id, workspace)
	return channelToPB(updated), nil
}

func (s *Server) DeleteChannel(ctx context.Context, req *v1.DeleteChannelRequest) (*emptypb.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, err := s.checkPermission(ctx, "channels:*")
	if err != nil {
		return nil, err
	}
	workspace := user.Workspace
	if workspace == "" {
		workspace = "default"
	}
	if existing, err := s.store.GetChannelNoCtx(req.Id, workspace); err != nil || existing == nil {
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

	user, err := s.checkPermission(ctx, "rules:read")
	if err != nil {
		return nil, err
	}
	workspace := user.Workspace
	if workspace == "" {
		workspace = "default"
	}
	offset, limit := normalizedListWindow(req.Offset, req.Limit)

	rules, err := s.store.ListRulesNoCtx(workspace)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list rules: %v", err)
	}
	// Inline pagination for rules
	total := len(rules)
	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	page := rules[start:end]
	pagination := newPagination(total, offset, limit, len(page))

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

	user, err := s.checkPermission(ctx, "rules:read")
	if err != nil {
		return nil, err
	}

	r, err := s.store.GetRuleNoCtx(req.Id, user.Workspace)
	if err != nil || r == nil {
		return nil, status.Errorf(codes.NotFound, "rule not found: %s", req.Id)
	}
	return ruleToPB(r), nil
}

func (s *Server) CreateRule(ctx context.Context, req *v1.CreateRuleRequest) (*v1.Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, err := s.checkPermission(ctx, "rules:*")
	if err != nil {
		return nil, err
	}
	workspace := user.Workspace
	if workspace == "" {
		workspace = "default"
	}

	rule := pbToRuleConfig(req)
	rule.WorkspaceID = workspace

	if err := s.store.SaveRuleNoCtx(rule); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create rule: %v", err)
	}

	created, err := s.store.GetRuleNoCtx(rule.ID, workspace)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "rule created but could not be retrieved: %v", err)
	}
	return ruleToPB(created), nil
}

func (s *Server) UpdateRule(ctx context.Context, req *v1.UpdateRuleRequest) (*v1.Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, err := s.checkPermission(ctx, "rules:*")
	if err != nil {
		return nil, err
	}

	existing, err := s.store.GetRuleNoCtx(req.Id, user.Workspace)
	if err != nil || existing == nil {
		return nil, status.Errorf(codes.NotFound, "rule not found: %s", req.Id)
	}

	applyRuleUpdates(existing, req)
	if err := s.store.SaveRuleNoCtx(existing); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update rule: %v", err)
	}

	updated, _ := s.store.GetRuleNoCtx(req.Id, user.Workspace)
	return ruleToPB(updated), nil
}

func (s *Server) DeleteRule(ctx context.Context, req *v1.DeleteRuleRequest) (*emptypb.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, err := s.checkPermission(ctx, "rules:*")
	if err != nil {
		return nil, err
	}
	workspace := user.Workspace
	if workspace == "" {
		workspace = "default"
	}
	if existing, err := s.store.GetRuleNoCtx(req.Id, workspace); err != nil || existing == nil {
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

	user, err := s.checkPermission(ctx, "souls:read")
	if err != nil {
		return nil, err
	}
	workspace := user.Workspace
	if workspace == "" {
		workspace = "default"
	}
	offset, limit := normalizedListWindow(req.Offset, req.Limit)

	journeys, err := s.store.ListJourneysNoCtx(workspace, 0, 0)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list journeys: %v", err)
	}
	// Inline pagination for journeys
	total := len(journeys)
	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	page := journeys[start:end]
	pagination := newPagination(total, offset, limit, len(page))

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

	user, err := s.checkPermission(ctx, "souls:read")
	if err != nil {
		return nil, err
	}
	workspace := user.Workspace
	if workspace == "" {
		workspace = "default"
	}

	j, err := s.store.GetJourneyNoCtx(req.Id)
	if err != nil || j == nil {
		return nil, status.Errorf(codes.NotFound, "journey not found: %s", req.Id)
	}
	if j.WorkspaceID != "" && j.WorkspaceID != workspace {
		return nil, status.Error(codes.PermissionDenied, "access denied: journey belongs to another workspace")
	}
	return journeyToPB(j), nil
}

func (s *Server) CreateJourney(ctx context.Context, req *v1.CreateJourneyRequest) (*v1.Journey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, err := s.checkPermission(ctx, "souls:*")
	if err != nil {
		return nil, err
	}
	workspace := user.Workspace
	if workspace == "" {
		workspace = "default"
	}

	journey := pbToJourneyConfig(req)
	journey.WorkspaceID = workspace

	if err := s.store.SaveJourneyNoCtx(journey); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create journey: %v", err)
	}

	created, err := s.store.GetJourneyNoCtx(journey.ID)
	if err != nil || created == nil {
		return nil, status.Errorf(codes.Internal, "journey created but could not be retrieved")
	}
	return journeyToPB(created), nil
}

func (s *Server) UpdateJourney(ctx context.Context, req *v1.UpdateJourneyRequest) (*v1.Journey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, err := s.checkPermission(ctx, "souls:*")
	if err != nil {
		return nil, err
	}
	workspace := user.Workspace
	if workspace == "" {
		workspace = "default"
	}

	existing, err := s.store.GetJourneyNoCtx(req.Id)
	if err != nil || existing == nil {
		return nil, status.Errorf(codes.NotFound, "journey not found: %s", req.Id)
	}
	if existing.WorkspaceID != "" && existing.WorkspaceID != workspace {
		return nil, status.Error(codes.PermissionDenied, "access denied: journey belongs to another workspace")
	}

	applyJourneyUpdates(existing, req)
	if err := s.store.SaveJourneyNoCtx(existing); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update journey: %v", err)
	}

	updated, _ := s.store.GetJourneyNoCtx(req.Id)
	return journeyToPB(updated), nil
}

func (s *Server) DeleteJourney(ctx context.Context, req *v1.DeleteJourneyRequest) (*emptypb.Empty, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, err := s.checkPermission(ctx, "souls:*")
	if err != nil {
		return nil, err
	}
	workspace := user.Workspace
	if workspace == "" {
		workspace = "default"
	}
	existing, err := s.store.GetJourneyNoCtx(req.Id)
	if err != nil || existing == nil {
		return nil, status.Errorf(codes.NotFound, "journey not found: %s", req.Id)
	}
	if existing.WorkspaceID != "" && existing.WorkspaceID != workspace {
		return nil, status.Error(codes.PermissionDenied, "access denied: journey belongs to another workspace")
	}
	if err := s.store.DeleteJourneyNoCtx(req.Id); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete journey: %v", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *Server) RunJourney(ctx context.Context, req *v1.RunJourneyRequest) (*v1.RunJourneyResponse, error) {
	user, err := s.checkPermission(ctx, "souls:*")
	if err != nil {
		return nil, err
	}
	workspace := user.Workspace
	if workspace == "" {
		workspace = "default"
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
	user, err := s.checkPermission(ctx, "souls:read")
	if err != nil {
		return nil, err
	}
	workspace := user.Workspace
	if workspace == "" {
		workspace = "default"
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
	user, err := s.checkPermission(ctx, "souls:read")
	if err != nil {
		return nil, err
	}
	workspace := user.Workspace
	if workspace == "" {
		workspace = "default"
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
	if _, err := s.checkPermission(ctx, "souls:read"); err != nil {
		return nil, err
	}
	return &v1.ClusterStatus{
		Clustered: false,
		IsLeader:  true,
		NodeId:    "single-node",
		NodeCount: 1,
	}, nil
}

// --- Streaming RPCs ---

func (s *Server) StreamJudgments(req *v1.StreamRequest, stream v1.AnubisWatchService_StreamJudgmentsServer) error {
	user, err := s.checkPermission(stream.Context(), "judgments:read")
	if err != nil {
		return err
	}
	workspace := user.Workspace
	if workspace == "" {
		workspace = "default"
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
	// Enforce the same RBAC as ListVerdicts (souls:read); previously this
	// stream only authenticated the caller, letting any authenticated principal
	// stream verdicts regardless of role.
	user, err := s.checkPermission(stream.Context(), "souls:read")
	if err != nil {
		return err
	}
	workspace := user.Workspace
	if workspace == "" {
		workspace = "default"
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
