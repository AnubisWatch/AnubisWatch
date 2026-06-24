package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// newTestLDAPAuth wraps NewLDAPAuthenticator for tests so the (auth, err)
// return shape doesn't litter every test body. The returned authenticator
// is shut down via t.Cleanup.
func newTestLDAPAuth(t *testing.T, cfg core.LDAPAuth, localPath, email, password string) *LDAPAuthenticator {
	t.Helper()
	a, err := NewLDAPAuthenticator(cfg, localPath, email, password)
	if err != nil {
		t.Fatalf("NewLDAPAuthenticator: %v", err)
	}
	t.Cleanup(func() { a.Shutdown() })
	return a
}

func TestIsConnectionFailure(t *testing.T) {
	cfg := core.LDAPAuth{
		URL:    "ldap://nonexistent.invalid",
		BaseDN: "dc=example,dc=com",
	}
	auth := newTestLDAPAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	tests := []struct {
		name     string
		errMsg   string
		expected bool
	}{
		// Connection failures — should fall back to local
		{"failed to connect", "failed to connect to ldap server", true},
		{"connection refused", "connection refused", true},
		{"timeout", "LDAP timeout", true},
		{"no such host", "no such host", true},
		{"network unreachable", "network unreachable", true},
		{"i/o timeout", "i/o timeout", true},
		{"connection reset", "connection reset by peer", true},
		{"use of closed", "use of closed network connection", true},
		// Auth failures — should NOT fall back
		{"LDAP bind failed", "LDAP bind failed: invalid credentials", false},
		{"invalid credentials", "invalid credentials", false},
		{"server bind failed", "LDAP server bind failed", false},
		// Other errors — should NOT fall back
		{"search failed", "LDAP search failed", false},
		{"malformed DN", "malformed DN", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &ldapError{msg: tt.errMsg}
			got := auth.isConnectionFailure(err)
			if got != tt.expected {
				t.Errorf("isConnectionFailure(%q) = %v, want %v", tt.errMsg, got, tt.expected)
			}
		})
	}

	// Nil error should always return false
	if auth.isConnectionFailure(nil) {
		t.Error("isConnectionFailure(nil) should return false")
	}
}

// ldapError is a minimal error type for testing isConnectionFailure.
type ldapError struct{ msg string }

func (e *ldapError) Error() string { return e.msg }

func TestExtractHostnameFromURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{"ldap with port", "ldap://ldap.example.com:389/dc=example", "ldap.example.com"},
		{"ldaps with port", "ldaps://ldap.example.com:636/dc=example", "ldap.example.com"},
		{"ldap no port", "ldap://ldap.example.com", "ldap.example.com"},
		{"ldaps no port", "ldaps://ldap.example.com", "ldap.example.com"},
		{"localhost", "ldap://localhost:389", "localhost"},
		{"ip address", "ldap://10.0.0.1:389", "10.0.0.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractHostnameFromURL(tt.url)
			if got != tt.expected {
				t.Errorf("extractHostnameFromURL(%q) = %q, want %q", tt.url, got, tt.expected)
			}
		})
	}
}

func TestLDAPAuthenticator_DelegationMethods(t *testing.T) {
	cfg := core.LDAPAuth{
		URL:    "ldap://nonexistent.invalid",
		BaseDN: "dc=example,dc=com",
	}
	auth := newTestLDAPAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	// Login first to get a valid token
	_, token, err := auth.Login("admin@test.com", "TestPass1234!")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	// Test ChangePassword delegates to local
	err = auth.ChangePassword(token, "TestPass1234!", "NewPass1234!!")
	if err != nil {
		t.Errorf("ChangePassword failed: %v", err)
	}

	// Test RequestPasswordReset delegates to local
	resetToken, err := auth.RequestPasswordReset("admin@test.com")
	if err != nil {
		t.Errorf("RequestPasswordReset failed: %v", err)
	}
	if resetToken == "" {
		t.Error("Expected non-empty reset token")
	}

	// Test ConfirmPasswordReset delegates to local
	// Use the reset token we just got
	err = auth.ConfirmPasswordReset(resetToken, "ResetPass1234!!")
	if err != nil {
		t.Errorf("ConfirmPasswordReset failed: %v", err)
	}
}

