package storage

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func newDBOrder(t *testing.T, order int) *CobaltDB {
	dir := t.TempDir()
	cfg := core.StorageConfig{Path: dir, BTreeOrder: order}
	db, err := NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("failed to create test DB: %v", err)
	}
	return db
}

// TestBTreeSplitNoDataLoss is a regression test for a B+Tree leaf-split bug
// that silently dropped the separator key on every leaf split, losing data
// once a workspace grew past a single leaf (e.g. 61/1000 keys at order 32).
func TestBTreeSplitNoDataLoss(t *testing.T) {
	for _, order := range []int{4, 5, 8, 32, 64, 128} {
		t.Run(fmt.Sprintf("order-%d", order), func(t *testing.T) {
			db := newDBOrder(t, order)
			defer db.Close()

			const n = 2000
			// Insert in shuffled order to exercise many split shapes.
			idxs := rand.New(rand.NewSource(int64(order))).Perm(n)
			for _, i := range idxs {
				key := fmt.Sprintf("key-%06d", i)
				if err := db.Put(key, []byte(fmt.Sprintf("val-%d", i))); err != nil {
					t.Fatalf("put: %v", err)
				}
			}

			lost, wrong := 0, 0
			for i := 0; i < n; i++ {
				key := fmt.Sprintf("key-%06d", i)
				want := fmt.Sprintf("val-%d", i)
				got, err := db.Get(key)
				if err != nil || got == nil {
					lost++
					continue
				}
				if string(got) != want {
					wrong++
				}
			}
			if lost > 0 || wrong > 0 {
				t.Fatalf("DATA LOSS order=%d: lost=%d wrong=%d of %d", order, lost, wrong, n)
			}

			// Updates to existing keys must not create duplicates or lose data.
			for i := 0; i < n; i += 3 {
				key := fmt.Sprintf("key-%06d", i)
				if err := db.Put(key, []byte(fmt.Sprintf("upd-%d", i))); err != nil {
					t.Fatalf("update put: %v", err)
				}
			}
			for i := 0; i < n; i++ {
				key := fmt.Sprintf("key-%06d", i)
				want := fmt.Sprintf("val-%d", i)
				if i%3 == 0 {
					want = fmt.Sprintf("upd-%d", i)
				}
				got, err := db.Get(key)
				if err != nil || string(got) != want {
					t.Fatalf("after update order=%d key=%s want=%s got=%q err=%v", order, key, want, got, err)
				}
			}

			// PrefixScan must return every key exactly once, sorted.
			res, err := db.PrefixScan("key-")
			if err != nil {
				t.Fatalf("prefixscan: %v", err)
			}
			if len(res) != n {
				t.Fatalf("prefixscan order=%d returned %d keys, want %d", order, len(res), n)
			}
			keys := make([]string, 0, len(res))
			for k := range res {
				keys = append(keys, k)
			}
			if !sort.StringsAreSorted(keys) {
				sort.Strings(keys)
			}
			for i := 0; i < n; i++ {
				if _, ok := res[fmt.Sprintf("key-%06d", i)]; !ok {
					t.Fatalf("prefixscan order=%d missing key-%06d", order, i)
				}
			}
		})
	}
}
