package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/api"
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
	// login attempts tracking for brute force protection
	loginAttempts map[string]*loginAttempt
	attemptsMu    sync.RWMutex
	// password reset tokens (protected by a.mu)
	resetTokens map[string]*resetToken
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
	// AdminEmail/AdminPasswordHash persist a password changed at runtime
	// (change-password, reset, `anubis admin reset-password`). Without them a
	// restart silently reverted to the config file's password.
	AdminEmail        string `json:"admin_email,omitempty"`
	AdminPasswordHash string `json:"admin_password_hash,omitempty"`
	// AdminSeedHash is a bcrypt hash of the config-file password in effect
	// when AdminPasswordHash was set, so rotating the password in the config
	// file still takes effect.
	AdminSeedHash string `json:"admin_seed_hash,omitempty"`
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
		resetTokens:       make(map[string]*resetToken),
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

	// Start background cleanup goroutine
	go a.cleanupExpiredSessions()

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
func (a *LocalAuthenticator) saveSessionsLocked() {
	if a.sessionPath == "" {
		return
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
	a.attemptsMu.RUnlock()

	data := sessionData{
		Tokens:            a.tokens,
		Users:             a.users,
		ResetTokens:       a.resetTokens,
		Lockouts:          lockoutsCopy,
		AdminEmail:        a.adminEmail,
		AdminPasswordHash: a.adminPasswordHash,
		AdminSeedHash:     a.configSeedHash,
	}

	jsonData, err := saveSessionsJSON(data)
	if err != nil {
		return
	}

	// Ensure directory exists
	dir := filepath.Dir(a.sessionPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return
	}

	// Write atomically using temp file
	tmpPath := a.sessionPath + ".tmp"
	if err := os.WriteFile(tmpPath, jsonData, 0600); err != nil {
		return
	}
	// Ensure permissions are set correctly before rename (race condition protection)
	if err := os.Chmod(tmpPath, 0600); err != nil {
		os.Remove(tmpPath)
		return
	}
	if err := os.Rename(tmpPath, a.sessionPath); err != nil {
		os.Remove(tmpPath)
		return
	}
	// Ensure final file has correct permissions (defense in depth).
	// A failed chmod leaves the file with the umask default; surfaced
	// nowhere because the authenticator has no logger — acceptable as
	// the write itself already used 0600 creation semantics.
	_ = os.Chmod(a.sessionPath, 0600)
}

// cleanupInterval is the interval between session cleanup runs.
// Tests can shorten this to verify the cleanup loop without waiting 5 minutes.
var cleanupInterval = 5 * time.Minute

// cleanupExpiredSessions removes expired sessions periodically
func (a *LocalAuthenticator) cleanupExpiredSessions() {
	ticker := time.NewTicker(cleanupInterval)
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
			a.saveSessionsLocked()
			a.mu.Unlock()

		case <-a.stopCleanup:
			a.mu.Lock()
			a.saveSessionsLocked()
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
		a.saveSessionsLocked()
		return nil, errors.New("token expired")
	}

	user := a.users[sess.UserID]
	if user == nil {
		return nil, errors.New("user not found")
	}

	return copyUser(user), nil
}

