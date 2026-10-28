package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// ClusterStateStore is a quorum-backed, linearizable authentication authority.
// Barrier must wait for a committed entry to apply locally before Load. A CAS
// conflict is a normal false result; storage/quorum uncertainty is an error.
type ClusterStateStore interface {
	Barrier() error
	Load() (*ClusterAuthState, error)
	CompareAndSwap(expected uint64, writeID string, data json.RawMessage) (bool, error)
}

// ClusterAuthState is persisted under one fixed key and included in snapshots.
// Receipts bound commit-outcome tracking without creating per-operation keys.
type ClusterAuthState struct {
	Revision uint64               `json:"revision"`
	Data     json.RawMessage      `json:"data"`
	Receipts []ClusterAuthReceipt `json:"receipts"`
}
type ClusterAuthReceipt struct {
	Revision uint64 `json:"revision"`
	WriteID  string `json:"write_id"`
}
type ClusterAuthChange struct {
	Expected uint64          `json:"expected"`
	WriteID  string          `json:"write_id"`
	Data     json.RawMessage `json:"data"`
}

const MaxClusterAuthStateBytes = 192 * 1024
const clusterAuthReceiptLimit = 64

// maxSharedLockouts bounds the failed-login sources replicated with the
// authentication state (see snapshotClusterAuth).
const maxSharedLockouts = 64

// BuildClusterAuthState performs a deterministic CAS transition. Nil means
// conflict or replay, not an FSM error: conflicts must not stop Raft apply.
func BuildClusterAuthState(current *ClusterAuthState, change ClusterAuthChange) (*ClusterAuthState, error) {
	revision := uint64(0)
	if current != nil {
		revision = current.Revision
	}
	if revision != change.Expected {
		return nil, nil
	}
	if change.WriteID == "" {
		return nil, errors.New("authentication mutation has no receipt ID")
	}
	if revision == ^uint64(0) {
		return nil, errors.New("authentication revision exhausted")
	}
	if err := validateClusterAuthData(change.Data); err != nil {
		return nil, err
	}
	next := &ClusterAuthState{Revision: revision + 1, Data: bytes.Clone(change.Data)}
	if current != nil {
		next.Receipts = append(next.Receipts, current.Receipts...)
	}
	next.Receipts = append(next.Receipts, ClusterAuthReceipt{Revision: next.Revision, WriteID: change.WriteID})
	if len(next.Receipts) > clusterAuthReceiptLimit {
		next.Receipts = next.Receipts[len(next.Receipts)-clusterAuthReceiptLimit:]
	}
	return next, nil
}

// ClusterAuthCommitOutcome distinguishes conflict from an uncertain outcome
// after a writer's receipt aged out. Uncertainty must never trigger rollback.
func ClusterAuthCommitOutcome(current *ClusterAuthState, expected uint64, writeID string) (bool, error) {
	if current == nil || current.Revision <= expected {
		return false, errors.New("authentication commit not visible")
	}
	for _, receipt := range current.Receipts {
		if receipt.WriteID == writeID {
			return true, nil
		}
	}
	if len(current.Receipts) > 0 && current.Receipts[0].Revision > expected+1 {
		return false, errors.New("authentication commit outcome is uncertain; retry from current state")
	}
	return false, nil
}

// ClusterAuthenticator evaluates LocalAuthenticator semantics against shared
// snapshots. Every request first proves quorum, so a partitioned replica cannot
// authorize a token that another majority has revoked. Standalone auth is unchanged.
type ClusterAuthenticator struct {
	mu      sync.Mutex
	local   *LocalAuthenticator
	store   ClusterStateStore
	seed    json.RawMessage
	stopped bool
	// importedRevision is the shared revision a.local mirrors; 0 forces the
	// next sync to import. Guarded by mu.
	importedRevision uint64
	// barriers coalesces the quorum barrier every request needs, and runs
	// it without mu, so one slow barrier does not queue the whole node.
	barriers barrierGroup
}

// barrierGroup shares quorum barriers between concurrent callers. A caller
// only ever waits on a barrier that started after it arrived, so each one
// still observes every write acknowledged before its call.
type barrierGroup struct {
	mu      sync.Mutex
	running *barrierCall
	next    *barrierCall // starts when running ends; joined meanwhile
}

type barrierCall struct {
	done chan struct{}
	err  error
}

func (g *barrierGroup) do(fn func() error) error {
	g.mu.Lock()
	if g.running == nil {
		c := &barrierCall{done: make(chan struct{})}
		g.running = c
		g.mu.Unlock()
		g.run(c, fn)
		return c.err
	}
	if c := g.next; c != nil {
		// Not started yet, so it starts after this call arrived.
		g.mu.Unlock()
		<-c.done
		return c.err
	}
	// The running barrier may have started before this call: schedule the
	// next one and run it once the current one ends.
	c := &barrierCall{done: make(chan struct{})}
	g.next = c
	running := g.running
	g.mu.Unlock()
	<-running.done
	g.mu.Lock()
	g.running, g.next = c, nil
	g.mu.Unlock()
	g.run(c, fn)
	return c.err
}

