package grpcapi

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	v1 "github.com/AnubisWatch/anubiswatch/internal/grpcapi/v1"
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

func (m *failingMockGRPCStore) ListJudgmentsNoCtx(soulID string, start, end time.Time, limit int) ([]interface{}, error) {
	if m.listJudgmentsErr {
		return nil, fmt.Errorf("db error")
	}
	return m.mockGRPCStore.ListJudgmentsNoCtx(soulID, start, end, limit)
}

func (m *failingMockGRPCStore) ListEvents(soulID string, limit int) ([]interface{}, error) {
	if m.listEventsErr {
		return nil, fmt.Errorf("db error")
	}
	return m.mockGRPCStore.ListEvents(soulID, limit)
}

func (m *failingMockGRPCStore) GetJourneyRunNoCtx(workspace, journeyID, runID string) (interface{}, error) {
	if m.getJourneyRunErr {
		return nil, fmt.Errorf("db error")
	}
	return m.mockGRPCStore.GetJourneyRunNoCtx(workspace, journeyID, runID)
}

func (m *failingMockGRPCStore) ListSoulsNoCtx(ws string, o, l int) ([]interface{}, error) {
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
	srv := NewServer("invalid://:abc", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
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
	store.judgments = []interface{}{
		&mockJudgment{id: "j1", soulID: "s1", status: "alive", duration: 10 * time.Millisecond, message: "ok", timestamp: time.Now()},
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
	store.events = []interface{}{
		&mockAlertEvent{id: "evt_1", soulID: "s1", status: "firing", severity: "critical", message: "alert", timestamp: time.Now()},
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

func TestServer_ListSouls_PaginationHasMore(t *testing.T) {
	store := newMockGRPCStore()
	for i := 0; i < 5; i++ {
		_ = store.SaveSoulNoCtx(map[string]interface{}{"name": fmt.Sprintf("soul-%d", i)})
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
		store.channels[id] = &mockChannel{id: id, name: id, chType: "webhook"}
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
		store.rules[id] = &mockRule{id: id, name: id}
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
		store.journeys[id] = &mockJourney{id: id, name: id}
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
	store.souls["s1"] = &mockSoul{id: "s1", name: "test"}
	now := time.Now()
	store.judgments = []interface{}{
		&mockJudgment{id: "j1", soulID: "s1", status: "alive", duration: time.Millisecond, timestamp: now},
		&mockJudgment{id: "j2", soulID: "s1", status: "dead", duration: time.Millisecond, timestamp: now},
		&mockJudgment{id: "j3", soulID: "s1", status: "alive", duration: time.Millisecond, timestamp: now},
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
	store.events = []interface{}{
		&mockAlertEvent{id: "evt_1", soulID: "s1", status: "firing", severity: "critical", timestamp: now},
		&mockAlertEvent{id: "evt_2", soulID: "s1", status: "resolved", severity: "critical", timestamp: now},
		&mockAlertEvent{id: "evt_3", soulID: "s1", status: "firing", severity: "warning", timestamp: now},
		&mockAlertEvent{id: "evt_4", soulID: "s1", status: "firing", severity: "critical", timestamp: now},
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
