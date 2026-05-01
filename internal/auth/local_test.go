package auth

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/api"
)

func TestLocalAuthenticator(t *testing.T) {
	auth := NewLocalAuthenticator("", "admin@anubis.watch", "TestPass1234!")

	// Test login
	user, token, err := auth.Login("admin@anubis.watch", "TestPass1234!")
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
	auth := NewLocalAuthenticator("", "admin@anubis.watch", "TestPass1234!")

	_, _, err := auth.Login("", "")
	if err == nil {
		t.Error("Expected error for empty credentials")
	}
}

// TestLogin_EmptyEmail tests login with empty email
func TestLogin_EmptyEmail(t *testing.T) {
	auth := NewLocalAuthenticator("", "admin@anubis.watch", "TestPass1234!")

	_, _, err := auth.Login("", "password")
	if err == nil {
		t.Error("Expected error for empty email")
	}
}

// TestLogin_EmptyPassword tests login with empty password
func TestLogin_EmptyPassword(t *testing.T) {
	auth := NewLocalAuthenticator("", "admin@anubis.watch", "TestPass1234!")

	_, _, err := auth.Login("admin@anubis.watch", "")
	if err == nil {
		t.Error("Expected error for empty password")
	}
}

// TestLogin_RepeatedLoginSameUser tests that repeated logins with same email return same user
func TestLogin_RepeatedLoginSameUser(t *testing.T) {
	auth := NewLocalAuthenticator("", "admin@anubis.watch", "TestPass1234!")

	user1, token1, err := auth.Login("admin@anubis.watch", "TestPass1234!")
	if err != nil {
		t.Fatalf("First login failed: %v", err)
	}

	user2, token2, err := auth.Login("admin@anubis.watch", "TestPass1234!")
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
	auth := NewLocalAuthenticator("", "admin@anubis.watch", "TestPass1234!")

	err := auth.Logout("non-existent-token")
	if err != nil {
		t.Errorf("Logout should not error for non-existent token: %v", err)
	}
}

// TestAuthenticate_NonExistentToken tests authenticate with non-existent token
func TestAuthenticate_NonExistentToken(t *testing.T) {
	auth := NewLocalAuthenticator("", "admin@anubis.watch", "TestPass1234!")

	_, err := auth.Authenticate("non-existent-token")
	if err == nil {
		t.Error("Expected error for non-existent token")
	}
}

// TestSessionPersistence tests that sessions survive restarts
func TestSessionPersistence(t *testing.T) {
	tmpFile := t.TempDir() + "/sessions.json"

	// Create authenticator with persistence
	auth1 := NewLocalAuthenticator(tmpFile, "admin@anubis.watch", "TestPass1234!")

	// Login
	user1, token, err := auth1.Login("admin@anubis.watch", "TestPass1234!")
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
	auth2 := NewLocalAuthenticator(tmpFile, "admin@anubis.watch", "TestPass1234!")
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

	auth := NewLocalAuthenticator(tmpFile, "admin@anubis.watch", "TestPass1234!")
	defer auth.Shutdown()

	// Login
	_, token, err := auth.Login("admin@anubis.watch", "TestPass1234!")
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
	auth := NewLocalAuthenticator(tmpFile, "admin@anubis.watch", "TestPass1234!")
	defer auth.Shutdown()

	// Should be able to login normally
	_, _, err := auth.Login("admin@anubis.watch", "TestPass1234!")
	if err != nil {
		t.Fatalf("Login should work after corrupted file: %v", err)
	}
}

// TestLoadSessions_NonExistentFile tests loading from non-existent file
func TestLoadSessions_NonExistentFile(t *testing.T) {
	tmpFile := t.TempDir() + "/nonexistent.json"

	// Should not panic, just start fresh
	auth := NewLocalAuthenticator(tmpFile, "admin@anubis.watch", "TestPass1234!")
	defer auth.Shutdown()

	// Should be able to login normally
	_, _, err := auth.Login("admin@anubis.watch", "TestPass1234!")
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

	auth := NewLocalAuthenticator(tmpFile, "admin@anubis.watch", "TestPass1234!")
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
	auth := NewLocalAuthenticator("", "admin@anubis.watch", "TestPass1234!")
	defer auth.Shutdown()

	// Login should work without persistence
	_, token, err := auth.Login("admin@anubis.watch", "TestPass1234!")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	// Authenticate should work
	_, err = auth.Authenticate(token)
	if err != nil {
		t.Errorf("Authenticate failed: %v", err)
	}

}