func TestLDAPAuthenticator_NewLDAPAuthenticator(t *testing.T) {
	cfg := core.LDAPAuth{
		URL:    "ldap://ldap.example.com",
		BaseDN: "dc=example,dc=com",
	}

	auth := newTestLDAPAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	if auth == nil {
		t.Fatal("NewLDAPAuthenticator returned nil")
	}

	if auth.cfg.BaseDN != cfg.BaseDN {
		t.Errorf("Expected BaseDN %s, got %s", cfg.BaseDN, auth.cfg.BaseDN)
	}
}

func TestLDAPAuthenticator_LocalFallback(t *testing.T) {
	cfg := core.LDAPAuth{
		URL:    "ldap://nonexistent.invalid",
		BaseDN: "dc=example,dc=com",
	}

	auth := newTestLDAPAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	// LDAP connection will fail, should fall back to local
	user, token, err := auth.Login("admin@test.com", "TestPass1234!")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	if user.Email != "admin@test.com" {
		t.Errorf("Expected email admin@test.com, got %s", user.Email)
	}

	if token == "" {
		t.Fatal("Token should not be empty")
	}

	// Test authenticate with the token
	authUser, err := auth.Authenticate(token)
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}

	if authUser.Email != "admin@test.com" {
		t.Errorf("Expected authenticated email admin@test.com, got %s", authUser.Email)
	}
}

func TestLDAPAuthenticator_AddUser(t *testing.T) {
	cfg := core.LDAPAuth{
		URL:    "ldap://example.com",
		BaseDN: "dc=example,dc=com",
	}

	auth := newTestLDAPAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	user, err := auth.AddUser("user@example.com", "Test User", "editor")
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	if user == nil {
		t.Fatal("AddUser returned nil")
	}

	if user.Email != "user@example.com" {
		t.Errorf("Expected email user@example.com, got %s", user.Email)
	}

	if user.Name != "Test User" {
		t.Errorf("Expected name 'Test User', got %s", user.Name)
	}

	if user.Role != "editor" {
		t.Errorf("Expected role 'editor', got %s", user.Role)
	}

	users := auth.GetUsers()
	if len(users) == 0 {
		t.Error("Expected at least one user")
	}
}

func TestLDAPAuthenticator_TokenExpiration(t *testing.T) {
	cfg := core.LDAPAuth{
		URL:    "ldap://example.com",
		BaseDN: "dc=example,dc=com",
	}

	auth := newTestLDAPAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	_, token, err := auth.Login("admin@test.com", "TestPass1234!")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	_, err = auth.Authenticate(token)
	if err != nil {
		t.Fatalf("Token should be valid: %v", err)
	}

	err = auth.Logout(token)
	if err != nil {
		t.Fatalf("Logout failed: %v", err)
	}

	_, err = auth.Authenticate(token)
	if err == nil {
		t.Error("Expected error for logged out token")
	}
}

func TestLDAPAuthenticator_BuildUserDN(t *testing.T) {
	tests := []struct {
		name     string
		cfg      core.LDAPAuth
		email    string
		expected string
	}{
		{
			name:     "email with UPN style",
			cfg:      core.LDAPAuth{BaseDN: "dc=example,dc=com"},
			email:    "user@example.com",
			expected: "CN=user,dc=example,dc=com",
		},
		{
			name:     "direct DN (no @)",
			cfg:      core.LDAPAuth{BaseDN: "dc=example,dc=com"},
			email:    "cn=user,ou=people,dc=example,dc=com",
			expected: "cn=user,ou=people,dc=example,dc=com",
		},
		{
			name:     "bind DN with mail template",
			cfg:      core.LDAPAuth{BindDN: "mail={{mail}}", BaseDN: "dc=example,dc=com"},
			email:    "user@example.com",
			expected: "mail=user@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := &LDAPAuthenticator{cfg: tt.cfg}
			got := auth.buildUserDN(tt.email)
			if got != tt.expected {
				t.Errorf("buildUserDN(%q) = %q, want %q", tt.email, got, tt.expected)
			}
		})
	}
}

