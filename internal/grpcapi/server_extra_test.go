package grpcapi

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	v1 "github.com/AnubisWatch/anubiswatch/internal/grpcapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// testUserContext is imported from server_test.go - but since it's in the same package, we can use it directly.

// failingMockGRPCStore wraps mockGRPCStore and can return errors
type failingMockGRPCStore struct {
	*mockGRPCStore
	listJudgmentsErr bool
	listEventsErr    bool
	getJourneyRunErr bool
	listSoulsErr     bool
}

func (m *failingMockGRPCStore) ListJudgmentsNoCtx(soulID string, start, end time.Time, limit int) ([]*core.Judgment, error) {
	if m.listJudgmentsErr {
		return nil, fmt.Errorf("db error")
	}
	return m.mockGRPCStore.ListJudgmentsNoCtx(soulID, start, end, limit)
}

func (m *failingMockGRPCStore) ListEvents(soulID string, limit int) ([]*core.AlertEvent, error) {
	if m.listEventsErr {
		return nil, fmt.Errorf("db error")
	}
	return m.mockGRPCStore.ListEvents(soulID, limit)
}

func (m *failingMockGRPCStore) GetJourneyRunNoCtx(workspace, journeyID, runID string) (*core.JourneyRun, error) {
	if m.getJourneyRunErr {
		return nil, fmt.Errorf("db error")
	}
	return m.mockGRPCStore.GetJourneyRunNoCtx(workspace, journeyID, runID)
}

func (m *failingMockGRPCStore) ListSoulsNoCtx(ws string, o, l int) ([]*core.Soul, error) {
	if m.listSoulsErr {
		return nil, fmt.Errorf("db error")
	}
	return m.mockGRPCStore.ListSoulsNoCtx(ws, o, l)
}

type errorJudgmentsStream struct {
	baseServerStream
	ctx context.Context
}

func (m *errorJudgmentsStream) Context() context.Context  { return m.ctx }
func (m *errorJudgmentsStream) Send(j *v1.Judgment) error { return errors.New("send error") }

type errorVerdictsStream struct {
	baseServerStream
	ctx context.Context
}

func (m *errorVerdictsStream) Context() context.Context { return m.ctx }
func (m *errorVerdictsStream) Send(v *v1.Verdict) error { return errors.New("send error") }

