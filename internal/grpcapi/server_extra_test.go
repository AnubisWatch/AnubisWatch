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

func (m *failingMockGRPCStore) GetJourneyRunNoCtx(journeyID, runID string) (interface{}, error) {
	if m.getJourneyRunErr {
		return nil, fmt.Errorf("db error")
	}
	return m.mockGRPCStore.GetJourneyRunNoCtx(journeyID, runID)
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
	srv := NewServer("invalid://:abc", newMockGRPCStore(), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil)
	if err := srv.Start(); err == nil {
		t.Error("Expected error for invalid listen address")
	}
}

func TestServer_StreamJudgments_StoreError(t *testing.T) {
	store := &failingMockGRPCStore{mockGRPCStore: newMockGRPCStore(), listJudgmentsErr: true}
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
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
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 1100*time.Millisecond)
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
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
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
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 1100*time.Millisecond)
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
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil)

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
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil)

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
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil)

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
	srv := NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil)

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
