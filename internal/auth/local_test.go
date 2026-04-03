package auth

import (
	"testing"
)

func TestLocalAuthenticator(t *testing.T) {
	auth := NewLocalAuthenticator()

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
	auth := NewLocalAuthenticator()

	_, _, err := auth.Login("", "")
	if err == nil {
		t.Error("Expected error for empty credentials")
	}
}
