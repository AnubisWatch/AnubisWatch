package auth

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/api"
)

func TestLocalAuthenticator(t *testing.T) {
	auth := NewLocalAuthenticator("")

	// Test login
	user, token, err := auth.Login("test@example.com", "password")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	if user == nil {
		t.Fatal("User should not be nil")
	}

	if token == "" {
		t.Fatal("Token should not be empty")
	}

	// Test authenticate with valid token
	authUser, err := auth.Authenticate(token)
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}

	if authUser == nil {
		t.Fatal("Authenticated user should not be nil")
	}

	// Test logout
	if err := auth.Logout(token); err != nil {
		t.Fatalf("Logout failed: %v", err)
	}

	// Token should be invalid after logout
	_, err = auth.Authenticate(token)
	if err == nil {
		t.Error("Expected error for invalid token")
	}
}

func TestInvalidCredentials(t *testing.T) {
	auth := NewLocalAuthenticator("")

	_, _, err := auth.Login("", "")
	if err == nil {
		t.Error("Expected error for empty credentials")
	}
}

// TestLogin_EmptyEmail tests login with empty email
func TestLogin_EmptyEmail(t *testing.T) {
	auth := NewLocalAuthenticator("")

	_, _, err := auth.Login("", "password")
	if err == nil {
		t.Error("Expected error for empty email")
	}
}

// TestLogin_EmptyPassword tests login with empty password
func TestLogin_EmptyPassword(t *testing.T) {
	auth := NewLocalAuthenticator("")

	_, _, err := auth.Login("test@example.com", "")
	if err == nil {
		t.Error("Expected error for empty password")
	}
}

// TestLogin_RepeatedLoginSameUser tests that repeated logins with same email return same user
func TestLogin_RepeatedLoginSameUser(t *testing.T) {
	auth := NewLocalAuthenticator("")

	user1, token1, err := auth.Login("same@example.com", "password1")
	if err != nil {
		t.Fatalf("First login failed: %v", err)
	}

	user2, token2, err := auth.Login("same@example.com", "password2")
	if err != nil {
		t.Fatalf("Second login failed: %v", err)
	}

	if user1.ID != user2.ID {
		t.Error("Expected same user ID for repeated logins")
	}

	if token1 == token2 {
		t.Error("Expected different tokens for separate logins")
	}
}

// TestLogout_NonExistentToken tests logout with non-existent token
func TestLogout_NonExistentToken(t *testing.T) {
	auth := NewLocalAuthenticator("")

	err := auth.Logout("non-existent-token")
	if err != nil {
		t.Errorf("Logout should not error for non-existent token: %v", err)
	}
}

// TestAuthenticate_NonExistentToken tests authenticate with non-existent token
func TestAuthenticate_NonExistentToken(t *testing.T) {
	auth := NewLocalAuthenticator("")

	_, err := auth.Authenticate("non-existent-token")
	if err == nil {
		t.Error("Expected error for non-existent token")
	}
}

// TestSessionPersistence tests that sessions survive restarts
func TestSessionPersistence(t *testing.T) {
	tmpFile := t.TempDir() + "/sessions.json"

	// Create authenticator with persistence
	auth1 := NewLocalAuthenticator(tmpFile)

	// Login
	user1, token, err := auth1.Login("persist@example.com", "password")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	t.Logf("Logged in: user=%s, token=%s", user1.ID, token)

	// Verify token works
	_, err = auth1.Authenticate(token)
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}

	// Manually save sessions to disk using proper structure
	auth1.mu.RLock()
	data := sessionData{
		Tokens: auth1.tokens,
		Users:  auth1.users,
	}
	jsonData, _ := json.Marshal(data)
	auth1.mu.RUnlock()

	if err := os.WriteFile(tmpFile, jsonData, 0600); err != nil {
		t.Fatalf("Failed to write session file: %v", err)
	}
	t.Logf("Saved session file: %s", tmpFile)

	// Stop the cleanup goroutine to prevent resource leak
	auth1.stopCleanup <- struct{}{}
	<-auth1.cleanupDone

	// Read back and verify
	fileData, _ := os.ReadFile(tmpFile)
	t.Logf("File contents: %s", string(fileData))

	// Create new authenticator (simulating restart)
	auth2 := NewLocalAuthenticator(tmpFile)
	defer func() {
		auth2.stopCleanup <- struct{}{}
		<-auth2.cleanupDone
	}()

	t.Logf("auth2 tokens: %d, users: %d", len(auth2.tokens), len(auth2.users))

	// Token should still be valid
	user2, err := auth2.Authenticate(token)
	if err != nil {
		t.Fatalf("Token should be valid after restart: %v", err)
	}

	// Should be same user
	if user1.ID != user2.ID {
		t.Error("Expected same user after restart")
	}
}

