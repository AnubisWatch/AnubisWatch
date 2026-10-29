package storage

import (
	"strconv"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Installing a snapshot replaced the store one key at a time, one fsync each;
// with a real dataset that took minutes. ReplaceApplicationState does it in
// batched frames with the same result.
func TestReplaceApplicationStateMatchesPerKeyRestore(t *testing.T) {
	cfg := core.StorageConfig{Path: t.TempDir(), Encryption: core.EncryptionConfig{Enabled: true, Key: strings.Repeat("k", 32)}}
	db, err := NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{"default/souls/stale": "old", "default/souls/kept": "old", "raft/log/1": "entry", "raft/hard-state": "hs"} {
		if err := db.Put(k, []byte(v)); err != nil {
			t.Fatal(err)
		}
	}
	data := map[string][]byte{"default/souls/kept": []byte("new"), "default/souls/added": []byte("added"), "raft/log/1": []byte("ignored")}
	for i := 0; i < 3000; i++ {
		data["default/judgments/s/"+strings.Repeat("x", i%40)+"-"+strconv.Itoa(i)] = []byte(strings.Repeat("j", 200))
	}
	if err := db.ReplaceApplicationState(data); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if v, err := db.Get("default/souls/stale"); err == nil && v != nil {
		t.Fatal("a key missing from the snapshot survived the restore")
	}
	for k, want := range map[string]string{"default/souls/kept": "new", "default/souls/added": "added", "raft/log/1": "entry", "raft/hard-state": "hs"} {
		if got, err := db.Get(k); err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v; want %q", k, got, err, want)
		}
	}
	keys, _ := db.List("default/judgments/")
	if len(keys) != 3000 {
		t.Fatalf("restored %d judgments, want 3000", len(keys))
	}
}
