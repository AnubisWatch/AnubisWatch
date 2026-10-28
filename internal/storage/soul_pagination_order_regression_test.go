package storage

import (
	"context"
	"fmt"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestListSoulsPaginationUsesStableIDOrder(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	for i := 15; i >= 0; i-- {
		id := fmt.Sprintf("s-%02d", i)
		if err := db.SaveSoul(context.Background(), &core.Soul{ID: id, WorkspaceID: "default", Name: id}); err != nil {
			t.Fatalf("SaveSoul(%s): %v", id, err)
		}
	}

	assertPage := func(offset, limit int, want ...string) {
		t.Helper()
		for repeat := 0; repeat < 3; repeat++ {
			got, err := db.ListSouls(context.Background(), "default", offset, limit)
			if err != nil {
				t.Fatalf("ListSouls(%d,%d): %v", offset, limit, err)
			}
			if len(got) != len(want) {
				t.Fatalf("ListSouls(%d,%d) len = %d, want %d", offset, limit, len(got), len(want))
			}
			for i := range want {
				if got[i].ID != want[i] {
					t.Fatalf("ListSouls(%d,%d)[%d].ID = %q, want %q", offset, limit, i, got[i].ID, want[i])
				}
			}
		}
	}

	assertPage(0, 4, "s-00", "s-01", "s-02", "s-03")
	assertPage(4, 4, "s-04", "s-05", "s-06", "s-07")
	assertPage(15, 4, "s-15")
	assertPage(16, 4)
}
