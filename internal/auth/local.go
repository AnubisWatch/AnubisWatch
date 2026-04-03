package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/api"
)

// LocalAuthenticator implements simple token-based auth
type LocalAuthenticator struct {
	mu      sync.RWMutex
	tokens  map[string]*session
	users   map[string]*api.User
}

type session struct {
	userID    string
	expiresAt time.Time
}

// NewLocalAuthenticator creates a new local authenticator
func NewLocalAuthenticator() *LocalAuthenticator {
	return &LocalAuthenticator{
		tokens: make(map[string]*session),
		users:  make(map[string]*api.User),
	}
}

// Authenticate validates a token and returns the user
func (a *LocalAuthenticator) Authenticate(token string) (*api.User, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	sess, ok := a.tokens[token]
	if !ok {
		return nil, errors.New("invalid token")
	}

	if time.Now().After(sess.expiresAt) {
		delete(a.tokens, token)
		return nil, errors.New("token expired")
	}

	user := a.users[sess.userID]
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
		userID:    user.ID,
		expiresAt: time.Now().Add(24 * time.Hour),
	}

	return user, token, nil
}

// Logout invalidates a token
func (a *LocalAuthenticator) Logout(token string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	delete(a.tokens, token)
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
