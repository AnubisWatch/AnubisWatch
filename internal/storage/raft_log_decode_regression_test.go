package storage

import (
	"bytes"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"reflect"
	"testing"
)

func TestLogStorePreservesStoredEntry(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()
	store := NewCobaltDBLogStore(db)
	cases := []core.RaftLogEntry{
		{Index: 1, Term: 42, Data: []byte("control")},
		{Index: 2, Term: 9007199254740993, Type: core.LogNoOp},
		{Index: 3, Term: ^uint64(0), Type: core.LogConfiguration, Data: []byte("max")},
		{Index: 4, Term: 0, Data: []byte{}},
		{Index: 5, Term: 1, Data: nil},
	}
	got := core.RaftLogEntry{Term: 7, Data: []byte("stale")}
	for _, want := range cases {
		if err := store.StoreLog(&want); err != nil {
			t.Fatal(err)
		}
		if err := store.GetLog(want.Index, &got); err != nil {
			t.Fatal(err)
		}
		if got.Index != want.Index || got.Term != want.Term || got.Type != want.Type || !bytes.Equal(got.Data, want.Data) || (want.Data == nil) != (got.Data == nil) {
			t.Fatalf("EXPECTED: %#v ACTUAL: %#v", want, got)
		}
	}
	before := got
	if err := store.GetLog(999, &got); err == nil || !reflect.DeepEqual(got, before) {
		t.Fatalf("missing record changed receiver: %#v error=%v", got, err)
	}
	if err := db.Put("raft/log/6", []byte(`{"index":999,"term":12,"type":1,"data":null}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.GetLog(6, &got); err != nil || got.Index != 6 || got.Term != 12 || got.Data != nil {
		t.Fatalf("key index not authoritative: %#v error=%v", got, err)
	}
	before = got
	if err := db.Put("raft/log/7", []byte(`{"term":`)); err != nil {
		t.Fatal(err)
	}
	if err := store.GetLog(7, &got); err == nil || !reflect.DeepEqual(got, before) {
		t.Fatalf("decode failure changed receiver: %#v error=%v", got, err)
	}
}
