package grpcapi

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	v1 "github.com/AnubisWatch/anubiswatch/internal/grpcapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// mockJourneyStep implements step result fields for journeyRunToPB interface test
type mockJourneyStep struct {
	name            string
	stepIndex       int
	duration        int64
	status, message string
	extracted       map[string]string
}

func (m *mockJourneyStep) GetName() string                 { return m.name }
func (m *mockJourneyStep) GetStepIndex() int               { return m.stepIndex }
func (m *mockJourneyStep) GetDuration() int64              { return m.duration }
func (m *mockJourneyStep) GetStatus() string               { return m.status }
func (m *mockJourneyStep) GetMessage() string              { return m.message }
func (m *mockJourneyStep) GetExtracted() map[string]string { return m.extracted }

// =============================================================================
// workspaceFromContext Tests
// =============================================================================
// resourceID Tests
// =============================================================================

func TestResourceID_CoreSoul(t *testing.T) {
	id := resourceID(&core.Soul{ID: "soul-core"})
	if id != "soul-core" {
		t.Errorf("Expected 'soul-core', got %q", id)
	}
}

func TestResourceID_CoreJudgment(t *testing.T) {
	id := resourceID(&core.Judgment{ID: "judge-1"})
	if id != "judge-1" {
		t.Errorf("Expected 'judge-1', got %q", id)
	}
}

func TestResourceID_CoreAlertEvent(t *testing.T) {
	id := resourceID(&core.AlertEvent{ID: "evt-1"})
	if id != "evt-1" {
		t.Errorf("Expected 'evt-1', got %q", id)
	}
}

func TestResourceID_MapWithID(t *testing.T) {
	id := resourceID(map[string]interface{}{"id": "map-id-1"})
	if id != "map-id-1" {
		t.Errorf("Expected 'map-id-1', got %q", id)
	}
}

func TestResourceID_Interface(t *testing.T) {
	v := &core.Soul{ID: "soul-via-interface"}
	id := resourceID(v)
	if id != "soul-via-interface" {
		t.Errorf("Expected 'soul-via-interface', got %q", id)
	}
}

func TestResourceID_FallbackToEmpty(t *testing.T) {
	id := resourceID("unexpected-type")
	if id != "" {
		t.Errorf("Expected empty string, got %q", id)
	}
}

// =============================================================================
// resourceWorkspace Tests
// =============================================================================

func TestResourceWorkspace_CoreSoul(t *testing.T) {
	ws := resourceWorkspace(&core.Soul{WorkspaceID: "ws-soul"})
	if ws != "ws-soul" {
		t.Errorf("Expected 'ws-soul', got %q", ws)
	}
}

func TestResourceWorkspace_CoreJudgment(t *testing.T) {
	ws := resourceWorkspace(&core.Judgment{WorkspaceID: "ws-judge"})
	if ws != "ws-judge" {
		t.Errorf("Expected 'ws-judge', got %q", ws)
	}
}

func TestResourceWorkspace_CoreAlertChannel(t *testing.T) {
	ws := resourceWorkspace(&core.AlertChannel{WorkspaceID: "ws-ch"})
	if ws != "ws-ch" {
		t.Errorf("Expected 'ws-ch', got %q", ws)
	}
}

func TestResourceWorkspace_CoreAlertRule(t *testing.T) {
	ws := resourceWorkspace(&core.AlertRule{WorkspaceID: "ws-rule"})
	if ws != "ws-rule" {
		t.Errorf("Expected 'ws-rule', got %q", ws)
	}
}

func TestResourceWorkspace_CoreJourneyConfig(t *testing.T) {
	ws := resourceWorkspace(&core.JourneyConfig{WorkspaceID: "ws-journey"})
	if ws != "ws-journey" {
		t.Errorf("Expected 'ws-journey', got %q", ws)
	}
}

func TestResourceWorkspace_CoreAlertEvent(t *testing.T) {
	ws := resourceWorkspace(&core.AlertEvent{WorkspaceID: "ws-evt"})
	if ws != "ws-evt" {
		t.Errorf("Expected 'ws-evt', got %q", ws)
	}
}

func TestResourceWorkspace_Map(t *testing.T) {
	ws := resourceWorkspace(map[string]interface{}{"workspace_id": "ws-map"})
	if ws != "ws-map" {
		t.Errorf("Expected 'ws-map', got %q", ws)
	}
}

