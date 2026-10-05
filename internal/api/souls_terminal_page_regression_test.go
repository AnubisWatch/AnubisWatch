package api

import (
	"encoding/json"
	"fmt"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"net/http/httptest"
	"testing"
)

func TestSoulsTerminalPageRegression(t *testing.T) {
	for _, tc := range []struct {
		total, offset, limit int
		more                 bool
	}{{0, 0, 20, false}, {19, 0, 20, false}, {20, 0, 20, false}, {21, 0, 20, true}, {40, 20, 20, false}, {41, 20, 20, true}, {100, 0, 100, false}, {101, 0, 100, true}} {
		t.Run(fmt.Sprintf("%d_%d_%d", tc.total, tc.offset, tc.limit), func(t *testing.T) {
			store := newMockStorage()
			for i := 0; i < tc.total; i++ {
				id := fmt.Sprint(i)
				store.souls[id] = &core.Soul{ID: id, Name: id, WorkspaceID: "default"}
			}
			server := &RESTServer{store: store, logger: newTestLogger()}
			w := httptest.NewRecorder()
			ctx := &Context{Request: httptest.NewRequest("GET", fmt.Sprintf("/souls?limit=%d&offset=%d", tc.limit, tc.offset), nil), Response: w, Workspace: "default"}
			if err := server.handleListSouls(ctx); err != nil {
				t.Fatal(err)
			}
			var out struct {
				Data       []json.RawMessage `json:"data"`
				Pagination Pagination        `json:"pagination"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			wantCount := min(max(tc.total-tc.offset, 0), tc.limit)
			if len(out.Data) != wantCount || out.Pagination.HasMore != tc.more || out.Pagination.Limit != tc.limit || out.Pagination.Offset != tc.offset {
				t.Fatalf("wrong page %+v count=%d", out.Pagination, len(out.Data))
			}
			if tc.more {
				if out.Pagination.NextOffset == nil || *out.Pagination.NextOffset != tc.offset+tc.limit {
					t.Fatal("missing successor")
				}
			} else if out.Pagination.NextOffset != nil {
				t.Fatal("terminal page has successor")
			}
		})
	}
}
