package grpcapi

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	v1 "github.com/AnubisWatch/anubiswatch/internal/grpcapi/v1"
)

// mockGRPCStore implements Store with in-memory data
type mockGRPCStore struct {
	souls     map[string]interface{}
	judgments []interface{}
	channels  map[string]interface{}
	rules     map[string]interface{}
	journeys  map[string]interface{}
	events    []interface{}
	nextID    int
}

func newMockGRPCStore() *mockGRPCStore {
	return &mockGRPCStore{
		souls:    make(map[string]interface{}),
		channels: make(map[string]interface{}),
		rules:    make(map[string]interface{}),
		journeys: make(map[string]interface{}),
		events:   []interface{}{},
	}
}

func (m *mockGRPCStore) GetSoulNoCtx(id string) (interface{}, error) { return m.souls[id], nil }
func (m *mockGRPCStore) ListSoulsNoCtx(ws string, o, l int) ([]interface{}, error) {
	result := make([]interface{}, 0, len(m.souls))
	for _, s := range m.souls {
		result = append(result, s)
	}
	return result, nil
}
func (m *mockGRPCStore) SaveSoulNoCtx(s interface{}) error {
	m.nextID++
	id := fmt.Sprintf("soul_%d", m.nextID)
	name := ""
	soulType := ""
	target := ""
	if mp, ok := s.(map[string]interface{}); ok {
		if v, ok := mp["name"].(string); ok {
			name = v
		}
		if v, ok := mp["type"].(string); ok {
			soulType = v
		}
		if v, ok := mp["target"].(string); ok {
			target = v
		}
	}
	if name == "" {
		name = "test-soul"
	}
	m.souls[id] = &mockSoul{id: id, name: name, status: "alive", soulType: soulType, target: target}
	return nil
}
func (m *mockGRPCStore) DeleteSoulNoCtx(id string) error { delete(m.souls, id); return nil }
func (m *mockGRPCStore) ListJudgmentsNoCtx(soulID string, start, end time.Time, limit int) ([]interface{}, error) {
	return m.judgments, nil
}
func (m *mockGRPCStore) GetChannelNoCtx(id string, ws string) (interface{}, error) {
	return m.channels[id], nil
}
func (m *mockGRPCStore) ListChannelsNoCtx(ws string) ([]interface{}, error) {
	result := make([]interface{}, 0, len(m.channels))
	for _, c := range m.channels {
		result = append(result, c)
	}
	return result, nil
}
func (m *mockGRPCStore) SaveChannelNoCtx(ch interface{}) error {
	m.nextID++
	id := fmt.Sprintf("ch_%d", m.nextID)
	name := "test-channel"
	chType := "slack"
	if mp, ok := ch.(map[string]interface{}); ok {
		if v, ok := mp["name"].(string); ok {
			name = v
		}
		if v, ok := mp["type"].(string); ok {
			chType = v
		}
	}
	m.channels[id] = &mockChannel{id: id, name: name, chType: chType}
	return nil
}
func (m *mockGRPCStore) DeleteChannelNoCtx(id string, ws string) error {
	delete(m.channels, id)
	return nil
}
func (m *mockGRPCStore) GetRuleNoCtx(id string, ws string) (interface{}, error) {
	return m.rules[id], nil
}
func (m *mockGRPCStore) ListRulesNoCtx(ws string) ([]interface{}, error) {
	result := make([]interface{}, 0, len(m.rules))
	for _, r := range m.rules {
		result = append(result, r)
	}
	return result, nil
}
func (m *mockGRPCStore) SaveRuleNoCtx(rule interface{}) error {
	m.nextID++
	id := fmt.Sprintf("rule_%d", m.nextID)
	name := "test-rule"
	if mp, ok := rule.(map[string]interface{}); ok {
		if v, ok := mp["name"].(string); ok {
			name = v
		}
	}
	m.rules[id] = &mockRule{id: id, name: name}
	return nil
}
func (m *mockGRPCStore) DeleteRuleNoCtx(id string, ws string) error     { delete(m.rules, id); return nil }
func (m *mockGRPCStore) GetJourneyNoCtx(id string) (interface{}, error) { return m.journeys[id], nil }
func (m *mockGRPCStore) ListJourneysNoCtx(ws string, o, l int) ([]interface{}, error) {
	result := make([]interface{}, 0, len(m.journeys))
	for _, j := range m.journeys {
		result = append(result, j)
	}
	return result, nil
}
func (m *mockGRPCStore) SaveJourneyNoCtx(j interface{}) error {
	m.nextID++
	id := fmt.Sprintf("journey_%d", m.nextID)
	name := "test-journey"
	if mp, ok := j.(map[string]interface{}); ok {
		if v, ok := mp["name"].(string); ok {
			name = v
		}
	}
	m.journeys[id] = &mockJourney{id: id, name: name}
	return nil
}
func (m *mockGRPCStore) DeleteJourneyNoCtx(id string) error { delete(m.journeys, id); return nil }
func (m *mockGRPCStore) ListJourneyRunsNoCtx(journeyID string, limit int) ([]interface{}, error) {
	return nil, nil
}
func (m *mockGRPCStore) GetJourneyRunNoCtx(journeyID, runID string) (interface{}, error) {
	return nil, nil
}
func (m *mockGRPCStore) ListEvents(soulID string, limit int) ([]interface{}, error) {
	return m.events, nil
}