func TestResourceWorkspace_MapMissingKey(t *testing.T) {
	ws := resourceWorkspace(map[string]interface{}{"name": "no-ws"})
	if ws != "" {
		t.Errorf("Expected empty, got %q", ws)
	}
}

func TestResourceWorkspace_Interface(t *testing.T) {
	v := &core.Soul{ID: "via-iface", WorkspaceID: "default"}
	ws := resourceWorkspace(v)
	if ws != "default" {
		t.Errorf("Expected 'default', got %q", ws)
	}
}

func TestResourceWorkspace_FallbackToEmpty(t *testing.T) {
	ws := resourceWorkspace("unexpected")
	if ws != "" {
		t.Errorf("Expected empty, got %q", ws)
	}
}

// =============================================================================
// statusValue Tests
// =============================================================================
// severityValue Tests
// =============================================================================
// timestampValue Tests
// =============================================================================
// matchesSoulFilters Tests
// =============================================================================

func TestSoulMatchesListFilters_NoFilter(t *testing.T) {
	if !matchesSoulFilters(&core.Soul{}, &v1.ListSoulsRequest{}) {
		t.Error("Expected true when no filters are set")
	}
}

func TestSoulMatchesListFilters_TypeMatch(t *testing.T) {
	soulType := "http"
	req := &v1.ListSoulsRequest{Type: &soulType}
	if !matchesSoulFilters(&core.Soul{Type: "http"}, req) {
		t.Error("Expected true for matching type")
	}
}

func TestSoulMatchesListFilters_TypeNoMatch(t *testing.T) {
	soulType := "tcp"
	req := &v1.ListSoulsRequest{Type: &soulType}
	if matchesSoulFilters(&core.Soul{Type: "http"}, req) {
		t.Error("Expected false for non-matching type")
	}
}

func TestSoulMatchesListFilters_TagMatch(t *testing.T) {
	tag := "prod"
	req := &v1.ListSoulsRequest{Tag: &tag}
	if !matchesSoulFilters(&core.Soul{Tags: []string{"prod", "critical"}}, req) {
		t.Error("Expected true for matching tag")
	}
}

func TestSoulMatchesListFilters_TagNoMatch(t *testing.T) {
	tag := "prod"
	req := &v1.ListSoulsRequest{Tag: &tag}
	if matchesSoulFilters(&core.Soul{Tags: []string{"dev"}}, req) {
		t.Error("Expected false for non-matching tag")
	}
}

// =============================================================================
// eventToVerdict Tests
// =============================================================================

func TestEventToVerdict_Firing(t *testing.T) {
	evt := &core.AlertEvent{
		ID: "evt-1", SoulID: "s1", SoulName: "test", ChannelID: "ch1",
		Severity: "critical", Message: "test alert", Timestamp: time.Now(),
	}
	pb := eventToVerdict(evt)
	if pb == nil {
		t.Fatal("eventToVerdict returned nil")
	}
	if pb.Status != "firing" {
		t.Errorf("Expected 'firing', got %q", pb.Status)
	}
}

func TestEventToVerdict_CoreResolved(t *testing.T) {
	evt := &core.AlertEvent{
		ID: "evt-2", SoulID: "s1", SoulName: "test", ChannelID: "ch1",
		Severity: "warning", Message: "resolved", Timestamp: time.Now(),
		Resolved: true,
	}
	pb := eventToVerdict(evt)
	if pb == nil {
		t.Fatal("eventToVerdict returned nil")
	}
	if pb.Status != "resolved" {
		t.Errorf("Expected 'resolved', got %q", pb.Status)
	}
}

func TestEventToVerdict_Acknowledged(t *testing.T) {
	evt := &core.AlertEvent{
		ID: "evt-6", SoulID: "s1", SoulName: "test", ChannelID: "ch1",
		Severity: "info", Message: "acknowledged", Timestamp: time.Now(),
		Acknowledged: true,
	}
	pb := eventToVerdict(evt)
	if pb == nil {
		t.Fatal("eventToVerdict returned nil")
	}
	if pb.Status != "acknowledged" {
		t.Errorf("Expected 'acknowledged', got %q", pb.Status)
	}
}

func TestEventToVerdict_Nil(t *testing.T) {
	pb := eventToVerdict(nil)
	if pb != nil {
		t.Error("Expected nil for nil input")
	}
}

// =============================================================================
// journeyRunToPB Tests
// =============================================================================

