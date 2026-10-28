package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/raft"
)

func readStoredSnapshot(t *testing.T, store *CobaltDBSnapshotStore, id string) []byte {
	t.Helper()
	r, e := store.Open(id)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	b, e := io.ReadAll(r)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func writeStoredSnapshot(t *testing.T, store *CobaltDBSnapshotStore, index uint64, data []byte) string {
	t.Helper()
	s, e := store.Create(1, index, 1, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Write(data); e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	return s.ID()
}

func TestSnapshotChunksRoundTripAcrossWALLimitAndRestart(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		t.Run(fmt.Sprint(encrypted), func(t *testing.T) {
			cfg := core.StorageConfig{Path: t.TempDir(), Encryption: core.EncryptionConfig{Enabled: encrypted, Key: "snapshot-encryption-test-key"}}
			db, e := NewEngine(cfg, newTestLogger())
			if e != nil {
				t.Fatal(e)
			}
			for i, size := range []int{0, snapshotChunkSize, snapshotChunkSize + 1, 2 * 1024 * 1024} {
				store := NewCobaltDBSnapshotStore(db)
				want := bytes.Repeat([]byte{byte(i + 1)}, size)
				id := writeStoredSnapshot(t, store, uint64(i+1), want)
				if !bytes.Equal(readStoredSnapshot(t, store, id), want) {
					t.Fatalf("size %d mismatch", size)
				}
				chunks, e := db.PrefixScan("raft/snapshot-data/")
				if e != nil {
					t.Fatal(e)
				}
				if len(chunks) != (size+snapshotChunkSize-1)/snapshotChunkSize {
					t.Fatalf("obsolete chunks leaked: %d", len(chunks))
				}
				if e := db.Close(); e != nil {
					t.Fatal(e)
				}
				db, e = NewEngine(cfg, newTestLogger())
				if e != nil {
					t.Fatal(e)
				}
				if !bytes.Equal(readStoredSnapshot(t, NewCobaltDBSnapshotStore(db), id), want) {
					t.Fatalf("restart size %d mismatch", size)
				}
			}
			db.Close()
		})
	}
}

func TestSnapshotChunksReadLegacyAndRejectSupersededID(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	store := NewCobaltDBSnapshotStore(db)
	payload := []byte("legacy image")
	if e := db.Put("raft/snapshot", payload); e != nil {
		t.Fatal(e)
	}
	metadata, _ := json.Marshal([]raft.SnapshotMeta{{ID: "legacy", Index: 1, Term: 1, Size: int64(len(payload)), Version: 1}})
	if e := db.Put("raft/snapshot-meta", metadata); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(readStoredSnapshot(t, store, "legacy"), payload) {
		t.Fatal("legacy data changed")
	}
	// A crashed writer may have left chunks without publishing their manifest.
	if e := db.Put("raft/snapshot-data/abandoned/0", []byte("orphan")); e != nil {
		t.Fatal(e)
	}
	id := writeStoredSnapshot(t, store, 2, []byte("new image"))
	if _, e := store.Open("legacy"); e == nil {
		t.Fatal("superseded ID accepted")
	}
	if !bytes.Equal(readStoredSnapshot(t, store, id), []byte("new image")) {
		t.Fatal("new image missing")
	}
	if _, e := db.Get("raft/snapshot"); e == nil {
		t.Fatal("obsolete legacy data left behind")
	}
	if _, e := db.Get("raft/snapshot-data/abandoned/0"); e == nil {
		t.Fatal("abandoned snapshot chunks left behind")
	}
}

func TestSnapshotChunksFailedPublicationKeepsPreviousImage(t *testing.T) {
	for _, stage := range []string{"chunk", "manifest"} {
		t.Run(stage, func(t *testing.T) {
			db := newTestDB(t)
			defer db.Close()
			store := NewCobaltDBSnapshotStore(db)
			old := []byte("committed image")
			oldID := writeStoredSnapshot(t, store, 1, old)
			original := walWriteSeam
			walWriteSeam = func(f *os.File, b []byte) (int, error) {
				var entry walEntry
				if len(b) > 4 && json.Unmarshal(b[4:], &entry) == nil && entry.Op == "PUT" {
					fail := stage == "manifest" && entry.Key == "raft/snapshot-meta" || stage == "chunk" && strings.HasPrefix(entry.Key, "raft/snapshot-data/") && strings.HasSuffix(entry.Key, "/1")
					if fail {
						return 0, errors.New("injected publication failure")
					}
				}
				return original(f, b)
			}
			t.Cleanup(func() { walWriteSeam = original })
			sink, e := store.Create(1, 2, 1, nil)
			if e != nil {
				t.Fatal(e)
			}
			sink.Write(bytes.Repeat([]byte("x"), snapshotChunkSize+1))
			if e := sink.Close(); e == nil {
				t.Fatal("snapshot write failure hidden")
			}
			if e := sink.Close(); e == nil {
				t.Fatal("repeated Close hid original failure")
			}
			walWriteSeam = original
			if !bytes.Equal(readStoredSnapshot(t, store, oldID), old) {
				t.Fatal("failed publication changed previous image")
			}
			chunks, e := db.PrefixScan("raft/snapshot-data/")
			if e != nil || len(chunks) != 1 {
				t.Fatalf("failed writer left chunks: %d %v", len(chunks), e)
			}
		})
	}
}

func TestSnapshotImageDoesNotRecursivelyIncludeSnapshotChunks(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	store := NewCobaltDBSnapshotStore(db)
	fsm := raft.NewStorageFSM(db)
	if e := db.Put("default/souls/k", []byte("application")); e != nil {
		t.Fatal(e)
	}
	size := 0
	for i := 1; i <= 3; i++ {
		image, e := fsm.Snapshot()
		if e != nil {
			t.Fatal(e)
		}
		if i > 1 && len(image.Value) != size {
			t.Fatalf("snapshot grew with internal records: %d -> %d", size, len(image.Value))
		}
		size = len(image.Value)
		writeStoredSnapshot(t, store, uint64(i), image.Value)
	}
}