func TestServer_Start_InvalidAddress(t *testing.T) {
	srv := NewServer("invalID://:abc", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	if err := srv.Start(); err == nil {
		t.Error("Expected error for invalid listen address")
	}
}

func TestServer_StreamJudgments_StoreError(t *testing.T) {
	store := &failingMockGRPCStore{mockGRPCStore: newMockGRPCStore(), listJudgmentsErr: true}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	ctx, cancel := context.WithTimeout(testUserContext(), 150*time.Millisecond)
	defer cancel()

	soulID := "s1"
	stream := &mockJudgmentsStream{ctx: ctx}
	err := srv.StreamJudgments(&v1.StreamRequest{SoulId: &soulID}, stream)
	if err != nil {
		t.Fatalf("StreamJudgments failed: %v", err)
	}
}

func TestServer_StreamJudgments_SendError(t *testing.T) {
	store := newMockGRPCStore()
	store.judgments = []*core.Judgment{
		{ID: "j1", SoulID: "s1", Status: "alive", Duration: 10 * time.Millisecond, Message: "ok", Timestamp: time.Now()},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	ctx, cancel := context.WithTimeout(testUserContext(), 1100*time.Millisecond)
	defer cancel()

	soulID := "s1"
	stream := &errorJudgmentsStream{ctx: ctx}
	err := srv.StreamJudgments(&v1.StreamRequest{SoulId: &soulID}, stream)
	if err == nil {
		t.Error("Expected error from send failure")
	}
}

func TestServer_StreamVerdicts_StoreError(t *testing.T) {
	store := &failingMockGRPCStore{mockGRPCStore: newMockGRPCStore(), listEventsErr: true}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	ctx, cancel := context.WithTimeout(testUserContext(), 150*time.Millisecond)
	defer cancel()

	soulID := "s1"
	stream := &mockVerdictsStream{ctx: ctx}
	err := srv.StreamVerdicts(&v1.StreamRequest{SoulId: &soulID}, stream)
	if err != nil {
		t.Fatalf("StreamVerdicts failed: %v", err)
	}
}

func TestServer_StreamVerdicts_SendError(t *testing.T) {
	store := newMockGRPCStore()
	store.events = []*core.AlertEvent{
		{ID: "evt_1", SoulID: "s1", Status: "firing", Severity: "critical", Message: "alert", Timestamp: time.Now()},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	ctx, cancel := context.WithTimeout(testUserContext(), 1100*time.Millisecond)
	defer cancel()

	soulID := "s1"
	stream := &errorVerdictsStream{ctx: ctx}
	err := srv.StreamVerdicts(&v1.StreamRequest{SoulId: &soulID}, stream)
	if err == nil {
		t.Error("Expected error from send failure")
	}
}

func TestServer_GetJourneyRun_NotFound(t *testing.T) {
	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	_, err := srv.GetJourneyRun(testUserContext(), &v1.GetJourneyRunRequest{
		JourneyId: "missing",
		RunId:     "missing",
	})
	if err == nil {
		t.Error("Expected error for missing journey run")
	}
}

func TestServer_GetJourneyRun_StorageError(t *testing.T) {
	store := &failingMockGRPCStore{mockGRPCStore: newMockGRPCStore(), getJourneyRunErr: true}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	_, err := srv.GetJourneyRun(testUserContext(), &v1.GetJourneyRunRequest{
		JourneyId: "j1",
		RunId:     "r1",
	})
	if err == nil {
		t.Error("Expected error for storage failure")
	}
}

func TestServer_ListSouls_StoreError(t *testing.T) {
	store := &failingMockGRPCStore{mockGRPCStore: newMockGRPCStore(), listSoulsErr: true}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	_, err := srv.ListSouls(testUserContext(), &v1.ListSoulsRequest{})
	if err == nil {
		t.Error("Expected error for storage failure")
	}
}

func TestServer_ListSouls_FiltersBeforePagination(t *testing.T) {
	store := newMockGRPCStore()
	store.souls["dns-prod"] = &core.Soul{ID: "dns-prod", Name: "DNS prod", Type: "dns", Tags: []string{"prod"}}
	store.souls["http-prod"] = &core.Soul{ID: "http-prod", Name: "HTTP prod", Type: "http", Tags: []string{"prod", "critical"}}
	store.souls["http-dev"] = &core.Soul{ID: "http-dev", Name: "HTTP dev", Type: "http", Tags: []string{"dev"}}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	soulType := "http"
	tag := "prod"
	resp, err := srv.ListSouls(testUserContext(), &v1.ListSoulsRequest{Limit: 1, Type: &soulType, Tag: &tag})
	if err != nil {
		t.Fatalf("ListSouls failed: %v", err)
	}
	if len(resp.Souls) != 1 || resp.Souls[0].Id != "http-prod" {
		t.Fatalf("expected only http-prod, got %+v", resp.Souls)
	}
	assertPagination(t, resp.Pagination, 1, 0, 1, false, -1)
}

func TestServer_GettersReturnNotFoundForNilResource(t *testing.T) {
	store := newMockGRPCStore()
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	tests := []struct {
		name string
		call func() error
	}{
		{name: "soul", call: func() error { _, err := srv.GetSoul(testUserContext(), &v1.GetSoulRequest{Id: "missing"}); return err }},
		{name: "channel", call: func() error {
			_, err := srv.GetChannel(testUserContext(), &v1.GetChannelRequest{Id: "missing"})
			return err
		}},
		{name: "rule", call: func() error { _, err := srv.GetRule(testUserContext(), &v1.GetRuleRequest{Id: "missing"}); return err }},
		{name: "journey", call: func() error {
			_, err := srv.GetJourney(testUserContext(), &v1.GetJourneyRequest{Id: "missing"})
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := status.Code(tt.call()); got != codes.NotFound {
				t.Fatalf("expected NotFound, got %s", got)
			}
		})
	}
}

func TestServer_ListSouls_PaginationHasMore(t *testing.T) {
	store := newMockGRPCStore()
	for i := 0; i < 5; i++ {
		_ = store.SaveSoulNoCtx(&core.Soul{Name: fmt.Sprintf("soul-%d", i)})
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.ListSouls(testUserContext(), &v1.ListSoulsRequest{Limit: 3})
	if err != nil {
		t.Fatalf("ListSouls failed: %v", err)
	}
	if !resp.Pagination.HasMore {
		t.Error("Expected HasMore to be true")
	}
	if resp.Pagination.NextOffset == nil || *resp.Pagination.NextOffset != 3 {
		t.Errorf("Expected NextOffset=3, got %v", resp.Pagination.NextOffset)
	}
}

func assertPagination(t *testing.T, got *v1.Pagination, total, offset, limit int32, hasMore bool, nextOffset int32) {
	t.Helper()
	if got == nil {
		t.Fatal("Expected pagination, got nil")
	}
	if got.Total != total || got.Offset != offset || got.Limit != limit || got.HasMore != hasMore {
		t.Fatalf("Unexpected pagination: got total=%d offset=%d limit=%d has_more=%t",
			got.Total, got.Offset, got.Limit, got.HasMore)
	}
	if nextOffset < 0 {
		if got.NextOffset != nil {
			t.Fatalf("Expected no next offset, got %d", *got.NextOffset)
		}
		return
	}
	if got.NextOffset == nil || *got.NextOffset != nextOffset {
		t.Fatalf("Expected next offset %d, got %v", nextOffset, got.NextOffset)
	}
}

func TestServer_ListChannels_AppliesPagination(t *testing.T) {
	store := newMockGRPCStore()
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("ch_%d", i)
		store.channels[id] = &core.AlertChannel{ID: id, Name: id, Type: "webhook"}
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.ListChannels(testUserContext(), &v1.ListChannelsRequest{Offset: 1, Limit: 2})
	if err != nil {
		t.Fatalf("ListChannels failed: %v", err)
	}
	if len(resp.Channels) != 2 {
		t.Fatalf("Expected 2 channels, got %d", len(resp.Channels))
	}
	assertPagination(t, resp.Pagination, 5, 1, 2, true, 3)
}

func TestServer_ListRules_AppliesPagination(t *testing.T) {
	store := newMockGRPCStore()
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("rule_%d", i)
		store.rules[id] = &core.AlertRule{ID: id, Name: id}
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.ListRules(testUserContext(), &v1.ListRulesRequest{Offset: 2, Limit: 2})
	if err != nil {
		t.Fatalf("ListRules failed: %v", err)
	}
	if len(resp.Rules) != 2 {
		t.Fatalf("Expected 2 rules, got %d", len(resp.Rules))
	}
	assertPagination(t, resp.Pagination, 5, 2, 2, true, 4)
}

func TestServer_ListJourneys_AppliesPagination(t *testing.T) {
	store := newMockGRPCStore()
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("journey_%d", i)
		store.journeys[id] = &core.JourneyConfig{ID: id, Name: id}
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)

	resp, err := srv.ListJourneys(testUserContext(), &v1.ListJourneysRequest{Offset: 3, Limit: 2})
	if err != nil {
		t.Fatalf("ListJourneys failed: %v", err)
	}
	if len(resp.Journeys) != 2 {
		t.Fatalf("Expected 2 journeys, got %d", len(resp.Journeys))
	}
	assertPagination(t, resp.Pagination, 5, 3, 2, false, -1)
}

func TestServer_ListJudgments_FiltersStatusAndOffsets(t *testing.T) {
	store := newMockGRPCStore()
	store.souls["s1"] = &core.Soul{ID: "s1", Name: "test"}
	now := time.Now()
	store.judgments = []*core.Judgment{
		{ID: "j1", SoulID: "s1", Status: "alive", Duration: time.Millisecond, Timestamp: now},
		&core.Judgment{ID: "j2", SoulID: "s1", Status: "dead", Duration: time.Millisecond, Timestamp: now},
		&core.Judgment{ID: "j3", SoulID: "s1", Status: "alive", Duration: time.Millisecond, Timestamp: now},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	statusFilter := "alive"

	resp, err := srv.ListJudgments(testUserContext(), &v1.ListJudgmentsRequest{
		Status: &statusFilter,
		Offset: 1,
		Limit:  1,
	})
	if err != nil {
		t.Fatalf("ListJudgments failed: %v", err)
	}
	if len(resp.Judgments) != 1 || resp.Judgments[0].Id != "j3" {
		t.Fatalf("Expected only j3, got %#v", resp.Judgments)
	}
	assertPagination(t, resp.Pagination, 2, 1, 1, false, -1)
}

func TestServer_ListVerdicts_FiltersStatusSeverityAndOffsets(t *testing.T) {
	store := newMockGRPCStore()
	now := time.Now()
	store.events = []*core.AlertEvent{
		{ID: "evt_1", SoulID: "s1", Status: "firing", Severity: "critical", Timestamp: now},
		&core.AlertEvent{ID: "evt_2", SoulID: "s1", Status: "resolved", Severity: "critical", Timestamp: now},
		&core.AlertEvent{ID: "evt_3", SoulID: "s1", Status: "firing", Severity: "warning", Timestamp: now},
		&core.AlertEvent{ID: "evt_4", SoulID: "s1", Status: "firing", Severity: "critical", Timestamp: now},
	}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
	statusFilter := "firing"
	severityFilter := "critical"

	resp, err := srv.ListVerdicts(testUserContext(), &v1.ListVerdictsRequest{
		Status:   &statusFilter,
		Severity: &severityFilter,
		Offset:   1,
		Limit:    1,
	})
	if err != nil {
		t.Fatalf("ListVerdicts failed: %v", err)
	}
	if len(resp.Verdicts) != 1 || resp.Verdicts[0].Id != "evt_4" {
		t.Fatalf("Expected only evt_4, got %#v", resp.Verdicts)
	}
	assertPagination(t, resp.Pagination, 2, 1, 1, false, -1)
}

// applyChannelConfig tests

func TestApplyChannelConfig_NilConfig(t *testing.T) {
	applyChannelConfig(nil, map[string]string{"webhook_url": "http://example.com"})
}

func TestApplyChannelConfig_AllowedFields(t *testing.T) {
	config := make(map[string]interface{})
	updates := map[string]string{
		"webhook_url": "http://example.com",
		"secret":      "my-secret",
		"method":      "POST",
		"url":         "http://test.com",
	}
	applyChannelConfig(config, updates)

	if config["webhook_url"] != "http://example.com" {
		t.Errorf("Expected webhook_url to be set")
	}
	if config["secret"] != "my-secret" {
		t.Errorf("Expected secret to be set")
	}
	if config["method"] != "POST" {
		t.Errorf("Expected method to be set")
	}
	if config["url"] != "http://test.com" {
		t.Errorf("Expected url to be set")
	}
}

func TestApplyChannelConfig_DisallowedFields(t *testing.T) {
	config := make(map[string]interface{})
	updates := map[string]string{
		"invalid_field": "should be ignored",
		"name":          "should be ignored",
	}
	applyChannelConfig(config, updates)

	if _, exists := config["invalid_field"]; exists {
		t.Errorf("invalid_field should not be set")
	}
	if _, exists := config["name"]; exists {
		t.Errorf("name should not be set")
	}
}

// applyRuleConfig tests

func TestApplyRuleConfig(t *testing.T) {
	m := make(map[string]interface{})
	updates := map[string]string{
		"channel_ids":        "ch1,ch2",
		"cooldown":           "5m",
		"severity":           "critical",
		"notification_delay": "30s",
	}
	applyRuleConfig(m, updates)

	if m["channel_ids"] != "ch1,ch2" {
		t.Errorf("Expected channel_ids to be set")
	}
	if m["cooldown"] != "5m" {
		t.Errorf("Expected cooldown to be set")
	}
	if m["severity"] != "critical" {
		t.Errorf("Expected severity to be set")
	}
	if m["notification_delay"] != "30s" {
		t.Errorf("Expected notification_delay to be set")
	}
}

func TestApplyRuleConfig_DisallowedFields(t *testing.T) {
	m := make(map[string]interface{})
	updates := map[string]string{
		"invalid_field": "should be ignored",
		"name":          "should be ignored",
	}
	applyRuleConfig(m, updates)

	if _, exists := m["invalid_field"]; exists {
		t.Errorf("invalid_field should not be set")
	}
	if _, exists := m["name"]; exists {
		t.Errorf("name should not be set")
	}
}

// applyRuleCoreConfig tests

func TestApplyRuleCoreConfig_Severity(t *testing.T) {
	rule := &core.AlertRule{Severity: core.SeverityWarning}
	updates := map[string]string{"severity": "critical"}
	applyRuleCoreConfig(rule, updates)

	if rule.Severity != core.SeverityCritical {
		t.Errorf("Expected severity to be critical, got %s", rule.Severity)
	}
}

func TestApplyRuleCoreConfig_ChannelIDs(t *testing.T) {
	rule := &core.AlertRule{Channels: []string{}}
	updates := map[string]string{"channel_ids": "ch1,ch2,ch3"}
	applyRuleCoreConfig(rule, updates)

	if len(rule.Channels) != 3 {
		t.Errorf("Expected 3 channels, got %d", len(rule.Channels))
	}
	if rule.Channels[0] != "ch1" || rule.Channels[1] != "ch2" || rule.Channels[2] != "ch3" {
		t.Errorf("Unexpected channel values: %v", rule.Channels)
	}
}

func TestApplyRuleCoreConfig_ChannelIDsEmpty(t *testing.T) {
	rule := &core.AlertRule{Channels: []string{"old"}}
	updates := map[string]string{"channel_ids": ""}
	applyRuleCoreConfig(rule, updates)

	// Empty string doesn't clear channels - original remains
	if len(rule.Channels) != 1 || rule.Channels[0] != "old" {
		t.Errorf("Expected channels to remain unchanged with empty string, got %v", rule.Channels)
	}
}

func TestApplyRuleCoreConfig_Cooldown(t *testing.T) {
	rule := &core.AlertRule{}
	updates := map[string]string{"cooldown": "10m30s"}
	applyRuleCoreConfig(rule, updates)

	expected := 10*time.Minute + 30*time.Second
	if rule.Cooldown.Duration != expected {
		t.Errorf("Expected cooldown %v, got %v", expected, rule.Cooldown.Duration)
	}
}

func TestApplyRuleCoreConfig_InvalidCooldown(t *testing.T) {
	rule := &core.AlertRule{}
	updates := map[string]string{"cooldown": "invalid"}
	applyRuleCoreConfig(rule, updates)

	if rule.Cooldown.Duration != 0 {
		t.Errorf("Expected cooldown to remain 0 for invalid duration")
	}
}

func TestApplyRuleCoreConfig_UnknownKey(t *testing.T) {
	rule := &core.AlertRule{Severity: core.SeverityCritical}
	updates := map[string]string{"unknown_key": "value"}
	applyRuleCoreConfig(rule, updates)

	if rule.Severity != core.SeverityCritical {
		t.Errorf("Severity should not change for unknown key")
	}
}

// applyJourneyUpdates tests

func TestApplyJourneyUpdates_Name(t *testing.T) {
	journey := &core.JourneyConfig{Name: "old-name"}
	name := "new-name"
	req := &v1.UpdateJourneyRequest{Name: &name}
	applyJourneyUpdates(journey, req)

	if journey.Name != "new-name" {
		t.Errorf("Expected name to be 'new-name', got '%s'", journey.Name)
	}
}

func TestApplyJourneyUpdates_Description(t *testing.T) {
	journey := &core.JourneyConfig{}
	desc := "new description"
	req := &v1.UpdateJourneyRequest{Description: &desc}
	applyJourneyUpdates(journey, req)

	if journey.Description != "new description" {
		t.Errorf("Expected description to be set")
	}
}

func TestApplyJourneyUpdates_Interval(t *testing.T) {
	journey := &core.JourneyConfig{}
	interval := int32(60)
	req := &v1.UpdateJourneyRequest{Interval: &interval}
	applyJourneyUpdates(journey, req)

	expected := 60 * time.Second
	if journey.Weight.Duration != expected {
		t.Errorf("Expected interval %v, got %v", expected, journey.Weight.Duration)
	}
}

func TestApplyJourneyUpdates_Enabled(t *testing.T) {
	journey := &core.JourneyConfig{Enabled: false}
	enabled := true
	req := &v1.UpdateJourneyRequest{Enabled: &enabled}
	applyJourneyUpdates(journey, req)

	if !journey.Enabled {
		t.Errorf("Expected Enabled to be true")
	}
}

func TestApplyJourneyUpdates_UpdatedAt(t *testing.T) {
	before := time.Now().Add(-time.Hour)
	journey := &core.JourneyConfig{UpdatedAt: before}
	enabled := true
	req := &v1.UpdateJourneyRequest{Enabled: &enabled}
	applyJourneyUpdates(journey, req)

	if journey.UpdatedAt.Before(before) {
		t.Errorf("UpdatedAt should be updated")
	}
}

func TestApplyJourneyUpdates_AllFields(t *testing.T) {
	journey := &core.JourneyConfig{}
	name := "journey-name"
	desc := "journey-desc"
	interval := int32(120)
	enabled := true
	req := &v1.UpdateJourneyRequest{
		Name:        &name,
		Description: &desc,
		Interval:    &interval,
		Enabled:     &enabled,
	}
	applyJourneyUpdates(journey, req)

	if journey.Name != "journey-name" {
		t.Errorf("Expected name to be set")
	}
	if journey.Description != "journey-desc" {
		t.Errorf("Expected description to be set")
	}
	if journey.Weight.Duration != 120*time.Second {
		t.Errorf("Expected interval to be set")
	}
	if !journey.Enabled {
		t.Errorf("Expected enabled to be set")
	}
}

// applySoulUpdates tests

func TestApplySoulUpdates_Name(t *testing.T) {
	soul := &core.Soul{Name: "old-name"}
	name := "new-name"
	req := &v1.UpdateSoulRequest{Name: &name}
	applySoulUpdates(soul, req)

	if soul.Name != "new-name" {
		t.Errorf("Expected name to be 'new-name', got '%s'", soul.Name)
	}
}

func TestApplySoulUpdates_Target(t *testing.T) {
	soul := &core.Soul{Target: "old-target"}
	target := "http://new-target.com"
	req := &v1.UpdateSoulRequest{Target: &target}
	applySoulUpdates(soul, req)

	if soul.Target != "http://new-target.com" {
		t.Errorf("Expected target to be updated")
	}
}

func TestApplySoulUpdates_Interval(t *testing.T) {
	soul := &core.Soul{}
	interval := int32(30)
	req := &v1.UpdateSoulRequest{Interval: &interval}
	applySoulUpdates(soul, req)

	expected := 30 * time.Second
	if soul.Weight.Duration != expected {
		t.Errorf("Expected interval %v, got %v", expected, soul.Weight.Duration)
	}
}

func TestApplySoulUpdates_Timeout(t *testing.T) {
	soul := &core.Soul{}
	timeout := int32(10)
	req := &v1.UpdateSoulRequest{Timeout: &timeout}
	applySoulUpdates(soul, req)

	expected := 10 * time.Second
	if soul.Timeout.Duration != expected {
		t.Errorf("Expected timeout %v, got %v", expected, soul.Timeout.Duration)
	}
}

func TestApplySoulUpdates_Enabled(t *testing.T) {
	soul := &core.Soul{Enabled: false}
	enabled := true
	req := &v1.UpdateSoulRequest{Enabled: &enabled}
	applySoulUpdates(soul, req)

	if !soul.Enabled {
		t.Errorf("Expected Enabled to be true")
	}
}

func TestApplySoulUpdates_Tags(t *testing.T) {
	soul := &core.Soul{}
	tags := []string{"tag1", "tag2", "tag3"}
	req := &v1.UpdateSoulRequest{Tags: tags}
	applySoulUpdates(soul, req)

	if len(soul.Tags) != 3 {
		t.Errorf("Expected 3 tags, got %d", len(soul.Tags))
	}
	if soul.Tags[0] != "tag1" || soul.Tags[1] != "tag2" || soul.Tags[2] != "tag3" {
		t.Errorf("Unexpected Tags: %v", soul.Tags)
	}
}

func TestApplySoulUpdates_UpdatedAt(t *testing.T) {
	before := time.Now().Add(-time.Hour)
	soul := &core.Soul{UpdatedAt: before}
	name := "new-name"
	req := &v1.UpdateSoulRequest{Name: &name}
	applySoulUpdates(soul, req)

	if soul.UpdatedAt.Before(before) {
		t.Errorf("UpdatedAt should be updated")
	}
}

// applyChannelUpdates tests

func TestApplyChannelUpdates_Name(t *testing.T) {
	channel := &core.AlertChannel{Name: "old-name"}
	name := "new-name"
	req := &v1.UpdateChannelRequest{Name: &name}
	applyChannelUpdates(channel, req)

	if channel.Name != "new-name" {
		t.Errorf("Expected name to be 'new-name', got '%s'", channel.Name)
	}
}

func TestApplyChannelUpdates_Enabled(t *testing.T) {
	channel := &core.AlertChannel{Enabled: false}
	enabled := true
	req := &v1.UpdateChannelRequest{Enabled: &enabled}
	applyChannelUpdates(channel, req)

	if !channel.Enabled {
		t.Errorf("Expected Enabled to be true")
	}
}

func TestApplyChannelUpdates_Config(t *testing.T) {
	channel := &core.AlertChannel{}
	config := map[string]string{
		"webhook_url": "http://example.com",
		"secret":      "my-secret",
	}
	req := &v1.UpdateChannelRequest{Config: config}
	applyChannelUpdates(channel, req)

	if channel.Config == nil {
		t.Fatal("Expected Config to be initialized")
	}
	if channel.Config["webhook_url"] != "http://example.com" {
		t.Errorf("Expected webhook_url to be set")
	}
	if channel.Config["secret"] != "my-secret" {
		t.Errorf("Expected secret to be set")
	}
}

func TestApplyChannelUpdates_ConfigMerge(t *testing.T) {
	channel := &core.AlertChannel{
		Config: map[string]interface{}{"existing": "value"},
	}
	config := map[string]string{
		"webhook_url": "http://example.com",
	}
	req := &v1.UpdateChannelRequest{Config: config}
	applyChannelUpdates(channel, req)

	if channel.Config["existing"] != "value" {
		t.Errorf("Expected existing config to be preserved")
	}
	if channel.Config["webhook_url"] != "http://example.com" {
		t.Errorf("Expected new config to be added")
	}
}

func TestApplyChannelUpdates_UpdatedAt(t *testing.T) {
	before := time.Now().Add(-time.Hour)
	channel := &core.AlertChannel{UpdatedAt: before}
	enabled := true
	req := &v1.UpdateChannelRequest{Enabled: &enabled}
	applyChannelUpdates(channel, req)

	if channel.UpdatedAt.Before(before) {
		t.Errorf("UpdatedAt should be updated")
	}
}

// applyRuleUpdates tests

func TestApplyRuleUpdates_Name(t *testing.T) {
	rule := &core.AlertRule{Name: "old-name"}
	name := "new-name"
	req := &v1.UpdateRuleRequest{Name: &name}
	applyRuleUpdates(rule, req)

	if rule.Name != "new-name" {
		t.Errorf("Expected name to be 'new-name', got '%s'", rule.Name)
	}
}

func TestApplyRuleUpdates_Enabled(t *testing.T) {
	rule := &core.AlertRule{Enabled: false}
	enabled := true
	req := &v1.UpdateRuleRequest{Enabled: &enabled}
	applyRuleUpdates(rule, req)

	if !rule.Enabled {
		t.Errorf("Expected Enabled to be true")
	}
}

func TestApplyRuleUpdates_ConfigSeverity(t *testing.T) {
	rule := &core.AlertRule{Severity: core.SeverityWarning}
	config := map[string]string{"severity": "critical"}
	req := &v1.UpdateRuleRequest{Config: config}
	applyRuleUpdates(rule, req)

	if rule.Severity != core.SeverityCritical {
		t.Errorf("Expected severity to be critical, got %s", rule.Severity)
	}
}

func TestApplyRuleUpdates_ConfigCooldown(t *testing.T) {
	rule := &core.AlertRule{}
	config := map[string]string{"cooldown": "5m"}
	req := &v1.UpdateRuleRequest{Config: config}
	applyRuleUpdates(rule, req)

	expected := 5 * time.Minute
	if rule.Cooldown.Duration != expected {
		t.Errorf("Expected cooldown %v, got %v", expected, rule.Cooldown.Duration)
	}
}

func TestApplyRuleUpdates_ConfigChannelIDs(t *testing.T) {
	rule := &core.AlertRule{}
	config := map[string]string{"channel_ids": "ch1,ch2"}
	req := &v1.UpdateRuleRequest{Config: config}
	applyRuleUpdates(rule, req)

	if len(rule.Channels) != 2 {
		t.Errorf("Expected 2 channels, got %d", len(rule.Channels))
	}
}

func TestApplyRuleUpdates_AllFields(t *testing.T) {
	rule := &core.AlertRule{}
	name := "rule-name"
	enabled := true
	config := map[string]string{
		"severity":    "critical",
		"channel_ids": "ch1,ch2",
		"cooldown":    "10m",
	}
	req := &v1.UpdateRuleRequest{Name: &name, Enabled: &enabled, Config: config}
	applyRuleUpdates(rule, req)

	if rule.Name != "rule-name" {
		t.Errorf("Expected name to be set")
	}
	if !rule.Enabled {
		t.Errorf("Expected enabled to be set")
	}
	if rule.Severity != core.SeverityCritical {
		t.Errorf("Expected severity to be critical")
	}
	if len(rule.Channels) != 2 {
		t.Errorf("Expected channels to be set")
	}
	if rule.Cooldown.Duration != 10*time.Minute {
		t.Errorf("Expected cooldown to be set")
	}
}