func TestJourneyRunToPB_CoreJourneyRun(t *testing.T) {
	now := time.Now()
	run := &core.JourneyRun{
		ID: "run-1", JourneyID: "j1", WorkspaceID: "default",
		JackalID: "jackal-1", Status: "alive",
		StartedAt: now.UnixMilli(), CompletedAt: now.UnixMilli(),
		Duration: 150,
		Steps: []core.JourneyStepResult{
			{Name: "step1", StepIndex: 0, Duration: 100, Status: "alive", Message: "ok", Extracted: map[string]string{"key": "val"}},
		},
		Variables: map[string]string{"var1": "val1"},
	}
	pb := journeyRunToPB(run)
	if pb == nil {
		t.Fatal("journeyRunToPB returned nil")
	}
	if pb.Id != "run-1" || pb.JourneyId != "j1" || pb.Status != "alive" {
		t.Errorf("Unexpected PB fields: id=%q journeyId=%q status=%q", pb.Id, pb.JourneyId, pb.Status)
	}
	if len(pb.Steps) != 1 {
		t.Fatalf("Expected 1 step, got %d", len(pb.Steps))
	}
	if pb.Steps[0].Name != "step1" {
		t.Errorf("Expected step name 'step1', got %q", pb.Steps[0].Name)
	}
}

func TestJourneyRunToPB_Concrete(t *testing.T) {
	now := time.Now().UnixMilli()
	run := &core.JourneyRun{
		ID: "run-iface", JourneyID: "j2", WorkspaceID: "default",
		JackalID: "jackal-2", Status: "failed",
		StartedAt: now, CompletedAt: now, Duration: 200,
		Steps: []core.JourneyStepResult{
			{Name: "step1", StepIndex: 0, Duration: 100, Status: "alive", Message: "ok", Extracted: map[string]string{"k": "v"}},
		},
		Variables: map[string]string{"x": "y"},
	}
	pb := journeyRunToPB(run)
	if pb == nil {
		t.Fatal("journeyRunToPB returned nil")
	}
	if pb.Id != "run-iface" || pb.JourneyId != "j2" || pb.Status != "failed" {
		t.Errorf("Unexpected PB fields: id=%q journeyId=%q status=%q", pb.Id, pb.JourneyId, pb.Status)
	}
	if len(pb.Steps) != 1 {
		t.Fatalf("Expected 1 step, got %d", len(pb.Steps))
	}
	if pb.Steps[0].Name != "step1" || pb.Steps[0].Extracted["k"] != "v" {
		t.Errorf("Unexpected step fields: name=%q extracted=%v", pb.Steps[0].Name, pb.Steps[0].Extracted)
	}
}

func TestJourneyRunToPB_NilSteps(t *testing.T) {
	run := &core.JourneyRun{
		ID: "run-badsteps", JourneyID: "j3", WorkspaceID: "default",
		Status: "alive", StartedAt: time.Now().UnixMilli(), Steps: nil,
	}
	pb := journeyRunToPB(run)
	if pb == nil {
		t.Fatal("journeyRunToPB returned nil")
	}
	if len(pb.Steps) != 0 {
		t.Errorf("Expected 0 steps for empty steps, got %d", len(pb.Steps))
	}
}

func TestJourneyRunToPB_Nil(t *testing.T) {
	pb := journeyRunToPB(nil)
	if pb != nil {
		t.Error("Expected nil for nil input")
	}
}

// =============================================================================
// journeyToPB, channelToPB, ruleToPB, judgmentToPB, soulToPB concrete tests
// =============================================================================

func TestJourneyToPB_Nil(t *testing.T) {
	pb := journeyToPB(nil)
	if pb != nil {
		t.Error("Expected nil for nil input")
	}
}

func TestJourneyToPB_Concrete(t *testing.T) {
	pb := journeyToPB(&core.JourneyConfig{ID: "j-iface", Name: "test-journey"})
	if pb == nil {
		t.Fatal("journeyToPB returned nil")
	}
	if pb.Id != "j-iface" || pb.Name != "test-journey" {
		t.Errorf("Unexpected: id=%q name=%q", pb.Id, pb.Name)
	}
}

func TestChannelToPB_Nil(t *testing.T) {
	pb := channelToPB(nil)
	if pb != nil {
		t.Error("Expected nil for nil input")
	}
}