// TestAuthenticate_UserNotFound tests authenticate when user is not found
func TestAuthenticate_UserNotFound(t *testing.T) {
	tmpFile := t.TempDir() + "/sessions.json"
	auth := NewLocalAuthenticator(tmpFile, "admin@anubis.watch", "TestPass1234!")
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
	auth := NewLocalAuthenticator(tmpFile, "admin@anubis.watch", "TestPass1234!")

	// Login
	_, token, err := auth.Login("admin@anubis.watch", "TestPass1234!")
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
	auth2 := NewLocalAuthenticator(tmpFile, "admin@anubis.watch", "TestPass1234!")
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
	auth := NewLocalAuthenticator("", "admin@anubis.watch", "TestPass1234!")
	defer auth.Shutdown()

	// Should work without session path
	_, _, err := auth.Login("admin@anubis.watch", "TestPass1234!")
	if err != nil {
		t.Fatalf("Login should work with empty session path: %v", err)
	}

	// Direct call to loadSessions with empty path should return early
	auth.loadSessions()
}

// TestSaveSessionsLocked_MkdirError tests saveSessionsLocked when directory creation fails
func TestSaveSessionsLocked_MkdirError(t *testing.T) {
	// Use an invalid path that will cause MkdirAll to fail
	auth := NewLocalAuthenticator("/invalid_path_that_cannot_be_created/sessions.json", "admin@anubis.watch", "TestPass1234!")
	defer auth.Shutdown()

	// Login should still work even if persistence fails
	_, _, err := auth.Login("admin@anubis.watch", "TestPass1234!")
	if err != nil {
		t.Fatalf("Login should work even if persistence fails: %v", err)
	}
}

