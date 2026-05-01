package grpcapi

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	v1 "github.com/AnubisWatch/anubiswatch/internal/grpcapi/v1"
)

// =============================================================================
// Auth Interceptor Tests
// =============================================================================

func TestAuthInterceptor_MissingMetadata(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	ctx := context.Background()
	_, err := srv.authInterceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test"}, nil)
	if err == nil {
		t.Error("Expected error for missing metadata")
	}
}

func TestAuthInterceptor_MissingAuthHeader(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	ctx := metadata.NewIncomingContext(context.Background(), metadata.MD{})
	_, err := srv.authInterceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test"}, nil)
	if err == nil {
		t.Error("Expected error for missing authorization header")
	}
}

func TestAuthInterceptor_InvalidToken(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		"authorization": "Bearer invalid-token",
	}))
	_, err := srv.authInterceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test"}, nil)
	if err == nil {
		t.Error("Expected error for invalid token")
	}
}

func TestAuthInterceptor_ValidToken(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		"authorization": "Bearer valid-token",
	}))

	handlerCalled := false
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		handlerCalled = true
		user, ok := GetUserFromContext(ctx)
		if !ok {
			t.Error("User not found in context")
		}
		if user.ID != "user-1" {
			t.Errorf("Expected user-1, got %s", user.ID)
		}
		return "success", nil
	}

	result, err := srv.authInterceptor(ctx, "test-request", &grpc.UnaryServerInfo{FullMethod: "/test"}, handler)
	if err != nil {
		t.Fatalf("authInterceptor failed: %v", err)
	}
	if !handlerCalled {
		t.Error("Handler was not called")
	}
	if result != "success" {
		t.Errorf("Expected success, got %v", result)
	}
}

func TestAuthInterceptor_NoBearerPrefix(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		"authorization": "valid-token",
	}))

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "success", nil
	}

	result, err := srv.authInterceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test"}, handler)
	if err != nil {
		t.Fatalf("authInterceptor failed: %v", err)
	}
	if result != "success" {
		t.Errorf("Expected success, got %v", result)
	}
}

// =============================================================================
// Stream Auth Interceptor Tests
// =============================================================================

type errorStream struct {
	baseServerStream
	ctx context.Context
}

func (e *errorStream) Context() context.Context { return e.ctx }
func (e *errorStream) SendMsg(m interface{}) error {
	return errors.New("send error")
}
func (e *errorStream) RecvMsg(m interface{}) error {
	return errors.New("recv error")
}

func TestAuthStreamInterceptor_MissingMetadata(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	ss := &errorStream{ctx: context.Background()}
	err := srv.authStreamInterceptor(nil, ss, &grpc.StreamServerInfo{}, nil)
	if err == nil {
		t.Error("Expected error for missing metadata")
	}
}

func TestAuthStreamInterceptor_MissingAuthHeader(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	ctx := metadata.NewIncomingContext(context.Background(), metadata.MD{})
	ss := &errorStream{ctx: ctx}
	err := srv.authStreamInterceptor(nil, ss, &grpc.StreamServerInfo{}, nil)
	if err == nil {
		t.Error("Expected error for missing authorization header")
	}
}

func TestAuthStreamInterceptor_InvalidToken(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		"authorization": "Bearer invalid-token",
	}))
	ss := &errorStream{ctx: ctx}
	err := srv.authStreamInterceptor(nil, ss, &grpc.StreamServerInfo{}, nil)
	if err == nil {
		t.Error("Expected error for invalid token")
	}
}

func TestAuthStreamInterceptor_ValidToken(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		"authorization": "Bearer valid-token",
	}))

	wrapped := &wrappedStream{
		ServerStream: &baseServerStream{},
		ctx:          ctx,
	}

	handlerCalled := false
	handler := func(srv interface{}, ss grpc.ServerStream) error {
		handlerCalled = true
		user, ok := GetUserFromContext(ss.Context())
		if !ok {
			t.Error("User not found in context")
		}
		if user.ID != "user-1" {
			t.Errorf("Expected user-1, got %s", user.ID)
		}
		return nil
	}

	err := srv.authStreamInterceptor(nil, wrapped, &grpc.StreamServerInfo{}, handler)
	if err != nil {
		t.Fatalf("authStreamInterceptor failed: %v", err)
	}
	if !handlerCalled {
		t.Error("Handler was not called")
	}
}