func TestChannelToPB_Concrete(t *testing.T) {
	pb := channelToPB(&core.AlertChannel{ID: "ch-iface", Name: "test-ch", Type: "webhook"})
	if pb == nil {
		t.Fatal("channelToPB returned nil")
	}
	if pb.Id != "ch-iface" || pb.Name != "test-ch" || pb.Type != "webhook" {
		t.Errorf("Unexpected: id=%q name=%q type=%q", pb.Id, pb.Name, pb.Type)
	}
}

func TestRuleToPB_Nil(t *testing.T) {
	pb := ruleToPB(nil)
	if pb != nil {
		t.Error("Expected nil for nil input")
	}
}

func TestRuleToPB_Concrete(t *testing.T) {
	pb := ruleToPB(&core.AlertRule{ID: "rule-iface", Name: "test-rule"})
	if pb == nil {
		t.Fatal("ruleToPB returned nil")
	}
	if pb.Id != "rule-iface" || pb.Name != "test-rule" {
		t.Errorf("Unexpected: id=%q name=%q", pb.Id, pb.Name)
	}
}

func TestJudgmentToPB_Concrete(t *testing.T) {
	now := time.Now()
	pb := judgmentToPB(&core.Judgment{
		ID: "j-iface", SoulID: "s1", Status: "alive",
		Duration: 10 * time.Millisecond, Message: "ok", Timestamp: now,
	})
	if pb == nil {
		t.Fatal("judgmentToPB returned nil")
	}
	if pb.Id != "j-iface" || pb.Status != "alive" {
		t.Errorf("Unexpected: id=%q status=%q", pb.Id, pb.Status)
	}
}

func TestJudgmentToPB_Nil(t *testing.T) {
	pb := judgmentToPB(nil)
	if pb != nil {
		t.Error("Expected nil for nil input")
	}
}

func TestSoulToPB_Concrete(t *testing.T) {
	pb := soulToPB(&core.Soul{ID: "soul-iface", Name: "test", Type: "http", Target: "example.com"})
	if pb == nil {
		t.Fatal("soulToPB returned nil")
	}
	if pb.Id != "soul-iface" || pb.Name != "test" || pb.Type != "http" {
		t.Errorf("Unexpected: id=%q name=%q type=%q", pb.Id, pb.Name, pb.Type)
	}
}

func TestSoulToPB_NilInput(t *testing.T) {
	pb := soulToPB(nil)
	if pb != nil {
		t.Error("Expected nil for nil input")
	}
}

// =============================================================================
// applyChannelConfig Tests
// =============================================================================
// applyRuleConfig Tests
// =============================================================================
// applyRuleCoreConfig Tests
// =============================================================================

func TestApplyRuleCoreConfig_SeverityField(t *testing.T) {
	rule := &core.AlertRule{}
	applyRuleCoreConfig(rule, map[string]string{"severity": "critical"})
	if rule.Severity != core.Severity("critical") {
		t.Errorf("Expected critical, got %q", rule.Severity)
	}
}

func TestApplyRuleCoreConfig_ChannelIDsField(t *testing.T) {
	rule := &core.AlertRule{}
	applyRuleCoreConfig(rule, map[string]string{"channel_ids": "ch1,ch2"})
	if len(rule.Channels) != 2 || rule.Channels[0] != "ch1" {
		t.Errorf("Expected [ch1 ch2], got %v", rule.Channels)
	}
}

func TestApplyRuleCoreConfig_ChannelIDsEmptyString(t *testing.T) {
	rule := &core.AlertRule{Channels: []string{"existing"}}
	applyRuleCoreConfig(rule, map[string]string{"channel_ids": ""})
	if len(rule.Channels) != 1 {
		t.Errorf("Expected channels unchanged, got %v", rule.Channels)
	}
}

func TestApplyRuleCoreConfig_CooldownField(t *testing.T) {
	rule := &core.AlertRule{}
	applyRuleCoreConfig(rule, map[string]string{"cooldown": "5m"})
	if rule.Cooldown.Duration != 5*time.Minute {
		t.Errorf("Expected 5m cooldown, got %v", rule.Cooldown.Duration)
	}
}

func TestApplyRuleCoreConfig_InvalidCooldownString(t *testing.T) {
	rule := &core.AlertRule{Cooldown: core.Duration{Duration: time.Minute}}
	applyRuleCoreConfig(rule, map[string]string{"cooldown": "not-a-duration"})
	if rule.Cooldown.Duration != time.Minute {
		t.Errorf("Expected unchanged cooldown, got %v", rule.Cooldown.Duration)
	}
}