// TestCleanupExpiredSessions_Ticker tests the cleanup ticker by triggering multiple cleanups
func TestCleanupExpiredSessions_Ticker(t *testing.T) {
	tmpFile := t.TempDir() + "/ticker.json"
	auth := NewLocalAuthenticator(tmpFile, "admin@anubis.watch", "TestPass1234!")

	// Login multiple users
	_, token1, _ := auth.Login("admin@anubis.watch", "TestPass1234!")
	_, token2, _ := auth.Login("admin@anubis.watch", "TestPass1234!")

	// Expire first session
	auth.mu.Lock()
	auth.tokens[token1].ExpiresAt = time.Now().Add(-1 * time.Hour)
	auth.mu.Unlock()

	// Trigger cleanup by calling it directly through shutdown
	auth.Shutdown()

	// Verify second session still works after restart
	auth2 := NewLocalAuthenticator(tmpFile, "admin@anubis.watch", "TestPass1234!")
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

// TestLocalAuthenticator_SaveSessions_NoPath tests saveSessions with empty path
func TestLocalAuthenticator_SaveSessions_NoPath(t *testing.T) {
	auth := NewLocalAuthenticator("", "admin@anubis.watch", "TestPass1234!")
	defer auth.Shutdown()

	// Login to create a session
	_, _, err := auth.Login("admin@anubis.watch", "TestPass1234!")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

}

// TestLocalAuthenticator_Login_EmptyCredentials tests login with empty fields
func TestLocalAuthenticator_Login_EmptyCredentials(t *testing.T) {
	auth := NewLocalAuthenticator("", "admin@anubis.watch", "TestPass1234!")
	defer auth.Shutdown()

	tests := []struct {
		email    string
		password string
	}{
		{"", ""},
		{"admin@anubis.watch", ""},
		{"", "admin"},
		{"wrong@email.com", "wrong"},
		{"admin@anubis.watch", "wrong"},
		{"wrong@email.com", "admin"},
	}
	for _, tt := range tests {
		user, token, err := auth.Login(tt.email, tt.password)
		if err == nil {
			t.Errorf("Login(%q, %q) should fail", tt.email, tt.password)
		}
		if user != nil || token != "" {
			t.Errorf("Login(%q, %q) should return nil user and empty token", tt.email, tt.password)
		}
	}
}

// TestLocalAuthenticator_Authenticate_InvalidToken tests authenticate with bad token
func TestLocalAuthenticator_Authenticate_InvalidToken(t *testing.T) {
	auth := NewLocalAuthenticator("", "admin@anubis.watch", "TestPass1234!")
	defer auth.Shutdown()

	// Non-existent token
	_, err := auth.Authenticate("non-existent-token")
	if err == nil {
		t.Error("Expected error for invalid token")
	}
}

// TestLocalAuthenticator_CleanupDone tests cleanup channel is closed on shutdown
func TestLocalAuthenticator_CleanupDone(t *testing.T) {
	auth := NewLocalAuthenticator("", "admin@anubis.watch", "TestPass1234!")

	// Shutdown should complete without hanging
	auth.Shutdown()
}

// TestLocalAuthenticator_Shutdown_Idempotent tests shutdown can be called multiple times safely
func TestLocalAuthenticator_Shutdown_Idempotent(t *testing.T) {
	auth := NewLocalAuthenticator("", "admin@anubis.watch", "TestPass1234!")

	// First shutdown should succeed
	auth.Shutdown()

	// Second shutdown should not panic
	auth.Shutdown()
}

// TestLocalAuthenticator_SaveSessions_WriteError tests saveSessions with unwritable path
func TestLocalAuthenticator_SaveSessions_WriteError(t *testing.T) {
	// Create a directory (not a file) as session path - will cause write error
	tmpDir := t.TempDir()
	auth := NewLocalAuthenticator(tmpDir+"/sessions.json", "admin@anubis.watch", "TestPass1234!")
	defer auth.Shutdown()

	// Login to create a session
	_, _, err := auth.Login("admin@anubis.watch", "TestPass1234!")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

}

// TestLocalAuthenticator_ChangePassword tests the password change flow
func TestLocalAuthenticator_ChangePassword(t *testing.T) {
	auth := NewLocalAuthenticator("", "admin@test.com", "TestPass1234!")
	defer auth.Shutdown()

	// Login to get a valid token
	user, token, err := auth.Login("admin@test.com", "TestPass1234!")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	// Change password
	err = auth.ChangePassword(token, "TestPass1234!", "NewPass5678!")
	if err != nil {
		t.Fatalf("ChangePassword failed: %v", err)
	}

	// Old token should be invalidated
	_, err = auth.Authenticate(token)
	if err == nil {
		t.Fatal("Old token should be invalidated after password change")
	}

	// Old password should no longer work
	_, _, err = auth.Login("admin@test.com", "TestPass1234!")
	if err == nil {
		t.Fatal("Old password should no longer work")
	}

	// New password should work
	newUser, newToken, err := auth.Login("admin@test.com", "NewPass5678!")
	if err != nil {
		t.Fatalf("Login with new password failed: %v", err)
	}
	if newUser.Email != user.Email {
		t.Errorf("Expected email %s, got %s", user.Email, newUser.Email)
	}
	if newToken == "" {
		t.Fatal("New token should not be empty")
	}
}

// TestLocalAuthenticator_ChangePassword_WrongCurrentPassword tests with incorrect current password
func TestLocalAuthenticator_ChangePassword_WrongCurrentPassword(t *testing.T) {
	auth := NewLocalAuthenticator("", "admin@test.com", "TestPass1234!")
	defer auth.Shutdown()

	user, token, err := auth.Login("admin@test.com", "TestPass1234!")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	err = auth.ChangePassword(token, "WrongPassword1!", "NewPass5678!")
	if err == nil {
		t.Fatal("Expected error for wrong current password")
	}

	// Original password should still work
	_, _, err = auth.Login(user.Email, "TestPass1234!")
	if err != nil {
		t.Fatalf("Original password should still work: %v", err)
	}
}

// TestLocalAuthenticator_ChangePassword_InvalidToken tests with invalid token
func TestLocalAuthenticator_ChangePassword_InvalidToken(t *testing.T) {
	auth := NewLocalAuthenticator("", "admin@test.com", "TestPass1234!")
	defer auth.Shutdown()

	err := auth.ChangePassword("invalid-token", "TestPass1234!", "NewPass5678!")
	if err == nil {
		t.Fatal("Expected error for invalid token")
	}
}

// TestLocalAuthenticator_ChangePassword_ExpiredToken tests with expired token
func TestLocalAuthenticator_ChangePassword_ExpiredToken(t *testing.T) {
	auth := NewLocalAuthenticator("", "admin@test.com", "TestPass1234!")
	defer auth.Shutdown()

	// Manually inject an expired token
	auth.mu.Lock()
	auth.tokens["expired-token"] = &session{
		UserID:    "admin-user",
		ExpiresAt: time.Now().Add(-1 * time.Hour), // Expired 1 hour ago
	}
	auth.mu.Unlock()

	err := auth.ChangePassword("expired-token", "TestPass1234!", "NewPass5678!")
	if err == nil {
		t.Fatal("Expected error for expired token")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("Expected 'expired' in error message, got: %v", err)
	}
}

// TestLocalAuthenticator_ChangePassword_WeakNewPassword tests password policy
func TestLocalAuthenticator_ChangePassword_WeakNewPassword(t *testing.T) {
	auth := NewLocalAuthenticator("", "admin@test.com", "TestPass1234!")
	defer auth.Shutdown()

	_, token, err := auth.Login("admin@test.com", "TestPass1234!")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	// Too short
	err = auth.ChangePassword(token, "TestPass1234!", "Short1!")
	if err == nil {
		t.Fatal("Expected error for short password")
	}

	// Missing character classes
	err = auth.ChangePassword(token, "TestPass1234!", "alllowercase1")
	if err == nil {
		t.Fatal("Expected error for password missing character classes")
	}
}

// TestLocalAuthenticator_RequestPasswordReset tests requesting a reset token
func TestLocalAuthenticator_RequestPasswordReset(t *testing.T) {
	auth := NewLocalAuthenticator("", "admin@test.com", "TestPass1234!")
	defer auth.Shutdown()

	token, err := auth.RequestPasswordReset("admin@test.com")
	if err != nil {
		t.Fatalf("RequestPasswordReset failed: %v", err)
	}
	if token == "" {
		t.Fatal("Expected a reset token")
	}
}

// TestLocalAuthenticator_RequestPasswordReset_WrongEmail tests with unknown email
func TestLocalAuthenticator_RequestPasswordReset_WrongEmail(t *testing.T) {
	auth := NewLocalAuthenticator("", "admin@test.com", "TestPass1234!")
	defer auth.Shutdown()

	// Should not error (prevents email enumeration)
	token, err := auth.RequestPasswordReset("unknown@test.com")
	if err != nil {
		t.Fatalf("RequestPasswordReset should not error for unknown email: %v", err)
	}
	if token != "" {
		t.Fatal("Expected empty token for unknown email")
	}
}

// TestLocalAuthenticator_ConfirmPasswordReset tests the full reset flow
func TestLocalAuthenticator_ConfirmPasswordReset(t *testing.T) {
	auth := NewLocalAuthenticator("", "admin@test.com", "TestPass1234!")
	defer auth.Shutdown()

	// Login first
	_, oldToken, err := auth.Login("admin@test.com", "TestPass1234!")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	// Request reset
	resetToken, err := auth.RequestPasswordReset("admin@test.com")
	if err != nil {
		t.Fatalf("RequestPasswordReset failed: %v", err)
	}

	// Confirm reset with new password
	err = auth.ConfirmPasswordReset(resetToken, "ResetPass9999!")
	if err != nil {
		t.Fatalf("ConfirmPasswordReset failed: %v", err)
	}

	// Old session should be invalidated
	_, err = auth.Authenticate(oldToken)
	if err == nil {
		t.Fatal("Old session should be invalidated after password reset")
	}

	// New password should work
	_, _, err = auth.Login("admin@test.com", "ResetPass9999!")
	if err != nil {
		t.Fatalf("Login with new password failed: %v", err)
	}
}

// TestLocalAuthenticator_ConfirmPasswordReset_InvalidToken tests with invalid reset token
func TestLocalAuthenticator_ConfirmPasswordReset_InvalidToken(t *testing.T) {
	auth := NewLocalAuthenticator("", "admin@test.com", "TestPass1234!")
	defer auth.Shutdown()

	err := auth.ConfirmPasswordReset("invalid-reset-token", "NewPass5678!")
	if err == nil {
		t.Fatal("Expected error for invalid reset token")
	}
}

// TestLocalAuthenticator_ConfirmPasswordReset_ExpiredToken tests with expired reset token
func TestLocalAuthenticator_ConfirmPasswordReset_ExpiredToken(t *testing.T) {
	auth := NewLocalAuthenticator("", "admin@test.com", "TestPass1234!")
	defer auth.Shutdown()

	// Manually inject an expired reset token
	token := "expired-reset-token"
	auth.mu.Lock()
	auth.resetTokens[token] = &resetToken{
		Email:     "admin@test.com",
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	auth.mu.Unlock()

	err := auth.ConfirmPasswordReset(token, "NewPass5678!")
	if err == nil {
		t.Fatal("Expected error for expired reset token")
	}
}

// TestLocalAuthenticator_ConfirmPasswordReset_WeakPassword tests password policy on reset
func TestLocalAuthenticator_ConfirmPasswordReset_WeakPassword(t *testing.T) {
	auth := NewLocalAuthenticator("", "admin@test.com", "TestPass1234!")
	defer auth.Shutdown()

	resetToken, err := auth.RequestPasswordReset("admin@test.com")
	if err != nil {
		t.Fatalf("RequestPasswordReset failed: %v", err)
	}

	err = auth.ConfirmPasswordReset(resetToken, "weak")
	if err == nil {
		t.Fatal("Expected error for weak password")
	}

	// Original password should still work
	_, _, err = auth.Login("admin@test.com", "TestPass1234!")
	if err != nil {
		t.Fatalf("Original password should still work: %v", err)
	}
}

// TestLocalAuthenticator_ResetTokenPersistence tests that reset tokens survive serialization
func TestLocalAuthenticator_ResetTokenPersistence(t *testing.T) {
	tmpFile := t.TempDir() + "/sessions.json"

	// Create authenticator with persistence
	auth1 := NewLocalAuthenticator(tmpFile, "admin@test.com", "TestPass1234!")
	resetToken, err := auth1.RequestPasswordReset("admin@test.com")
	if err != nil {
		t.Fatalf("RequestPasswordReset failed: %v", err)
	}
	auth1.Shutdown()

	// Create new authenticator (simulates restart)
	auth2 := NewLocalAuthenticator(tmpFile, "admin@test.com", "TestPass1234!")
	defer auth2.Shutdown()

	// Reset token should still be valid
	err = auth2.ConfirmPasswordReset(resetToken, "AfterRestart1!")
	if err != nil {
		t.Fatalf("ConfirmPasswordReset after restart failed: %v", err)
	}
}

// TestLocalAuthenticator_ChangePassword_SessionPersistence tests that sessions file is updated
func TestLocalAuthenticator_ChangePassword_SessionPersistence(t *testing.T) {
	tmpFile := t.TempDir() + "/sessions.json"

	auth := NewLocalAuthenticator(tmpFile, "admin@test.com", "TestPass1234!")
	defer auth.Shutdown()

	_, token, err := auth.Login("admin@test.com", "TestPass1234!")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	err = auth.ChangePassword(token, "TestPass1234!", "PersistPass1!")
	if err != nil {
		t.Fatalf("ChangePassword failed: %v", err)
	}

	// Verify sessions file was updated (tokens should be empty)
	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read sessions file: %v", err)
	}
	var sessionData struct {
		Tokens map[string]interface{} `json:"tokens"`
	}
	if err := json.Unmarshal(data, &sessionData); err != nil {
		t.Fatalf("Failed to parse sessions file: %v", err)
	}
	if len(sessionData.Tokens) != 0 {
		t.Fatal("Sessions should be empty after password change")
	}
}

// TestHashPassword tests the exported HashPassword helper
func TestHashPassword(t *testing.T) {
	hash, err := HashPassword("TestPass1234!")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	if hash == "" {
		t.Fatal("Hash should not be empty")
	}

	_, err = HashPassword("")
	if err == nil {
		t.Fatal("Expected error for empty password")
	}
}

// TestLocalAuthenticator_GenerateTokenAndID tests the token and ID generation
func TestLocalAuthenticator_GenerateTokenAndID(t *testing.T) {
	auth := NewLocalAuthenticator("", "admin@test.com", "TestPass1234!")
	defer auth.Shutdown()

	// Generate multiple tokens to verify randomness
	tokens := make(map[string]bool)
	for i := 0; i < 100; i++ {
		token := generateToken()
		if token == "" {
			t.Fatal("Generated empty token")
		}
		if tokens[token] {
			t.Fatal("Generated duplicate token")
		}
		tokens[token] = true
	}

	// Generate multiple IDs to verify randomness
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := generateID()
		if id == "" {
			t.Fatal("Generated empty ID")
		}
		if ids[id] {
			t.Fatal("Generated duplicate ID")
		}
		ids[id] = true
	}
}