// Login creates a new session and returns a token
func (a *LocalAuthenticator) Login(email, password string) (*api.User, string, error) {
	// Check brute force protection first
	if err := a.checkBruteForceProtection(email); err != nil {
		return nil, "", err
	}

	// An empty password does the same work for every address, so the response time
	// cannot reveal whether the address is the configured account.
	if password == "" {
		dummyHash, _ := bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing"), bcrypt.DefaultCost)
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		a.mu.Lock()
		a.recordFailedAttempt(email)
		a.mu.Unlock()
		return nil, "", errors.New("invalid credentials")
	}

	// Anything that is not the configured account is rejected before the auth lock
	// is taken: adminEmail is immutable, this branch mutates nothing, and holding
	// the lock across the dummy bcrypt let an unauthenticated caller stall every
	// other request, because Authenticate needs the same lock.
	if subtle.ConstantTimeCompare([]byte(email), []byte(a.adminEmail)) != 1 {
		// MED-11: Run dummy bcrypt comparison to prevent timing-based user enumeration
		// This ensures the response time is consistent whether the email exists or not
		dummyHash, _ := bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing"), bcrypt.DefaultCost)
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return nil, "", errors.New("invalid credentials")
	}

	// Verify the password outside the auth lock: bcrypt at the configured cost
	// takes hundreds of milliseconds and every authenticated request needs that
	// lock. The credential is snapshotted first so the comparison has a stable
	// input.
	a.mu.Lock()
	expectedHash := a.adminPasswordHash
	a.mu.Unlock()

	if bcrypt.CompareHashAndPassword([]byte(expectedHash), []byte(password)) != nil {
		a.mu.Lock()
		a.recordFailedAttempt(email)
		a.mu.Unlock()
		return nil, "", errors.New("invalid credentials")
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// The password was verified outside the lock, so re-check it here: a rotation
	// that interleaved must not let the previous password open a session that
	// outlives it.
	if a.adminPasswordHash != expectedHash {
		return nil, "", errors.New("invalid credentials")
	}

	// Clear failed attempts on successful login
	a.clearFailedAttempts(email)

	// Get or create admin user
	var user *api.User
	for _, u := range a.users {
		if u.Email == email {
			user = u
			break
		}
	}

	// Create admin user if not found
	if user == nil {
		newID, err := generateID()
		if err != nil {
			return nil, "", err
		}
		user = &api.User{
			ID:        newID,
			Email:     email,
			Name:      "Administrator",
			Role:      "admin",
			Workspace: "default",
			CreatedAt: time.Now(),
		}
		a.users[user.ID] = user
	}

	// Generate token
	token, err := generateToken()
	if err != nil {
		return nil, "", err
	}
	a.tokens[token] = &session{
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}

	// Persist sessions if configured
	a.saveSessionsLocked()

	return copyUser(user), token, nil
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

	delete(a.tokens, token)
	a.saveSessionsLocked()
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

	sess, ok := a.tokens[token]
	if !ok {
		return nil, errors.New("invalid token")
	}
	if time.Now().After(sess.ExpiresAt) {
		delete(a.tokens, token)
		a.saveSessionsLocked()
		return nil, errors.New("token expired")
	}

	user := a.users[sess.UserID]
	if user == nil {
		return nil, errors.New("user not found")
	}
	user.Workspace = workspace
	a.saveSessionsLocked()
	return copyUser(user), nil
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
	maxLoginAttempts   = 5
	lockoutDuration    = 15 * time.Minute
	attemptResetWindow = 30 * time.Minute
	minPasswordLength  = 12
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

// checkBruteForceProtection checks if the account is locked due to failed attempts
func (a *LocalAuthenticator) checkBruteForceProtection(email string) error {
	a.attemptsMu.Lock()
	defer a.attemptsMu.Unlock()

	attempt, exists := a.loginAttempts[email]
	if !exists {
		return nil
	}

	// Check if account is locked
	if !attempt.LockedUntil.IsZero() && time.Now().Before(attempt.LockedUntil) {
		remaining := time.Until(attempt.LockedUntil)
		return errors.New("account temporarily locked due to failed login attempts. Try again in " + remaining.String())
	}

	// Reset lock if expired
	if !attempt.LockedUntil.IsZero() && time.Now().After(attempt.LockedUntil) {
		attempt.LockedUntil = time.Time{}
		attempt.Count = 0
	}

	return nil
}

// recordFailedAttempt records a failed login attempt.
// MED-13: persists the updated lockout map so brute-force state survives
// process restart. Callers must hold a.mu so saveSessionsLocked can read
// sessions/users safely; attemptsMu is acquired and released here.
func (a *LocalAuthenticator) recordFailedAttempt(email string) {
	// Only the configured account can authenticate -- Login rejects every other
	// address before any password check -- so an attempt against another address
	// can never affect a lockout decision. Recording it would let an
	// unauthenticated caller grow this store, and the file it is persisted to, by
	// choosing addresses freely.
	if email != a.adminEmail {
		return
	}

	a.attemptsMu.Lock()
	attempt, exists := a.loginAttempts[email]
	if !exists {
		attempt = &loginAttempt{}
		a.loginAttempts[email] = attempt
	}

	// Reset count if too much time has passed
	if time.Since(attempt.LastTry) > attemptResetWindow {
		attempt.Count = 0
	}

	attempt.Count++
	attempt.LastTry = time.Now()

	// Lock account if max attempts reached
	if attempt.Count >= maxLoginAttempts {
		attempt.LockedUntil = time.Now().Add(lockoutDuration)
	}
	a.attemptsMu.Unlock()

	// Persist after releasing attemptsMu so saveSessionsLocked can re-acquire
	// its RLock without re-entrancy.
	a.saveSessionsLocked()
}

// clearFailedAttempts clears failed login attempts on successful login.
// MED-13: also persists so a successful login removes the lockout entry
// from the on-disk session file.
func (a *LocalAuthenticator) clearFailedAttempts(email string) {
	a.attemptsMu.Lock()
	delete(a.loginAttempts, email)
	a.attemptsMu.Unlock()

	a.saveSessionsLocked()
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

// ChangePassword validates the current password and sets a new one.
// All existing sessions are invalidated on success.
func (a *LocalAuthenticator) ChangePassword(token, currentPassword, newPassword string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Validate token
	sess, ok := a.tokens[token]
	if !ok {
		return errors.New("invalid token")
	}
	if time.Now().After(sess.ExpiresAt) {
		delete(a.tokens, token)
		return errors.New("token expired")
	}

	// Verify current password
	if err := bcrypt.CompareHashAndPassword([]byte(a.adminPasswordHash), []byte(currentPassword)); err != nil {
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
	a.adminPasswordHash = string(newHash)

	// Invalidate all existing sessions (force re-login)
	a.tokens = make(map[string]*session)
	a.saveSessionsLocked()

	return nil
}

// RequestPasswordReset generates a time-limited reset token for the given email.
// The token is logged to the server log (since no email infrastructure exists).
func (a *LocalAuthenticator) RequestPasswordReset(email string) (string, error) {
	// Use constant-time comparison to prevent user enumeration.
	// The equalisation work below depends on nothing but a constant and adminEmail
	// is immutable, so it runs before the auth lock is taken: holding that lock
	// across bcrypt let an unauthenticated caller stall every other request.
	if subtle.ConstantTimeCompare([]byte(email), []byte(a.adminEmail)) != 1 {
		// Run dummy bcrypt to prevent timing attacks (same as login)
		dummyHash, _ := bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing"), bcrypt.DefaultCost)
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte("dummy"))
		// Still return success to prevent email enumeration
		return "", nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// Generate reset token
	token, err := generateToken()
	if err != nil {
		return "", err
	}
	a.resetTokens[token] = &resetToken{
		Email:     email,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	a.saveSessionsLocked()

	// Emit to structured audit log instead of stdout.
	// In production this should be sent via email to the admin.
	slog.Info("password reset requested",
		slog.String("email", email),
		slog.Time("expires", time.Now().Add(1*time.Hour)))

	return token, nil
}

// ConfirmPasswordReset validates a reset token and sets a new password.
// All existing sessions are invalidated on success.
func (a *LocalAuthenticator) ConfirmPasswordReset(token, newPassword string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

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
	a.adminPasswordHash = string(newHash)

	// Delete used reset token
	delete(a.resetTokens, token)

	// Invalidate all existing sessions (force re-login)
	a.tokens = make(map[string]*session)
	a.saveSessionsLocked()

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
	delete(data.Lockouts, adminEmail)
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