func (g *barrierGroup) run(c *barrierCall, fn func() error) {
	c.err = fn()
	g.mu.Lock()
	if g.running == c {
		g.running = nil
	}
	g.mu.Unlock()
	close(c.done)
}

func NewClusterAuthenticator(local *LocalAuthenticator, store ClusterStateStore) (*ClusterAuthenticator, error) {
	if local == nil || store == nil {
		return nil, errors.New("cluster authentication requires local evaluator and shared store")
	}
	local.mu.Lock()
	local.sessionPath = "" // Shared state is the authority; never persist stale pod-local sessions.
	local.mu.Unlock()
	seed, err := snapshotClusterAuth(local)
	if err != nil {
		return nil, err
	}
	return &ClusterAuthenticator{local: local, store: store, seed: seed}, nil
}

func validateClusterAuthData(raw []byte) error {
	if len(raw) > MaxClusterAuthStateBytes {
		return fmt.Errorf("authentication state (%d bytes: sessions, accounts and invitations) exceeds the %d-byte replicated transaction budget; revoke unused sessions or invitations, or delete unused accounts", len(raw), MaxClusterAuthStateBytes)
	}
	var data sessionData
	if err := json.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("decode shared authentication state: %w", err)
	}
	for id, user := range data.Users {
		if user == nil || id == "" || user.ID != id {
			return errors.New("invalid shared authentication user")
		}
	}
	for _, sess := range data.Tokens {
		if sess == nil || data.Users[sess.UserID] == nil {
			return errors.New("invalid shared authentication session")
		}
	}
	for _, token := range data.ResetTokens {
		if token == nil {
			return errors.New("invalid shared reset token")
		}
	}
	for _, attempt := range data.Lockouts {
		if attempt == nil {
			return errors.New("invalid shared lockout")
		}
	}
	for id, rec := range data.Accounts {
		if rec == nil || data.Users[id] == nil {
			return errors.New("invalid shared account")
		}
	}
	for id, inv := range data.Invitations {
		if inv == nil || inv.ID != id || inv.TokenHash == "" {
			return errors.New("invalid shared invitation")
		}
	}
	return nil
}

