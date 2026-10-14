package grpcapi

// Regression coverage for Pagination.Total on the gRPC ListVerdicts RPC.
//
// Server.ListVerdicts used to fetch only offset+limit+1 events whenever the
// caller supplied neither a status nor a severity filter, then handed that
// truncated slice to its inline pagination, which derives Total from
// len(filtered). Unlike ListJudgments, this path had no `soulID == ""` escape,
// so *every* unfiltered request reported the fetch size as the total: a soul
// with 50 verdicts listed with Limit=10 reported Total=11.
//
// Sibling list RPCs (ListJourneys, ListChannels, ListRules) and ListJudgments
// all fetch the full set and report the true total.

import (
	"fmt"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	v1 "github.com/AnubisWatch/anubiswatch/internal/grpcapi/v1"
)

func verdictTotalFixture(count int) *mockGRPCStore {
	store := newMockGRPCStore()
	store.souls["s1"] = &core.Soul{
		ID: "s1", Name: "API", Type: core.CheckHTTP,
		Target: "http://example.com", WorkspaceID: "default", Enabled: true,
	}
	now := time.Now().UTC()
	for i := 0; i < count; i++ {
		store.events = append(store.events, &core.AlertEvent{
			ID:          fmt.Sprintf("evt_%d", i),
			SoulID:      "s1",
			WorkspaceID: "default",
			Status:      "firing",
			Severity:    core.SeverityWarning,
			Timestamp:   now.Add(-time.Duration(i) * time.Minute),
		})
	}
	return store
}

func verdictTotalServer(count int) *Server {
	return NewServer(":0", verdictTotalFixture(count), &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
}

func verdictPtr(s string) *string { return &s }

// TestListVerdictsTotalWithSoulFilter pins that filtering by soul_id reports how
// many verdicts that soul actually has, not the number of rows fetched.
func TestListVerdictsTotalWithSoulFilter(t *testing.T) {
	const total = 10
	resp, err := verdictTotalServer(total).ListVerdicts(testUserContext(), &v1.ListVerdictsRequest{
		SoulId: verdictPtr("s1"),
		Offset: 0,
		Limit:  3,
	})
	if err != nil {
		t.Fatalf("ListVerdicts failed: %v", err)
	}
	if len(resp.Verdicts) != 3 {
		t.Fatalf("expected a 3-item page, got %d", len(resp.Verdicts))
	}
	if got := int(resp.Pagination.Total); got != total {
		t.Fatalf("Pagination.Total = %d, want the true count %d", got, total)
	}
	if !resp.Pagination.HasMore {
		t.Fatal("HasMore must be true when more verdicts exist beyond the page")
	}
}

// TestListVerdictsTotalWithoutFilters pins the workspace-wide request, which had
// no soul_id escape hatch and therefore truncated on every unfiltered call.
func TestListVerdictsTotalWithoutFilters(t *testing.T) {
	const total = 10
	resp, err := verdictTotalServer(total).ListVerdicts(testUserContext(), &v1.ListVerdictsRequest{
		Offset: 0,
		Limit:  3,
	})
	if err != nil {
		t.Fatalf("ListVerdicts failed: %v", err)
	}
	if got := int(resp.Pagination.Total); got != total {
		t.Fatalf("Pagination.Total = %d on an unfiltered workspace-wide request, want %d", got, total)
	}
}

// TestListVerdictsTotalExceedsSinglePage is the regression's core: a soul with far
// more verdicts than one page must not report Total as the fetch size.
func TestListVerdictsTotalExceedsSinglePage(t *testing.T) {
	const total = 50
	resp, err := verdictTotalServer(total).ListVerdicts(testUserContext(), &v1.ListVerdictsRequest{
		SoulId: verdictPtr("s1"),
		Offset: 0,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("ListVerdicts failed: %v", err)
	}
	if got := int(resp.Pagination.Total); got != total {
		t.Fatalf("Pagination.Total = %d for %d verdicts; the total must be the true "+
			"count, not the number of rows fetched", got, total)
	}
}

// TestListVerdictsTotalOnFinalPage covers the boundary where HasMore must be
// false: offset 9 with limit 3 is the final page of 10.
func TestListVerdictsTotalOnFinalPage(t *testing.T) {
	const total = 10
	resp, err := verdictTotalServer(total).ListVerdicts(testUserContext(), &v1.ListVerdictsRequest{
		SoulId: verdictPtr("s1"),
		Offset: 9,
		Limit:  3,
	})
	if err != nil {
		t.Fatalf("ListVerdicts failed: %v", err)
	}
	if len(resp.Verdicts) != 1 {
		t.Fatalf("expected 1 verdict on the final page, got %d", len(resp.Verdicts))
	}
	if got := int(resp.Pagination.Total); got != total {
		t.Fatalf("Pagination.Total = %d on the final page, want %d", got, total)
	}
	if resp.Pagination.HasMore {
		t.Fatal("HasMore must be false on the final page of the result set")
	}
}

// TestListVerdictsTotalWithStatusFilter is the control: the status-filtered path
// already fetched fully and must keep reporting the true total.
func TestListVerdictsTotalWithStatusFilter(t *testing.T) {
	const total = 10
	resp, err := verdictTotalServer(total).ListVerdicts(testUserContext(), &v1.ListVerdictsRequest{
		SoulId: verdictPtr("s1"),
		Status: verdictPtr("firing"),
		Offset: 0,
		Limit:  3,
	})
	if err != nil {
		t.Fatalf("ListVerdicts failed: %v", err)
	}
	if got := int(resp.Pagination.Total); got != total {
		t.Fatalf("status-filtered path reported %d, want %d", got, total)
	}
}

// TestListVerdictsTotalWithSeverityFilter is a second control on the other
// branch that already disabled the bounded fetch.
func TestListVerdictsTotalWithSeverityFilter(t *testing.T) {
	const total = 10
	resp, err := verdictTotalServer(total).ListVerdicts(testUserContext(), &v1.ListVerdictsRequest{
		SoulId:   verdictPtr("s1"),
		Severity: verdictPtr("warning"),
		Offset:   0,
		Limit:    3,
	})
	if err != nil {
		t.Fatalf("ListVerdicts failed: %v", err)
	}
	if got := int(resp.Pagination.Total); got != total {
		t.Fatalf("severity-filtered path reported %d, want %d", got, total)
	}
}
