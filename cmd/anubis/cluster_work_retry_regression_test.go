package main

// Regression coverage for the failure classification of the replicated
// judgment handler.
//
// A replicated judgment is a committed write: the leader already accepted it
// and every node must end up holding it. The handler therefore has to say
// whether a failure is transient.
//
//   - A storage failure may still succeed on a later attempt, so it is
//     wrapped with raft.Retryable and the node retries the committed entry.
//   - A payload that cannot be decoded can never be applied, so it stays a
//     plain error: the element is skipped and the node keeps applying the
//     rest of the log.
//
// Without the retryable wrapper the committed judgment was lost for good: the
// element was skipped, the applied index advanced, and the node never
// converged on the value the leader had already committed.

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// flakyJudgmentStore is the storage the replicated-judgment handler writes
// through. It fails the first writes on demand and reports every attempt on a
// channel, so a test can order its assertions on real events instead of
// sleeping.
type flakyJudgmentStore struct {
	mu       sync.Mutex
	failures int
	saved    []string

	attempts chan string
}

func newFlakyJudgmentStore(failures int) *flakyJudgmentStore {
	return &flakyJudgmentStore{failures: failures, attempts: make(chan string, 64)}
}

// GetJudgmentByID reports the judgment as missing, so the handler writes it.
func (s *flakyJudgmentStore) GetJudgmentByID(_ context.Context, workspaceID, id string) (*core.Judgment, error) {
	return nil, &core.NotFoundError{Entity: "judgment", ID: id}
}

// SaveJudgment records the judgment, or fails while the store still has
// failures left to report.
func (s *flakyJudgmentStore) SaveJudgment(_ context.Context, j *core.Judgment) error {
	s.mu.Lock()
	failing := s.failures > 0
	if failing {
		s.failures--
	} else {
		s.saved = append(s.saved, j.ID)
	}
	s.mu.Unlock()

	select {
	case s.attempts <- j.ID:
	default:
	}
	if failing {
		return errors.New("transient storage failure")
	}
	return nil
}

// savedJudgments returns the ids of the judgments that were stored.
func (s *flakyJudgmentStore) savedJudgments() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.saved...)
}

// waitForAttempt blocks until the handler tried to write the judgment.
func waitForAttempt(t *testing.T, store *flakyJudgmentStore, id string) {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		select {
		case got := <-store.attempts:
			if got == id {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for the write attempt of %q", id)
		}
	}
}

// waitForSaved blocks until the judgment is stored.
func waitForSaved(t *testing.T, store *flakyJudgmentStore, id string) {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		for _, saved := range store.savedJudgments() {
			if saved == id {
				return
			}
		}
		select {
		case <-store.attempts:
		case <-deadline:
			t.Fatalf("timed out waiting for %q to be stored", id)
		}
	}
}

// replicatedJudgmentBatch builds the batch the production path replicates: a
// judgment command tagged with the judgment table, so the FSM hands it to the
// replicated-judgment handler instead of writing its key directly.
func replicatedJudgmentBatch(t *testing.T, value []byte) core.FSMCommand {
	t.Helper()
	raw, err := json.Marshal([]core.FSMCommand{
		{Op: core.FSMSet, Key: "default/souls/soul-1", Value: []byte(`{"id":"soul-1"}`)},
		{Op: core.FSMSet, Table: judgmentTable, Key: "default/judgments/soul-1/1", Value: value},
	})
	if err != nil {
		t.Fatalf("failed to encode the batch: %v", err)
	}
	return core.FSMCommand{Op: core.FSMBatch, Value: raw}
}

