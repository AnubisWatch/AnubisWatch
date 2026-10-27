package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"golang.org/x/crypto/bcrypt"
)

// LocalAuthenticator implements simple token-based auth
type LocalAuthenticator struct {
	mu                sync.RWMutex
	tokens            map[string]*session
	users             map[string]*api.User
	adminEmail        string
	adminPasswordHash string // bcrypt hashed password
	// configSeedHash identifies the config-file password the current hash was
	// derived from; a different config password at startup replaces a
	// password changed at runtime (see loadSessions).
	configSeedHash string
	// configPassword is the configured password, held only during
	// construction so loadSessions can compare it against configSeedHash.
	configPassword string
	sessionPath    string
	stopCleanup    chan struct{}
	cleanupDone    chan struct{}
	// stopOnce makes Shutdown idempotent — t.Cleanup hooks plus
	// explicit defer Shutdown() calls in tests can both fire safely.
	stopOnce sync.Once
	// Brute-force protection, all guarded by attemptsMu.
	//
	// loginAttempts holds failures per (account, client IP) source; five of
	// them lock that source out (see loginSourceKey). accountFailures counts
	// failures for an account across every source and, past
	// accountBackoffThreshold, slows the account down with an exponential
	// backoff instead of a hard lock. knownLoginIPs records the client IPs an
	// account recently signed in from successfully; they are exempt from the
	// account-wide backoff, so a flood of failures from elsewhere cannot lock
	// the legitimate user out of their own machine.
	loginAttempts   map[string]*loginAttempt
	accountFailures map[string]*loginAttempt
	knownLoginIPs   map[string]time.Time
	attemptsMu      sync.RWMutex
	// password reset tokens (protected by a.mu)
	resetTokens     map[string]*resetToken
	comparePassword func([]byte, []byte) error
	// accounts and invitations make up the multi-user directory (users.go),
	// guarded by a.mu and persisted with the sessions.
	accounts    map[string]*accountRecord
	invitations map[string]*invitationRecord
	// memberQuota enforces tenants.default_quotas.max_team_members.
	memberQuota func(workspace string, current int) error
	// onRevokeUser lets an embedding OIDC/LDAP authenticator end the
	// sessions it keeps itself when an account loses access. Called with
	// a.mu held.
	onRevokeUser func(userID string)
	// credIndex maps each normalized email that can sign in with a password
	// to its bcrypt hash. It is an immutable snapshot rebuilt after every
	// mutation, so Login and RequestPasswordReset reject unknown addresses
	// without touching a.mu (see TestLoginRejectionPathsDoNotHoldAuthLock).
	credIndex atomic.Pointer[map[string]string]
}

type resetToken struct {
	Email     string    `json:"email"`
	ExpiresAt time.Time `json:"expires_at"`
}

// loginAttempt is exported via JSON tags so the field names persist into
// the on-disk session file (MED-13 — lockout state survives restart).
type loginAttempt struct {
	Count       int       `json:"count"`
	LastTry     time.Time `json:"last_try"`
	LockedUntil time.Time `json:"locked_until"`
}