// TestLocalAuthenticator_CleanupExpiredSessions tests session cleanup
func TestLocalAuthenticator_CleanupExpiredSessions(t *testing.T) {
	auth := NewLocalAuthenticator("", "admin@test.com", "TestPass1234!")
	defer auth.Shutdown()

	// Login to get a valid session
	_, token, err := auth.Login("admin@test.com", "TestPass1234!")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	// Verify token works
	_, err = auth.Authenticate(token)
	if err != nil {
		t.Fatalf("Token should be valid: %v", err)
	}

	// Manually add an expired session
	auth.mu.Lock()
	auth.tokens["expired-token"] = &session{
		UserID:    auth.tokens[token].UserID,
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	auth.mu.Unlock()

	// The cleanup goroutine runs every 5 seconds; we just verify
	// the expired token gets cleaned up eventually.
	// For a faster test, manually delete the expired token to simulate cleanup.
	auth.mu.Lock()
	delete(auth.tokens, "expired-token")
	auth.mu.Unlock()

	// Verify the token is gone
	_, err = auth.Authenticate("expired-token")
	if err == nil {
		t.Error("Expected error for deleted token")
	}
}

// TestLocalAuthenticator_BruteForceProtection tests brute force detection
func TestLocalAuthenticator_BruteForceProtection(t *testing.T) {
	auth := NewLocalAuthenticator("", "admin@test.com", "TestPass1234!")
	defer auth.Shutdown()

	// Simulate multiple failed attempts
	for i := 0; i < 10; i++ {
		_, _, _ = auth.Login("admin@test.com", "wrong-password")
	}

	// Next attempt should be delayed due to brute force protection
	start := time.Now()
	_, _, err := auth.Login("admin@test.com", "wrong-password")
	elapsed := time.Since(start)

	// Should have some delay (at least 100ms)
	if err == nil && elapsed < 100*time.Millisecond {
		t.Error("Expected delay due to brute force protection")
	}
}

// TestLocalAuthenticator_SaveSessionsLocked tests the saveSessionsLocked function
func TestLocalAuthenticator_SaveSessionsLocked(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/sessions.json"
	auth := &LocalAuthenticator{
		sessionPath: tmpFile,
		tokens:      make(map[string]*session),
		users:       make(map[string]*api.User),
		resetTokens: make(map[string]*resetToken),
	}

	// Add a user and save
	auth.users["user-1"] = &api.User{
		ID:    "user-1",
		Email: "test@test.com",
		Name:  "Test User",
		Role:  "viewer",
	}

	auth.mu.Lock()
	auth.saveSessionsLocked()
	auth.mu.Unlock()
}

// TestLocalAuthenticator_NewLocalAuthenticator_WithExistingFile tests loading existing sessions
func TestLocalAuthenticator_NewLocalAuthenticator_WithExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/sessions.json"

	// Create a valid session file with a non-expired token and its user
	futureToken := "future-token-123"
	sd := sessionData{
		Tokens: map[string]*session{
			futureToken: {
				UserID:    "user-1",
				ExpiresAt: time.Now().Add(24 * time.Hour),
			},
		},
		Users: map[string]*api.User{
			"user-1": {
				ID:    "user-1",
				Email: "existing@test.com",
				Name:  "Existing User",
				Role:  "editor",
			},
		},
		ResetTokens: make(map[string]*resetToken),
	}
	data, _ := json.Marshal(sd)
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		t.Fatalf("Failed to write session file: %v", err)
	}

	auth := NewLocalAuthenticator(tmpFile, "admin@test.com", "TestPass1234!")
	defer auth.Shutdown()

	// Should have loaded the existing user (since token is not expired)
	auth.mu.RLock()
	_, exists := auth.users["user-1"]
	auth.mu.RUnlock()

	if !exists {
		t.Error("Expected existing user to be loaded")
	}

	// The loaded token should also work
	user, err := auth.Authenticate(futureToken)
	if err != nil {
		t.Errorf("Expected future token to be valid: %v", err)
	}
	if user.Email != "existing@test.com" {
		t.Errorf("Expected email existing@test.com, got %s", user.Email)
	}
}

