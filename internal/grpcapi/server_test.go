package grpcapi

import (
	"context"
	"fmt"
	"net"
	"sort"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	v1 "github.com/AnubisWatch/anubiswatch/internal/grpcapi/v1"
)

// testUserContext returns a context with an authenticated test user
func testUserContext() context.Context {
	user := &api.User{ID: "user-1", Email: "test@example.com", Role: "owner", Workspace: "default"}
	return context.WithValue(context.Background(), userContextKey, user)
}

// mockAuthenticator implements Authenticator interface for testing
type mockAuthenticator struct{}

func (m *mockAuthenticator) Authenticate(token string) (*api.User, error) {
	if token == "valid-token" {
		return &api.User{ID: "user-1", Email: "test@example.com", Role: "owner", Workspace: "default"}, nil
	}
	return nil, fmt.Errorf("invalid token")
}

// mockGRPCStore implements Store with in-memory data using concrete types
type mockGRPCStore struct {
	souls           map[string]*core.Soul
	judgments       []*core.Judgment
	channels        map[string]*core.AlertChannel
	rules           map[string]*core.AlertRule
	journeys        map[string]*core.JourneyConfig
	journeyRuns     []*core.JourneyRun
	events          []*core.AlertEvent
	nextID          int
	saveSoulErr     error
	deleteSoulErr   error
	getSoulAfterErr error
}

func newMockGRPCStore() *mockGRPCStore {
	return &mockGRPCStore{
		souls:       make(map[string]*core.Soul),
		channels:    make(map[string]*core.AlertChannel),
		rules:       make(map[string]*core.AlertRule),
		journeys:    make(map[string]*core.JourneyConfig),
		journeyRuns: []*core.JourneyRun{},
		events:      []*core.AlertEvent{},
	}
}