func snapshotClusterAuth(a *LocalAuthenticator) (json.RawMessage, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	a.attemptsMu.RLock()
	defer a.attemptsMu.RUnlock()
	now := time.Now()
	tokens := make(map[string]*session, len(a.tokens))
	for token, value := range a.tokens {
		if value != nil && value.ExpiresAt.After(now) {
			tokens[token] = value
		}
	}
	resets := make(map[string]*resetToken, len(a.resetTokens))
	for token, value := range a.resetTokens {
		if value != nil && value.ExpiresAt.After(now) {
			resets[token] = value
		}
	}
	lockouts := make(map[string]*loginAttempt, len(a.loginAttempts))
	for email, attempt := range a.loginAttempts {
		if attempt != nil && (attempt.LockedUntil.After(now) || now.Sub(attempt.LastTry) <= attemptResetWindow) {
			lockouts[email] = attempt
		}
	}
	// Failed-login sources are unbounded input (any address may guess), so
	// only the most recent maxSharedLockouts enter the shared state.
	if len(lockouts) > maxSharedLockouts {
		keys := make([]string, 0, len(lockouts))
		for k := range lockouts {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return lockouts[keys[i]].LastTry.After(lockouts[keys[j]].LastTry) })
		for _, k := range keys[maxSharedLockouts:] {
			delete(lockouts, k)
		}
	}
	invitations := make(map[string]*invitationRecord, len(a.invitations))
	for id, inv := range a.invitations {
		if inv != nil && inv.ExpiresAt.After(now) {
			invitations[id] = inv
		}
	}
	raw, err := json.Marshal(sessionData{Tokens: tokens, Users: a.users, ResetTokens: resets, Lockouts: lockouts, AdminEmail: a.adminEmail, AdminPasswordHash: a.adminPasswordHash, AdminSeedHash: a.configSeedHash,
		Accounts: a.accounts, Invitations: invitations})
	if err != nil {
		return nil, err
	}
	if err := validateClusterAuthData(raw); err != nil {
		return nil, err
	}
	return raw, nil
}
func importClusterAuth(a *LocalAuthenticator, raw []byte) error {
	if err := validateClusterAuthData(raw); err != nil {
		return err
	}
	var data sessionData
	if err := json.Unmarshal(raw, &data); err != nil {
		return err
	}
	if data.Tokens == nil {
		data.Tokens = make(map[string]*session)
	}
	if data.Users == nil {
		data.Users = make(map[string]*api.User)
	}
	if data.ResetTokens == nil {
		data.ResetTokens = make(map[string]*resetToken)
	}
	if data.Lockouts == nil {
		data.Lockouts = make(map[string]*loginAttempt)
	}
	if data.Accounts == nil {
		data.Accounts = make(map[string]*accountRecord)
	}
	for _, rec := range data.Accounts {
		if rec.Memberships == nil {
			rec.Memberships = make(map[string]core.MemberRole)
		}
	}
	if data.Invitations == nil {
		data.Invitations = make(map[string]*invitationRecord)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.attemptsMu.Lock()
	defer a.attemptsMu.Unlock()
	a.tokens, a.users, a.resetTokens, a.loginAttempts = data.Tokens, data.Users, data.ResetTokens, data.Lockouts
	a.adminEmail, a.adminPasswordHash, a.configSeedHash = data.AdminEmail, data.AdminPasswordHash, data.AdminSeedHash
	a.accounts, a.invitations = data.Accounts, data.Invitations
	// Shared state written by a version without the directory has users but
	// no accounts; give them the same migration a session file gets.
	a.migrateDirectoryLocked()
	a.refreshCredIndexLocked()
	return nil
}

// barrier proves quorum and makes the next local read linearizable. It is
// shared between concurrent callers and must run without a.mu held.
func (a *ClusterAuthenticator) barrier() error {
	if err := a.barriers.do(a.store.Barrier); err != nil {
		return fmt.Errorf("authentication quorum unavailable: %w", err)
	}
	return nil
}

// sync brings a.local up to the shared state already made visible by a
// barrier, importing it only when its revision changed. Caller holds a.mu.
func (a *ClusterAuthenticator) sync() (*ClusterAuthState, error) {
	if a.stopped {
		return nil, errors.New("authenticator is stopped")
	}
	for attempt := 0; attempt < 8; attempt++ {
		state, err := a.store.Load()
		if err != nil {
			return nil, err
		}
		if state != nil {
			if state.Revision == 0 {
				return nil, errors.New("invalid authentication revision")
			}
			if state.Revision != a.importedRevision {
				a.importedRevision = 0
				if err := importClusterAuth(a.local, state.Data); err != nil {
					return nil, err
				}
				a.importedRevision = state.Revision
			}
			return state, nil
		}
		id, err := generateToken()
		if err != nil {
			return nil, err
		}
		_, err = a.store.CompareAndSwap(0, id, a.seed)
		if err != nil {
			return nil, err
		}
	}
	return nil, errors.New("authentication initialization contention; retry")
}

// mutate re-evaluates after CAS conflicts. Never return generated tokens or
// claim a successful revocation until its shared transaction is confirmed.
//
// The barrier runs once, before a.mu: a CAS retry needs no new one, because
// the failed CAS returned only after this node applied the conflicting write.
func (a *ClusterAuthenticator) mutate(fn func() error) error {
	if err := a.barrier(); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for attempt := 0; attempt < 16; attempt++ {
		state, err := a.sync()
		if err != nil {
			return err
		}
		before, err := snapshotClusterAuth(a.local)
		if err != nil {
			return err
		}
		// fn changes a.local; until a CAS commits that change, a.local no
		// longer mirrors any shared revision.
		a.importedRevision = 0
		operationErr := fn()
		after, err := snapshotClusterAuth(a.local)
		if err != nil {
			return err
		}
		if bytes.Equal(before, after) {
			return operationErr
		}
		id, err := generateToken()
		if err != nil {
			return err
		}
		applied, err := a.store.CompareAndSwap(state.Revision, id, after)
		if err != nil {
			return err
		}
		if applied {
			return operationErr
		}
	}
	return errors.New("authentication state changed concurrently; retry")
}
func (a *ClusterAuthenticator) Authenticate(token string) (*api.User, error) {
	if err := a.barrier(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, err := a.sync(); err != nil {
		return nil, err
	}
	return a.local.Authenticate(token)
}
func (a *ClusterAuthenticator) Login(email, password string) (user *api.User, token string, err error) {
	err = a.mutate(func() error { var e error; user, token, e = a.local.Login(email, password); return e })
	if err != nil {
		return nil, "", err
	}
	return user, token, nil
}
func (a *ClusterAuthenticator) Logout(token string) error {
	return a.mutate(func() error { return a.local.Logout(token) })
}
func (a *ClusterAuthenticator) ChangePassword(token, current, next string) error {
	return a.mutate(func() error { return a.local.ChangePassword(token, current, next) })
}
func (a *ClusterAuthenticator) RequestPasswordReset(email string) (token string, err error) {
	err = a.mutate(func() error { var e error; token, e = a.local.RequestPasswordReset(email); return e })
	if err != nil {
		return "", err
	}
	return token, nil
}
func (a *ClusterAuthenticator) ConfirmPasswordReset(token, next string) error {
	return a.mutate(func() error { return a.local.ConfirmPasswordReset(token, next) })
}
func (a *ClusterAuthenticator) SwitchWorkspace(token, workspace string) (user *api.User, err error) {
	err = a.mutate(func() error { var e error; user, e = a.local.SwitchWorkspace(token, workspace); return e })
	if err != nil {
		return nil, err
	}
	return user, nil
}
func (a *ClusterAuthenticator) Shutdown() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.stopped {
		a.stopped = true
		a.local.Shutdown()
	}
}