// TestReplicatedJudgmentStorageFailureIsRetriedAndConverges is the core of
// the classification on the production path: a committed judgment whose
// storage write fails transiently must be retried and must end up stored.
//
// Before the fix the handler returned the storage error unwrapped, so the
// node classified it as deterministic: the element was skipped, the applied
// index advanced, and the committed judgment was gone.
func TestReplicatedJudgmentStorageFailureIsRetriedAndConverges(t *testing.T) {
	store := newFlakyJudgmentStore(1)

	var mu sync.Mutex
	calls := 0
	tn := newRetryTestNode(t, judgmentTable, func(cmd *core.FSMCommand) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return applyReplicatedJudgment(cmd, store)
	})
	tn.waitForLeader(t)

	judgment := core.Judgment{
		ID:          "judgment-1",
		SoulID:      "soul-1",
		WorkspaceID: "default",
		Status:      core.SoulAlive,
		Timestamp:   time.Now(),
	}
	value, err := json.Marshal(judgment)
	if err != nil {
		t.Fatalf("failed to encode the judgment: %v", err)
	}

	index, _, _, applyErr := tn.node.Apply(replicatedJudgmentBatch(t, value), 15*time.Second)

	// The gate: the first write attempt happened and failed.
	waitForAttempt(t, store, "judgment-1")

	// The retry converges: the committed judgment ends up stored. Before the
	// fix the element was skipped instead, and the judgment was lost.
	waitForSaved(t, store, "judgment-1")

	// The failure was surfaced to the caller, because the entry could not be
	// applied on the first attempt.
	if applyErr == nil {
		t.Fatal("the transient storage failure was swallowed: the element was skipped instead of retried")
	}

	// The committed entry is consumed, so the applied index moved past it.
	tn.waitForApplied(t, index, "the retried entry never converged")

	// The judgment was stored exactly once: the retry did not duplicate it.
	saved := store.savedJudgments()
	if len(saved) != 1 || saved[0] != "judgment-1" {
		t.Fatalf("the stored judgments are %v, want exactly [judgment-1]", saved)
	}

	// The handler ran twice: the failed attempt and the retry.
	mu.Lock()
	handlerCalls := calls
	mu.Unlock()
	if handlerCalls != 2 {
		t.Fatalf("the handler ran %d times, want 2: the failed attempt was not retried", handlerCalls)
	}
}

// TestReplicatedJudgmentDecodeFailureIsSkippedAndAdvancesOnce covers the
// other half of the classification: a payload that can never be decoded is
// deterministic. The element is skipped and the entry is consumed, exactly
// once, so the node keeps applying the rest of the log.
func TestReplicatedJudgmentDecodeFailureIsSkippedAndAdvancesOnce(t *testing.T) {
	store := newFlakyJudgmentStore(0)

	var mu sync.Mutex
	calls := 0
	tn := newRetryTestNode(t, judgmentTable, func(cmd *core.FSMCommand) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return applyReplicatedJudgment(cmd, store)
	})
	tn.waitForLeader(t)

	// The judgment payload cannot be decoded, so no attempt can apply it.
	index, _, _, applyErr := tn.node.Apply(replicatedJudgmentBatch(t, []byte("{not json")), 15*time.Second)
	if applyErr != nil {
		t.Fatalf("a deterministic failure must not fail the entry: %v", applyErr)
	}

	// The gate: the entry was consumed, so the applied index moved past it.
	tn.waitForApplied(t, index, "the skipped element wedged the node")

	// The undecodable judgment was never stored.
	if saved := store.savedJudgments(); len(saved) != 0 {
		t.Fatalf("the stored judgments are %v, want none", saved)
	}

	// The skip was decided exactly once: the element was attempted once, not
	// retried.
	mu.Lock()
	handlerCalls := calls
	mu.Unlock()
	if handlerCalls != 1 {
		t.Fatalf("the rejected element was attempted %d times, want 1", handlerCalls)
	}
	if got := tn.logs.count("Skipping batch element that failed to apply"); got != 1 {
		t.Fatalf("the skip was decided %d times, want exactly 1", got)
	}

	// The rest of the batch was applied, so the node keeps progressing.
	if _, err := tn.store.Get("default/souls/soul-1"); err != nil {
		t.Fatalf("the rest of the batch was not applied: %v", err)
	}
}