func sortedMockKeys[T any](items map[string]T) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (m *mockGRPCStore) GetSoulNoCtx(id string) (*core.Soul, error) {
	if m.getSoulAfterErr != nil && m.nextID > 0 {
		return nil, m.getSoulAfterErr
	}
	return m.souls[id], nil
}
func (m *mockGRPCStore) ListSoulsNoCtx(ws string, o, l int) ([]*core.Soul, error) {
	keys := sortedMockKeys(m.souls)
	result := make([]*core.Soul, 0, len(keys))
	for _, k := range keys {
		result = append(result, m.souls[k])
	}
	return result, nil
}
func (m *mockGRPCStore) SaveSoulNoCtx(soul *core.Soul) error {
	m.nextID++
	if soul.ID == "" {
		soul.ID = fmt.Sprintf("soul_%d", m.nextID)
	}
	if soul.Name == "" {
		soul.Name = "test-soul"
	}
	m.souls[soul.ID] = soul
	return m.saveSoulErr
}
func (m *mockGRPCStore) DeleteSoulNoCtx(id string) error {
	delete(m.souls, id)
	return m.deleteSoulErr
}
func (m *mockGRPCStore) ListJudgmentsNoCtx(soulID string, start, end time.Time, limit int) ([]*core.Judgment, error) {
	result := make([]*core.Judgment, 0, len(m.judgments))
	for _, j := range m.judgments {
		if soulID != "" && j.SoulID != soulID {
			continue
		}
		result = append(result, j)
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result, nil
}
func (m *mockGRPCStore) GetChannelNoCtx(id string, ws string) (*core.AlertChannel, error) {
	return m.channels[id], nil
}
func (m *mockGRPCStore) ListChannelsNoCtx(ws string) ([]*core.AlertChannel, error) {
	keys := sortedMockKeys(m.channels)
	result := make([]*core.AlertChannel, 0, len(keys))
	for _, k := range keys {
		result = append(result, m.channels[k])
	}
	return result, nil
}
func (m *mockGRPCStore) SaveChannelNoCtx(ch *core.AlertChannel) error {
	m.nextID++
	if ch.ID == "" {
		ch.ID = fmt.Sprintf("ch_%d", m.nextID)
	}
	if ch.Name == "" {
		ch.Name = "test-channel"
	}
	m.channels[ch.ID] = ch
	return nil
}
func (m *mockGRPCStore) DeleteChannelNoCtx(id string, ws string) error {
	delete(m.channels, id)
	return nil
}
func (m *mockGRPCStore) GetRuleNoCtx(id string, ws string) (*core.AlertRule, error) {
	return m.rules[id], nil
}
func (m *mockGRPCStore) ListRulesNoCtx(ws string) ([]*core.AlertRule, error) {
	keys := sortedMockKeys(m.rules)
	result := make([]*core.AlertRule, 0, len(keys))
	for _, k := range keys {
		result = append(result, m.rules[k])
	}
	return result, nil
}
func (m *mockGRPCStore) SaveRuleNoCtx(rule *core.AlertRule) error {
	m.nextID++
	if rule.ID == "" {
		rule.ID = fmt.Sprintf("rule_%d", m.nextID)
	}
	if rule.Name == "" {
		rule.Name = "test-rule"
	}
	m.rules[rule.ID] = rule
	return nil
}
func (m *mockGRPCStore) DeleteRuleNoCtx(id string, ws string) error { delete(m.rules, id); return nil }
func (m *mockGRPCStore) GetJourneyNoCtx(id string) (*core.JourneyConfig, error) {
	return m.journeys[id], nil
}
func (m *mockGRPCStore) ListJourneysNoCtx(ws string, o, l int) ([]*core.JourneyConfig, error) {
	keys := sortedMockKeys(m.journeys)
	result := make([]*core.JourneyConfig, 0, len(keys))
	for _, k := range keys {
		result = append(result, m.journeys[k])
	}
	return result, nil
}
func (m *mockGRPCStore) SaveJourneyNoCtx(j *core.JourneyConfig) error {
	m.nextID++
	if j.ID == "" {
		j.ID = fmt.Sprintf("journey_%d", m.nextID)
	}
	if j.Name == "" {
		j.Name = "test-journey"
	}
	m.journeys[j.ID] = j
	return nil
}
func (m *mockGRPCStore) DeleteJourneyNoCtx(id string) error { delete(m.journeys, id); return nil }
func (m *mockGRPCStore) RunJourneyNoCtx(workspace, journeyID string) (*core.JourneyRun, error) {
	if _, ok := m.journeys[journeyID]; !ok {
		return nil, fmt.Errorf("journey not found")
	}
	now := time.Now().UnixMilli()
	run := &core.JourneyRun{
		ID:          fmt.Sprintf("run_%d", len(m.journeyRuns)+1),
		JourneyID:   journeyID,
		WorkspaceID: workspace,
		Status:      "alive",
		StartedAt:   now,
		CompletedAt: now,
		Duration:    1,
		Variables:   map[string]string{},
	}
	m.journeyRuns = append([]*core.JourneyRun{run}, m.journeyRuns...)
	return run, nil
}
func (m *mockGRPCStore) ListJourneyRunsNoCtx(workspace, journeyID string, limit int) ([]*core.JourneyRun, error) {
	var result []*core.JourneyRun
	for _, r := range m.journeyRuns {
		if r.WorkspaceID == workspace && r.JourneyID == journeyID {
			result = append(result, r)
			if limit > 0 && len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}
func (m *mockGRPCStore) GetJourneyRunNoCtx(workspace, journeyID, runID string) (*core.JourneyRun, error) {
	for _, r := range m.journeyRuns {
		if r.WorkspaceID == workspace && r.JourneyID == journeyID && r.ID == runID {
			return r, nil
		}
	}
	return nil, fmt.Errorf("not found")
}
func (m *mockGRPCStore) ListEvents(soulID string, limit int) ([]*core.AlertEvent, error) {
	result := make([]*core.AlertEvent, 0, len(m.events))
	for _, event := range m.events {
		if soulID != "" && event.SoulID != soulID {
			continue
		}
		result = append(result, event)
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result, nil
}

type mockGRPCProbe struct {
	upserted []*core.Soul
	removed  []string
}

func (m *mockGRPCProbe) ForceCheck(soulID string) (*core.Judgment, error) {
	return &core.Judgment{
		ID:        soulID + "-judge",
		SoulID:    soulID,
		Status:    "alive",
		Duration:  5 * time.Millisecond,
		Message:   "forced check",
		Timestamp: time.Now(),
	}, nil
}

func (m *mockGRPCProbe) UpsertSoul(soul *core.Soul) {
	m.upserted = append(m.upserted, soul)
}

func (m *mockGRPCProbe) RemoveSoul(soulID string) {
	m.removed = append(m.removed, soulID)
}

func TestPBToSoulConfig_PreservesCreateSecrets(t *testing.T) {
	feather := "250ms"
	soul := pbToSoulConfig(&v1.CreateSoulRequest{
		Name:   "API",
		Type:   "http",
		Target: "https://example.com",
		CheckConfig: &v1.CreateSoulRequest_Http{Http: &v1.HTTPCheck{
			Method:          "POST",
			Headers:         map[string]string{"Authorization": "Bearer secret"},
			Body:            "secret body",
			BodyContains:    []string{"healthy"},
			ResponseHeaders: []string{"X-Trace:present"},
			Feather:         &feather,
		}},
	})

	if soul.HTTP == nil {
		t.Fatal("expected HTTP config")
	}
	if soul.HTTP.Headers["Authorization"] != "Bearer secret" || soul.HTTP.Body != "secret body" {
		t.Fatalf("create credentials were not preserved: %#v", soul.HTTP)
	}
	if soul.HTTP.BodyContains != "healthy" || soul.HTTP.ResponseHeaders["X-Trace"] != "present" {
		t.Fatalf("create check config was not converted: %#v", soul.HTTP)
	}
	if soul.HTTP.Feather.Duration != 250*time.Millisecond {
		t.Fatalf("unexpected feather duration: %v", soul.HTTP.Feather.Duration)
	}
}

func TestServer_SoulMutationsSynchronizeProbe(t *testing.T) {
	store := newMockGRPCStore()
	probe := &mockGRPCProbe{}
	srv := NewServer(":0", store, probe, &mockAuthenticator{}, nil, nil, false)

	created, err := srv.CreateSoul(testUserContext(), &v1.CreateSoulRequest{
		Name: "API", Type: "http", Target: "https://example.com", Enabled: true,
	})
	if err != nil {
		t.Fatalf("CreateSoul failed: %v", err)
	}
	if len(probe.upserted) != 1 || probe.upserted[0].ID != created.Id {
		t.Fatalf("create did not upsert the stored soul: %#v", probe.upserted)
	}

	name := "API updated"
	if _, err := srv.UpdateSoul(testUserContext(), &v1.UpdateSoulRequest{Id: created.Id, Name: &name}); err != nil {
		t.Fatalf("UpdateSoul failed: %v", err)
	}
	if len(probe.upserted) != 2 || probe.upserted[1].Name != name {
		t.Fatalf("update did not upsert the updated soul: %#v", probe.upserted)
	}

	if _, err := srv.DeleteSoul(testUserContext(), &v1.DeleteSoulRequest{Id: created.Id}); err != nil {
		t.Fatalf("DeleteSoul failed: %v", err)
	}
	if len(probe.removed) != 1 || probe.removed[0] != created.Id {
		t.Fatalf("delete did not remove the soul from the probe: %#v", probe.removed)
	}
}

func TestServer_CreateSoulSynchronizesProbeWhenResponseReloadFails(t *testing.T) {
	store := newMockGRPCStore()
	store.getSoulAfterErr = fmt.Errorf("reload failed")
	probe := &mockGRPCProbe{}
	srv := NewServer(":0", store, probe, &mockAuthenticator{}, nil, nil, false)

	_, err := srv.CreateSoul(testUserContext(), &v1.CreateSoulRequest{
		Name: "API", Type: "http", Target: "https://example.com", Enabled: true,
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("expected Internal reload error, got %v", err)
	}
	if len(probe.upserted) != 1 {
		t.Fatalf("persisted create was not synchronized before response reload: %#v", probe.upserted)
	}
}

func TestServer_UpdateSoulRejectsSecretCarryoverToNewTarget(t *testing.T) {
	store := newMockGRPCStore()
	store.souls["soul-1"] = &core.Soul{
		ID:          "soul-1",
		WorkspaceID: "default",
		Name:        "API",
		Type:        core.CheckHTTP,
		Target:      "https://old.example.com",
		HTTP: &core.HTTPConfig{
			Headers: map[string]string{"Authorization": "Bearer secret"},
		},
	}
	probe := &mockGRPCProbe{}
	srv := NewServer(":0", store, probe, &mockAuthenticator{}, nil, nil, false)
	newTarget := "https://new.example.com"

	_, err := srv.UpdateSoul(testUserContext(), &v1.UpdateSoulRequest{Id: "soul-1", Target: &newTarget})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expected FailedPrecondition, got %v", err)
	}
	if store.souls["soul-1"].Target != "https://old.example.com" {
		t.Fatalf("rejected update mutated target: %q", store.souls["soul-1"].Target)
	}
	if len(probe.upserted) != 0 {
		t.Fatalf("rejected update synchronized probe: %#v", probe.upserted)
	}
}

// mockAlertEvent implements a minimal alert event for verdict conversion
func TestNewServer(t *testing.T) {
	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	if srv == nil {
		t.Fatal("NewServer returned nil")
	}
	if srv.grpc == nil {
		t.Fatal("gRPC server not initialized")
	}
}

func TestServer_ListSouls(t *testing.T) {
	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.ListSouls(testUserContext(), &v1.ListSoulsRequest{
		Offset: 0,
		Limit:  20,
	})
	if err != nil {
		t.Fatalf("ListSouls failed: %v", err)
	}
	if resp.Pagination.Total != 0 {
		t.Errorf("Expected 0 souls, got %d", resp.Pagination.Total)
	}
}

func TestServer_GetSoul_NotFound(t *testing.T) {
	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	_, err := srv.GetSoul(testUserContext(), &v1.GetSoulRequest{Id: "nonexistent"})
	if err == nil {
		t.Error("Expected error for nonexistent soul")
	}
}

func TestServer_GetClusterStatus(t *testing.T) {
	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.GetClusterStatus(testUserContext(), nil)
	if err != nil {
		t.Fatalf("GetClusterStatus failed: %v", err)
	}
	if resp.NodeId != "single-node" {
		t.Errorf("Expected node ID 'single-node', got %s", resp.NodeId)
	}
	if !resp.IsLeader {
		t.Error("Expected IsLeader to be true")
	}
}

func TestServer_ListChannels(t *testing.T) {
	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.ListChannels(testUserContext(), &v1.ListChannelsRequest{})
	if err != nil {
		t.Fatalf("ListChannels failed: %v", err)
	}
	if len(resp.Channels) != 0 {
		t.Errorf("Expected 0 channels, got %d", len(resp.Channels))
	}
}

func TestServer_ListRules(t *testing.T) {
	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.ListRules(testUserContext(), &v1.ListRulesRequest{})
	if err != nil {
		t.Fatalf("ListRules failed: %v", err)
	}
	if len(resp.Rules) != 0 {
		t.Errorf("Expected 0 rules, got %d", len(resp.Rules))
	}
}

func TestServer_ListJourneys(t *testing.T) {
	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.ListJourneys(testUserContext(), &v1.ListJourneysRequest{})
	if err != nil {
		t.Fatalf("ListJourneys failed: %v", err)
	}
	if len(resp.Journeys) != 0 {
		t.Errorf("Expected 0 journeys, got %d", len(resp.Journeys))
	}
}

// TestGRPCServer_Listen tests that the server can actually listen and accept connections
func TestGRPCServer_Listen(t *testing.T) {
	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	if err := srv.Start(); err != nil {
		t.Fatalf("Failed to start gRPC server: %v", err)
	}
	defer srv.Stop()

	// Try to connect
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	addr := srv.listener.Addr().String()
	conn, err := grpc.DialContext(ctx, addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		t.Fatalf("Failed to connect to gRPC server: %v", err)
	}
	defer conn.Close()

	client := v1.NewAnubisWatchServiceClient(conn)

	// Add authorization header to context
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer valid-token")

	status, err := client.GetClusterStatus(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetClusterStatus RPC failed: %v", err)
	}
	if status.NodeId != "single-node" {
		t.Errorf("Expected node ID 'single-node', got %s", status.NodeId)
	}
}

// TestGRPCServer_Bufconn tests the server with an in-memory buffer connection
func TestGRPCServer_Bufconn(t *testing.T) {
	const bufSize = 1024 * 1024
	lis := bufconn.Listen(bufSize)

	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	go func() {
		srv.grpc.Serve(lis)
	}()
	defer srv.grpc.GracefulStop()

	// Dial with bufconn
	dialer := func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, "bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(dialer),
		grpc.WithBlock(),
	)
	if err != nil {
		t.Fatalf("Failed to dial bufnet: %v", err)
	}
	defer conn.Close()

	client := v1.NewAnubisWatchServiceClient(conn)

	// Add authorization header to context
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer valid-token")

	// Test ListSouls
	resp, err := client.ListSouls(ctx, &v1.ListSoulsRequest{Limit: 10})
	if err != nil {
		t.Fatalf("ListSouls failed: %v", err)
	}
	if resp == nil {
		t.Fatal("ListSouls returned nil response")
	}

	// Test GetClusterStatus
	status, err := client.GetClusterStatus(ctx, nil)
	if err != nil {
		t.Fatalf("GetClusterStatus failed: %v", err)
	}
	if status.NodeCount != 1 {
		t.Errorf("Expected 1 node, got %d", status.NodeCount)
	}
}

// TestServer_ListVerdicts tests the ListVerdicts RPC
func TestServer_ListVerdicts(t *testing.T) {
	store := newMockGRPCStore()
	store.events = []*core.AlertEvent{
		&core.AlertEvent{
			ID: "evt_1", SoulID: "soul_1", SoulName: "test-soul",
			ChannelID: "ch_1", Status: "firing", Severity: "critical",
			Message: "Test alert", Timestamp: time.Now(),
		},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.ListVerdicts(testUserContext(), &v1.ListVerdictsRequest{
		Limit: 20,
	})
	if err != nil {
		t.Fatalf("ListVerdicts failed: %v", err)
	}
	if len(resp.Verdicts) != 1 {
		t.Errorf("Expected 1 verdict, got %d", len(resp.Verdicts))
	}
	if resp.Verdicts[0].Severity != "critical" {
		t.Errorf("Expected severity 'critical', got %s", resp.Verdicts[0].Severity)
	}
}

// TestServer_CreateSoul tests the CreateSoul mutation RPC
func TestServer_CreateSoul(t *testing.T) {
	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	name := "test-soul"
	target := "example.com"
	interval := int32(60)
	timeout := int32(10)

	resp, err := srv.CreateSoul(testUserContext(), &v1.CreateSoulRequest{
		Name:     name,
		Type:     "http",
		Target:   target,
		Interval: interval,
		Timeout:  timeout,
		Enabled:  true,
		Tags:     []string{"test"},
	})
	if err != nil {
		t.Fatalf("CreateSoul failed: %v", err)
	}
	if resp == nil {
		t.Fatal("CreateSoul returned nil")
	}
}

// TestServer_DeleteSoul tests the DeleteSoul RPC
func TestServer_DeleteSoul(t *testing.T) {
	store := newMockGRPCStore()
	store.souls["soul_1"] = &core.Soul{ID: "soul_1", Name: "test"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	_, err := srv.DeleteSoul(testUserContext(), &v1.DeleteSoulRequest{Id: "soul_1"})
	if err != nil {
		t.Fatalf("DeleteSoul failed: %v", err)
	}
	// Verify deletion
	_, err = srv.GetSoul(testUserContext(), &v1.GetSoulRequest{Id: "soul_1"})
	if err == nil {
		t.Error("Expected error after deletion")
	}
}

// TestServer_CreateChannel tests the CreateChannel mutation RPC
func TestServer_CreateChannel(t *testing.T) {
	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.CreateChannel(testUserContext(), &v1.CreateChannelRequest{
		Name:      "test-slack",
		Type:      "slack",
		Enabled:   true,
		Config:    map[string]string{"webhook_url": "https://hooks.slack.com/test"},
		Workspace: "default",
	})
	if err != nil {
		t.Fatalf("CreateChannel failed: %v", err)
	}
	if resp == nil {
		t.Fatal("CreateChannel returned nil")
	}
}

// TestServer_DeleteChannel tests the DeleteChannel RPC
func TestServer_DeleteChannel(t *testing.T) {
	store := newMockGRPCStore()
	store.channels["ch_1"] = &core.AlertChannel{ID: "ch_1", Name: "test", Type: "slack"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	_, err := srv.DeleteChannel(testUserContext(), &v1.DeleteChannelRequest{Id: "ch_1"})
	if err != nil {
		t.Fatalf("DeleteChannel failed: %v", err)
	}
}

// TestServer_CreateRule tests the CreateRule mutation RPC
func TestServer_CreateRule(t *testing.T) {
	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.CreateRule(testUserContext(), &v1.CreateRuleRequest{
		Name:          "test-rule",
		ConditionType: "consecutive_failures",
		ChannelId:     "ch_1",
		Workspace:     "default",
		Enabled:       true,
		Config:        map[string]string{"threshold": "3"},
	})
	if err != nil {
		t.Fatalf("CreateRule failed: %v", err)
	}
	if resp == nil {
		t.Fatal("CreateRule returned nil")
	}
}

// TestServer_DeleteRule tests the DeleteRule RPC
func TestServer_DeleteRule(t *testing.T) {
	store := newMockGRPCStore()
	store.rules["rule_1"] = &core.AlertRule{ID: "rule_1", Name: "test"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	_, err := srv.DeleteRule(testUserContext(), &v1.DeleteRuleRequest{Id: "rule_1"})
	if err != nil {
		t.Fatalf("DeleteRule failed: %v", err)
	}
}

// TestServer_CreateJourney tests the CreateJourney mutation RPC
func TestServer_CreateJourney(t *testing.T) {
	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.CreateJourney(testUserContext(), &v1.CreateJourneyRequest{
		Name:        "test-journey",
		Description: "Test journey description",
		Interval:    300,
		Enabled:     true,
		Workspace:   "default",
		Steps: []*v1.JourneyStep{
			{
				Name:    "Check API",
				Type:    "http",
				Target:  "https://api.example.com/health",
				Timeout: 10,
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateJourney failed: %v", err)
	}
	if resp == nil {
		t.Fatal("CreateJourney returned nil")
	}
	if resp.Id == "" {
		t.Fatal("CreateJourney returned an empty generated ID")
	}
	if stored := store.journeys[resp.Id]; stored == nil {
		t.Fatalf("CreateJourney returned ID %q that was not saved", resp.Id)
	}
}

// TestServer_DeleteJourney tests the DeleteJourney RPC
func TestServer_DeleteJourney(t *testing.T) {
	store := newMockGRPCStore()
	store.journeys["j_1"] = &core.JourneyConfig{ID: "j_1", Name: "test"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	_, err := srv.DeleteJourney(testUserContext(), &v1.DeleteJourneyRequest{Id: "j_1"})
	if err != nil {
		t.Fatalf("DeleteJourney failed: %v", err)
	}
}

type baseServerStream struct{}

func (baseServerStream) SetHeader(md metadata.MD) error  { return nil }
func (baseServerStream) SendHeader(md metadata.MD) error { return nil }
func (baseServerStream) SetTrailer(md metadata.MD)       {}
func (baseServerStream) Context() context.Context        { return context.Background() }
func (baseServerStream) SendMsg(m interface{}) error     { return nil }
func (baseServerStream) RecvMsg(m interface{}) error     { return nil }

type mockJudgmentsStream struct {
	baseServerStream
	ctx context.Context
}

func (m *mockJudgmentsStream) Context() context.Context  { return m.ctx }
func (m *mockJudgmentsStream) Send(j *v1.Judgment) error { return nil }

type mockVerdictsStream struct {
	baseServerStream
	ctx context.Context
}

func (m *mockVerdictsStream) Context() context.Context { return m.ctx }
func (m *mockVerdictsStream) Send(v *v1.Verdict) error { return nil }

func TestServer_GetSoul_Found(t *testing.T) {
	store := newMockGRPCStore()
	store.souls["soul_1"] = &core.Soul{ID: "soul_1", Name: "test"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.GetSoul(testUserContext(), &v1.GetSoulRequest{Id: "soul_1"})
	if err != nil {
		t.Fatalf("GetSoul failed: %v", err)
	}
	if resp.Id != "soul_1" {
		t.Errorf("Expected soul_1, got %s", resp.Id)
	}
}

func TestServer_UpdateSoul(t *testing.T) {
	store := newMockGRPCStore()
	store.souls["soul_1"] = &core.Soul{ID: "soul_1", Name: "old", Type: "http", Target: "old.com"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	name := "updated"
	_, err := srv.UpdateSoul(testUserContext(), &v1.UpdateSoulRequest{
		Id:   "soul_1",
		Name: &name,
	})
	if err != nil {
		t.Fatalf("UpdateSoul failed: %v", err)
	}
}

// mockJudgment implements a minimal judgment with getters
func TestServer_ListJudgments(t *testing.T) {
	store := newMockGRPCStore()
	store.souls["s1"] = &core.Soul{ID: "s1", Name: "test"}
	store.judgments = []*core.Judgment{
		&core.Judgment{ID: "j1", SoulID: "s1", Status: "alive", Duration: 10 * time.Millisecond, Message: "ok", Timestamp: time.Now()},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.ListJudgments(testUserContext(), &v1.ListJudgmentsRequest{Limit: 10})
	if err != nil {
		t.Fatalf("ListJudgments failed: %v", err)
	}
	if len(resp.Judgments) != 1 {
		t.Errorf("Expected 1 judgment, got %d", len(resp.Judgments))
	}
}

func TestServer_GetSoulJudgments(t *testing.T) {
	store := newMockGRPCStore()
	store.judgments = []*core.Judgment{
		&core.Judgment{ID: "j1", SoulID: "s1", Status: "alive", Duration: 10 * time.Millisecond, Message: "ok", Timestamp: time.Now()},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.GetSoulJudgments(testUserContext(), &v1.GetSoulJudgmentsRequest{SoulId: "s1", Limit: 10})
	if err != nil {
		t.Fatalf("GetSoulJudgments failed: %v", err)
	}
	if len(resp.Judgments) != 1 {
		t.Errorf("Expected 1 judgment, got %d", len(resp.Judgments))
	}
}

func TestServer_JudgeSoul(t *testing.T) {
	store := newMockGRPCStore()
	store.souls["s1"] = &core.Soul{ID: "s1", Name: "test"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	_, err := srv.JudgeSoul(testUserContext(), &v1.JudgeSoulRequest{SoulId: "s1"})
	if err != nil {
		t.Fatalf("JudgeSoul failed: %v", err)
	}
}

func TestServer_GetChannel(t *testing.T) {
	store := newMockGRPCStore()
	store.channels["ch_1"] = &core.AlertChannel{ID: "ch_1", Name: "test", Type: "slack"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.GetChannel(testUserContext(), &v1.GetChannelRequest{Id: "ch_1"})
	if err != nil {
		t.Fatalf("GetChannel failed: %v", err)
	}
	if resp.Id != "ch_1" {
		t.Errorf("Expected ch_1, got %s", resp.Id)
	}
}

func TestServer_UpdateChannel(t *testing.T) {
	store := newMockGRPCStore()
	store.channels["ch_1"] = &core.AlertChannel{ID: "ch_1", Name: "old", Type: "slack"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	name := "updated"
	_, err := srv.UpdateChannel(testUserContext(), &v1.UpdateChannelRequest{
		Id:   "ch_1",
		Name: &name,
	})
	if err != nil {
		t.Fatalf("UpdateChannel failed: %v", err)
	}
}

func TestServer_GetRule(t *testing.T) {
	store := newMockGRPCStore()
	store.rules["rule_1"] = &core.AlertRule{ID: "rule_1", Name: "test"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.GetRule(testUserContext(), &v1.GetRuleRequest{Id: "rule_1"})
	if err != nil {
		t.Fatalf("GetRule failed: %v", err)
	}
	if resp.Id != "rule_1" {
		t.Errorf("Expected rule_1, got %s", resp.Id)
	}
}

func TestServer_UpdateRule(t *testing.T) {
	store := newMockGRPCStore()
	store.rules["rule_1"] = &core.AlertRule{ID: "rule_1", Name: "old"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	name := "updated"
	_, err := srv.UpdateRule(testUserContext(), &v1.UpdateRuleRequest{
		Id:   "rule_1",
		Name: &name,
	})
	if err != nil {
		t.Fatalf("UpdateRule failed: %v", err)
	}
}

func TestServer_GetJourney(t *testing.T) {
	store := newMockGRPCStore()
	store.journeys["j_1"] = &core.JourneyConfig{ID: "j_1", Name: "test"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.GetJourney(testUserContext(), &v1.GetJourneyRequest{Id: "j_1"})
	if err != nil {
		t.Fatalf("GetJourney failed: %v", err)
	}
	if resp.Id != "j_1" {
		t.Errorf("Expected j_1, got %s", resp.Id)
	}
}

func TestServer_GetJourney_DeniesOtherWorkspace(t *testing.T) {
	store := newMockGRPCStore()
	store.journeys["j_1"] = &core.JourneyConfig{ID: "j_1", Name: "test", WorkspaceID: "tenant-b"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	_, err := srv.GetJourney(testUserContext(), &v1.GetJourneyRequest{Id: "j_1"})
	if err == nil {
		t.Fatal("Expected GetJourney to deny cross-workspace access")
	}
}

func TestServer_UpdateJourney(t *testing.T) {
	store := newMockGRPCStore()
	store.journeys["j_1"] = &core.JourneyConfig{ID: "j_1", Name: "old"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	name := "updated"
	_, err := srv.UpdateJourney(testUserContext(), &v1.UpdateJourneyRequest{
		Id:   "j_1",
		Name: &name,
	})
	if err != nil {
		t.Fatalf("UpdateJourney failed: %v", err)
	}
}

func TestServer_RunJourney(t *testing.T) {
	store := newMockGRPCStore()
	store.journeys["j_1"] = &core.JourneyConfig{ID: "j_1", Name: "test"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.RunJourney(testUserContext(), &v1.RunJourneyRequest{Id: "j_1"})
	if err != nil {
		t.Fatalf("RunJourney failed: %v", err)
	}
	if resp.Status != "alive" {
		t.Errorf("Expected alive, got %s", resp.Status)
	}
	if len(store.journeyRuns) != 1 {
		t.Errorf("Expected 1 stored run, got %d", len(store.journeyRuns))
	}
}

func TestServer_ListJourneyRuns(t *testing.T) {
	store := newMockGRPCStore()
	store.journeyRuns = []*core.JourneyRun{
		&core.JourneyRun{ID: "run_1", JourneyID: "j_1", WorkspaceID: "default", Status: "success"},
		&core.JourneyRun{ID: "run_2", JourneyID: "j_1", WorkspaceID: "tenant-b", Status: "failed"},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.ListJourneyRuns(testUserContext(), &v1.ListJourneyRunsRequest{JourneyId: "j_1", Limit: 10})
	if err != nil {
		t.Fatalf("ListJourneyRuns failed: %v", err)
	}
	if len(resp.Runs) != 1 {
		t.Errorf("Expected 1 run, got %d", len(resp.Runs))
	}
}

func TestServer_GetJourneyRun(t *testing.T) {
	store := newMockGRPCStore()
	store.journeyRuns = []*core.JourneyRun{
		&core.JourneyRun{ID: "run_1", JourneyID: "j_1", WorkspaceID: "default", Status: "success"},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.GetJourneyRun(testUserContext(), &v1.GetJourneyRunRequest{JourneyId: "j_1", RunId: "run_1"})
	if err != nil {
		t.Fatalf("GetJourneyRun failed: %v", err)
	}
	if resp.Id != "run_1" {
		t.Errorf("Expected run_1, got %s", resp.Id)
	}
}

func TestServer_StreamJudgments(t *testing.T) {
	store := newMockGRPCStore()
	store.judgments = []*core.Judgment{
		&core.Judgment{ID: "j1", SoulID: "s1", Status: "alive", Duration: 10 * time.Millisecond, Message: "ok", Timestamp: time.Now()},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	ctx, cancel := context.WithTimeout(testUserContext(), 100*time.Millisecond)
	defer cancel()

	soulID := "s1"
	stream := &mockJudgmentsStream{ctx: ctx}
	err := srv.StreamJudgments(&v1.StreamRequest{SoulId: &soulID}, stream)
	if err != nil {
		t.Fatalf("StreamJudgments failed: %v", err)
	}
}

func TestServer_StreamVerdicts(t *testing.T) {
	store := newMockGRPCStore()
	store.events = []*core.AlertEvent{
		&core.AlertEvent{ID: "evt_1", SoulID: "s1", Status: "firing", Severity: "critical", Message: "alert", Timestamp: time.Now()},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	ctx, cancel := context.WithTimeout(testUserContext(), 100*time.Millisecond)
	defer cancel()

	soulID := "s1"
	stream := &mockVerdictsStream{ctx: ctx}
	err := srv.StreamVerdicts(&v1.StreamRequest{SoulId: &soulID}, stream)
	if err != nil {
		t.Fatalf("StreamVerdicts failed: %v", err)
	}
}

func TestServer_RBAC_Enforcement(t *testing.T) {
	store := newMockGRPCStore()
	store.souls = map[string]*core.Soul{
		"soul_1": {ID: "soul_1", WorkspaceID: "default"},
	}
	srv := NewServer("0:0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	// Create a user context with 'viewer' role (which should fail soul mutations)
	viewerUser := &api.User{ID: "user-viewer", Email: "viewer@example.com", Role: "viewer", Workspace: "default"}
	viewerCtx := context.WithValue(context.Background(), userContextKey, viewerUser)

	// Test GetSoul (which viewers are allowed to access)
	_, err := srv.GetSoul(viewerCtx, &v1.GetSoulRequest{Id: "soul_1"})
	if err != nil {
		t.Fatalf("GetSoul should allow viewer access, got: %v", err)
	}

	// Test CreateSoul (which viewers are NOT allowed to access)
	_, err = srv.CreateSoul(viewerCtx, &v1.CreateSoulRequest{
		Name: "New Soul",
	})
	if err == nil {
		t.Fatal("CreateSoul should have failed for viewer but succeeded")
	}
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("CreateSoul expected PermissionDenied, got: %v", err)
	}

	// Test DeleteSoul (which viewers are NOT allowed to access)
	_, err = srv.DeleteSoul(viewerCtx, &v1.DeleteSoulRequest{Id: "soul_1"})
	if err == nil {
		t.Fatal("DeleteSoul should have failed for viewer but succeeded")
	}
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("DeleteSoul expected PermissionDenied, got: %v", err)
	}
}