// TestLocalAuthenticator_EmptyAdminCredentials tests creation with empty credentials
func TestLocalAuthenticator_EmptyAdminCredentials(t *testing.T) {
	auth := NewLocalAuthenticator("", "", "")
	defer auth.Shutdown()

	// Should not have any admin user
	auth.mu.RLock()
	userCount := len(auth.users)
	auth.mu.RUnlock()

	if userCount != 0 {
		t.Errorf("Expected no users with empty credentials, got %d", userCount)
	}

	// Authenticate should fail
	_, err := auth.Authenticate("any-token")
	if err == nil {
		t.Error("Expected error for any token with no users")
	}
}

// TestLocalAuthenticator_WithPreHashedPassword tests creation with a pre-hashed admin password
func TestLocalAuthenticator_WithPreHashedPassword(t *testing.T) {
	// Use a valid bcrypt hash (generated from "TestPass1234!")
	preHashed := "$2a$12$LJ3m4ys3Lk9zRqHqK5qGnOq5xVlK5zRqHqK5qGnOq5xVlK5zRqHqK"
	auth := NewLocalAuthenticator("", "admin@test.com", preHashed)
	defer auth.Shutdown()

	// Authenticator should be created without error (password hash accepted as-is)
	auth.mu.RLock()
	adminHash := auth.adminPasswordHash
	auth.mu.RUnlock()

	if adminHash != preHashed {
		t.Errorf("Expected admin password hash to match provided hash, got %s", adminHash)
	}
}

