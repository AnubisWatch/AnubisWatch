package grpcapi

// Regression coverage for Pagination.Total on the gRPC ListJudgments RPC.
//
// Server.ListJudgments used to fetch only offset+limit+1 rows whenever the
// caller filtered by soul_id and supplied no status filter, then handed that
// truncated slice to paginateJudgments, which derives Total from len(items).
// A soul with 10 judgments listed with Limit=3 therefore reported Total=4.
//
// Every sibling list RPC in this file (ListJourneys, ListChannels, ListRules)
// fetches the full matching set and paginates in memory, and
// TestListJourneys_Pagination pins that contract explicitly (Total==5 with
// Limit==3). These tests keep ListJudgments consistent with it, and cover the
// page/has_more arithmetic that depends on the true count.

import (
	"fmt"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	v1 "github.com/AnubisWatch/anubiswatch/internal/grpcapi/v1"
)

func judgmentTotalStore(t *testing.T, soulID string, count int, status core.SoulStatus) *mockGRPCStore {
	t.Helper()
	store := newMockGRPCStore()
	store.souls[soulID] = &core.Soul{
		ID: soulID, Name: "API", Type: core.CheckHTTP,
		Target: "http://example.com", WorkspaceID: "default", Enabled: true,
	}
	now := time.Now().UTC()
	for i := 0; i < count; i++ {
		store.judgments = append(store.judgments, &core.Judgment{
			ID:        fmt.Sprintf("%s-judgment-%d", soulID, i),
			SoulID:    soulID,
			Status:    status,
			Timestamp: now.Add(-time.Duration(i) * time.Minute),
		})
	}
	return store
}

func judgmentTotalServer(store *mockGRPCStore) *Server {
	return NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
}

func ptrString(s string) *string { return &s }

// TestListJudgmentsTotalReportsTrueCountWithSoulFilter pins that filtering by
// soul_id still reports how many judgments that soul actually has, rather than
// the number of rows the server happened to fetch.
func TestListJudgmentsTotalReportsTrueCountWithSoulFilter(t *testing.T) {
	const total = 10
	srv := judgmentTotalServer(judgmentTotalStore(t, "soul-1", total, core.SoulAlive))

	resp, err := srv.ListJudgments(testUserContext(), &v1.ListJudgmentsRequest{
		SoulId: ptrString("soul-1"),
		Offset: 0,
		Limit:  3,
	})
	if err != nil {
		t.Fatalf("ListJudgments failed: %v", err)
	}
	if len(resp.Judgments) != 3 {
		t.Fatalf("expected a 3-item page, got %d", len(resp.Judgments))
	}
	if got := int(resp.Pagination.Total); got != total {
		t.Fatalf("Pagination.Total = %d, want the true count %d", got, total)
	}
	if !resp.Pagination.HasMore {
		t.Fatal("HasMore must be true when more judgments exist beyond the page")
	}
}

// TestListJudgmentsTotalOnLaterPage covers the boundary where the request is
// already past the first page, where the offset is added to the reported total.
// Offset 7 with limit 3 is the final page of 10, so HasMore must be false.
func TestListJudgmentsTotalOnLaterPage(t *testing.T) {
	const total = 10
	srv := judgmentTotalServer(judgmentTotalStore(t, "soul-1", total, core.SoulAlive))

	resp, err := srv.ListJudgments(testUserContext(), &v1.ListJudgmentsRequest{
		SoulId: ptrString("soul-1"),
		Offset: 7,
		Limit:  3,
	})
	if err != nil {
		t.Fatalf("ListJudgments failed: %v", err)
	}
	if len(resp.Judgments) != 3 {
		t.Fatalf("expected a 3-item page at offset 7, got %d", len(resp.Judgments))
	}
	if got := int(resp.Pagination.Total); got != total {
		t.Fatalf("Pagination.Total = %d on the final page, want %d", got, total)
	}
	if resp.Pagination.HasMore {
		t.Fatal("HasMore must be false on the final page of the result set")
	}
}

// TestListJudgmentsTotalExceedsSinglePage is the regression's core: a soul with
// far more judgments than one page must not report Total as the fetch size.
func TestListJudgmentsTotalExceedsSinglePage(t *testing.T) {
	const total = 50
	srv := judgmentTotalServer(judgmentTotalStore(t, "soul-1", total, core.SoulAlive))

	resp, err := srv.ListJudgments(testUserContext(), &v1.ListJudgmentsRequest{
		SoulId: ptrString("soul-1"),
		Offset: 0,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("ListJudgments failed: %v", err)
	}
	if got := int(resp.Pagination.Total); got != total {
		t.Fatalf("Pagination.Total = %d for %d judgments; the total must be the true count, "+
			"not the number of rows fetched", got, total)
	}
}

// TestListJudgmentsTotalWithoutSoulFilter is the control: the workspace-wide
// path already reported the true total and must keep doing so.
func TestListJudgmentsTotalWithoutSoulFilter(t *testing.T) {
	const total = 10
	srv := judgmentTotalServer(judgmentTotalStore(t, "soul-1", total, core.SoulAlive))

	resp, err := srv.ListJudgments(testUserContext(), &v1.ListJudgmentsRequest{
		Offset: 0,
		Limit:  3,
	})
	if err != nil {
		t.Fatalf("ListJudgments failed: %v", err)
	}
	if got := int(resp.Pagination.Total); got != total {
		t.Fatalf("workspace-wide path reported %d, want %d", got, total)
	}
}

// TestListJudgmentsTotalWithStatusFilter is a second control: the status-filtered
// path already fetched fully and must keep reporting the true total.
func TestListJudgmentsTotalWithStatusFilter(t *testing.T) {
	const total = 10
	srv := judgmentTotalServer(judgmentTotalStore(t, "soul-1", total, core.SoulAlive))

	resp, err := srv.ListJudgments(testUserContext(), &v1.ListJudgmentsRequest{
		SoulId: ptrString("soul-1"),
		Status: ptrString("alive"),
		Offset: 0,
		Limit:  3,
	})
	if err != nil {
		t.Fatalf("ListJudgments failed: %v", err)
	}
	if got := int(resp.Pagination.Total); got != total {
		t.Fatalf("status-filtered path reported %d, want %d", got, total)
	}
}
