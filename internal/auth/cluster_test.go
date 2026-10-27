package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// This store models the atomic transition executed by the Raft FSM. Hooks gate
// races at the CAS boundary without scheduler timing or sleeps.
type memoryClusterAuthStore struct {
	mu           sync.Mutex
	state        *ClusterAuthState
	unavailable  bool
	failBefore   bool
	failAfter    bool
	conflictOnce func()
}

func (s *memoryClusterAuthStore) Barrier() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.unavailable {
		return errors.New("no quorum")
	}
	return nil
}
func (s *memoryClusterAuthStore) Load() (*ClusterAuthState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == nil {
		return nil, nil
	}
	raw, _ := json.Marshal(s.state)
	var out ClusterAuthState
	_ = json.Unmarshal(raw, &out)
	return &out, nil
}
func (s *memoryClusterAuthStore) CompareAndSwap(expected uint64, id string, data json.RawMessage) (bool, error) {
	s.mu.Lock()
	hook := s.conflictOnce
	s.conflictOnce = nil
	s.mu.Unlock()
	if hook != nil {
		hook()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failBefore {
		return false, errors.New("write failed")
	}
	next, err := BuildClusterAuthState(s.state, ClusterAuthChange{Expected: expected, WriteID: id, Data: data})
	if err != nil {
		return false, err
	}
	if next == nil {
		return false, nil
	}
	s.state = next
	if s.failAfter {
		return false, errors.New("commit reply lost")
	}
	return true, nil
}
func newTestClusterAuth(t *testing.T, s *memoryClusterAuthStore) *ClusterAuthenticator {
	t.Helper()
	local := newTestLocalAuth(t, "", "admin@example.com", "TestPass1234!")
	a, err := NewClusterAuthenticator(local, s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Shutdown)
	return a
}
func clusterLogin(t *testing.T, a *ClusterAuthenticator, password string) string {
	t.Helper()
	_, token, err := a.Login("admin@example.com", password)
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("empty token")
	}
	return token
}
func TestClusterAuthSharedLifecycle(t *testing.T) {
	s := &memoryClusterAuthStore{}
	a, b := newTestClusterAuth(t, s), newTestClusterAuth(t, s)
	token := clusterLogin(t, a, "TestPass1234!")
	if _, err := b.Authenticate(token); err != nil {
		t.Fatal("cross-node session", err)
	}
	if _, err := b.SwitchWorkspace(token, "other"); err != nil {
		t.Fatal(err)
	}
	if u, err := a.Authenticate(token); err != nil || u.Workspace != "other" {
		t.Fatalf("workspace not shared: %v %v", u, err)
	}
	if err := b.ChangePassword(token, "TestPass1234!", "ChangedPass456!"); err != nil {
		t.Fatal(err)
	}
	for _, node := range []*ClusterAuthenticator{a, b} {
		if _, err := node.Authenticate(token); err == nil {
			t.Fatal("rotation retained session")
		}
		if _, _, err := node.Login("admin@example.com", "TestPass1234!"); err == nil {
			t.Fatal("rotation retained password")
		}
	}
	token = clusterLogin(t, a, "ChangedPass456!")
	restarted := newTestClusterAuth(t, s)
	if _, err := restarted.Authenticate(token); err != nil {
		t.Fatal("shared state lost on restart", err)
	}
	if err := b.Logout(token); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Authenticate(token); err == nil {
		t.Fatal("logout not shared")
	}
	reset, err := a.RequestPasswordReset("admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.ConfirmPasswordReset(reset, "ResetPass789!"); err != nil {
		t.Fatal(err)
	}
	if err := a.ConfirmPasswordReset(reset, "AnotherPass012!"); err == nil {
		t.Fatal("reset token replay accepted")
	}
	clusterLogin(t, restarted, "ResetPass789!")
}
func TestClusterAuthQuorumAndUncertainCommit(t *testing.T) {
	s := &memoryClusterAuthStore{}
	a, b := newTestClusterAuth(t, s), newTestClusterAuth(t, s)
	token := clusterLogin(t, a, "TestPass1234!")
	s.mu.Lock()
	s.unavailable = true
	s.mu.Unlock()
	if _, err := a.Authenticate(token); err == nil {
		t.Fatal("partition authorized token")
	}
	if _, got, err := a.Login("admin@example.com", "TestPass1234!"); err == nil || got != "" {
		t.Fatal("partition returned session")
	}
	s.mu.Lock()
	s.unavailable = false
	s.failBefore = true
	s.mu.Unlock()
	if err := a.Logout(token); err == nil {
		t.Fatal("failed commit claimed logout")
	}
	s.mu.Lock()
	s.failBefore = false
	s.mu.Unlock()
	if _, err := b.Authenticate(token); err != nil {
		t.Fatal("failed commit revoked token")
	}
	s.mu.Lock()
	s.failAfter = true
	s.mu.Unlock()
	if err := a.ChangePassword(token, "TestPass1234!", "ChangedPass456!"); err == nil {
		t.Fatal("uncertain commit claimed success")
	}
	s.mu.Lock()
	s.failAfter = false
	s.mu.Unlock()
	if _, err := b.Authenticate(token); err == nil {
		t.Fatal("committed revocation rolled back")
	}
	clusterLogin(t, a, "ChangedPass456!")
	a.Shutdown()
	if _, err := a.Authenticate(token); err == nil {
		t.Fatal("stopped auth accepted request")
	}
}
func TestClusterAuthConflictReevaluates(t *testing.T) {
	s := &memoryClusterAuthStore{}
	a, b := newTestClusterAuth(t, s), newTestClusterAuth(t, s)
	token := clusterLogin(t, a, "TestPass1234!")
	// A evaluates its old-password login, then B rotates before A's CAS.
	s.mu.Lock()
	s.conflictOnce = func() {
		if err := b.ChangePassword(token, "TestPass1234!", "ChangedPass456!"); err != nil {
			t.Fatal(err)
		}
	}
	s.mu.Unlock()
	if _, got, err := a.Login("admin@example.com", "TestPass1234!"); err == nil || got != "" {
		t.Fatal("stale evaluated login escaped CAS")
	}
	clusterLogin(t, a, "ChangedPass456!")
}
func TestClusterAuthSharedLockout(t *testing.T) {
	s := &memoryClusterAuthStore{}
	a, b := newTestClusterAuth(t, s), newTestClusterAuth(t, s)
	for i := 0; i < maxLoginAttempts; i++ {
		node := a
		if i%2 != 0 {
			node = b
		}
		if _, _, err := node.Login("admin@example.com", "wrong"); err == nil {
			t.Fatal("wrong password accepted")
		}
	}
	if _, _, err := b.Login("admin@example.com", "TestPass1234!"); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("lockout not shared: %v", err)
	}
	// Unknown emails cannot grow the bounded replicated authority, even with an empty password.
	before, _ := s.Load()
	for i := 0; i < 3; i++ {
		_, _, _ = a.Login(fmt.Sprintf("unknown%d@example.com", i), "")
	}
	after, _ := s.Load()
	if after.Revision != before.Revision {
		t.Fatal("unknown emails changed state")
	}
}
func TestClusterAuthReceiptsAndBounds(t *testing.T) {
	a := newTestLocalAuth(t, "", "admin@example.com", "TestPass1234!")
	raw, err := snapshotClusterAuth(a)
	if err != nil {
		t.Fatal(err)
	}
	var state *ClusterAuthState
	for i := uint64(0); i < 70; i++ {
		state, err = BuildClusterAuthState(state, ClusterAuthChange{Expected: i, WriteID: fmt.Sprint(i), Data: raw})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(state.Receipts) != clusterAuthReceiptLimit {
		t.Fatal("unbounded receipts")
	}
	if ok, err := ClusterAuthCommitOutcome(state, 68, "68"); !ok || err != nil {
		t.Fatal("recent committed receipt lost")
	}
	if _, err := ClusterAuthCommitOutcome(state, 0, "0"); err == nil {
		t.Fatal("evicted receipt incorrectly called conflict")
	}
	if next, err := BuildClusterAuthState(state, ClusterAuthChange{Expected: 0, WriteID: "stale", Data: raw}); next != nil || err != nil {
		t.Fatal("CAS conflict must be no-op")
	}
	if _, err := ClusterAuthCommitOutcome(nil, 0, "missing"); err == nil {
		t.Fatal("absent commit accepted")
	}
	for _, bad := range []json.RawMessage{[]byte("{"), []byte(`{"users":{"x":null}}`), []byte(`{"tokens":{"x":{"user_id":"missing"}}}`), []byte(strings.Repeat(" ", MaxClusterAuthStateBytes+1))} {
		if _, err := BuildClusterAuthState(nil, ClusterAuthChange{WriteID: "bad", Data: bad}); err == nil {
			t.Fatal("invalid state accepted")
		}
	}
	// Max-sized valid JSON remains below the WAL 1MiB cap after all base64 envelopes.
	prefix := `{"admin_email":"`
	suffix := `"}`
	padded := json.RawMessage(prefix + strings.Repeat("a", MaxClusterAuthStateBytes-len(prefix)-len(suffix)) + suffix)
	if err := validateClusterAuthData(padded); err != nil {
		t.Fatal(err)
	}
	change, _ := json.Marshal(ClusterAuthChange{WriteID: strings.Repeat("a", 64), Data: padded})
	cmd, _ := json.Marshal(core.FSMCommand{Key: "system/auth/state", Table: "cluster-auth", Value: change})
	entry, _ := json.Marshal(core.RaftLogEntry{Data: cmd})
	wal, _ := json.Marshal(struct {
		Op    string `json:"op"`
		Key   string `json:"key"`
		Value []byte `json:"value"`
	}{"put", "raft/log/123", entry})
	if len(wal) >= 1024*1024 || len(wal) < MaxClusterAuthStateBytes {
		t.Fatalf("WAL envelope size invalid: %d", len(wal))
	}
	a.mu.Lock()
	for _, u := range a.users {
		a.tokens["expired"] = &session{UserID: u.ID, ExpiresAt: time.Now().Add(-time.Hour)}
	}
	a.mu.Unlock()
	expired, _ := snapshotClusterAuth(a)
	if strings.Contains(string(expired), "expired") {
		t.Fatal("expired sessions retained")
	}
}

func TestClusterAuthCapacityRejectsWithoutLeakingSession(t *testing.T) {
	s := &memoryClusterAuthStore{}
	a := newTestClusterAuth(t, s)
	token := clusterLogin(t, a, "TestPass1234!")
	state, _ := s.Load()
	var data sessionData
	if err := json.Unmarshal(state.Data, &data); err != nil {
		t.Fatal(err)
	}
	existing := data.Tokens[token]
	padding := strings.Repeat("p", MaxClusterAuthStateBytes)
	data.Tokens[padding] = existing
	raw, _ := json.Marshal(data)
	// Choose a valid existing session key to leave one byte of transaction budget.
	padding = padding[:len(padding)-(len(raw)-MaxClusterAuthStateBytes)-1]
	delete(data.Tokens, strings.Repeat("p", MaxClusterAuthStateBytes))
	data.Tokens[padding] = existing
	raw, _ = json.Marshal(data)
	if len(raw) != MaxClusterAuthStateBytes-1 {
		t.Fatalf("bad capacity fixture: %d", len(raw))
	}
	s.mu.Lock()
	s.state.Data = raw
	revision := s.state.Revision
	s.mu.Unlock()
	if user, got, err := a.Login("admin@example.com", "TestPass1234!"); err == nil || user != nil || got != "" {
		t.Fatal("over-budget session escaped failed commit")
	}
	after, _ := s.Load()
	if after.Revision != revision {
		t.Fatal("failed session changed authority")
	}
	if _, err := a.Authenticate(token); err != nil {
		t.Fatal("capacity error wedged existing sessions", err)
	}
	if err := a.Logout(padding); err != nil {
		t.Fatal("capacity error prevented cleanup", err)
	}
	clusterLogin(t, a, "TestPass1234!")
}