// TestSessionExpiration tests that expired sessions are cleaned up
func TestSessionExpiration(t *testing.T) {
	tmpFile := t.TempDir() + "/sessions.json"

	auth := NewLocalAuthenticator(tmpFile)
	defer auth.Shutdown()

	// Login
	_, token, err := auth.Login("expire@example.com", "password")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	// Manually expire the session (for testing)
	auth.mu.Lock()
	if sess, ok := auth.tokens[token]; ok {
		sess.ExpiresAt = time.Now().Add(-1 * time.Hour)
	}
	auth.mu.Unlock()

	// Token should be invalid now
	_, err = auth.Authenticate(token)
	if err == nil {
		t.Error("Expected error for expired token")
	}
}

// TestLoadSessions_CorruptedFile tests loading corrupted session file
func TestLoadSessions_CorruptedFile(t *testing.T) {
	tmpFile := t.TempDir() + "/corrupted.json"

	// Write corrupted JSON
	if err := os.WriteFile(tmpFile, []byte("not valid json"), 0600); err != nil {
		t.Fatalf("Failed to write corrupted file: %v", err)
	}

	// Should not panic, just start fresh
	auth := NewLocalAuthenticator(tmpFile)
	defer auth.Shutdown()

	// Should be able to login normally
	_, _, err := auth.Login("test@example.com", "password")
	if err != nil {
		t.Fatalf("Login should work after corrupted file: %v", err)
	}
}

// TestLoadSessions_NonExistentFile tests loading from non-existent file
func TestLoadSessions_NonExistentFile(t *testing.T) {
	tmpFile := t.TempDir() + "/nonexistent.json"

	// Should not panic, just start fresh
	auth := NewLocalAuthenticator(tmpFile)
	defer auth.Shutdown()

	// Should be able to login normally
	_, _, err := auth.Login("test@example.com", "password")
	if err != nil {
		t.Fatalf("Login should work with non-existent file: %v", err)
	}
}

// TestLoadSessions_ExpiredSessionsFiltered tests that expired sessions are filtered on load
func TestLoadSessions_ExpiredSessionsFiltered(t *testing.T) {
	tmpFile := t.TempDir() + "/expired_sessions.json"

	// Create session data with expired session
	data := sessionData{
		Tokens: map[string]*session{
			"valid_token": {
				UserID:    "user1",
				ExpiresAt: time.Now().Add(24 * time.Hour),
			},
			"expired_token": {
				UserID:    "user2",
				ExpiresAt: time.Now().Add(-1 * time.Hour),
			},
		},
		Users: map[string]*api.User{
			"user1": {ID: "user1", Email: "valid@example.com"},
			"user2": {ID: "user2", Email: "expired@example.com"},
		},
	}

	jsonData, _ := json.Marshal(data)
	if err := os.WriteFile(tmpFile, jsonData, 0600); err != nil {
		t.Fatalf("Failed to write session file: %v", err)
	}

	auth := NewLocalAuthenticator(tmpFile)
	defer auth.Shutdown()

	// Valid token should work
	user, err := auth.Authenticate("valid_token")
	if err != nil {
		t.Errorf("Valid token should work: %v", err)
	}
	if user == nil || user.ID != "user1" {
		t.Error("Should get correct user for valid token")
	}

	// Expired token should not work
	_, err = auth.Authenticate("expired_token")
	if err == nil {
		t.Error("Expired token should be rejected")
	}
}

// TestSaveSessions_NoSessionPath tests saveSessions with no session path
func TestSaveSessions_NoSessionPath(t *testing.T) {
	auth := NewLocalAuthenticator("")
	defer auth.Shutdown()

	// Login should work without persistence
	_, token, err := auth.Login("test@example.com", "password")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	// Authenticate should work
	_, err = auth.Authenticate(token)
	if err != nil {
		t.Errorf("Authenticate failed: %v", err)
	}

	// Call saveSessions directly - should not panic
	auth.saveSessions()
}