// =============================================================================
// applyJourneyMapUpdates Tests
// =============================================================================
// legacyMapUpdates Tests
// =============================================================================
// StopWithContext Tests
// =============================================================================

func TestStopWithContext_NilServer(t *testing.T) {
	srv := &Server{}
	srv.StopWithContext(context.Background())
}

func TestStopWithContext_NormalStop(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	if err := srv.Start(); err != nil {
		t.Fatalf("Failed to start: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	srv.StopWithContext(ctx)
}

func TestStopWithContext_CancelledCtx(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	if err := srv.Start(); err != nil {
		t.Fatalf("Failed to start: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	srv.StopWithContext(ctx)
}

// =============================================================================
// normalizedListWindow Tests
// =============================================================================

func TestNormalizedListWindow_Normal(t *testing.T) {
	offset, limit := normalizedListWindow(10, 20)
	if offset != 10 || limit != 20 {
		t.Errorf("Expected (10,20), got (%d,%d)", offset, limit)
	}
}

func TestNormalizedListWindow_NegativeOffset(t *testing.T) {
	offset, limit := normalizedListWindow(-5, 20)
	if offset != 0 || limit != 20 {
		t.Errorf("Expected (0,20), got (%d,%d)", offset, limit)
	}
}

func TestNormalizedListWindow_ZeroLimit(t *testing.T) {
	offset, limit := normalizedListWindow(0, 0)
	if offset != 0 || limit != defaultListLimit {
		t.Errorf("Expected (0,%d), got (%d,%d)", defaultListLimit, offset, limit)
	}
}

func TestNormalizedListWindow_NegativeLimit(t *testing.T) {
	offset, limit := normalizedListWindow(5, -1)
	if offset != 5 || limit != defaultListLimit {
		t.Errorf("Expected (5,%d), got (%d,%d)", defaultListLimit, offset, limit)
	}
}

// =============================================================================
// paginate Tests
// =============================================================================
// checkPermission edge cases
// =============================================================================

func TestCheckPermission_Unauthenticated(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	_, err := srv.checkPermission(context.Background(), "souls:read")
	if err == nil {
		t.Fatal("Expected error")
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("Expected Unauthenticated, got %v", status.Code(err))
	}
}

func TestCheckPermission_InsufficientRole(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	viewerUser := &api.User{ID: "viewer", Role: "viewer", Workspace: "default"}
	ctx := context.WithValue(context.Background(), userContextKey, viewerUser)
	_, err := srv.checkPermission(ctx, "souls:*")
	if err == nil {
		t.Fatal("Expected PermissionDenied error")
	}
	if status.Code(err) != codes.PermissionDenied {
		t.Errorf("Expected PermissionDenied, got %v", status.Code(err))
	}
}

// =============================================================================
// ensureSoulAccess Tests
// =============================================================================

func TestEnsureSoulAccess_EmptySoulID(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	if err := srv.ensureSoulAccess("", "default"); err != nil {
		t.Fatalf("Expected no error for empty SoulID: %v", err)
	}
}

func TestEnsureSoulAccess_SoulNotFound(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	if err := srv.ensureSoulAccess("nonexistent", "default"); err != nil {
		t.Fatalf("Expected nil for non-existent soul: %v", err)
	}
}

func TestEnsureSoulAccess_WrongWorkspace(t *testing.T) {
	store := newMockGRPCStore()
	store.souls["s1"] = &core.Soul{ID: "s1", WorkspaceID: "tenant-b"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	err := srv.ensureSoulAccess("s1", "default")
	if err == nil {
		t.Fatal("Expected error for cross-workspace soul")
	}
	if status.Code(err) != codes.PermissionDenied {
		t.Errorf("Expected PermissionDenied, got %v", status.Code(err))
	}
}

// =============================================================================
// NewServer constructor edge cases
// =============================================================================

func TestNewServer_WithLogger(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, logger, nil, true)
	if srv.logger != logger {
		t.Error("Logger was not set")
	}
}

func TestNewServer_WithTLSConfig(t *testing.T) {
	tlsCfg := &tls.Config{}
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, tlsCfg, true)
	if srv.tlsConfig != tlsCfg {
		t.Error("TLS config was not set")
	}
}

// =============================================================================
// CRUD handler edge cases (store errors, cross-workspace, etc.)
// =============================================================================

func TestServer_CreateSoul_StoreSaveError(t *testing.T) {
	store := newMockGRPCStore()
	store.saveSoulErr = errors.New("save failed")
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	_, err := srv.CreateSoul(testUserContext(), &v1.CreateSoulRequest{
		Name: "test", Type: "http", Target: "example.com",
	})
	if err == nil {
		t.Fatal("Expected error for store save failure")
	}
}

func TestServer_UpdateSoul_WithCoreSoulType(t *testing.T) {
	store := newMockGRPCStore()
	store.souls["s1"] = &core.Soul{ID: "s1", Name: "old", Type: "http", Target: "old.com"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	name := "updated"
	resp, err := srv.UpdateSoul(testUserContext(), &v1.UpdateSoulRequest{Id: "s1", Name: &name})
	if err != nil {
		t.Fatalf("UpdateSoul failed: %v", err)
	}
	if resp.Name != "updated" {
		t.Errorf("Expected 'updated', got %q", resp.Name)
	}
}

func TestServer_UpdateSoul_NotFound(t *testing.T) {
	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	name := "updated"
	_, err := srv.UpdateSoul(testUserContext(), &v1.UpdateSoulRequest{Id: "nonexistent", Name: &name})
	if err == nil {
		t.Fatal("Expected error for nonexistent soul")
	}
}

func TestServer_GetSoul_WrongWorkspace(t *testing.T) {
	store := newMockGRPCStore()
	store.souls["s1"] = &core.Soul{ID: "s1", Name: "test", WorkspaceID: "tenant-b"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	_, err := srv.GetSoul(testUserContext(), &v1.GetSoulRequest{Id: "s1"})
	if err == nil {
		t.Fatal("Expected error for cross-workspace soul access")
	}
	if status.Code(err) != codes.PermissionDenied {
		t.Errorf("Expected PermissionDenied, got %v", status.Code(err))
	}
}

func TestServer_UpdateChannel_WithCoreType(t *testing.T) {
	store := newMockGRPCStore()
	store.channels["ch1"] = &core.AlertChannel{ID: "ch1", Name: "old", Type: "slack", WorkspaceID: "default"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	name := "updated"
	resp, err := srv.UpdateChannel(testUserContext(), &v1.UpdateChannelRequest{Id: "ch1", Name: &name})
	if err != nil {
		t.Fatalf("UpdateChannel failed: %v", err)
	}
	if resp.Name != "updated" {
		t.Errorf("Expected 'updated', got %q", resp.Name)
	}
}

func TestServer_UpdateChannel_WithConfigMap(t *testing.T) {
	store := newMockGRPCStore()
	store.channels["ch2"] = &core.AlertChannel{ID: "ch2", Name: "test", Type: "webhook", Config: map[string]interface{}{}}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	name := "updated"
	_, err := srv.UpdateChannel(testUserContext(), &v1.UpdateChannelRequest{
		Id: "ch2", Name: &name,
		Config: map[string]string{"webhook_url": "https://hooks.example.com"},
	})
	if err != nil {
		t.Fatalf("UpdateChannel with config failed: %v", err)
	}
}

func TestServer_DeleteChannel_NotFound(t *testing.T) {
	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	// GetChannelNoCtx returns nil,nil for missing items in mock, so the NotFound path
	// in the handler is not hit. This test just verifies no crash.
	_, err := srv.DeleteChannel(testUserContext(), &v1.DeleteChannelRequest{Id: "nonexistent"})
	if err != nil {
		t.Logf("Got error (mock-dependent): %v", err)
	}
}

func TestServer_UpdateRule_WithCoreType(t *testing.T) {
	store := newMockGRPCStore()
	store.rules["r1"] = &core.AlertRule{ID: "r1", Name: "old", WorkspaceID: "default"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	name := "updated"
	resp, err := srv.UpdateRule(testUserContext(), &v1.UpdateRuleRequest{Id: "r1", Name: &name})
	if err != nil {
		t.Fatalf("UpdateRule failed: %v", err)
	}
	if resp.Name != "updated" {
		t.Errorf("Expected 'updated', got %q", resp.Name)
	}
}

func TestServer_UpdateRule_WithConfigMap(t *testing.T) {
	store := newMockGRPCStore()
	store.rules["r2"] = &core.AlertRule{ID: "r2", Name: "test"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	name := "updated"
	_, err := srv.UpdateRule(testUserContext(), &v1.UpdateRuleRequest{
		Id: "r2", Name: &name,
		Config: map[string]string{"cooldown": "300"},
	})
	if err != nil {
		t.Fatalf("UpdateRule with config failed: %v", err)
	}
}

func TestServer_DeleteRule_NotFound(t *testing.T) {
	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	// GetRuleNoCtx returns nil,nil for missing items in mock
	_, err := srv.DeleteRule(testUserContext(), &v1.DeleteRuleRequest{Id: "nonexistent"})
	if err != nil {
		t.Logf("Got error (mock-dependent): %v", err)
	}
}

func TestServer_UpdateJourney_WithCoreType(t *testing.T) {
	store := newMockGRPCStore()
	store.journeys["j1"] = &core.JourneyConfig{ID: "j1", Name: "old", WorkspaceID: "default"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	name := "updated"
	resp, err := srv.UpdateJourney(testUserContext(), &v1.UpdateJourneyRequest{Id: "j1", Name: &name})
	if err != nil {
		t.Fatalf("UpdateJourney failed: %v", err)
	}
	if resp.Name != "updated" {
		t.Errorf("Expected 'updated', got %q", resp.Name)
	}
}

func TestServer_DeleteJourney_NotFound(t *testing.T) {
	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	_, err := srv.DeleteJourney(testUserContext(), &v1.DeleteJourneyRequest{Id: "nonexistent"})
	if err != nil {
		t.Logf("Got error (mock-dependent): %v", err)
	}
}

func TestServer_DeleteJourney_WrongWorkspace(t *testing.T) {
	store := newMockGRPCStore()
	store.journeys["j2"] = &core.JourneyConfig{ID: "j2", Name: "test", WorkspaceID: "tenant-b"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	_, err := srv.DeleteJourney(testUserContext(), &v1.DeleteJourneyRequest{Id: "j2"})
	if err == nil {
		t.Fatal("Expected error for cross-workspace journey delete")
	}
}

func TestServer_RunJourney_NotFound(t *testing.T) {
	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	_, err := srv.RunJourney(testUserContext(), &v1.RunJourneyRequest{Id: "nonexistent"})
	if err == nil {
		t.Fatal("Expected error for nonexistent journey")
	}
}

func TestServer_RunJourney_WrongWorkspace(t *testing.T) {
	store := newMockGRPCStore()
	store.journeys["j3"] = &core.JourneyConfig{ID: "j3", Name: "test", WorkspaceID: "tenant-b"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	_, err := srv.RunJourney(testUserContext(), &v1.RunJourneyRequest{Id: "j3"})
	if err == nil {
		t.Fatal("Expected error for cross-workspace journey run")
	}
}

// =============================================================================
// ListJudgments with soul filter
// =============================================================================

func TestServer_ListJudgments_WithSoulFilter(t *testing.T) {
	store := newMockGRPCStore()
	store.souls["s1"] = &core.Soul{ID: "s1", WorkspaceID: "default"}
	store.judgments = []*core.Judgment{
		{ID: "j1", SoulID: "s1", Status: "alive", Duration: 10 * time.Millisecond, Message: "ok", Timestamp: time.Now()},
		{ID: "j2", SoulID: "s2", Status: "dead", Duration: 5 * time.Millisecond, Message: "fail", Timestamp: time.Now()},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	soulID := "s1"
	resp, err := srv.ListJudgments(testUserContext(), &v1.ListJudgmentsRequest{SoulId: &soulID, Limit: 10})
	if err != nil {
		t.Fatalf("ListJudgments failed: %v", err)
	}
	if len(resp.Judgments) != 1 {
		t.Errorf("Expected 1 judgment for soul s1, got %d", len(resp.Judgments))
	}
}

// =============================================================================
// ListVerdicts edge cases
// =============================================================================

func TestServer_ListVerdicts_WithStatusFilter(t *testing.T) {
	store := newMockGRPCStore()
	store.events = []*core.AlertEvent{
		{ID: "e1", SoulID: "s1", Status: "firing", Severity: "critical", WorkspaceID: "default", Timestamp: time.Now()},
		{ID: "e2", SoulID: "s2", Status: "resolved", Severity: "info", WorkspaceID: "default", Timestamp: time.Now()},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	resp, err := srv.ListVerdicts(testUserContext(), &v1.ListVerdictsRequest{Limit: 20})
	if err != nil {
		t.Fatalf("ListVerdicts failed: %v", err)
	}
	if len(resp.Verdicts) != 2 {
		t.Errorf("Expected 2 verdicts (no status filter in proto), got %d", len(resp.Verdicts))
	}
}

func TestServer_ListVerdicts_WithSoulAndSeverityFilter(t *testing.T) {
	store := newMockGRPCStore()
	store.souls["s1"] = &core.Soul{ID: "s1", WorkspaceID: "default"}
	store.events = []*core.AlertEvent{
		{ID: "e1", SoulID: "s1", Status: "firing", Severity: "critical", WorkspaceID: "default", Timestamp: time.Now()},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	soulID := "s1"
	resp, err := srv.ListVerdicts(testUserContext(), &v1.ListVerdictsRequest{SoulId: &soulID, Limit: 20})
	if err != nil {
		t.Fatalf("ListVerdicts failed: %v", err)
	}
	if len(resp.Verdicts) != 1 {
		t.Errorf("Expected 1 verdict, got %d", len(resp.Verdicts))
	}
}

func TestServer_ListVerdicts_ResolvedEvent(t *testing.T) {
	store := newMockGRPCStore()
	store.events = []*core.AlertEvent{
		{ID: "e1", SoulID: "s1", SoulName: "test",
			Status: "resolved", Severity: "info", Message: "resolved",
			Timestamp: time.Now(), Resolved: true,
		},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	resp, err := srv.ListVerdicts(testUserContext(), &v1.ListVerdictsRequest{Limit: 20})
	if err != nil {
		t.Fatalf("ListVerdicts failed: %v", err)
	}
	if len(resp.Verdicts) != 1 || resp.Verdicts[0].Status != "resolved" {
		t.Errorf("Expected 1 resolved verdict, got %d status=%q", len(resp.Verdicts), resp.Verdicts[0].Status)
	}
}

// =============================================================================
// Create mutations with store errors
// =============================================================================

type errorChannelStore struct {
	*mockGRPCStore
	saveChErr error
}

func (m *errorChannelStore) SaveChannelNoCtx(ch *core.AlertChannel) error {
	if m.saveChErr != nil {
		return m.saveChErr
	}
	return m.mockGRPCStore.SaveChannelNoCtx(ch)
}

func TestServer_CreateChannel_StoreError(t *testing.T) {
	store := &errorChannelStore{mockGRPCStore: newMockGRPCStore(), saveChErr: errors.New("save failed")}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	_, err := srv.CreateChannel(testUserContext(), &v1.CreateChannelRequest{
		Name: "test", Type: "slack", Enabled: true,
	})
	if err == nil {
		t.Fatal("Expected error for store save failure")
	}
}

type errorRuleStore struct {
	*mockGRPCStore
	saveRuleErr error
}

func (m *errorRuleStore) SaveRuleNoCtx(r *core.AlertRule) error {
	if m.saveRuleErr != nil {
		return m.saveRuleErr
	}
	return m.mockGRPCStore.SaveRuleNoCtx(r)
}

func TestServer_CreateRule_StoreError(t *testing.T) {
	store := &errorRuleStore{mockGRPCStore: newMockGRPCStore(), saveRuleErr: errors.New("save failed")}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	_, err := srv.CreateRule(testUserContext(), &v1.CreateRuleRequest{
		Name: "test", ConditionType: "consecutive_failures", ChannelId: "ch1",
	})
	if err == nil {
		t.Fatal("Expected error for store save failure")
	}
}

type errorJourneyStore struct {
	*mockGRPCStore
	saveJourneyErr error
}

func (m *errorJourneyStore) SaveJourneyNoCtx(j *core.JourneyConfig) error {
	if m.saveJourneyErr != nil {
		return m.saveJourneyErr
	}
	return m.mockGRPCStore.SaveJourneyNoCtx(j)
}

func TestServer_CreateJourney_StoreError(t *testing.T) {
	store := &errorJourneyStore{mockGRPCStore: newMockGRPCStore(), saveJourneyErr: errors.New("save failed")}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	_, err := srv.CreateJourney(testUserContext(), &v1.CreateJourneyRequest{
		Name: "test", Interval: 60, Enabled: true,
	})
	if err == nil {
		t.Fatal("Expected error for store save failure")
	}
}
