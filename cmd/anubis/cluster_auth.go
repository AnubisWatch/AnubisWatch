package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/auth"
	"github.com/AnubisWatch/anubiswatch/internal/cluster"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/raft"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

const clusterAuthTable = "cluster-auth"
const clusterAuthStateKey = "system/auth/state"
const clusterAuthBarrierKey = "system/auth/barrier"
const clusterAuthTimeout = 5 * time.Second

// clusterAuthStorage is the storage surface the shared-authentication handler
// uses. It is an interface so the handler's failure classification -- a
// storage failure is transient, a rejected change is not -- can be exercised
// without a real disk.
type clusterAuthStorage interface {
	Get(key string) ([]byte, error)
	Put(key string, value []byte) error
}

type clusterAuthStore struct {
	mgr   *cluster.Manager
	store clusterAuthStorage
}

func newClusterAuthenticator(local *auth.LocalAuthenticator, mgr *cluster.Manager, store *storage.CobaltDB) (*auth.ClusterAuthenticator, error) {
	shared := &clusterAuthStore{mgr: mgr, store: store}
	mgr.SetCommandHandler(clusterAuthTable, shared.apply)
	return auth.NewClusterAuthenticator(local, shared)
}

// Barrier makes the next Load linearizable. It is a ReadIndex round trip to
// the leader, not a log entry: it runs before every authenticated request.
// Only a leader too old to answer read_index gets the old write barrier.
func (s *clusterAuthStore) Barrier() error {
	_, err := s.mgr.ReadBarrier(clusterAuthTimeout)
	if !errors.Is(err, raft.ErrReadIndexUnsupported) {
		return err
	}
	_, _, err = s.mgr.ApplyMutation(core.FSMCommand{Op: core.FSMSet, Key: clusterAuthBarrierKey, Value: []byte("1")}, clusterAuthTimeout)
	return err
}
func (s *clusterAuthStore) Load() (*auth.ClusterAuthState, error) {
	raw, err := s.store.Get(clusterAuthStateKey)
	if err != nil {
		var missing *core.NotFoundError
		if errors.As(err, &missing) {
			return nil, nil
		}
		return nil, err
	}
	var state auth.ClusterAuthState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("read shared authentication state: %w", err)
	}
	return &state, nil
}
func (s *clusterAuthStore) CompareAndSwap(expected uint64, id string, data json.RawMessage) (bool, error) {
	change := auth.ClusterAuthChange{Expected: expected, WriteID: id, Data: data}
	// Validate the full state/size before it enters a committed Raft log. Invalid
	// client state must not become an unapplyable entry that wedges every node.
	if _, err := auth.BuildClusterAuthState(&auth.ClusterAuthState{Revision: expected}, change); err != nil {
		return false, err
	}
	raw, err := json.Marshal(change)
	if err != nil {
		return false, err
	}
	_, _, err = s.mgr.ApplyMutation(core.FSMCommand{Op: core.FSMSet, Table: clusterAuthTable, Key: clusterAuthStateKey, Value: raw}, clusterAuthTimeout)
	if err != nil {
		return false, fmt.Errorf("authentication commit failed or outcome uncertain: %w", err)
	}
	state, err := s.Load()
	if err != nil {
		return false, err
	}
	return auth.ClusterAuthCommitOutcome(state, expected, id)
}
func (s *clusterAuthStore) apply(cmd *core.FSMCommand) error {
	if cmd.Op != core.FSMSet || cmd.Key != clusterAuthStateKey {
		return errors.New("unsupported shared authentication command")
	}
	var change auth.ClusterAuthChange
	if err := json.Unmarshal(cmd.Value, &change); err != nil {
		return err
	}
	current, err := s.Load()
	if err != nil {
		return err
	}
	next, err := auth.BuildClusterAuthState(current, change)
	if err != nil {
		return err
	}
	if next == nil {
		return nil
	} // Deterministic CAS conflict/replay is a committed no-op.
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	// The write is the only step that can fail transiently: a storage error
	// may still succeed on a later attempt, so it is marked retryable and the
	// node retries the committed entry. The decode and validation failures
	// above can never be applied, so they stay plain errors and the element is
	// skipped.
	if err := s.store.Put(clusterAuthStateKey, raw); err != nil {
		return raft.Retryable(err)
	}
	return nil
}
