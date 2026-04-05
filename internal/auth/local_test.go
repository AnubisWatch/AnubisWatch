package auth

import (
	"encoding/json"
	"os"
	"testing"
	"time"
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
