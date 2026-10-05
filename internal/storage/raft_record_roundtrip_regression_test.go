package storage

import (
	"bytes"
	"context"
	"fmt"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"testing"
)

func TestRaftRecordRoundtripRegression(t *testing.T) {
	cfg := core.StorageConfig{Path: t.TempDir()}
	db, err := NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	terms := []uint64{0, 5, 1<<53 + 1, ^uint64(0)}
	payloads := [][]byte{nil, {}, {0, 255, 1}, []byte("record")}
	for i, term := range terms {
		if err := db.SaveRaftLogEntry(ctx, uint64(i+1), term, payloads[i]); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.SaveRaftState(ctx, terms[3], "node-1"); err != nil {
		t.Fatal(err)
	}
	check := func() {
		t.Helper()
		term, vote, err := db.GetRaftState(ctx)
		if err != nil || term != terms[3] || vote != "node-1" {
			t.Fatalf("state %d/%s err=%v", term, vote, err)
		}
		for i, want := range terms {
			term, data, err := db.GetRaftLogEntry(ctx, uint64(i+1))
			if err != nil || term != want || !bytes.Equal(data, payloads[i]) {
				t.Fatalf("entry%d term=%d data=%v err=%v", i, term, data, err)
			}
		}
	}
	check()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	check()
	for _, bad := range []string{`{"current_term":1.5}`, `{"current_term":"bad"}`} {
		if err := db.Put("raft/state", []byte(bad)); err != nil {
			t.Fatal(err)
		}
		if _, _, err := db.GetRaftState(ctx); err == nil {
			t.Fatalf("expected malformed state error: %s", bad)
		}
	}
	if err := db.Put("raft/log/5", []byte(`{"term":7,"data":"!"}`)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.GetRaftLogEntry(ctx, 5); err == nil {
		t.Fatal("expected invalid base64 error")
	}
	fmt.Println("roundtrip boundaries including restart verified")
}