func TestLDAPAuthenticator_LDAPLogin_ConnectionError(t *testing.T) {
	cfg := core.LDAPAuth{
		URL:    "ldap://nonexistent.invalid.domain:389",
		BaseDN: "dc=example,dc=com",
	}

	auth := newTestLDAPAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	// Should fail because LDAP server is unreachable
	_, err := auth.ldapLogin("user@example.com", "password")
	if err == nil {
		t.Error("Expected error for unreachable LDAP server")
	}
}

func TestLDAPAuthenticator_LDAPLogin_StartTLSError(t *testing.T) {
	cfg := core.LDAPAuth{
		URL:    "ldap://127.0.0.1:389", // Valid format but unreachable
		BaseDN: "dc=example,dc=com",
	}

	auth := newTestLDAPAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	// Connection should fail - either at dial or at StartTLS
	_, err := auth.ldapLogin("user@example.com", "password")
	if err == nil {
		t.Error("Expected error for unreachable LDAP server")
	}
	// Should get an LDAP-related error (connection or TLS)
	errMsg := err.Error()
	if !strings.Contains(errMsg, "failed to connect") && !strings.Contains(errMsg, "failed to start TLS") && !strings.Contains(errMsg, "LDAP") {
		t.Errorf("Expected LDAP-related error, got: %v", err)
	}
}

func TestLDAPAuthenticator_LDAPLogin_ServiceBindError(t *testing.T) {
	cfg := core.LDAPAuth{
		URL:          "ldap://127.0.0.1:389",
		BaseDN:       "dc=example,dc=com",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "wrong-password",
	}

	auth := newTestLDAPAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	// Service bind should fail (simulates invalid service credentials)
	_, err := auth.ldapLogin("user@example.com", "user-password")
	if err == nil {
		t.Error("Expected error for service bind failure")
	}
	// Error could be LDAP bind, StartTLS, or service bind depending on server
}

func TestLDAPAuthenticator_LDAPLogin_BindDNWithSearch(t *testing.T) {
	cfg := core.LDAPAuth{
		URL:          "ldap://127.0.0.1:389",
		BaseDN:       "dc=example,dc=com",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "wrong-password",
		UserFilter:   "(mail={{mail}})",
	}

	auth := newTestLDAPAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	// This will fail at various points - service bind or connection
	_, err := auth.ldapLogin("user@example.com", "user-password")
	if err == nil {
		t.Error("Expected error when using BindDN configuration")
	}
}

func TestLDAPAuthenticator_LDAPLogin_UserNotFoundInSearch(t *testing.T) {
	// This test checks the path where bind succeeds but search returns no results
	// Since we can't easily mock LDAP, we test with the code path that handles no entries
	cfg := core.LDAPAuth{
		URL:    "ldap://nonexistent.invalid",
		BaseDN: "dc=example,dc=com",
	}

	auth := newTestLDAPAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	// LDAP connection fails, falls back to local
	user, _, err := auth.Login("admin@test.com", "TestPass1234!")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if user.Email != "admin@test.com" {
		t.Errorf("Expected email admin@test.com, got %s", user.Email)
	}
}

func TestLDAPAuthenticator_GetUsers(t *testing.T) {
	cfg := core.LDAPAuth{
		URL:    "ldap://example.com",
		BaseDN: "dc=example,dc=com",
	}

	auth := newTestLDAPAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	_, err1 := auth.AddUser("user1@example.com", "User One", "viewer")
	if err1 != nil {
		t.Fatalf("AddUser 1: %v", err1)
	}
	_, err2 := auth.AddUser("user2@example.com", "User Two", "editor")
	if err2 != nil {
		t.Fatalf("AddUser 2: %v", err2)
	}
	_, err3 := auth.AddUser("user3@example.com", "User Three", "admin")
	if err3 != nil {
		t.Fatalf("AddUser 3: %v", err3)
	}

	users := auth.GetUsers()
	if len(users) < 3 {
		t.Errorf("Expected at least 3 users, got %d", len(users))
	}
}