type mockGRPCProbe struct{}

func (m *mockGRPCProbe) ForceCheck(soulID string) (interface{}, error) {
	return nil, nil
}

// mockSoul implements a minimal soul with getters for PB conversion
type mockSoul struct {
	id, name, status, soulType, target string
}

func (m *mockSoul) GetID() string              { return m.id }
func (m *mockSoul) GetName() string            { return m.name }
func (m *mockSoul) GetStatus() string          { return m.status }
func (m *mockSoul) GetType() string            { return m.soulType }
func (m *mockSoul) GetTarget() string          { return m.target }
func (m *mockSoul) GetInterval() time.Duration { return 60 * time.Second }
func (m *mockSoul) GetTimeout() time.Duration  { return 10 * time.Second }
func (m *mockSoul) GetEnabled() bool           { return true }
func (m *mockSoul) GetTags() []string          { return nil }
func (m *mockSoul) GetWorkspaceID() string     { return "default" }
func (m *mockSoul) GetRegion() string          { return "" }
func (m *mockSoul) GetCreatedAt() time.Time    { return time.Time{} }
func (m *mockSoul) GetUpdatedAt() time.Time    { return time.Time{} }
func (m *mockSoul) GetHTTP() interface{}       { return nil }
func (m *mockSoul) GetTCP() interface{}        { return nil }
func (m *mockSoul) GetDNS() interface{}        { return nil }
func (m *mockSoul) GetTLS() interface{}        { return nil }
func (m *mockSoul) GetGRPC() interface{}       { return nil }

// mockChannel implements a minimal channel with getters
type mockChannel struct {
	id, name, chType string
}

func (m *mockChannel) GetID() string                     { return m.id }
func (m *mockChannel) GetName() string                   { return m.name }
func (m *mockChannel) GetType() string                   { return m.chType }
func (m *mockChannel) GetEnabled() bool                  { return true }
func (m *mockChannel) GetConfig() map[string]interface{} { return make(map[string]interface{}) }
func (m *mockChannel) GetWorkspaceID() string            { return "default" }
func (m *mockChannel) GetCreatedAt() time.Time           { return time.Time{} }

// mockRule implements a minimal rule with getters
type mockRule struct {
	id, name string
}

func (m *mockRule) GetID() string           { return m.id }
func (m *mockRule) GetName() string         { return m.name }
func (m *mockRule) GetEnabled() bool        { return true }
func (m *mockRule) GetChannels() []string   { return []string{"ch_1"} }
func (m *mockRule) GetWorkspaceID() string  { return "default" }
func (m *mockRule) GetCreatedAt() time.Time { return time.Time{} }

// mockJourney implements a minimal journey with getters
type mockJourney struct {
	id, name string
}

func (m *mockJourney) GetID() string            { return m.id }
func (m *mockJourney) GetName() string          { return m.name }
func (m *mockJourney) GetEnabled() bool         { return true }
func (m *mockJourney) GetWorkspaceID() string   { return "default" }
func (m *mockJourney) GetDescription() string   { return "" }
func (m *mockJourney) GetWeight() time.Duration { return 0 }
func (m *mockJourney) GetSteps() []interface{}  { return nil }
func (m *mockJourney) GetCreatedAt() time.Time  { return time.Time{} }

// mockAlertEvent implements a minimal alert event for verdict conversion
type mockAlertEvent struct {
	id, soulID, soulName, channelID, status, severity, message string
	timestamp                                                  time.Time
}

func (m *mockAlertEvent) GetID() string           { return m.id }
func (m *mockAlertEvent) GetSoulID() string       { return m.soulID }
func (m *mockAlertEvent) GetSoulName() string     { return m.soulName }
func (m *mockAlertEvent) GetChannelID() string    { return m.channelID }
func (m *mockAlertEvent) GetStatus() string       { return m.status }
func (m *mockAlertEvent) GetSeverity() string     { return m.severity }
func (m *mockAlertEvent) GetMessage() string      { return m.message }
func (m *mockAlertEvent) GetTimestamp() time.Time { return m.timestamp }
func (m *mockAlertEvent) GetResolved() bool       { return false }
func (m *mockAlertEvent) GetAcknowledged() bool   { return false }

func TestNewServer(t *testing.T) {
	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, nil)
	if srv == nil {
		t.Fatal("NewServer returned nil")
	}
	if srv.grpc == nil {
		t.Fatal("gRPC server not initialized")
	}
}

