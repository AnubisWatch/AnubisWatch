package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	mu              sync.RWMutex
	tokens          map[string]*session
	users           map[string]*api.User
	adminEmail      string
	adminPasswordHash string // bcrypt hashed password
	sessionPath     string
	stopCleanup     chan struct{}
	cleanupDone     chan struct{}
	// login attempts tracking for brute force protection
	loginAttempts   map[string]*loginAttempt
	attemptsMu      sync.RWMutex
}

type loginAttempt struct {
	count       int
	lastTry     time.Time
	lockedUntil time.Time
}

type session struct {
	UserID    string    `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

// sessionData represents the data persisted to disk
type sessionData struct {
	Tokens map[string]*session  `json:"tokens"`
	Users  map[string]*api.User `json:"users"`
}

// NewLocalAuthenticator creates a new local authenticator
// If sessionPath is provided, sessions are persisted to disk
// adminPassword should be plaintext; it will be hashed internally
func NewLocalAuthenticator(sessionPath, adminEmail, adminPassword string) *LocalAuthenticator {
	// Hash the password if it's not already hashed
	var passwordHash string
	if adminPassword != "" {
		// Check if password is already bcrypt hashed
		if strings.HasPrefix(adminPassword, "$2a$") || strings.HasPrefix(adminPassword, "$2b$") || strings.HasPrefix(adminPassword, "$2y$") {
			passwordHash = adminPassword
		} else {
			// Hash the plaintext password
			hash, err := bcrypt.GenerateFromPassword([]byte(adminPassword), bcryptCost)
			if err != nil {
				// If hashing fails, we can't continue securely
				panic("failed to hash admin password: " + err.Error())
			}
			passwordHash = string(hash)
		}
	}

	a := &LocalAuthenticator{
		tokens:            make(map[string]*session),
		users:             make(map[string]*api.User),
		adminEmail:        adminEmail,
		adminPasswordHash: passwordHash,
		sessionPath:       sessionPath,
		stopCleanup:       make(chan struct{}),
		cleanupDone:       make(chan struct{}),
		loginAttempts:     make(map[string]*loginAttempt),
	}

	// Create admin user if credentials provided
	if adminEmail != "" && adminPassword != "" {
		adminUser := &api.User{
			ID:        generateID(),
			Email:     adminEmail,
			Name:      "Administrator",
			Role:      "admin",
			Workspace: "default",
			CreatedAt: time.Now(),
		}
		a.users[adminUser.ID] = adminUser
	}

	// Load persisted sessions if path provided
	if sessionPath != "" {
		a.loadSessions()
	}

	// Start background cleanup goroutine
	go a.cleanupExpiredSessions()

	return a
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

	// Only load non-expired sessions and their users
	now := time.Now()
	for token, sess := range sessionData.Tokens {
		if now.Before(sess.ExpiresAt) {
			a.tokens[token] = sess
			// Also load the associated user
			if user, ok := sessionData.Users[sess.UserID]; ok {
				a.users[sess.UserID] = user
			}
		}
	}
}

// saveSessionsLocked persists sessions and users to disk
// Must be called with a.mu held (at least RLock)
func (a *LocalAuthenticator) saveSessionsLocked() {
	if a.sessionPath == "" {
		return
	}

	data := sessionData{
		Tokens: a.tokens,
		Users:  a.users,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return
	}

	// Ensure directory exists
	dir := filepath.Dir(a.sessionPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
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
	// Ensure final file has correct permissions (defense in depth)
	os.Chmod(a.sessionPath, 0600)
}

// saveSessions persists sessions to disk (public version that acquires lock)
func (a *LocalAuthenticator) saveSessions() {
	if a.sessionPath == "" {
		return
	}

	a.mu.RLock()
	defer a.mu.RUnlock()
	a.saveSessionsLocked()
}

// cleanupExpiredSessions removes expired sessions periodically
func (a *LocalAuthenticator) cleanupExpiredSessions() {
	ticker := time.NewTicker(5 * time.Minute)
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

// Shutdown gracefully stops the authenticator
func (a *LocalAuthenticator) Shutdown() {
	select {
	case <-a.stopCleanup:
		// Already shutting down
		return
	default:
		close(a.stopCleanup)
	}
	<-a.cleanupDone
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

	return user, nil
}

// Login creates a new session and returns a token
func (a *LocalAuthenticator) Login(email, password string) (*api.User, string, error) {
	// Check brute force protection first
	if err := a.checkBruteForceProtection(email); err != nil {
		return nil, "", err
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// Validate credentials against configured admin
	if email == "" || password == "" {
		a.recordFailedAttempt(email)
		return nil, "", errors.New("invalid credentials")
	}

	// Constant-time comparison for email
	if subtle.ConstantTimeCompare([]byte(email), []byte(a.adminEmail)) != 1 {
		a.recordFailedAttempt(email)
		return nil, "", errors.New("invalid credentials")
	}

	// bcrypt password verification
	if err := bcrypt.CompareHashAndPassword([]byte(a.adminPasswordHash), []byte(password)); err != nil {
		a.recordFailedAttempt(email)
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
		user = &api.User{
			ID:        generateID(),
			Email:     email,
			Name:      "Administrator",
			Role:      "admin",
			Workspace: "default",
			CreatedAt: time.Now(),
		}
		a.users[user.ID] = user
	}

	// Generate token
	token := generateToken()
	a.tokens[token] = &session{
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}

	// Persist sessions if configured
	a.saveSessionsLocked()

	return user, token, nil
}

// Logout invalidates a token
func (a *LocalAuthenticator) Logout(token string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	delete(a.tokens, token)
	a.saveSessionsLocked()
	return nil
}

func generateToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Fail closed: panic rather than return a predictable token.
		// A CSPRNG failure is a critical system failure and must never
		// result in a token that an attacker could guess.
		panic("CSPRNG failure: cannot generate secure token: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fail closed: panic rather than return a predictable ID.
		panic("CSPRNG failure: cannot generate secure ID: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// Brute force protection functions
const (
	bcryptCost          = 12
	maxLoginAttempts    = 5
	lockoutDuration     = 15 * time.Minute
	attemptResetWindow  = 30 * time.Minute
)

// checkBruteForceProtection checks if the account is locked due to failed attempts
func (a *LocalAuthenticator) checkBruteForceProtection(email string) error {
	a.attemptsMu.Lock()
	defer a.attemptsMu.Unlock()

	attempt, exists := a.loginAttempts[email]
	if !exists {
		return nil
	}

	// Check if account is locked
	if !attempt.lockedUntil.IsZero() && time.Now().Before(attempt.lockedUntil) {
		remaining := time.Until(attempt.lockedUntil)
		return errors.New("account temporarily locked due to failed login attempts. Try again in " + remaining.String())
	}

	// Reset lock if expired
	if !attempt.lockedUntil.IsZero() && time.Now().After(attempt.lockedUntil) {
		attempt.lockedUntil = time.Time{}
		attempt.count = 0
	}

	return nil
}

// recordFailedAttempt records a failed login attempt
func (a *LocalAuthenticator) recordFailedAttempt(email string) {
	a.attemptsMu.Lock()
	defer a.attemptsMu.Unlock()

	attempt, exists := a.loginAttempts[email]
	if !exists {
		attempt = &loginAttempt{}
		a.loginAttempts[email] = attempt
	}

	// Reset count if too much time has passed
	if time.Since(attempt.lastTry) > attemptResetWindow {
		attempt.count = 0
	}

	attempt.count++
	attempt.lastTry = time.Now()

	// Lock account if max attempts reached
	if attempt.count >= maxLoginAttempts {
		attempt.lockedUntil = time.Now().Add(lockoutDuration)
	}
}

// clearFailedAttempts clears failed login attempts on successful login
func (a *LocalAuthenticator) clearFailedAttempts(email string) {
	a.attemptsMu.Lock()
	defer a.attemptsMu.Unlock()

	delete(a.loginAttempts, email)
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