func TestWrappedStream_Context(t *testing.T) {
	ctx := context.Background()
	w := &wrappedStream{ctx: ctx}
	if w.Context() != ctx {
		t.Error("Context not properly wrapped")
	}
}

// =============================================================================
// NewServer Tests
// =============================================================================

func TestNewServer_EnableReflection(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	if srv == nil {
		t.Fatal("NewServer returned nil")
	}
}

func TestNewServer_DisableReflection(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, false)
	if srv == nil {
		t.Fatal("NewServer returned nil")
	}
}

// =============================================================================
// Start error cases
// =============================================================================

func TestStart_InvalidAddress(t *testing.T) {
	srv := NewServer("invalid://:abc", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	err := srv.Start()
	if err == nil {
		t.Error("Expected error for invalid address")
	}
}

func TestStart_Success(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	err := srv.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	srv.Stop()
}

// =============================================================================
// soulToPB tests
// =============================================================================

func TestSoulToPB_UnknownType(t *testing.T) {
	pb := soulToPB("unknown")
	if pb != nil {
		t.Error("Expected nil for unknown type")
	}
}

func TestSoulToPB_NilInterface(t *testing.T) {
	var s interface{} = nil
	pb := soulToPB(s)
	if pb != nil {
		t.Error("Expected nil for nil interface")
	}
}

// =============================================================================
// channelToPB tests
// =============================================================================

func TestChannelToPB_UnknownType(t *testing.T) {
	pb := channelToPB("unknown")
	if pb != nil {
		t.Error("Expected nil for unknown type")
	}
}

// =============================================================================
// journeyRunToPB tests
// =============================================================================

func TestJourneyRunToPB_EmptySteps(t *testing.T) {
	r := &mockJourneyRun{
		id:          "run-1",
		journeyID:   "j-1",
		status:      "success",
		startedAt:   time.Now().UnixMilli(),
		completedAt: time.Now().UnixMilli() + 1000,
		duration:    1000,
		steps:       []interface{}{},
	}

	pb := journeyRunToPB(r)
	if pb == nil {
		t.Fatal("journeyRunToPB returned nil")
	}
	if pb.Id != "run-1" {
		t.Errorf("Expected run-1, got %s", pb.Id)
	}
	if len(pb.Steps) != 0 {
		t.Errorf("Expected 0 steps, got %d", len(pb.Steps))
	}
}

func TestJourneyRunToPB_UnknownType(t *testing.T) {
	pb := journeyRunToPB("unknown")
	if pb != nil {
		t.Error("Expected nil for unknown type")
	}
}

// =============================================================================
// journeyToPB tests
// =============================================================================

func TestJourneyToPB_EmptySteps(t *testing.T) {
	j := &mockJourney{
		id:   "j-1",
		name: "test-journey",
	}

	pb := journeyToPB(j)
	if pb == nil {
		t.Fatal("journeyToPB returned nil")
	}
	if pb.Id != "j-1" {
		t.Errorf("Expected j-1, got %s", pb.Id)
	}
	if len(pb.Steps) != 0 {
		t.Errorf("Expected 0 steps, got %d", len(pb.Steps))
	}
}

func TestJourneyToPB_UnknownType(t *testing.T) {
	pb := journeyToPB("unknown")
	if pb != nil {
		t.Error("Expected nil for unknown type")
	}
}

// =============================================================================
// UpdateSoul tests
// =============================================================================

func TestUpdateSoul_MapType(t *testing.T) {
	store := newMockGRPCStore()
	store.souls["soul_1"] = map[string]interface{}{
		"id":     "soul_1",
		"name":   "old-name",
		"type":   "http",
		"target": "old.com",
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	name := "updated-name"
	resp, err := srv.UpdateSoul(testUserContext(), &v1.UpdateSoulRequest{
		Id:   "soul_1",
		Name: &name,
	})
	if err != nil {
		t.Fatalf("UpdateSoul failed: %v", err)
	}
	if resp.Name != "updated-name" {
		t.Errorf("Expected updated-name, got %s", resp.Name)
	}
}

func TestUpdateSoul_NotFound(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	name := "updated"
	_, err := srv.UpdateSoul(testUserContext(), &v1.UpdateSoulRequest{
		Id:   "nonexistent",
		Name: &name,
	})
	if err == nil {
		t.Error("Expected error for nonexistent soul")
	}
}

func TestUpdateSoul_WorkspacePermissionDenied(t *testing.T) {
	store := newMockGRPCStore()
	store.souls["soul_1"] = &core.Soul{ID: "soul_1", WorkspaceID: "other-workspace"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	name := "updated"
	_, err := srv.UpdateSoul(testUserContext(), &v1.UpdateSoulRequest{
		Id:   "soul_1",
		Name: &name,
	})
	if err == nil {
		t.Error("Expected permission denied error")
	}
}

func TestUpdateSoul_UpdateMultipleFields(t *testing.T) {
	store := newMockGRPCStore()
	store.souls["soul_1"] = map[string]interface{}{
		"id":     "soul_1",
		"name":   "old",
		"type":   "http",
		"target": "old.com",
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	name := "new-name"
	target := "https://new.com"
	interval := int32(120)
	timeout := int32(30)
	enabled := false

	resp, err := srv.UpdateSoul(testUserContext(), &v1.UpdateSoulRequest{
		Id:       "soul_1",
		Name:     &name,
		Target:   &target,
		Interval: &interval,
		Timeout:  &timeout,
		Enabled:  &enabled,
	})
	if err != nil {
		t.Fatalf("UpdateSoul failed: %v", err)
	}
	if resp.Name != "new-name" {
		t.Errorf("Expected new-name, got %s", resp.Name)
	}
}

// =============================================================================
// DeleteSoul tests
// =============================================================================

func TestDeleteSoul_MapType(t *testing.T) {
	store := newMockGRPCStore()
	store.souls["soul_1"] = map[string]interface{}{"id": "soul_1", "name": "test"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	_, err := srv.DeleteSoul(testUserContext(), &v1.DeleteSoulRequest{Id: "soul_1"})
	if err != nil {
		t.Fatalf("DeleteSoul failed: %v", err)
	}
}

func TestDeleteSoul_WorkspacePermissionDenied(t *testing.T) {
	store := newMockGRPCStore()
	store.souls["soul_1"] = &core.Soul{ID: "soul_1", WorkspaceID: "other-workspace"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	_, err := srv.DeleteSoul(testUserContext(), &v1.DeleteSoulRequest{Id: "soul_1"})
	if err == nil {
		t.Error("Expected permission denied error")
	}
}

// =============================================================================
// ListJudgments tests
// =============================================================================

func TestListJudgments_WithSoulFilter(t *testing.T) {
	store := newMockGRPCStore()
	store.judgments = []interface{}{
		&mockJudgment{id: "j1", soulID: "s1", status: "alive", duration: 10 * time.Millisecond, message: "ok", timestamp: time.Now()},
		&mockJudgment{id: "j2", soulID: "s2", status: "dead", duration: 10 * time.Millisecond, message: "fail", timestamp: time.Now()},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	soulID := "s1"
	resp, err := srv.ListJudgments(testUserContext(), &v1.ListJudgmentsRequest{
		SoulId: &soulID,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("ListJudgments failed: %v", err)
	}
	if len(resp.Judgments) != 2 {
		t.Errorf("Expected 2 judgments, got %d", len(resp.Judgments))
	}
}

func TestListJudgments_WithTimeRange(t *testing.T) {
	store := newMockGRPCStore()
	store.judgments = []interface{}{
		&mockJudgment{id: "j1", soulID: "s1", status: "alive", duration: 10 * time.Millisecond, message: "ok", timestamp: time.Now()},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	since := timestamppb.New(time.Now().Add(-1 * time.Hour))
	until := timestamppb.New(time.Now().Add(1 * time.Hour))
	resp, err := srv.ListJudgments(testUserContext(), &v1.ListJudgmentsRequest{
		Since: since,
		Until: until,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListJudgments failed: %v", err)
	}
	if len(resp.Judgments) != 1 {
		t.Errorf("Expected 1 judgment, got %d", len(resp.Judgments))
	}
}

func TestListJudgments_SoulWorkspacePermissionDenied(t *testing.T) {
	store := newMockGRPCStore()
	store.souls["soul_1"] = &core.Soul{ID: "soul_1", WorkspaceID: "other-workspace"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	soulID := "soul_1"
	_, err := srv.ListJudgments(testUserContext(), &v1.ListJudgmentsRequest{
		SoulId: &soulID,
		Limit:  10,
	})
	if err == nil {
		t.Error("Expected permission denied error")
	}
}

func TestListJudgments_DefaultLimit(t *testing.T) {
	store := newMockGRPCStore()
	store.judgments = []interface{}{
		&mockJudgment{id: "j1", soulID: "s1", status: "alive", duration: 10 * time.Millisecond, message: "ok", timestamp: time.Now()},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.ListJudgments(testUserContext(), &v1.ListJudgmentsRequest{Limit: 0})
	if err != nil {
		t.Fatalf("ListJudgments failed: %v", err)
	}
	if resp.Pagination.Limit != 20 {
		t.Errorf("Expected default limit 20, got %d", resp.Pagination.Limit)
	}
}

// =============================================================================
// GetSoul tests
// =============================================================================

func TestGetSoul_WorkspacePermissionDenied(t *testing.T) {
	store := newMockGRPCStore()
	store.souls["soul_1"] = &core.Soul{ID: "soul_1", WorkspaceID: "other-workspace"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	_, err := srv.GetSoul(testUserContext(), &v1.GetSoulRequest{Id: "soul_1"})
	if err == nil {
		t.Error("Expected permission denied error")
	}
}

// =============================================================================
// CreateSoul tests
// =============================================================================

func TestCreateSoul_DefaultWorkspace(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.CreateSoul(testUserContext(), &v1.CreateSoulRequest{
		Name:   "test-soul",
		Type:   "http",
		Target: "https://example.com",
	})
	if err != nil {
		t.Fatalf("CreateSoul failed: %v", err)
	}
	if resp.Workspace != "default" {
		t.Errorf("Expected default workspace, got %s", resp.Workspace)
	}
}

// =============================================================================
// ListChannels tests
// =============================================================================

func TestListChannels_Success(t *testing.T) {
	store := newMockGRPCStore()
	store.channels["ch_1"] = &mockChannel{id: "ch_1", name: "test", chType: "slack"}
	store.channels["ch_2"] = &mockChannel{id: "ch_2", name: "test2", chType: "email"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.ListChannels(testUserContext(), &v1.ListChannelsRequest{})
	if err != nil {
		t.Fatalf("ListChannels failed: %v", err)
	}
	if len(resp.Channels) != 2 {
		t.Errorf("Expected 2 channels, got %d", len(resp.Channels))
	}
}

func TestListChannels_Empty(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.ListChannels(testUserContext(), &v1.ListChannelsRequest{})
	if err != nil {
		t.Fatalf("ListChannels failed: %v", err)
	}
	if len(resp.Channels) != 0 {
		t.Errorf("Expected 0 channels, got %d", len(resp.Channels))
	}
}

// =============================================================================
// GetChannel tests
// =============================================================================

func TestGetChannel_Success(t *testing.T) {
	store := newMockGRPCStore()
	store.channels["ch_1"] = &mockChannel{id: "ch_1", name: "test", chType: "slack"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.GetChannel(testUserContext(), &v1.GetChannelRequest{Id: "ch_1"})
	if err != nil {
		t.Fatalf("GetChannel failed: %v", err)
	}
	if resp.Id != "ch_1" {
		t.Errorf("Expected ch_1, got %s", resp.Id)
	}
}

func TestGetChannel_NotFound(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	_, err := srv.GetChannel(testUserContext(), &v1.GetChannelRequest{Id: "nonexistent"})
	if err == nil {
		t.Error("Expected error for nonexistent channel")
	}
}

// =============================================================================
// UpdateChannel tests
// =============================================================================

func TestUpdateChannel_MapType(t *testing.T) {
	store := newMockGRPCStore()
	store.channels["ch_1"] = map[string]interface{}{
		"id":   "ch_1",
		"name": "old-name",
		"type": "slack",
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	name := "updated-name"
	resp, err := srv.UpdateChannel(testUserContext(), &v1.UpdateChannelRequest{
		Id:   "ch_1",
		Name: &name,
	})
	if err != nil {
		t.Fatalf("UpdateChannel failed: %v", err)
	}
	if resp.Name != "updated-name" {
		t.Errorf("Expected updated-name, got %s", resp.Name)
	}
}

func TestUpdateChannel_NotFound(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	name := "updated"
	_, err := srv.UpdateChannel(testUserContext(), &v1.UpdateChannelRequest{
		Id:   "nonexistent",
		Name: &name,
	})
	if err == nil {
		t.Error("Expected error for nonexistent channel")
	}
}

// =============================================================================
// ListRules tests
// =============================================================================

func TestListRules_Success(t *testing.T) {
	store := newMockGRPCStore()
	store.rules["rule_1"] = &mockRule{id: "rule_1", name: "test-rule"}
	store.rules["rule_2"] = &mockRule{id: "rule_2", name: "test-rule-2"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.ListRules(testUserContext(), &v1.ListRulesRequest{})
	if err != nil {
		t.Fatalf("ListRules failed: %v", err)
	}
	if len(resp.Rules) != 2 {
		t.Errorf("Expected 2 rules, got %d", len(resp.Rules))
	}
}

func TestListRules_Empty(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.ListRules(testUserContext(), &v1.ListRulesRequest{})
	if err != nil {
		t.Fatalf("ListRules failed: %v", err)
	}
	if len(resp.Rules) != 0 {
		t.Errorf("Expected 0 rules, got %d", len(resp.Rules))
	}
}

// =============================================================================
// GetRule tests
// =============================================================================

func TestGetRule_Success(t *testing.T) {
	store := newMockGRPCStore()
	store.rules["rule_1"] = &mockRule{id: "rule_1", name: "test-rule"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.GetRule(testUserContext(), &v1.GetRuleRequest{Id: "rule_1"})
	if err != nil {
		t.Fatalf("GetRule failed: %v", err)
	}
	if resp.Id != "rule_1" {
		t.Errorf("Expected rule_1, got %s", resp.Id)
	}
}

func TestGetRule_NotFound(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	_, err := srv.GetRule(testUserContext(), &v1.GetRuleRequest{Id: "nonexistent"})
	if err == nil {
		t.Error("Expected error for nonexistent rule")
	}
}

// =============================================================================
// UpdateRule tests
// =============================================================================

func TestUpdateRule_MapType(t *testing.T) {
	store := newMockGRPCStore()
	store.rules["rule_1"] = map[string]interface{}{
		"id":   "rule_1",
		"name": "old-name",
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	name := "updated-name"
	resp, err := srv.UpdateRule(testUserContext(), &v1.UpdateRuleRequest{
		Id:   "rule_1",
		Name: &name,
	})
	if err != nil {
		t.Fatalf("UpdateRule failed: %v", err)
	}
	if resp.Name != "updated-name" {
		t.Errorf("Expected updated-name, got %s", resp.Name)
	}
}

func TestUpdateRule_NotFound(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	name := "updated"
	_, err := srv.UpdateRule(testUserContext(), &v1.UpdateRuleRequest{
		Id:   "nonexistent",
		Name: &name,
	})
	if err == nil {
		t.Error("Expected error for nonexistent rule")
	}
}

// =============================================================================
// ListJourneys pagination
// =============================================================================

func TestListJourneys_Pagination(t *testing.T) {
	store := newMockGRPCStore()
	// Add journeys directly to store
	for i := 0; i < 5; i++ {
		j := &mockJourney{id: fmt.Sprintf("journey_%d", i+1), name: fmt.Sprintf("journey-%d", i+1)}
		store.journeys[j.id] = j
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.ListJourneys(testUserContext(), &v1.ListJourneysRequest{Limit: 3})
	if err != nil {
		t.Fatalf("ListJourneys failed: %v", err)
	}
	// Verify pagination info is returned
	if resp.Pagination == nil {
		t.Fatal("Expected pagination info")
	}
	if resp.Pagination.Total != 5 {
		t.Errorf("Expected total 5, got %d", resp.Pagination.Total)
	}
	if resp.Pagination.Limit != 3 {
		t.Errorf("Expected limit 3, got %d", resp.Pagination.Limit)
	}
}

// =============================================================================
// GetJourney tests
// =============================================================================

func TestGetJourney_Success(t *testing.T) {
	store := newMockGRPCStore()
	store.journeys["j_1"] = &mockJourney{id: "j_1", name: "test"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.GetJourney(testUserContext(), &v1.GetJourneyRequest{Id: "j_1"})
	if err != nil {
		t.Fatalf("GetJourney failed: %v", err)
	}
	if resp.Id != "j_1" {
		t.Errorf("Expected j_1, got %s", resp.Id)
	}
}

func TestGetJourney_NotFound(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	_, err := srv.GetJourney(testUserContext(), &v1.GetJourneyRequest{Id: "nonexistent"})
	if err == nil {
		t.Error("Expected error for nonexistent journey")
	}
}

// =============================================================================
// UpdateJourney tests
// =============================================================================

func TestUpdateJourney_MapType(t *testing.T) {
	store := newMockGRPCStore()
	store.journeys["j_1"] = map[string]interface{}{
		"id":   "j_1",
		"name": "old-name",
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	name := "updated-name"
	resp, err := srv.UpdateJourney(testUserContext(), &v1.UpdateJourneyRequest{
		Id:   "j_1",
		Name: &name,
	})
	if err != nil {
		t.Fatalf("UpdateJourney failed: %v", err)
	}
	if resp.Name != "updated-name" {
		t.Errorf("Expected updated-name, got %s", resp.Name)
	}
}

func TestUpdateJourney_NotFound(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	name := "updated"
	_, err := srv.UpdateJourney(testUserContext(), &v1.UpdateJourneyRequest{
		Id:   "nonexistent",
		Name: &name,
	})
	if err == nil {
		t.Error("Expected error for nonexistent journey")
	}
}

// =============================================================================
// ListJourneyRuns tests
// =============================================================================

func TestListJourneyRuns_NotFound(t *testing.T) {
	store := newMockGRPCStore()
	store.journeyRuns = []interface{}{
		&mockJourneyRun{id: "run_1", journeyID: "j_1", status: "success"},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.ListJourneyRuns(testUserContext(), &v1.ListJourneyRunsRequest{JourneyId: "nonexistent", Limit: 10})
	if err != nil {
		t.Fatalf("ListJourneyRuns failed: %v", err)
	}
	if len(resp.Runs) != 0 {
		t.Errorf("Expected 0 runs, got %d", len(resp.Runs))
	}
}

// =============================================================================
// eventToVerdict tests
// =============================================================================

func TestEventToVerdict_Resolved(t *testing.T) {
	e := &mockAlertEvent{
		id:        "evt_1",
		soulID:    "soul_1",
		soulName:  "test-soul",
		channelID: "ch_1",
		status:    "resolved",
		severity:  "critical",
		message:   "alert resolved",
		timestamp: time.Now(),
		resolved:  true,
	}

	v := eventToVerdict(e)
	if v == nil {
		t.Fatal("eventToVerdict returned nil")
	}
	if v.Status != "resolved" {
		t.Errorf("Expected resolved, got %s", v.Status)
	}
}

func TestEventToVerdict_NilTimestamp(t *testing.T) {
	e := &mockAlertEvent{
		id:        "evt_1",
		soulID:    "soul_1",
		status:    "firing",
		severity:  "critical",
		timestamp: time.Time{},
	}

	v := eventToVerdict(e)
	if v == nil {
		t.Fatal("eventToVerdict returned nil")
	}
}

// =============================================================================
// JudgeSoul tests
// =============================================================================

type errorMockGRPCProbe struct{}

func (m *errorMockGRPCProbe) ForceCheck(soulID string) (interface{}, error) {
	return nil, errors.New("probe error")
}

func TestJudgeSoul_ProbeError(t *testing.T) {
	srv := NewServer(":0", newMockGRPCStore(), &errorMockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	_, err := srv.JudgeSoul(testUserContext(), &v1.JudgeSoulRequest{SoulId: "s1"})
	if err == nil {
		t.Error("Expected error from probe failure")
	}
}

// =============================================================================
// StreamJudgments edge cases
// =============================================================================

func TestStreamJudgments_EmptySoulID(t *testing.T) {
	store := newMockGRPCStore()
	store.judgments = []interface{}{
		&mockJudgment{id: "j1", soulID: "s1", status: "alive", duration: 10 * time.Millisecond, message: "ok", timestamp: time.Now()},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	stream := &mockJudgmentsStream{ctx: ctx}
	err := srv.StreamJudgments(&v1.StreamRequest{}, stream)
	if err != nil {
		t.Fatalf("StreamJudgments failed: %v", err)
	}
}

func TestStreamJudgments_CanceledContext(t *testing.T) {
	store := newMockGRPCStore()
	store.judgments = []interface{}{
		&mockJudgment{id: "j1", soulID: "s1", status: "alive", duration: 10 * time.Millisecond, message: "ok", timestamp: time.Now()},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	soulID := "s1"
	stream := &mockJudgmentsStream{ctx: ctx}
	srv.StreamJudgments(&v1.StreamRequest{SoulId: &soulID}, stream)
}

// =============================================================================
// StreamVerdicts edge cases
// =============================================================================

func TestStreamVerdicts_EmptySoulID(t *testing.T) {
	store := newMockGRPCStore()
	store.events = []interface{}{
		&mockAlertEvent{id: "evt_1", soulID: "s1", status: "firing", severity: "critical", message: "alert", timestamp: time.Now()},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	stream := &mockVerdictsStream{ctx: ctx}
	err := srv.StreamVerdicts(&v1.StreamRequest{}, stream)
	if err != nil {
		t.Fatalf("StreamVerdicts failed: %v", err)
	}
}

func TestStreamVerdicts_CanceledContext(t *testing.T) {
	store := newMockGRPCStore()
	store.events = []interface{}{
		&mockAlertEvent{id: "evt_1", soulID: "s1", status: "firing", severity: "critical", message: "alert", timestamp: time.Now()},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	soulID := "s1"
	stream := &mockVerdictsStream{ctx: ctx}
	srv.StreamVerdicts(&v1.StreamRequest{SoulId: &soulID}, stream)
}

// =============================================================================
// ListVerdicts tests
// =============================================================================

func TestListVerdicts_WithEvents(t *testing.T) {
	store := newMockGRPCStore()
	store.events = []interface{}{
		&mockAlertEvent{
			id:        "evt_1",
			soulID:    "soul_1",
			soulName:  "test-soul",
			channelID: "ch_1",
			status:    "firing",
			severity:  "critical",
			message:   "Test alert",
			timestamp: time.Now(),
		},
		&mockAlertEvent{
			id:        "evt_2",
			soulID:    "soul_2",
			soulName:  "test-soul-2",
			channelID: "ch_1",
			status:    "resolved",
			severity:  "warning",
			message:   "Test alert 2",
			timestamp: time.Now(),
		},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.ListVerdicts(testUserContext(), &v1.ListVerdictsRequest{
		Limit: 20,
	})
	if err != nil {
		t.Fatalf("ListVerdicts failed: %v", err)
	}
	if len(resp.Verdicts) != 2 {
		t.Errorf("Expected 2 verdicts, got %d", len(resp.Verdicts))
	}
}

// =============================================================================
// GetClusterStatus test
// =============================================================================

func TestGetClusterStatus_SingleNode(t *testing.T) {
	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.GetClusterStatus(testUserContext(), nil)
	if err != nil {
		t.Fatalf("GetClusterStatus failed: %v", err)
	}
	if resp.NodeId != "single-node" {
		t.Errorf("Expected single-node, got %s", resp.NodeId)
	}
	if resp.IsLeader != true {
		t.Error("Expected IsLeader to be true")
	}
	if resp.NodeCount != 1 {
		t.Errorf("Expected 1 node, got %d", resp.NodeCount)
	}
}