// TestAuthenticate_UserNotFound tests authenticate when user is not found
func TestAuthenticate_UserNotFound(t *testing.T) {
	tmpFile := t.TempDir() + "/sessions.json"
	auth := NewLocalAuthenticator(tmpFile)
	defer auth.Shutdown()

	// Manually add a session without a corresponding user
	auth.mu.Lock()
	auth.tokens["orphan_token"] = &session{
		UserID:    "nonexistent_user",
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	auth.mu.Unlock()

	// Should get error for missing user
	_, err := auth.Authenticate("orphan_token")
	if err == nil {
		t.Error("Expected error when user not found")
	}
}

// TestCleanupExpiredSessions tests the cleanup goroutine
func TestCleanupExpiredSessions(t *testing.T) {
	tmpFile := t.TempDir() + "/cleanup.json"
	auth := NewLocalAuthenticator(tmpFile)

	// Login
	_, token, err := auth.Login("cleanup@example.com", "password")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	// Expire the session
	auth.mu.Lock()
	auth.tokens[token].ExpiresAt = time.Now().Add(-1 * time.Hour)
	auth.mu.Unlock()

	// Shutdown should trigger cleanup
	auth.Shutdown()

	// Create new authenticator and verify token is gone
	auth2 := NewLocalAuthenticator(tmpFile)
	defer auth2.Shutdown()

	_, err = auth2.Authenticate(token)
	if err == nil {
		t.Error("Expired token should be cleaned up after shutdown")
	}
}

// TestGenerateToken tests the token generation
func TestGenerateToken(t *testing.T) {
	token1 := generateToken()
	token2 := generateToken()

	if token1 == "" {
		t.Error("Token should not be empty")
	}

	if token1 == token2 {
		t.Error("Generated tokens should be unique")
	}

	if len(token1) < 10 {
		t.Error("Token should have reasonable length")
	}
}

// TestGenerateID tests the ID generation
func TestGenerateID(t *testing.T) {
	id1 := generateID()
	id2 := generateID()

	if id1 == "" {
		t.Error("ID should not be empty")
	}

	if id1 == id2 {
		t.Error("Generated IDs should be unique")
	}

	if len(id1) < 8 {
		t.Error("ID should have reasonable length")
	}
}

// TestLoadSessions_EmptyPath tests loadSessions with empty path
func TestLoadSessions_EmptyPath(t *testing.T) {
	auth := NewLocalAuthenticator("")
	defer auth.Shutdown()

	// Should work without session path
	_, _, err := auth.Login("test@example.com", "password")
	if err != nil {
		t.Fatalf("Login should work with empty session path: %v", err)
	}

	// Direct call to loadSessions with empty path should return early
	auth.loadSessions()
}

// TestSaveSessionsLocked_MkdirError tests saveSessionsLocked when directory creation fails
func TestSaveSessionsLocked_MkdirError(t *testing.T) {
	// Use an invalid path that will cause MkdirAll to fail
	auth := NewLocalAuthenticator("/invalid_path_that_cannot_be_created/sessions.json")
	defer auth.Shutdown()

	// Login should still work even if persistence fails
	_, _, err := auth.Login("test@example.com", "password")
	if err != nil {
		t.Fatalf("Login should work even if persistence fails: %v", err)
	}
}

// TestCleanupExpiredSessions_Ticker tests the cleanup ticker by triggering multiple cleanups
func TestCleanupExpiredSessions_Ticker(t *testing.T) {
	tmpFile := t.TempDir() + "/ticker.json"
	auth := NewLocalAuthenticator(tmpFile)

	// Login multiple users
	_, token1, _ := auth.Login("user1@example.com", "password")
	_, token2, _ := auth.Login("user2@example.com", "password")

	// Expire first session
	auth.mu.Lock()
	auth.tokens[token1].ExpiresAt = time.Now().Add(-1 * time.Hour)
	auth.mu.Unlock()

	// Trigger cleanup by calling it directly through shutdown
	auth.Shutdown()

	// Verify second session still works after restart
	auth2 := NewLocalAuthenticator(tmpFile)
	defer auth2.Shutdown()

	// Expired token should be gone
	_, err := auth2.Authenticate(token1)
	if err == nil {
		t.Error("Expired token should be removed")
	}

	// Valid token should still work
	_, err = auth2.Authenticate(token2)
	if err != nil {
		t.Errorf("Valid token should still work: %v", err)
	}
}