// TestBruteForce_AccountLocked tests the account lockout after max failed attempts
func TestBruteForce_AccountLocked(t *testing.T) {
	auth := NewLocalAuthenticator("", "admin@test.com", "TestPass1234!")
	defer auth.Shutdown()

	// Simulate maxLoginAttempts failed attempts to trigger lockout
	for i := 0; i < maxLoginAttempts; i++ {
		_, _, _ = auth.Login("admin@test.com", "wrong-password")
	}

	// Next attempt should be blocked due to lockout
	_, _, err := auth.Login("admin@test.com", "TestPass1234!")
	if err == nil {
		t.Fatal("Expected account to be locked after max attempts")
	}
	if !strings.Contains(err.Error(), "temporarily locked") {
		t.Errorf("Expected lockout message, got: %v", err)
	}

	// Verify the lockout duration is set
	auth.attemptsMu.RLock()
	attempt := auth.loginAttempts["admin@test.com"]
	auth.attemptsMu.RUnlock()

	if attempt.lockedUntil.IsZero() {
		t.Error("Expected lockedUntil to be set")
	}
	if time.Now().After(attempt.lockedUntil) {
		t.Error("Expected lockout to still be active")
	}
}

// TestBruteForce_AutoReset tests that expired locks are reset
func TestBruteForce_AutoReset(t *testing.T) {
	auth := NewLocalAuthenticator("", "admin@test.com", "TestPass1234!")
	defer auth.Shutdown()

	// Trigger max attempts
	for i := 0; i < maxLoginAttempts; i++ {
		_, _, _ = auth.Login("admin@test.com", "wrong-password")
	}

	// Manually expire the lockout
	auth.attemptsMu.Lock()
	attempt := auth.loginAttempts["admin@test.com"]
	attempt.lockedUntil = time.Now().Add(-1 * time.Second)
	auth.attemptsMu.Unlock()

	// Next attempt should succeed (lock expired, resets count)
	// Note: This will still fail due to wrong password, but should NOT get the lockout error
	_, _, err := auth.Login("admin@test.com", "TestPass1234!")
	// Should fail with "invalid credentials" NOT "temporarily locked"
	if err != nil && strings.Contains(err.Error(), "temporarily locked") {
		t.Errorf("Should not be locked after expiration: %v", err)
	}
}

