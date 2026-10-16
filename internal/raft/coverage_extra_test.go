package raft

import (
	"errors"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type failingStorage struct {
	data            map[string][]byte
	setErr          error
	deletePrefixErr error
	listErr         error
	deleteErr       error
	failSetKey      string
}

func (s *failingStorage) Get(key string) ([]byte, error) { return s.data[key], nil }
func (s *failingStorage) Set(key string, value []byte) error {
	if s.setErr != nil && (s.failSetKey == "" || s.failSetKey == key) {
		return s.setErr
	}
	s.data[key] = value
	return nil
}
func (s *failingStorage) Delete(key string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.data, key)
	return nil
}
func (s *failingStorage) DeletePrefix(string) error {
	for key := range s.data {
		delete(s.data, key)
	}
	return nil
}
func (s *failingStorage) List(string) ([]string, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	keys := make([]string, 0, len(s.data))
	for k := range s.data {
		keys = append(keys, k)
	}
	return keys, nil
}

func TestStorageFSMAdditionalErrorPaths(t *testing.T) {
	boom := errors.New("boom")
	store := &failingStorage{data: make(map[string][]byte), setErr: boom}
	fsm := NewStorageFSM(store)
	if got := fsm.Apply(&core.RaftLogEntry{Index: 1, Type: core.LogCommand, Data: []byte("not-json")}); got == nil {
		t.Fatal("expected command decode error")
	}
	cmd, err := fsm.encodeCommand(&core.FSMCommand{Op: core.FSMOp(255)})
	if err != nil {
		t.Fatal(err)
	}
	if got := fsm.Apply(&core.RaftLogEntry{Index: 1, Type: core.LogCommand, Data: cmd}); got == nil {
		t.Fatal("expected unknown operation error")
	}
	if got := fsm.Apply(&core.RaftLogEntry{Index: 1, Type: core.LogConfiguration, Data: []byte("config")}); !errors.Is(got.(error), boom) {
		t.Fatalf("configuration error = %v", got)
	}

	store.setErr = nil
	store.data["raft/dummy"] = []byte("x")
	store.data["workspace/souls/s1"] = []byte("data")
	store.listErr = boom
	if err := fsm.Restore([]byte(`{"key":"dmFsdWU="}`)); !errors.Is(err, boom) {
		t.Fatalf("list error during restore = %v", err)
	}
	store.listErr = nil
	store.deleteErr = boom
	if err := fsm.Restore([]byte(`{"key":"dmFsdWU="}`)); !errors.Is(err, boom) {
		t.Fatalf("delete error during restore = %v", err)
	}
	store.deleteErr = nil
	store.setErr = boom
	store.failSetKey = "key"
	if err := fsm.Restore([]byte(`{"key":"dmFsdWU="}`)); !errors.Is(err, boom) {
		t.Fatalf("restore set error = %v", err)
	}
}
