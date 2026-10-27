package storage

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func TestPutManyPublishesOnlyAfterDurableFrame(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	if err := db.putMany(map[string][]byte{"a": []byte("old"), "b": []byte("old")}); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	sync := walSyncSeam
	defer func() { walSyncSeam = sync }()
	walSyncSeam = func(f *os.File) error { close(entered); <-release; return sync(f) }
	done := make(chan error, 1)
	go func() { done <- db.putMany(map[string][]byte{"a": []byte("new"), "b": []byte("new")}) }()
	<-entered
	for _, key := range []string{"a", "b"} {
		if value, err := db.Get(key); err != nil || string(value) != "old" {
			t.Errorf("before sync, %s=%q err=%v", key, value, err)
		}
	}
	close(release)
	err := <-done
	walSyncSeam = sync
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"a", "b"} {
		if value, err := db.Get(key); err != nil || string(value) != "new" {
			t.Fatalf("after sync, %s=%q err=%v", key, value, err)
		}
	}
}

func TestWALBatchTornTailReplaysAllOrNone(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		for _, tail := range []string{"prefix", "body", "last-byte", "complete"} {
			t.Run(fmt.Sprintf("encrypted=%v/%s", encrypted, tail), func(t *testing.T) {
				cfg := core.StorageConfig{Path: t.TempDir(), Encryption: core.EncryptionConfig{Enabled: encrypted, Key: "wal-batch-restart-key"}}
				db, err := NewEngine(cfg, newTestLogger())
				if err != nil {
					t.Fatal(err)
				}
				if err := db.Put("stable", []byte("before")); err != nil {
					t.Fatal(err)
				}
				boundary := db.wal.currentSize()
				if err := db.putMany(map[string][]byte{"new/a": []byte("secret payload A"), "new/b": []byte("secret payload B")}); err != nil {
					t.Fatal(err)
				}
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(cfg.Path, "wal.log")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if encrypted && bytes.Contains(data, []byte("secret payload")) {
					t.Fatal("batch leaked plaintext values")
				}
				size := int64(len(data))
				switch tail {
				case "prefix":
					size = boundary + 2
				case "body":
					size = boundary + (size-boundary)/2
				case "last-byte":
					size--
				}
				if err := os.Truncate(path, size); err != nil {
					t.Fatal(err)
				}
				for restart := 0; restart < 2; restart++ {
					db, err = NewEngine(cfg, newTestLogger())
					if err != nil {
						t.Fatal(err)
					}
					if value, err := db.Get("stable"); err != nil || string(value) != "before" {
						t.Fatalf("stable prefix lost on restart%d: %q %v", restart, value, err)
					}
					for _, key := range []string{"new/a", "new/b"} {
						value, err := db.Get(key)
						if (err == nil) != (tail == "complete") {
							t.Fatalf("batch recovered partially/wrongly: key=%s tail=%s err=%v", key, tail, err)
						}
						if tail == "complete" {
							want := map[string]string{"new/a": "secret payload A", "new/b": "secret payload B"}[key]
							if string(value) != want {
								t.Fatalf("recovered value mismatch: key=%s value=%q", key, value)
							}
						}
					}
					_ = db.Close()
				}
			})
		}
	}
}

func TestWALCorruptBatchSkipsWholeFrameAndPreservesSuffix(t *testing.T) {
	db := newTestDB(t)
	dir := db.path
	if err := db.Put("before", []byte("stable")); err != nil {
		t.Fatal(err)
	}
	start := db.wal.currentSize()
	if err := db.putMany(map[string][]byte{"a": []byte("atomic"), "b": []byte("atomic")}); err != nil {
		t.Fatal(err)
	}
	end := db.wal.currentSize()
	if err := db.Put("after", []byte("stable")); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	path := filepath.Join(dir, "wal.log")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Change the batch payload while leaving a valid JSON/base64 frame.
	frame := data[start+4 : end]
	position := bytes.Index(frame, []byte(`"value":"`)) + len(`"value":"`)
	if position < len(`"value":"`) || position >= len(frame) {
		t.Fatal("batch payload fixture missing")
	}
	if frame[position] == 'A' {
		frame[position] = 'B'
	} else {
		frame[position] = 'A'
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewEngine(core.StorageConfig{Path: dir}, newTestLogger()); err == nil {
		t.Fatal("recovery accepted a corrupt mid-log batch without opt-in")
	}
	t.Setenv(WALSkipCorruptEnv, "1")
	db, err = NewEngine(core.StorageConfig{Path: dir}, newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, key := range []string{"before", "after"} {
		if _, err := db.Get(key); err != nil {
			t.Fatal("valid neighbor lost:", key, err)
		}
	}
	for _, key := range []string{"a", "b"} {
		if _, err := db.Get(key); err == nil {
			t.Fatal("corrupt batch partially recovered:", key)
		}
	}
}

func TestWALBatchReadsLegacyAndRejectsMalformedPayload(t *testing.T) {
	t.Run("legacy-unchecksummed", func(t *testing.T) {
		dir := t.TempDir()
		entry, err := json.Marshal(walEntry{Op: "PUT", Key: "legacy", Value: []byte("old value"), Time: 1})
		if err != nil {
			t.Fatal(err)
		}
		data := make([]byte, 4+len(entry))
		binary.BigEndian.PutUint32(data, uint32(len(entry)))
		copy(data[4:], entry)
		if err := os.WriteFile(filepath.Join(dir, "wal.log"), data, 0600); err != nil {
			t.Fatal(err)
		}
		cfg := core.StorageConfig{Path: dir}
		db, err := NewEngine(cfg, newTestLogger())
		if err != nil {
			t.Fatal(err)
		}
		if err := db.putMany(map[string][]byte{"new": []byte("new value")}); err != nil {
			t.Fatal(err)
		}
		_ = db.Close()
		db, err = NewEngine(cfg, newTestLogger())
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		for key, want := range map[string]string{"legacy": "old value", "new": "new value"} {
			if value, err := db.Get(key); err != nil || string(value) != want {
				t.Fatalf("mixed legacy/batch replay: %s=%q err=%v", key, value, err)
			}
		}
	})
	for _, malformed := range []string{"null", "{", `{"bad":"invalid base64"}`} {
		t.Run(malformed, func(t *testing.T) {
			db := newTestDB(t)
			dir := db.path
			db.wal.mu.Lock()
			err := db.wal.appendLocked(walEntry{Op: "BATCH", Value: []byte(malformed), Time: 1})
			db.wal.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			_ = db.Close()
			path := filepath.Join(dir, "wal.log")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			reopened, err := NewEngine(core.StorageConfig{Path: dir}, newTestLogger())
			if reopened != nil {
				_ = reopened.Close()
			}
			if err == nil {
				t.Fatal("malformed batch must fail startup")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("failed replay rewrote malformed evidence")
			}
		})
	}
}