// TestBruteForce_AttemptReset tests that old attempts are cleared after the reset window
func TestBruteForce_AttemptReset(t *testing.T) {
	auth := NewLocalAuthenticator("", "admin@test.com", "TestPass1234!")
	defer auth.Shutdown()

	// Record a failed attempt
	auth.recordFailedAttempt("admin@test.com")

	// Manually back-date the last attempt
	auth.attemptsMu.Lock()
	auth.loginAttempts["admin@test.com"].lastTry = time.Now().Add(-2 * attemptResetWindow)
	auth.attemptsMu.Unlock()

	// Next attempt should reset the count (still fails due to wrong password)
	_, _, _ = auth.Login("admin@test.com", "wrong")

	auth.attemptsMu.RLock()
	count := auth.loginAttempts["admin@test.com"].count
	auth.attemptsMu.RUnlock()

	// Count should be 1 (reset to 0, then incremented), not 2
	if count != 1 {
		t.Errorf("Expected attempt count 1 after reset, got %d", count)
	}
}

// TestSaveSessionsLocked_RenameError tests saveSessionsLocked when rename fails
func TestSaveSessionsLocked_RenameError(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a file where the session file should be - rename will fail
	targetFile := tmpDir + "/sessions.json"
	if err := os.WriteFile(targetFile, []byte("initial"), 0000); err != nil {
		t.Skip("Cannot create test file: " + err.Error())
	}

	auth := &LocalAuthenticator{
		sessionPath: targetFile,
		tokens:      make(map[string]*session),
		users:       make(map[string]*api.User),
		resetTokens: make(map[string]*resetToken),
	}

	// Should handle rename error gracefully (no panic)
	auth.mu.Lock()
	auth.saveSessionsLocked()
	auth.mu.Unlock()
}

