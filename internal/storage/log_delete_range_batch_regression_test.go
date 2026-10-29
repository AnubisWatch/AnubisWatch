package storage

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Raft log compaction deleted every entry with its own fsync, holding the
// Raft mutex for close to a minute on an ordinary disk. DeleteRange now
// writes batched tombstone frames that replay like DELETE entries.
func TestLogStoreDeleteRangeBatchesAndSurvivesReopen(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		cfg := core.StorageConfig{Path: t.TempDir()}
		if encrypted {
			cfg.Encryption = core.EncryptionConfig{Enabled: true, Key: strings.Repeat("k", 32)}
		}
		db, err := NewEngine(cfg, newTestLogger())
		if err != nil {
			t.Fatal(err)
		}
		logs := NewCobaltDBLogStore(db)
		var entries []core.RaftLogEntry
		for i := uint64(1); i <= 5000; i++ {
			entries = append(entries, core.RaftLogEntry{Index: i, Term: 1, Data: []byte("x")})
		}
		if err := logs.StoreLogs(entries); err != nil {
			t.Fatal(err)
		}

		var syncs atomic.Int32
		old := walSyncSeam
		walSyncSeam = func(f *os.File) error { syncs.Add(1); return old(f) }
		err = logs.DeleteRange(1, 4990)
		walSyncSeam = old
		if err != nil {
			t.Fatal(err)
		}
		if got := syncs.Load(); got > 3 {
			t.Fatalf("encrypted=%v: DeleteRange of 4990 entries took %d fsyncs, want at most 3", encrypted, got)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}

		db, err = NewEngine(cfg, newTestLogger())
		if err != nil {
			t.Fatal(err)
		}
		for _, i := range []int{1, 2500, 4990} {
			if v, err := db.Get(fmt.Sprintf("raft/log/%d", i)); err == nil && v != nil {
				t.Fatalf("encrypted=%v: deleted entry %d is back after reopen", encrypted, i)
			}
		}
		for i := 4991; i <= 5000; i++ {
			if _, err := db.Get(fmt.Sprintf("raft/log/%d", i)); err != nil {
				t.Fatalf("encrypted=%v: kept entry %d lost: %v", encrypted, i, err)
			}
		}
		_ = db.Close()
	}
}

// Batched frames must stay under the WAL frame limit however many small
// entries arrive at once (a 5000-entry StoreLogs exceeded it before the
// estimate counted the double base64 encoding).
func TestBatchedWritesStayUnderTheFrameLimit(t *testing.T) {
	db, err := NewEngine(core.StorageConfig{Path: t.TempDir(), Encryption: core.EncryptionConfig{Enabled: true, Key: strings.Repeat("k", 32)}}, newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var entries []core.RaftLogEntry
	for i := uint64(1); i <= 20000; i++ {
		entries = append(entries, core.RaftLogEntry{Index: i, Term: 1, Data: []byte(strings.Repeat("d", int(i%300)))})
	}
	if err := NewCobaltDBLogStore(db).StoreLogs(entries); err != nil {
		t.Fatalf("StoreLogs: %v", err)
	}
	var cmds []core.FSMCommand
	for i := 0; i < 5000; i++ {
		cmds = append(cmds, core.FSMCommand{Op: core.FSMSet, Key: fmt.Sprintf("default/souls/s%05d", i), Value: []byte(strings.Repeat("v", i%500))})
	}
	if err := db.ApplyFSMSets(cmds); err != nil {
		t.Fatalf("ApplyFSMSets: %v", err)
	}
}