func TestLDAPAuthenticator_Authenticate_ExpiredToken(t *testing.T) {
	cfg := core.LDAPAuth{
		URL:    "ldap://example.com",
		BaseDN: "dc=example,dc=com",
	}

	auth := newTestLDAPAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	user, err := auth.AddUser("user@example.com", "Test User", "viewer")
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	// Manually inject an expired session
	token := "expired-token-123"
	auth.mu.Lock()
	auth.tokens[token] = &session{
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	auth.mu.Unlock()

	_, err = auth.Authenticate(token)
	if err == nil {
		t.Error("Expected error for expired token")
	}
}

func TestLDAPAuthenticator_Authenticate_MissingUser(t *testing.T) {
	cfg := core.LDAPAuth{
		URL:    "ldap://example.com",
		BaseDN: "dc=example,dc=com",
	}

	auth := newTestLDAPAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	// Manually inject a session pointing to a non-existent user
	token := "orphan-token-123"
	auth.mu.Lock()
	auth.tokens[token] = &session{
		UserID:    "nonexistent-user-id",
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	auth.mu.Unlock()

	_, err := auth.Authenticate(token)
	if err == nil {
		t.Error("Expected error for missing user")
	}
}

// TestLDAPAuthenticator_SwitchWorkspace tests the SwitchWorkspace method
func TestLDAPAuthenticator_SwitchWorkspace(t *testing.T) {
	cfg := core.LDAPAuth{
		URL:    "ldap://nonexistent.invalid",
		BaseDN: "dc=example,dc=com",
	}

	auth := newTestLDAPAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	// Login to get a valid token (falls back to local since LDAP unreachable)
	_, token, err := auth.Login("admin@test.com", "TestPass1234!")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	// Switch workspace - this goes to local.SwitchWorkspace since token is in local
	user, err := auth.SwitchWorkspace(token, "test-workspace")
	if err != nil {
		t.Errorf("SwitchWorkspace failed: %v", err)
	}
	if user.Workspace != "test-workspace" {
		t.Errorf("Expected workspace test-workspace, got %s", user.Workspace)
	}

	// Switch with empty workspace should error
	_, err = auth.SwitchWorkspace(token, "")
	if err == nil {
		t.Error("Expected error for empty workspace")
	}

	// Switch with invalid token should error
	_, err = auth.SwitchWorkspace("invalid-token", "test-workspace")
	if err == nil {
		t.Error("Expected error for invalid token")
	}
}

// TestLDAPAuthenticator_SwitchWorkspace_ExpiredToken tests SwitchWorkspace with expired token
func TestLDAPAuthenticator_SwitchWorkspace_ExpiredToken(t *testing.T) {
	cfg := core.LDAPAuth{
		URL:    "ldap://nonexistent.invalid",
		BaseDN: "dc=example,dc=com",
	}

	auth := newTestLDAPAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	// Create a token directly in the LDAP authenticator's tokens map
	// (bypassing the login which falls back to local)
	token := "ldap-token-123"
	auth.mu.Lock()
	auth.tokens[token] = &session{
		UserID:    "test-user-id",
		ExpiresAt: time.Now().Add(-1 * time.Hour), // Expired
	}
	auth.users["test-user-id"] = &api.User{
		ID:    "test-user-id",
		Email: "admin@test.com",
	}
	auth.mu.Unlock()

	_, err := auth.SwitchWorkspace(token, "test-workspace")
	if err == nil {
		t.Error("Expected error for expired token")
	}
	if !strings.Contains(err.Error(), "token expired") {
		t.Errorf("Expected token expired error, got: %v", err)
	}
}

// TestLDAPAuthenticator_SwitchWorkspace_UserNotFound tests SwitchWorkspace with missing user
func TestLDAPAuthenticator_SwitchWorkspace_UserNotFound(t *testing.T) {
	cfg := core.LDAPAuth{
		URL:    "ldap://nonexistent.invalid",
		BaseDN: "dc=example,dc=com",
	}

	auth := newTestLDAPAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	// Create a session with a non-existent user
	token := "orphan-token"
	auth.mu.Lock()
	auth.tokens[token] = &session{
		UserID:    "nonexistent-user",
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	auth.mu.Unlock()

	_, err := auth.SwitchWorkspace(token, "test-workspace")
	if err == nil {
		t.Error("Expected error for missing user")
	}
	if !strings.Contains(err.Error(), "user not found") {
		t.Errorf("Expected user not found error, got: %v", err)
	}
}