func TestServer_ListSouls(t *testing.T) {
	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, nil)

	resp, err := srv.ListSouls(context.Background(), &v1.ListSoulsRequest{
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
	srv := NewServer(":0", store, &mockGRPCProbe{}, nil)

	_, err := srv.GetSoul(context.Background(), &v1.GetSoulRequest{Id: "nonexistent"})
	if err == nil {
		t.Error("Expected error for nonexistent soul")
	}
}

func TestServer_GetClusterStatus(t *testing.T) {
	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, nil)

	resp, err := srv.GetClusterStatus(context.Background(), nil)
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
	srv := NewServer(":0", store, &mockGRPCProbe{}, nil)

	resp, err := srv.ListChannels(context.Background(), &v1.ListChannelsRequest{})
	if err != nil {
		t.Fatalf("ListChannels failed: %v", err)
	}
	if len(resp.Channels) != 0 {
		t.Errorf("Expected 0 channels, got %d", len(resp.Channels))
	}
}

func TestServer_ListRules(t *testing.T) {
	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, nil)

	resp, err := srv.ListRules(context.Background(), &v1.ListRulesRequest{})
	if err != nil {
		t.Fatalf("ListRules failed: %v", err)
	}
	if len(resp.Rules) != 0 {
		t.Errorf("Expected 0 rules, got %d", len(resp.Rules))
	}
}

func TestServer_ListJourneys(t *testing.T) {
	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, nil)

	resp, err := srv.ListJourneys(context.Background(), &v1.ListJourneysRequest{})
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
	srv := NewServer("127.0.0.1:0", store, &mockGRPCProbe{}, nil)

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
	status, err := client.GetClusterStatus(ctx, nil)
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
	srv := NewServer("bufconn", store, &mockGRPCProbe{}, nil)

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
	store.events = []interface{}{
		&mockAlertEvent{
			id: "evt_1", soulID: "soul_1", soulName: "test-soul",
			channelID: "ch_1", status: "firing", severity: "critical",
			message: "Test alert", timestamp: time.Now(),
		},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, nil)

	resp, err := srv.ListVerdicts(context.Background(), &v1.ListVerdictsRequest{
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
	srv := NewServer(":0", store, &mockGRPCProbe{}, nil)

	name := "test-soul"
	target := "example.com"
	interval := int32(60)
	timeout := int32(10)

	resp, err := srv.CreateSoul(context.Background(), &v1.CreateSoulRequest{
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
	store.souls["soul_1"] = &mockSoul{id: "soul_1", name: "test"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, nil)

	_, err := srv.DeleteSoul(context.Background(), &v1.DeleteSoulRequest{Id: "soul_1"})
	if err != nil {
		t.Fatalf("DeleteSoul failed: %v", err)
	}
	// Verify deletion
	_, err = srv.GetSoul(context.Background(), &v1.GetSoulRequest{Id: "soul_1"})
	if err == nil {
		t.Error("Expected error after deletion")
	}
}

// TestServer_CreateChannel tests the CreateChannel mutation RPC
func TestServer_CreateChannel(t *testing.T) {
	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, nil)

	resp, err := srv.CreateChannel(context.Background(), &v1.CreateChannelRequest{
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
	store.channels["ch_1"] = &mockChannel{id: "ch_1", name: "test", chType: "slack"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, nil)

	_, err := srv.DeleteChannel(context.Background(), &v1.DeleteChannelRequest{Id: "ch_1"})
	if err != nil {
		t.Fatalf("DeleteChannel failed: %v", err)
	}
}

// TestServer_CreateRule tests the CreateRule mutation RPC
func TestServer_CreateRule(t *testing.T) {
	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, nil)

	resp, err := srv.CreateRule(context.Background(), &v1.CreateRuleRequest{
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
	store.rules["rule_1"] = &mockRule{id: "rule_1", name: "test"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, nil)

	_, err := srv.DeleteRule(context.Background(), &v1.DeleteRuleRequest{Id: "rule_1"})
	if err != nil {
		t.Fatalf("DeleteRule failed: %v", err)
	}
}

// TestServer_CreateJourney tests the CreateJourney mutation RPC
func TestServer_CreateJourney(t *testing.T) {
	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, nil)

	resp, err := srv.CreateJourney(context.Background(), &v1.CreateJourneyRequest{
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
}

// TestServer_DeleteJourney tests the DeleteJourney RPC
func TestServer_DeleteJourney(t *testing.T) {
	store := newMockGRPCStore()
	store.journeys["j_1"] = &mockJourney{id: "j_1", name: "test"}
	srv := NewServer(":0", store, &mockGRPCProbe{}, nil)

	_, err := srv.DeleteJourney(context.Background(), &v1.DeleteJourneyRequest{Id: "j_1"})
	if err != nil {
		t.Fatalf("DeleteJourney failed: %v", err)
	}
}
