package storage

import (
	"fmt"
	"sync"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// TestCheckpointConcurrentWithWrites runs writers and readers concurrently with
// repeated checkpoints and verifies every committed write survives. Run under
// -race, it also exercises the Put/Delete/Checkpoint lock discipline.
func TestCheckpointConcurrentWithWrites(t *testing.T) {
	db := newDBOrder(t, 16)
	defer db.Close()

	const writers = 8
	const perWriter = 300

	var wg sync.WaitGroup
	wg.Add(writers)
	for w := 0; w < writers; w++ {
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				k := fmt.Sprintf("w%02d/k-%05d", w, i)
				if err := db.Put(k, []byte(fmt.Sprintf("%d-%d", w, i))); err != nil {
					t.Errorf("put: %v", err)
					return
				}
			}
		}(w)
	}

	// Concurrent checkpoints and reads while writers run.
	stop := make(chan struct{})
	var bg sync.WaitGroup
	bg.Add(2)
	go func() {
		defer bg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = db.Checkpoint()
			}
		}
	}()
	go func() {
		defer bg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = db.Get("w00/k-00000")
			}
		}
	}()

	wg.Wait()
	close(stop)
	bg.Wait()

	// Every committed write must be present.
	for w := 0; w < writers; w++ {
		for i := 0; i < perWriter; i++ {
			k := fmt.Sprintf("w%02d/k-%05d", w, i)
			got, err := db.Get(k)
			if err != nil {
				t.Fatalf("lost key %s: %v", k, err)
			}
			if want := fmt.Sprintf("%d-%d", w, i); string(got) != want {
				t.Fatalf("key %s = %q, want %q", k, got, want)
			}
		}
	}

	// A final checkpoint + reopen must preserve everything.
	if err := db.Checkpoint(); err != nil {
		t.Fatalf("final checkpoint: %v", err)
	}
	path := db.path
	db.Close()
	db2, err := NewEngine(core.StorageConfig{Path: path, BTreeOrder: 16}, newTestLogger())
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Close()
	for w := 0; w < writers; w++ {
		for i := 0; i < perWriter; i++ {
			k := fmt.Sprintf("w%02d/k-%05d", w, i)
			if _, err := db2.Get(k); err != nil {
				t.Fatalf("key %s lost after reopen: %v", k, err)
			}
		}
	}
}
