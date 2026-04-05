package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/api"
)

// LocalAuthenticator implements simple token-based auth
type LocalAuthenticator struct {
	mu          sync.RWMutex
	tokens      map[string]*session
	users       map[string]*api.User
	sessionPath string
	stopCleanup chan struct{}
	cleanupDone chan struct{}
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
func NewLocalAuthenticator(sessionPath string) *LocalAuthenticator {
	a := &LocalAuthenticator{
		tokens:      make(map[string]*session),
		users:       make(map[string]*api.User),
		sessionPath: sessionPath,
		stopCleanup: make(chan struct{}),
		cleanupDone: make(chan struct{}),
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
	os.Rename(tmpPath, a.sessionPath)
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
	close(a.stopCleanup)
	<-a.cleanupDone
}

// Authenticate validates a token and returns the user
func (a *LocalAuthenticator) Authenticate(token string) (*api.User, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	sess, ok := a.tokens[token]
	if !ok {
		return nil, errors.New("invalid token")
	}

	if time.Now().After(sess.ExpiresAt) {
		delete(a.tokens, token)
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
	a.mu.Lock()
	defer a.mu.Unlock()

	// For demo: accept any non-empty credentials
	if email == "" || password == "" {
		return nil, "", errors.New("invalid credentials")
	}

	// Check if user exists by email
	var user *api.User
	for _, u := range a.users {
		if u.Email == email {
			user = u
			break
		}
	}

	// Create new user if not found
	if user == nil {
		user = &api.User{
			ID:        generateID(),
			Email:     email,
			Name:      email,
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
	rand.Read(b)
	return "aw_" + hex.EncodeToString(b)
}

func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "usr_" + hex.EncodeToString(b)[:16]
}
