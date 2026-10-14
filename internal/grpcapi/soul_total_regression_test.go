package grpcapi

// Regression coverage for Pagination.Total on the gRPC ListSouls RPC.
//
// Server.ListSouls' unfiltered branch used to fetch only offset+limit+1 souls
// and approximate the total as "offset + page size (+1 when hasMore)", so a
// workspace with 50 souls listed with Limit=10 reported Total=11 and a
// workspace with 12 reported Total=11. Pagination.Total must be the true count
// of matching souls, as ListSouls' own type/tag-filtered branch and the
// sibling list RPCs (ListJudgments, ListVerdicts, ListJourneys, ListChannels,
// ListRules) already do.

import (
	"fmt"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	v1 "github.com/AnubisWatch/anubiswatch/internal/grpcapi/v1"
)

// offsetFaithfulSoulStore honours offset/limit exactly as the real storage
// layer does (internal/storage/engine.go ListSouls: end = offset+limit, and
// limit <= 0 means unbounded). mockGRPCStore.ListSoulsNoCtx ignores both
// arguments, so it cannot exercise production pagination behaviour.
type offsetFaithfulSoulStore struct {
	*mockGRPCStore
}

func (s *offsetFaithfulSoulStore) ListSoulsNoCtx(ws string, offset, limit int) ([]*core.Soul, error) {
	keys := sortedMockKeys(s.souls)
	all := make([]*core.Soul, 0, len(keys))
	for _, k := range keys {
		all = append(all, s.souls[k])
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(all) {
		return []*core.Soul{}, nil
	}
	end := offset + limit
	if limit <= 0 || end > len(all) {
		end = len(all)
	}
	return all[offset:end], nil
}

func soulTotalServer(count int) *Server {
	base := newMockGRPCStore()
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("soul_%03d", i)
		base.souls[id] = &core.Soul{
			ID: id, Name: id, Type: core.CheckHTTP,
			Target: "http://example.com", WorkspaceID: "default", Enabled: true,
		}
	}
	store := &offsetFaithfulSoulStore{mockGRPCStore: base}
	return NewServer(":0", store, &mockGRPCProbe{}, &mockAuthenticator{}, nil, nil, true)
}

// TestListSoulsTotalFirstPage pins that the first page of a large workspace
// reports how many souls exist, not offset+limit+1.
func TestListSoulsTotalFirstPage(t *testing.T) {
	const total = 50
	resp, err := soulTotalServer(total).ListSouls(testUserContext(), &v1.ListSoulsRequest{
		Offset: 0,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("ListSouls failed: %v", err)
	}
	if len(resp.Souls) != 10 {
		t.Fatalf("expected a 10-item page, got %d", len(resp.Souls))
	}
	if got := int(resp.Pagination.Total); got != total {
		t.Fatalf("Pagination.Total = %d, want the true count %d", got, total)
	}
	if !resp.Pagination.HasMore {
		t.Fatal("HasMore must be true when more souls exist beyond the page")
	}
}

// TestListSoulsTotalMiddlePage pins that a page that is not the last still
// reports the true total.
func TestListSoulsTotalMiddlePage(t *testing.T) {
	const total = 50
	resp, err := soulTotalServer(total).ListSouls(testUserContext(), &v1.ListSoulsRequest{
		Offset: 20,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("ListSouls failed: %v", err)
	}
	if len(resp.Souls) != 10 {
		t.Fatalf("expected a 10-item page, got %d", len(resp.Souls))
	}
	if got := int(resp.Pagination.Total); got != total {
		t.Fatalf("Pagination.Total = %d at offset 20, want the true count %d", got, total)
	}
}

// TestListSoulsTotalExceedsSinglePage is the regression's core: barely more
// souls than one page must not report Total as the fetch size.
func TestListSoulsTotalExceedsSinglePage(t *testing.T) {
	const total = 12
	resp, err := soulTotalServer(total).ListSouls(testUserContext(), &v1.ListSoulsRequest{
		Offset: 0,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("ListSouls failed: %v", err)
	}
	if got := int(resp.Pagination.Total); got != total {
		t.Fatalf("Pagination.Total = %d for %d souls; the total must be the true "+
			"count, not the number of rows fetched", got, total)
	}
}

// TestListSoulsTotalOnFinalPage covers the boundary where HasMore must be false.
// It also passed before the fix, because offset+len(page) happens to equal the
// true total on the final page.
func TestListSoulsTotalOnFinalPage(t *testing.T) {
	const total = 50
	resp, err := soulTotalServer(total).ListSouls(testUserContext(), &v1.ListSoulsRequest{
		Offset: 45,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("ListSouls failed: %v", err)
	}
	if len(resp.Souls) != 5 {
		t.Fatalf("expected 5 souls on the final page, got %d", len(resp.Souls))
	}
	if got := int(resp.Pagination.Total); got != total {
		t.Fatalf("Pagination.Total = %d on the final page, want %d", got, total)
	}
	if resp.Pagination.HasMore {
		t.Fatal("HasMore must be false on the final page of the result set")
	}
}

// TestListSoulsTotalWithTypeFilter is the control for the sibling branch: the
// type-filtered path fetched fully and already reported the true total.
func TestListSoulsTotalWithTypeFilter(t *testing.T) {
	const total = 50
	checkType := "http"
	resp, err := soulTotalServer(total).ListSouls(testUserContext(), &v1.ListSoulsRequest{
		Type:   &checkType,
		Offset: 0,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("ListSouls failed: %v", err)
	}
	if got := int(resp.Pagination.Total); got != total {
		t.Fatalf("type-filtered path reported %d, want %d", got, total)
	}
}

// TestListSoulsTotalFitsSinglePage is a second control: when everything fits in
// one page the old arithmetic was already correct, and must stay correct.
func TestListSoulsTotalFitsSinglePage(t *testing.T) {
	const total = 5
	resp, err := soulTotalServer(total).ListSouls(testUserContext(), &v1.ListSoulsRequest{
		Offset: 0,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("ListSouls failed: %v", err)
	}
	if len(resp.Souls) != total {
		t.Fatalf("expected %d souls, got %d", total, len(resp.Souls))
	}
	if got := int(resp.Pagination.Total); got != total {
		t.Fatalf("single-page path reported %d, want %d", got, total)
	}
	if resp.Pagination.HasMore {
		t.Fatal("HasMore must be false when everything fits in one page")
	}
}