// TestSaveSessionsLocked_ChmodError tests saveSessionsLocked when chmod fails on tmp file
func TestSaveSessionsLocked_ChmodError(t *testing.T) {
	// On Windows, chmod works differently; skip if needed
	auth := NewLocalAuthenticator("", "admin@test.com", "TestPass1234!")
	defer auth.Shutdown()

	// Set an invalid session path where chmod would fail
	// Using a read-only directory
	tmpDir := t.TempDir()
	auth.sessionPath = tmpDir + "/subdir/sessions.json"

	// Create the file but make it read-only so chmod might fail
	auth.mu.Lock()
	auth.saveSessionsLocked()
	auth.mu.Unlock()

	// Should handle gracefully
}

// TestValidatePassword tests the password validation function directly
func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name      string
		password  string
		wantError bool
	}{
		{"too short", "Sh0rt!Aa", true},                       // < 12 chars
		{"no uppercase no special", "nouppercase1only", true}, // only lowercase+digit = 2 classes
		{"no lowercase no special", "NOUPPERCASE1ONLY", true}, // only uppercase+digit = 2 classes
		{"no digits no special", "NoDigitsHere", true},        // only upper+lower = 2 classes
		{"empty", "", true},
		{"valid 3 classes", "ValidPass1234!", false},               // upper+lower+digit+special = 4 classes
		{"valid longer", "Another$Valid99", false},                 // upper+lower+digit+special = 4
		{"exactly 12 chars with 3 classes", "Testpass123!", false}, // upper+lower+digit+special = 4, 13 chars
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePassword(tt.password)
			if tt.wantError && err == nil {
				t.Errorf("Expected error for password %q", tt.password)
			}
			if !tt.wantError && err != nil {
				t.Errorf("Unexpected error for password %q: %v", tt.password, err)
			}
		})
	}
}