type session struct {
	UserID    string    `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

// sessionData represents the data persisted to disk
type sessionData struct {
	Tokens      map[string]*session      `json:"tokens"`
	Users       map[string]*api.User     `json:"users"`
	ResetTokens map[string]*resetToken   `json:"reset_tokens"`
	Lockouts    map[string]*loginAttempt `json:"lockouts,omitempty"`
	// AccountBackoff and KnownLoginIPs persist the account-wide backoff and
	// the sources exempt from it, so a restart neither resets the backoff nor
	// forgets the user's own address.
	AccountBackoff map[string]*loginAttempt `json:"account_backoff,omitempty"`
	KnownLoginIPs  map[string]time.Time     `json:"known_login_ips,omitempty"`
	// AdminEmail/AdminPasswordHash persist a password changed at runtime
	// (change-password, reset, `anubis admin reset-password`). Without them a
	// restart silently reverted to the config file's password.
	AdminEmail        string `json:"admin_email,omitempty"`
	AdminPasswordHash string `json:"admin_password_hash,omitempty"`
	// AdminSeedHash is a bcrypt hash of the config-file password in effect
	// when AdminPasswordHash was set, so rotating the password in the config
	// file still takes effect.
	AdminSeedHash string `json:"admin_seed_hash,omitempty"`
	// Accounts and Invitations hold the multi-user directory (users.go).
	// Files written before it have neither; loading migrates them.
	Accounts    map[string]*accountRecord    `json:"accounts,omitempty"`
	Invitations map[string]*invitationRecord `json:"invitations,omitempty"`
}

// NewLocalAuthenticator creates a new local authenticator.
// If sessionPath is provided, sessions are persisted to disk.
// adminPassword should be plaintext; it will be hashed internally.
//
// Returns an error if the password fails policy validation (K6) or if
// bcrypt hashing fails. Previously these conditions caused the whole
// process to panic, which made the server refuse to start in containers
// with limited entropy and made unit tests flaky when a weak test
// password was supplied by mistake.
func NewLocalAuthenticator(sessionPath, adminEmail, adminPassword string) (*LocalAuthenticator, error) {
	// Hash the password if it's not already hashed
	var passwordHash string
	if adminPassword != "" {
		// Check if password is already bcrypt hashed
		if strings.HasPrefix(adminPassword, "$2a$") || strings.HasPrefix(adminPassword, "$2b$") || strings.HasPrefix(adminPassword, "$2y$") {
			passwordHash = adminPassword
		} else {
			// MED-12: Enforce minimum password policy before hashing
			if err := validatePassword(adminPassword); err != nil {
				return nil, fmt.Errorf("admin password policy violation: %w", err)
			}
			// Hash the plaintext password. bcrypt at cost 12 takes
			// ~250ms on modern hardware; a hash failure is a real
			// problem (out of memory, bcrypt module panic) but should
			// be surfaced to the caller, not crash the process.
			hash, err := bcrypt.GenerateFromPassword([]byte(adminPassword), bcryptCost)
			if err != nil {
				return nil, fmt.Errorf("failed to hash admin password: %w", err)
			}
			passwordHash = string(hash)
		}
	}

	a := &LocalAuthenticator{
		tokens:            make(map[string]*session),
		users:             make(map[string]*api.User),
		adminEmail:        adminEmail,
		adminPasswordHash: passwordHash,
		configSeedHash:    passwordHash,
		configPassword:    adminPassword,
		sessionPath:       sessionPath,
		stopCleanup:       make(chan struct{}),
		cleanupDone:       make(chan struct{}),
		loginAttempts:     make(map[string]*loginAttempt),
		accountFailures:   make(map[string]*loginAttempt),
		knownLoginIPs:     make(map[string]time.Time),
		resetTokens:       make(map[string]*resetToken),
		accounts:          make(map[string]*accountRecord),
		invitations:       make(map[string]*invitationRecord),
	}

	// Load persisted state before seeding the admin account. A persisted user
	// record is the account of record, and its ID is what workspace ownership is
	// keyed by (internal/api/rest.go canAccessWorkspace), so it must win over a
	// freshly minted identity.
	if sessionPath != "" {
		a.loadSessions()
	}
	a.configPassword = ""

	// Create the admin user when credentials were provided and persisted state
	// does not already represent that account. Minting a new identity on every
	// boot would change api.User.ID across restarts, orphaning every workspace
	// the account owns.
	if adminEmail != "" && adminPassword != "" && !a.hasUserWithEmail(adminEmail) {
		adminID, err := generateID()
		if err != nil {
			return nil, fmt.Errorf("failed to generate admin user ID: %w", err)
		}
		adminUser := &api.User{
			ID:        adminID,
			Email:     adminEmail,
			Name:      "Administrator",
			Role:      "admin",
			Workspace: "default",
			CreatedAt: time.Now(),
		}
		a.users[adminUser.ID] = adminUser
	}
	a.migrateDirectoryLocked()
	a.refreshCredIndexLocked()

	// Start background cleanup goroutine
	// The interval is read here, not in the goroutine, so tests that tune it
	// never race with a janitor that starts late.
	go a.cleanupExpiredSessions(cleanupInterval)

	return a, nil
}

// loadSessions loads sessions and users from disk
func (a *LocalAuthenticator) loadSessions() {
	if a.sessionPath == "" {
		return
	}

	data, err := os.ReadFile(a.sessionPath)
	if err != nil {
		// File doesn't exist yet, start fresh
		return
	}

	var sessionData sessionData
	if err := json.Unmarshal(data, &sessionData); err != nil {
		// Corrupted file, start fresh
		return
	}

	// A password changed at runtime survives restarts, but only while the
	// config file still holds the password it replaced: rotating the password
	// in the config (or ANUBIS_ADMIN_PASSWORD) takes effect on the next start.
	// It is also bound to the email, so renaming the admin starts over.
	if sessionData.AdminPasswordHash != "" && sessionData.AdminEmail == a.adminEmail &&
		configPasswordMatchesSeed(sessionData.AdminSeedHash, a.configPassword) {
		a.adminPasswordHash = sessionData.AdminPasswordHash
		if sessionData.AdminSeedHash != "" {
			a.configSeedHash = sessionData.AdminSeedHash
		}
	}

	// Restore persisted accounts independently of their sessions. An account
	// outlives its session, and its ID is what workspace ownership is keyed by,
	// so dropping it here would orphan the account's workspaces on every restart.
	for id, user := range sessionData.Users {
		if user == nil {
			continue
		}
		a.users[id] = user
	}
	for id, rec := range sessionData.Accounts {
		if rec != nil && sessionData.Users[id] != nil {
			if rec.Memberships == nil {
				rec.Memberships = make(map[string]core.MemberRole)
			}
			a.accounts[id] = rec
		}
	}
	now0 := time.Now()
	for id, inv := range sessionData.Invitations {
		if inv != nil && now0.Before(inv.ExpiresAt) {
			a.invitations[id] = inv
		}
	}

	// Only load non-expired sessions
	now := time.Now()
	for token, sess := range sessionData.Tokens {
		if sess == nil {
			continue
		}
		if now.Before(sess.ExpiresAt) {
			a.tokens[token] = sess
		}
	}
	// Load non-expired reset tokens
	for token, rt := range sessionData.ResetTokens {
		if rt == nil {
			continue
		}
		if now.Before(rt.ExpiresAt) {
			a.resetTokens[token] = rt
		}
	}
	// MED-13: restore lockouts so brute-force protection survives restart.
	// Drop entries whose lockout has already expired and whose last attempt
	// is outside the reset window — they no longer have effect.
	if len(sessionData.Lockouts) > 0 {
		a.attemptsMu.Lock()
		for email, att := range sessionData.Lockouts {
			if att == nil {
				continue
			}
			lockoutLive := !att.LockedUntil.IsZero() && now.Before(att.LockedUntil)
			withinResetWindow := !att.LastTry.IsZero() && now.Sub(att.LastTry) <= attemptResetWindow
			if lockoutLive || withinResetWindow {
				a.loginAttempts[email] = att
			}
		}
		a.attemptsMu.Unlock()
	}
	a.attemptsMu.Lock()
	for email, att := range sessionData.AccountBackoff {
		if att == nil {
			continue
		}
		if (!att.LockedUntil.IsZero() && now.Before(att.LockedUntil)) ||
			(!att.LastTry.IsZero() && now.Sub(att.LastTry) <= attemptResetWindow) {
			a.accountFailures[email] = att
		}
	}
	for key, at := range sessionData.KnownLoginIPs {
		if now.Sub(at) <= knownLoginIPTTL {
			a.knownLoginIPs[key] = at
		}
	}
	a.attemptsMu.Unlock()
}

// configPasswordMatchesSeed reports whether the configured password is the
// one recorded as the seed. An empty seed (nothing recorded) matches.
func configPasswordMatchesSeed(seed, configured string) bool {
	if seed == "" {
		return true
	}
	if configured == "" {
		return false
	}
	if strings.HasPrefix(configured, "$2a$") || strings.HasPrefix(configured, "$2b$") || strings.HasPrefix(configured, "$2y$") {
		return configured == seed
	}
	return bcrypt.CompareHashAndPassword([]byte(seed), []byte(configured)) == nil
}

// hasUserWithEmail reports whether an account with this email is already known.
// It is called from the constructor before the authenticator is shared, so it
// does not take a.mu.
func (a *LocalAuthenticator) hasUserWithEmail(email string) bool {
	for _, user := range a.users {
		if user != nil && user.Email == email {
			return true
		}
	}
	return false
}

// saveSessionsJSON is overwritten by tests to inject marshal failures.
var saveSessionsJSON = json.Marshal

// saveSessionsLocked persists sessions and users to disk
// Must be called with a.mu held (at least RLock)
func (a *LocalAuthenticator) saveSessionsLocked() error {
	if a.sessionPath == "" {
		return nil
	}

	// Snapshot lockouts under attemptsMu so the persist path doesn't race
	// with concurrent recordFailedAttempt/clearFailedAttempts. We don't
	// hold attemptsMu while writing to disk — only long enough to copy
	// the map pointers, since loginAttempt values are mutated in place.
	a.attemptsMu.RLock()
	lockoutsCopy := make(map[string]*loginAttempt, len(a.loginAttempts))
	for email, att := range a.loginAttempts {
		if att == nil {
			continue
		}
		// Shallow value copy so a subsequent mutation can't race the marshal.
		dup := *att
		lockoutsCopy[email] = &dup
	}
	var backoffCopy map[string]*loginAttempt
	if len(a.accountFailures) > 0 {
		backoffCopy = make(map[string]*loginAttempt, len(a.accountFailures))
		for email, att := range a.accountFailures {
			if att == nil {
				continue
			}
			dup := *att
			backoffCopy[email] = &dup
		}
	}
	var knownCopy map[string]time.Time
	if len(a.knownLoginIPs) > 0 {
		knownCopy = maps.Clone(a.knownLoginIPs)
	}
	a.attemptsMu.RUnlock()

	data := sessionData{
		Tokens:            a.tokens,
		Users:             a.users,
		ResetTokens:       a.resetTokens,
		Lockouts:          lockoutsCopy,
		AccountBackoff:    backoffCopy,
		KnownLoginIPs:     knownCopy,
		AdminEmail:        a.adminEmail,
		AdminPasswordHash: a.adminPasswordHash,
		AdminSeedHash:     a.configSeedHash,
		Accounts:          a.accounts,
		Invitations:       a.invitations,
	}

	jsonData, err := saveSessionsJSON(data)
	if err != nil {
		return fmt.Errorf("persist authentication state: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(a.sessionPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("persist authentication state: %w", err)
	}

	// Write atomically using temp file
	tmpPath := a.sessionPath + ".tmp"
	file, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("persist authentication state: %w", err)
	}
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(jsonData)
	}
	if err == nil {
		err = sessionFileSync(file)
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("persist authentication state: %w", err)
	}
	if err := os.Rename(tmpPath, a.sessionPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("persist authentication state: %w", err)
	}
	if err := sessionDirectorySync(dir); err != nil {
		return &authPersistenceError{err: fmt.Errorf("authentication state renamed but directory sync failed: %w", err), committed: true}
	}
	return nil
}

// A post-rename error reports uncertain crash durability. Keep the new memory
// state in that case because the pathname already names the new file.
type authPersistenceError struct {
	err       error
	committed bool
}

func (e *authPersistenceError) Error() string { return e.err.Error() }
func (e *authPersistenceError) Unwrap() error { return e.err }

var sessionFileSync = (*os.File).Sync
var sessionDirectorySync = syncSessionDirectory

// rollbackStateLocked snapshots the fields changed by security-sensitive mutations.
// Callers keep a.mu held through persistence and rollback.
func (a *LocalAuthenticator) rollbackStateLocked() func() {
	tokens, resets := maps.Clone(a.tokens), maps.Clone(a.resetTokens)
	users := make(map[string]*api.User, len(a.users))
	for id, u := range a.users {
		users[id] = copyUser(u)
	}
	hash := a.adminPasswordHash
	accounts := make(map[string]*accountRecord, len(a.accounts))
	for id, rec := range a.accounts {
		accounts[id] = rec.clone()
	}
	invitations := maps.Clone(a.invitations)
	return func() {
		a.tokens = tokens
		a.resetTokens = resets
		a.users = users
		a.adminPasswordHash = hash
		a.accounts = accounts
		a.invitations = invitations
	}
}
func (a *LocalAuthenticator) persistOrRollbackLocked(rollback func()) error {
	defer a.refreshCredIndexLocked()
	if err := a.saveSessionsLocked(); err != nil {
		var persisted *authPersistenceError
		if !errors.As(err, &persisted) || !persisted.committed {
			rollback()
		}
		return err
	}
	return nil
}

// cleanupInterval is the interval between session cleanup runs.
// Tests can shorten this to verify the cleanup loop without waiting 5 minutes.
var cleanupInterval = 5 * time.Minute

// cleanupExpiredSessions removes expired sessions periodically
func (a *LocalAuthenticator) cleanupExpiredSessions(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer close(a.cleanupDone)

	for {
		select {
		case <-ticker.C:
			a.mu.Lock()
			now := time.Now()
			for token, sess := range a.tokens {
				if now.After(sess.ExpiresAt) {
					delete(a.tokens, token)
				}
			}
			// Reset tokens expire too. Leaving them in place would let an
			// unauthenticated caller (POST /api/v1/auth/reset-password) grow this
			// map, and the file it is persisted to, for the lifetime of the
			// process: an entry otherwise leaves only when it is redeemed.
			for token, rt := range a.resetTokens {
				if now.After(rt.ExpiresAt) {
					delete(a.resetTokens, token)
				}
			}
			if err := a.saveSessionsLocked(); err != nil {
				slog.Warn("failed to persist session cleanup", "error", err)
			}
			a.mu.Unlock()

		case <-a.stopCleanup:
			a.mu.Lock()
			if err := a.saveSessionsLocked(); err != nil {
				slog.Warn("failed to persist sessions during shutdown", "error", err)
			}
			a.mu.Unlock()
			return
		}
	}
}

// Shutdown gracefully stops the authenticator. Idempotent — t.Cleanup
// hooks plus deferred Shutdown() calls in tests can both fire safely.
// Mirrors the LDAP and OIDC shutdown discipline.
func (a *LocalAuthenticator) Shutdown() {
	a.stopOnce.Do(func() {
		close(a.stopCleanup)
		<-a.cleanupDone
	})
}

// Authenticate validates a token and returns the user
func (a *LocalAuthenticator) Authenticate(token string) (*api.User, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	sess, ok := a.tokens[token]
	if !ok {
		return nil, errors.New("invalid token")
	}

	if time.Now().After(sess.ExpiresAt) {
		delete(a.tokens, token)
		if err := a.saveSessionsLocked(); err != nil {
			slog.Warn("failed to persist expired session removal", "error", err)
		}
		return nil, errors.New("token expired")
	}

	user := a.users[sess.UserID]
	if user == nil {
		return nil, errors.New("user not found")
	}

	return a.resolveLocked(user)
}

// Login creates a new session and returns a token. Failures are booked
// against an unknown client address; the REST layer calls LoginFrom so
// they are keyed by the caller's IP instead.
func (a *LocalAuthenticator) Login(email, password string) (*api.User, string, error) {
	return a.LoginFrom(email, password, "")
}

// LoginFrom is Login for a caller at clientIP. Brute-force protection keys
// failed attempts by (account, clientIP), so guessing from one address locks
// out only that address; see checkBruteForceProtectionFrom.
func (a *LocalAuthenticator) LoginFrom(email, password, clientIP string) (*api.User, string, error) {
	// Accounts are matched case-insensitively, and failures are booked under
	// the same normalized address, so "Admin@x" cannot sidestep a lockout.
	email = normalizeEmail(email)

	// Check brute force protection first
	if err := a.checkBruteForceProtectionFrom(email, clientIP); err != nil {
		return nil, "", err
	}

	// An empty password does the same work for every address, so the response time
	// cannot reveal whether the address is an account.
	if password == "" {
		dummyHash, _ := bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing"), bcrypt.DefaultCost)
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		a.mu.Lock()
		a.recordFailedAttemptFrom(email, clientIP)
		a.mu.Unlock()
		return nil, "", errors.New("invalid credentials")
	}

	// Snapshot the credential under the lock, then verify it outside: bcrypt at
	// the configured cost takes hundreds of milliseconds and every
	// authenticated request needs that lock.
	expectedHash := a.lookupCredential(email)

	if expectedHash == "" {
		// MED-11: Run dummy bcrypt comparison to prevent timing-based user enumeration
		// This ensures the response time is consistent whether the email exists or not
		dummyHash, _ := bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing"), bcrypt.DefaultCost)
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return nil, "", errors.New("invalid credentials")
	}

	compare := a.comparePassword
	if compare == nil {
		compare = bcrypt.CompareHashAndPassword
	}
	if compare([]byte(expectedHash), []byte(password)) != nil {
		a.mu.Lock()
		a.recordFailedAttemptFrom(email, clientIP)
		a.mu.Unlock()
		return nil, "", errors.New("invalid credentials")
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	rollback := a.rollbackStateLocked()

	// The password was verified outside the lock, so re-check it here: a rotation
	// that interleaved must not let the previous password open a session that
	// outlives it.
	if a.passwordHashForLocked(email) != expectedHash {
		return nil, "", errors.New("invalid credentials")
	}

	user := a.findByEmailLocked(email)
	if user == nil {
		// Only the configured admin can get here without an identity: create
		// it.
		newID, err := generateID()
		if err != nil {
			return nil, "", err
		}
		user = &api.User{
			ID:        newID,
			Email:     a.adminEmail,
			Name:      "Administrator",
			Role:      "admin",
			Workspace: "default",
			CreatedAt: time.Now(),
		}
		a.users[user.ID] = user
		a.migrateDirectoryLocked()
	}
	resolved, err := a.resolveLocked(user)
	if err != nil {
		// Disabled, or no workspace left. The password was right, so this is
		// not a failed guess; it still answers like one.
		rollback()
		return nil, "", errors.New("invalid credentials")
	}

	// Clear failed attempts on successful login
	a.clearFailedAttemptsFrom(email, clientIP)
	if rec := a.accounts[user.ID]; rec != nil {
		now := time.Now().UTC()
		rec.LastLoginAt = &now
	}

	// Generate token
	token, err := generateToken()
	if err != nil {
		return nil, "", err
	}
	a.capSessionsLocked(user.ID, time.Now())
	a.tokens[token] = &session{
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}

	// Persist sessions if configured
	if err := a.persistOrRollbackLocked(rollback); err != nil {
		return nil, "", err
	}

	return resolved, token, nil
}

// refreshCredIndexLocked rebuilds credIndex. Caller holds a.mu.
func (a *LocalAuthenticator) refreshCredIndexLocked() {
	idx := make(map[string]string, len(a.accounts)+1)
	for id, rec := range a.accounts {
		if u := a.users[id]; u != nil && rec != nil && rec.source() == SourceLocal && rec.PasswordHash != "" {
			idx[normalizeEmail(u.Email)] = rec.PasswordHash
		}
	}
	if a.adminEmail != "" && a.adminPasswordHash != "" {
		idx[normalizeEmail(a.adminEmail)] = a.adminPasswordHash
	}
	a.credIndex.Store(&idx)
}

// lookupCredential returns the hash email signs in with, from credIndex,
// without taking a.mu.
func (a *LocalAuthenticator) lookupCredential(email string) string {
	idx := a.credIndex.Load()
	if idx == nil {
		return ""
	}
	return (*idx)[normalizeEmail(email)]
}

// capSessionsLocked makes room for one more session of userID: expired
// sessions go first, then the account's oldest beyond maxSessionsPerUser, then
// the server's oldest beyond maxSessions. Evicting rather than refusing keeps
// sign-in available however many sessions accumulate. Caller holds a.mu.
func (a *LocalAuthenticator) capSessionsLocked(userID string, now time.Time) {
	mine := 0
	for t, sess := range a.tokens {
		if sess == nil || now.After(sess.ExpiresAt) {
			delete(a.tokens, t)
		} else if sess.UserID == userID {
			mine++
		}
	}
	evictOldest := func(match func(*session) bool) {
		oldest, at := "", time.Time{}
		for t, sess := range a.tokens {
			if match(sess) && (oldest == "" || sess.ExpiresAt.Before(at)) {
				oldest, at = t, sess.ExpiresAt
			}
		}
		delete(a.tokens, oldest)
	}
	for ; mine >= maxSessionsPerUser; mine-- {
		evictOldest(func(s *session) bool { return s.UserID == userID })
	}
	for len(a.tokens) >= maxSessions {
		evictOldest(func(*session) bool { return true })
	}
}

// passwordHashForLocked returns the bcrypt hash email signs in with, or ""
// when it cannot sign in with a password here. The configured admin's is
// AdminPasswordHash; other local accounts carry their own.
func (a *LocalAuthenticator) passwordHashForLocked(email string) string {
	if sameEmail(email, a.adminEmail) {
		return a.adminPasswordHash
	}
	user := a.findByEmailLocked(email)
	if user == nil {
		return ""
	}
	rec := a.accounts[user.ID]
	if rec == nil || rec.source() != SourceLocal {
		return ""
	}
	return rec.PasswordHash
}

// Logout invalidates a token
// copyUser returns a value the caller owns. Callers keep the *api.User they get
// from Authenticate/Login/SwitchWorkspace for the life of a request and read its
// fields without holding a.mu, while SwitchWorkspace rewrites the stored account
// in place -- handing out the stored pointer would race with that write, and a
// torn Workspace string is what request scoping is derived from. api.User is a
// flat value struct, so a shallow copy is a full copy.
func copyUser(u *api.User) *api.User {
	if u == nil {
		return nil
	}
	c := *u
	return &c
}

func (a *LocalAuthenticator) Logout(token string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	rollback := a.rollbackStateLocked()

	delete(a.tokens, token)
	if err := a.persistOrRollbackLocked(rollback); err != nil {
		return err
	}
	return nil
}

// SwitchWorkspace updates the authenticated user's active workspace for this
// token's user. Authorization for the target workspace is enforced by the API
// layer before this mutates auth state.
func (a *LocalAuthenticator) SwitchWorkspace(token, workspace string) (*api.User, error) {
	if workspace == "" {
		return nil, errors.New("workspace is required")
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	rollback := a.rollbackStateLocked()

	sess, ok := a.tokens[token]
	if !ok {
		return nil, errors.New("invalid token")
	}
	if time.Now().After(sess.ExpiresAt) {
		delete(a.tokens, token)
		if err := a.saveSessionsLocked(); err != nil {
			slog.Warn("failed to persist expired session removal", "error", err)
		}
		return nil, errors.New("token expired")
	}

	user := a.users[sess.UserID]
	if user == nil {
		return nil, errors.New("user not found")
	}
	user.Workspace = workspace
	resolved, err := a.resolveLocked(user)
	if err != nil {
		rollback()
		return nil, err
	}
	if err := a.persistOrRollbackLocked(rollback); err != nil {
		return nil, err
	}
	return resolved, nil
}

// generateToken returns a 256-bit hex-encoded random token.
// Go 1.26 made crypto/rand.Read runtime-fatal on error, so we no longer
// propagate an error — the CSPRNG is guaranteed available or the process
// terminates (see https://go.dev/issue/66821).
func generateToken() (string, error) {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b), nil
}

// generateID returns a 128-bit hex-encoded random ID.
// See generateToken for error-handling rationale.
func generateID() (string, error) {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b), nil
}

// Brute force protection functions
const (
	defaultBcryptCost  = 12
	maxLoginAttempts   = 5 // failures per (account, client IP) before that source is locked
	lockoutDuration    = 15 * time.Minute
	attemptResetWindow = 30 * time.Minute
	minPasswordLength  = 12

	// accountBackoffThreshold is the number of failures for one account,
	// across all client addresses, within attemptResetWindow after which every
	// further failure doubles the account's backoff, from accountBackoffBase
	// up to accountBackoffMax. Addresses the account recently signed in from
	// are exempt (knownLoginIPTTL).
	accountBackoffThreshold = 50
	accountBackoffBase      = time.Second
	accountBackoffMax       = lockoutDuration
	knownLoginIPTTL         = 30 * 24 * time.Hour
	maxKnownLoginIPs        = 64
	// maxTrackedLoginSources bounds the per-source table (and the session
	// file it is persisted to) against an attacker cycling addresses.
	maxTrackedLoginSources = 4096

	// Bounds on the authentication state, which in a cluster is one Raft
	// entry of at most MaxClusterAuthStateBytes: a signed-in account keeps
	// its maxSessionsPerUser newest sessions, the server its maxSessions
	// newest, and at most maxResetTokens reset tokens (one per account).
	maxSessionsPerUser = 10
	maxSessions        = 200
	maxResetTokens     = 20
)

var bcryptCost = defaultBcryptCost

// validatePassword enforces minimum password policy (MED-12)
func validatePassword(password string) error {
	if len(password) < minPasswordLength {
		return fmt.Errorf("password must be at least %d characters", minPasswordLength)
	}
	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, r := range password {
		switch {
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= 'a' && r <= 'z':
			hasLower = true
		case r >= '0' && r <= '9':
			hasDigit = true
		default:
			hasSpecial = true
		}
	}
	classes := 0
	if hasUpper {
		classes++
	}
	if hasLower {
		classes++
	}
	if hasDigit {
		classes++
	}
	if hasSpecial {
		classes++
	}
	if classes < 3 {
		return fmt.Errorf("password must contain at least 3 of: uppercase, lowercase, digits, special characters")
	}
	return nil
}

// loginSourceKey names the (account, client IP) pair failed attempts are
// counted against. An unknown address keys by the account alone, which is
// what callers without a client address (Login, the CLI) always used.
func loginSourceKey(email, clientIP string) string {
	if clientIP == "" {
		return email
	}
	return email + "|" + clientIP
}

// checkBruteForceProtectionFrom refuses a login attempt when
//   - the (account, clientIP) source is locked after maxLoginAttempts
//     failures, or
//   - the account as a whole is in its exponential backoff after
//     accountBackoffThreshold failures from any source, unless clientIP has
//     signed in to the account successfully within knownLoginIPTTL.
//
// The first stops guessing from one address; the second bounds distributed
// guessing without letting anyone lock the owner out of their usual address.
func (a *LocalAuthenticator) checkBruteForceProtectionFrom(email, clientIP string) error {
	a.attemptsMu.Lock()
	defer a.attemptsMu.Unlock()
	now := time.Now()

	if attempt := a.loginAttempts[loginSourceKey(email, clientIP)]; attempt != nil {
		// Check if this source is locked
		if !attempt.LockedUntil.IsZero() && now.Before(attempt.LockedUntil) {
			remaining := attempt.LockedUntil.Sub(now)
			return errors.New("account temporarily locked due to failed login attempts. Try again in " + remaining.String())
		}
		// Reset lock if expired
		if !attempt.LockedUntil.IsZero() && now.After(attempt.LockedUntil) {
			attempt.LockedUntil = time.Time{}
			attempt.Count = 0
		}
	}

	if acct := a.accountFailures[email]; acct != nil && now.Before(acct.LockedUntil) && !a.knownLoginIPLocked(email, clientIP, now) {
		remaining := acct.LockedUntil.Sub(now)
		return errors.New("account temporarily rate limited due to failed login attempts. Try again in " + remaining.String())
	}
	return nil
}

// knownLoginIPLocked reports whether clientIP signed in to email recently.
// Caller holds attemptsMu.
func (a *LocalAuthenticator) knownLoginIPLocked(email, clientIP string, now time.Time) bool {
	if clientIP == "" {
		return false
	}
	at, ok := a.knownLoginIPs[loginSourceKey(email, clientIP)]
	return ok && now.Sub(at) <= knownLoginIPTTL
}

// recordFailedAttemptFrom records a failed login attempt.
// MED-13: persists the updated lockout map so brute-force state survives
// process restart. Callers must hold a.mu so saveSessionsLocked can read
// sessions/users safely; attemptsMu is acquired and released here.
func (a *LocalAuthenticator) recordFailedAttemptFrom(email, clientIP string) {
	// Only the configured account can authenticate -- Login rejects every other
	// address before any password check -- so an attempt against another address
	// can never affect a lockout decision. Recording it would let an
	// unauthenticated caller grow this store, and the file it is persisted to, by
	// choosing addresses freely.
	if !a.knownEmailLocked(email) {
		return
	}

	now := time.Now()
	a.attemptsMu.Lock()
	key := loginSourceKey(email, clientIP)
	attempt, exists := a.loginAttempts[key]
	if !exists && len(a.loginAttempts) >= maxTrackedLoginSources {
		a.pruneLoginAttemptsLocked(now)
	}
	// With the source table still full (an attacker cycling addresses), the
	// failure is booked only against the account-wide counter below.
	if !exists && len(a.loginAttempts) < maxTrackedLoginSources {
		attempt = &loginAttempt{}
		a.loginAttempts[key] = attempt
	}
	if attempt != nil {
		// Reset count if too much time has passed
		if now.Sub(attempt.LastTry) > attemptResetWindow {
			attempt.Count = 0
		}
		attempt.Count++
		attempt.LastTry = now
		// Lock this source if max attempts reached
		if attempt.Count >= maxLoginAttempts {
			attempt.LockedUntil = now.Add(lockoutDuration)
		}
	}

	acct := a.accountFailures[email]
	if acct == nil {
		acct = &loginAttempt{}
		a.accountFailures[email] = acct
	}
	if now.Sub(acct.LastTry) > attemptResetWindow {
		acct.Count = 0
		acct.LockedUntil = time.Time{}
	}
	acct.Count++
	acct.LastTry = now
	if over := acct.Count - accountBackoffThreshold; over >= 0 {
		delay := accountBackoffMax
		if over < 20 {
			delay = min(accountBackoffBase<<over, accountBackoffMax)
		}
		acct.LockedUntil = now.Add(delay)
	}
	a.attemptsMu.Unlock()

	// Persist after releasing attemptsMu so saveSessionsLocked can re-acquire
	// its RLock without re-entrancy.
	if err := a.saveSessionsLocked(); err != nil {
		slog.Warn("failed to persist login lockout state", "error", err)
	}
}

// pruneLoginAttemptsLocked drops per-source entries that no longer affect a
// decision. Caller holds attemptsMu.
func (a *LocalAuthenticator) pruneLoginAttemptsLocked(now time.Time) {
	for key, att := range a.loginAttempts {
		if att == nil || (now.After(att.LockedUntil) && now.Sub(att.LastTry) > attemptResetWindow) {
			delete(a.loginAttempts, key)
		}
	}
}

// clearFailedAttemptsFrom clears the failures of the source that just signed
// in successfully and remembers it as a known address of the account.
// MED-13: also persists so a successful login removes the lockout entry
// from the on-disk session file. The account-wide counter is left to decay:
// one successful login must not hand an ongoing distributed guess a fresh
// budget.
func (a *LocalAuthenticator) clearFailedAttemptsFrom(email, clientIP string) {
	now := time.Now()
	a.attemptsMu.Lock()
	delete(a.loginAttempts, loginSourceKey(email, clientIP))
	if clientIP != "" {
		if len(a.knownLoginIPs) >= maxKnownLoginIPs {
			a.evictKnownLoginIPLocked(now)
		}
		a.knownLoginIPs[loginSourceKey(email, clientIP)] = now
	}
	a.attemptsMu.Unlock()

	if err := a.saveSessionsLocked(); err != nil {
		slog.Warn("failed to persist cleared login lockout", "error", err)
	}
}

// evictKnownLoginIPLocked drops expired known addresses and, if none
// expired, the least recently used one. Caller holds attemptsMu.
func (a *LocalAuthenticator) evictKnownLoginIPLocked(now time.Time) {
	oldestKey, oldest := "", now
	for key, at := range a.knownLoginIPs {
		if now.Sub(at) > knownLoginIPTTL {
			delete(a.knownLoginIPs, key)
			continue
		}
		if !at.After(oldest) {
			oldestKey, oldest = key, at
		}
	}
	if len(a.knownLoginIPs) >= maxKnownLoginIPs && oldestKey != "" {
		delete(a.knownLoginIPs, oldestKey)
	}
}

// HashPassword is a helper function to hash a password for storage
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", errors.New("password cannot be empty")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyPassword reports whether password matches a stored bcrypt hash. It is the
// counterpart to HashPassword, works with hashes produced at any bcrypt cost, and
// treats an empty hash or password as "cannot match" rather than as a comparison.
func VerifyPassword(hash, password string) bool {
	if hash == "" || password == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// setPasswordHashLocked stores hash as user's password: AdminPasswordHash
// for the configured admin, the account record for anyone else. It also
// clears a pending forced change.
func (a *LocalAuthenticator) setPasswordHashLocked(user *api.User, hash string) {
	rec := a.accounts[user.ID]
	if a.isConfiguredAdminLocked(user) {
		a.adminPasswordHash = hash
	} else if rec != nil {
		rec.PasswordHash = hash
	}
	if rec != nil {
		rec.MustChangePassword = false
		rec.UpdatedAt = time.Now().UTC()
	}
}

// revokeCredentialsLocked ends user's sessions and outstanding reset
// tokens: each reset token can set the password without knowing the
// current one, so none may outlive the password it was issued against.
func (a *LocalAuthenticator) revokeCredentialsLocked(user *api.User) {
	a.revokeUserSessionsLocked(user.ID)
	for token, rt := range a.resetTokens {
		if rt != nil && sameEmail(rt.Email, user.Email) {
			delete(a.resetTokens, token)
		}
	}
}

// ChangePassword validates the current password and sets a new one.
// All of the account's sessions are invalidated on success.
func (a *LocalAuthenticator) ChangePassword(token, currentPassword, newPassword string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	rollback := a.rollbackStateLocked()

	// Validate token
	sess, ok := a.tokens[token]
	if !ok {
		return errors.New("invalid token")
	}
	if time.Now().After(sess.ExpiresAt) {
		delete(a.tokens, token)
		return errors.New("token expired")
	}
	user := a.users[sess.UserID]
	if user == nil {
		return errors.New("user not found")
	}

	// Verify current password
	currentHash := a.passwordHashForLocked(user.Email)
	if currentHash == "" {
		return errors.New("this account has no local password to change")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(currentHash), []byte(currentPassword)); err != nil {
		return errors.New("current password is incorrect")
	}

	// Validate new password policy
	if err := validatePassword(newPassword); err != nil {
		return fmt.Errorf("new password does not meet policy: %w", err)
	}

	// Hash and set new password
	newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return fmt.Errorf("failed to hash new password: %w", err)
	}
	a.setPasswordHashLocked(user, string(newHash))

	// Invalidate the account's sessions (force re-login) and reset tokens.
	a.revokeCredentialsLocked(user)
	if err := a.persistOrRollbackLocked(rollback); err != nil {
		return err
	}

	return nil
}

// RequestPasswordReset generates a time-limited reset token for the given
// email when it belongs to an enabled local account; otherwise it returns
// "" after the same work, so the answer does not reveal which addresses
// exist.
func (a *LocalAuthenticator) RequestPasswordReset(email string) (string, error) {
	token, _, err := a.RequestPasswordResetTo(email)
	return token, err
}

// RequestPasswordResetTo is RequestPasswordReset that also returns the
// account's stored address, which is where the link must be sent: the typed
// address only matches it in canonical form. An account holds at most one
// reset token (a new request replaces it) and the server at most
// maxResetTokens, so unauthenticated requests cannot grow the (replicated)
// authentication state.
func (a *LocalAuthenticator) RequestPasswordResetTo(email string) (string, string, error) {
	// The lookup is lock-free; the equalisation bcrypt for unknown addresses
	// runs without the lock: holding it across bcrypt let an unauthenticated
	// caller stall every other request.
	if a.lookupCredential(email) == "" {
		// Run dummy bcrypt to prevent timing attacks (same as login)
		dummyHash, _ := bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing"), bcrypt.DefaultCost)
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte("dummy"))
		// Still return success to prevent email enumeration
		return "", "", nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	user := a.findByEmailLocked(email)
	if user == nil && a.passwordHashForLocked(email) != "" {
		// The configured admin before its first sign-in.
		user = &api.User{Email: a.adminEmail}
	}
	if user == nil || a.passwordHashForLocked(email) == "" || (a.accounts[user.ID] != nil && a.accounts[user.ID].Disabled) {
		return "", "", nil
	}
	rollback := a.rollbackStateLocked()

	now := time.Now()
	for t, rt := range a.resetTokens {
		if rt == nil || now.After(rt.ExpiresAt) || sameEmail(rt.Email, user.Email) {
			delete(a.resetTokens, t)
		}
	}
	for len(a.resetTokens) >= maxResetTokens {
		oldest, at := "", time.Time{}
		for t, rt := range a.resetTokens {
			if oldest == "" || rt.ExpiresAt.Before(at) {
				oldest, at = t, rt.ExpiresAt
			}
		}
		delete(a.resetTokens, oldest)
	}

	// Generate reset token
	token, err := generateToken()
	if err != nil {
		return "", "", err
	}
	a.resetTokens[token] = &resetToken{
		Email:     user.Email,
		ExpiresAt: now.Add(1 * time.Hour),
	}
	if err := a.persistOrRollbackLocked(rollback); err != nil {
		return "", "", err
	}

	// Emit to structured audit log; the token itself is never logged.
	slog.Info("password reset requested",
		slog.String("email", user.Email),
		slog.Time("expires", now.Add(1*time.Hour)))

	return token, user.Email, nil
}

// ConfirmPasswordReset validates a reset token and sets a new password.
// The account's sessions are invalidated on success.
func (a *LocalAuthenticator) ConfirmPasswordReset(token, newPassword string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	rollback := a.rollbackStateLocked()

	rt, ok := a.resetTokens[token]
	if !ok {
		return errors.New("invalid reset token")
	}
	if time.Now().After(rt.ExpiresAt) {
		delete(a.resetTokens, token)
		return errors.New("reset token expired")
	}

	// Validate new password policy
	if err := validatePassword(newPassword); err != nil {
		return fmt.Errorf("new password does not meet policy: %w", err)
	}

	// Hash and set new password
	newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return fmt.Errorf("failed to hash new password: %w", err)
	}
	user := a.findByEmailLocked(rt.Email)
	switch {
	case user != nil:
		a.setPasswordHashLocked(user, string(newHash))
		a.revokeCredentialsLocked(user)
	case sameEmail(rt.Email, a.adminEmail):
		// The configured admin before its first sign-in.
		a.adminPasswordHash = string(newHash)
	default:
		delete(a.resetTokens, token)
		return errors.New("invalid reset token")
	}

	// Delete used reset token
	delete(a.resetTokens, token)
	if err := a.persistOrRollbackLocked(rollback); err != nil {
		return err
	}

	return nil
}

// ResetLocalAdminPassword sets the local admin's password directly in the
// session file at sessionPath, for an operator with access to the data
// directory (`anubis admin reset-password`). The server must be stopped: a
// running server would overwrite the file on its next save. All sessions and
// pending reset tokens are revoked and the account's lockout is cleared.
func ResetLocalAdminPassword(sessionPath, adminEmail, newPassword string) error {
	if adminEmail == "" {
		return errors.New("admin email is required")
	}
	if err := validatePassword(newPassword); err != nil {
		return fmt.Errorf("new password does not meet policy: %w", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return fmt.Errorf("failed to hash new password: %w", err)
	}

	var data sessionData
	// #nosec G304 -- operator-supplied data directory.
	if raw, err := os.ReadFile(sessionPath); err == nil {
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("session file %s is not valid: %w", sessionPath, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	data.Tokens = nil
	data.ResetTokens = nil
	key := normalizeEmail(adminEmail)
	for k := range data.Lockouts {
		if k == adminEmail || k == key || strings.HasPrefix(k, key+"|") {
			delete(data.Lockouts, k)
		}
	}
	for k := range data.AccountBackoff {
		if k == adminEmail || k == key {
			delete(data.AccountBackoff, k)
		}
	}
	data.AdminEmail = adminEmail
	data.AdminPasswordHash = string(hash)

	out, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(sessionPath), 0750); err != nil {
		return err
	}
	tmp := sessionPath + ".tmp"
	if err := os.WriteFile(tmp, out, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, sessionPath); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Chmod(sessionPath, 0600)
}
