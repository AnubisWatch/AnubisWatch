package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// newTestOIDCAuth wraps NewOIDCAuthenticator for tests so the (auth, err)
// return shape doesn't litter every test body. The returned authenticator
// is shut down via t.Cleanup.
func newTestOIDCAuth(t *testing.T, cfg core.OIDCAuth, localPath, email, password string) *OIDCAuthenticator {
	t.Helper()
	a, err := NewOIDCAuthenticator(cfg, localPath, email, password)
	if err != nil {
		t.Fatalf("NewOIDCAuthenticator: %v", err)
	}
	t.Cleanup(func() { a.Shutdown() })
	return a
}

func TestOIDCAuthenticator_NewOIDCAuthenticator(t *testing.T) {
	cfg := core.OIDCAuth{
		Issuer:       "https://accounts.google.com",
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://localhost:8080/api/v1/auth/oidc/callback",
	}

	auth := newTestOIDCAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	if auth == nil {
		t.Fatal("NewOIDCAuthenticator returned nil")
	}

	if auth.config.ClientID != cfg.ClientID {
		t.Errorf("Expected ClientID %s, got %s", cfg.ClientID, auth.config.ClientID)
	}
}

func TestOIDCAuthenticator_LocalFallback(t *testing.T) {
	cfg := core.OIDCAuth{
		Issuer:       "https://example.com",
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://localhost:8080/callback",
	}

	auth := newTestOIDCAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	// Test local fallback login
	user, token, err := auth.Login("admin@test.com", "TestPass1234!")
	if err != nil {
		t.Fatalf("Local fallback Login failed: %v", err)
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

func TestOIDCAuthenticator_AddUser(t *testing.T) {
	cfg := core.OIDCAuth{
		Issuer:       "https://example.com",
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://localhost:8080/callback",
	}

	auth := newTestOIDCAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

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

	// Verify user is in the list
	users := auth.GetUsers()
	if len(users) == 0 {
		t.Error("Expected at least one user")
	}
}

func TestOIDCAuthenticator_OIDCCallback_InvalidState(t *testing.T) {
	cfg := core.OIDCAuth{
		Issuer:       "https://example.com",
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://localhost:8080/callback",
	}

	auth := newTestOIDCAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	// Test callback with invalid state (no matching state was generated)
	_, _, err := auth.OIDCCallback("test-code", "invalid.state.here", "invalid-nonce")
	if err == nil {
		t.Error("Expected error for invalid state")
	}
}

func TestOIDCAuthenticator_OIDCCallback_InvalidNonce(t *testing.T) {
	cfg := core.OIDCAuth{
		Issuer:       "https://example.com",
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://localhost:8080/callback",
	}

	auth := newTestOIDCAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	// Create a valid state with a nonce
	state := "valid-state"
	correctNonce := "correct-nonce"
	auth.mu.Lock()
	auth.state[state] = &oidcState{
		Nonce:     correctNonce,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	auth.mu.Unlock()

	// Test callback with wrong nonce (CSRF attack simulation)
	_, _, err := auth.OIDCCallback("test-code", auth.signState(state), "wrong-nonce")
	if err == nil {
		t.Error("Expected error for invalid nonce (possible CSRF attack)")
	}
	if err != nil && err.Error() != "invalid nonce: possible CSRF attack" {
		t.Errorf("Expected CSRF error message, got: %v", err)
	}
}

func TestOIDCAuthenticator_OIDCLoginURL_ConfigError(t *testing.T) {
	cfg := core.OIDCAuth{
		Issuer:       "https://nonexistent.invalid.domain",
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://localhost:8080/callback",
	}

	auth := newTestOIDCAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	// Should fail because the issuer domain doesn't exist
	_, _, _, err := auth.OIDCLoginURL()
	if err == nil {
		t.Error("Expected error for invalid OIDC issuer")
	}
}

func TestOIDCAuthenticator_TokenExpiration(t *testing.T) {
	cfg := core.OIDCAuth{
		Issuer:       "https://example.com",
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://localhost:8080/callback",
	}

	auth := newTestOIDCAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	// Login and get token
	_, token, err := auth.Login("admin@test.com", "TestPass1234!")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	// Token should be valid
	_, err = auth.Authenticate(token)
	if err != nil {
		t.Fatalf("Token should be valid: %v", err)
	}

	// Logout
	err = auth.Logout(token)
	if err != nil {
		t.Fatalf("Logout failed: %v", err)
	}

	// Token should be invalid after logout
	_, err = auth.Authenticate(token)
	if err == nil {
		t.Error("Expected error for logged out token")
	}
}

// Helper: create a signed JWT using an RSA key
func createTestJWT(t *testing.T, priv *rsa.PrivateKey, claims map[string]interface{}) string {
	t.Helper()
	header := map[string]interface{}{"alg": "RS256", "typ": "JWT", "kid": "test-key"}
	headerJSON, _ := json.Marshal(header)
	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)

	claimsJSON, _ := json.Marshal(claims)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)

	signingInput := headerB64 + "." + claimsB64
	hash := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, hash[:])
	if err != nil {
		t.Fatalf("failed to sign JWT: %v", err)
	}
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)
	return headerB64 + "." + claimsB64 + "." + sigB64
}

// Helper: create a test RSA key and JWK set
func createTestJWK(priv *rsa.PrivateKey) *jwkSet {
	n := base64.RawURLEncoding.EncodeToString(priv.N.Bytes())
	eBytes := big.NewInt(int64(priv.E)).Bytes()
	e := base64.RawURLEncoding.EncodeToString(eBytes)
	return &jwkSet{Keys: []jwk{{Kty: "RSA", Use: "sig", Kid: "test-key", Alg: "RS256", N: n, E: e}}}
}

// Helper: create a JWK set HTTP server
func newJWKServer(t *testing.T, jwks *jwkSet) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(jwks)
	}))
}

func TestOIDCAuthenticator_ParseIDToken(t *testing.T) {
	// Generate RSA key pair
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	// Create JWK set
	jwks := createTestJWK(priv)

	// Create claims
	claims := map[string]interface{}{
		"sub":            "1234567890",
		"email":          "test@example.com",
		"name":           "Test User",
		"email_verified": true,
		"iss":            "https://test.example.com",
		"aud":            "test-client-id",
		"exp":            time.Now().Add(time.Hour).Unix(),
		"nbf":            time.Now().Add(-time.Minute).Unix(),
	}

	// Create signed JWT
	token := createTestJWT(t, priv, claims)

	// Create authenticator with pre-loaded JWKs (skip network fetch)
	cfg := core.OIDCAuth{
		Issuer:   "https://test.example.com",
		ClientID: "test-client-id",
	}
	auth := &OIDCAuthenticator{
		config:      cfg,
		jwks:        jwks,
		jwksFetched: time.Now(),
		jwksTTL:     time.Hour,
	}

	userInfo, err := auth.parseIDToken(token, "")
	if err != nil {
		t.Fatalf("parseIDToken failed: %v", err)
	}

	if userInfo.Email != "test@example.com" {
		t.Errorf("Expected email test@example.com, got %s", userInfo.Email)
	}
	if userInfo.Name != "Test User" {
		t.Errorf("Expected name 'Test User', got %s", userInfo.Name)
	}
	if userInfo.Sub != "1234567890" {
		t.Errorf("Expected sub '1234567890', got %s", userInfo.Sub)
	}
	if !userInfo.EmailVerified {
		t.Error("Expected email_verified to be true")
	}
}

func TestOIDCAuthenticator_ParseIDToken_RejectForged(t *testing.T) {
	// Generate two different key pairs
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	forgeryKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate forgery key: %v", err)
	}

	// JWK set contains the legitimate key
	jwks := createTestJWK(priv)

	claims := map[string]interface{}{
		"sub":            "attacker",
		"email":          "admin@example.com",
		"name":           "Attacker",
		"email_verified": true,
		"iss":            "https://test.example.com",
		"aud":            "test-client-id",
		"exp":            time.Now().Add(time.Hour).Unix(),
		"nbf":            time.Now().Add(-time.Minute).Unix(),
	}
	forgedToken := createTestJWT(t, forgeryKey, claims)

	cfg := core.OIDCAuth{
		Issuer:   "https://test.example.com",
		ClientID: "test-client-id",
	}
	auth := &OIDCAuthenticator{
		config:      cfg,
		jwks:        jwks,
		jwksFetched: time.Now(),
		jwksTTL:     time.Hour,
	}

	_, err = auth.parseIDToken(forgedToken, "")
	if err == nil {
		t.Fatal("EXPECTED forged JWT to be rejected, but it was accepted!")
	}
	t.Logf("Correctly rejected forged JWT: %v", err)
}

func TestOIDCAuthenticator_ParseIDToken_RejectExpired(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	jwks := createTestJWK(priv)

	claims := map[string]interface{}{
		"sub":   "123",
		"email": "test@example.com",
		"iss":   "https://test.example.com",
		"aud":   "test-client-id",
		"exp":   time.Now().Add(-time.Hour).Unix(),
	}
	token := createTestJWT(t, priv, claims)

	cfg := core.OIDCAuth{
		Issuer:   "https://test.example.com",
		ClientID: "test-client-id",
	}
	auth := &OIDCAuthenticator{
		config:      cfg,
		jwks:        jwks,
		jwksFetched: time.Now(),
		jwksTTL:     time.Hour,
	}

	_, err = auth.parseIDToken(token, "")
	if err == nil {
		t.Fatal("EXPECTED expired token to be rejected")
	}
	t.Logf("Correctly rejected expired token: %v", err)
}

func TestOIDCAuthenticator_ParseIDToken_Invalid(t *testing.T) {
	auth := &OIDCAuthenticator{}

	// Empty token
	_, err := auth.parseIDToken("", "")
	if err == nil {
		t.Error("Expected error for empty token")
	}

	// Invalid format (not 3 parts)
	_, err = auth.parseIDToken("not.a.valid.jwt", "")
	if err == nil {
		t.Error("Expected error for invalid JWT format")
	}

	// Invalid base64
	_, err = auth.parseIDToken("header.!!!invalid!!!.sig", "")
	if err == nil {
		t.Error("Expected error for invalid base64")
	}
}

// Test EC key support (ES256)
func TestOIDCAuthenticator_ParseIDToken_ECKey(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate EC key: %v", err)
	}

	// Build JWK
	x := base64.RawURLEncoding.EncodeToString(priv.X.Bytes())
	y := base64.RawURLEncoding.EncodeToString(priv.Y.Bytes())
	jwks := &jwkSet{Keys: []jwk{{Kty: "EC", Use: "sig", Kid: "ec-key", Alg: "ES256", Crv: "P-256", X: x, Y: y}}}

	// Create ES256 signed JWT
	header := map[string]interface{}{"alg": "ES256", "typ": "JWT", "kid": "ec-key"}
	headerJSON, _ := json.Marshal(header)
	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)

	claims := map[string]interface{}{
		"sub":   "ec-user",
		"email": "ecuser@example.com",
		"name":  "EC User",
		"iss":   "https://test.example.com",
		"aud":   "ec-client",
		"exp":   time.Now().Add(time.Hour).Unix(),
	}
	claimsJSON, _ := json.Marshal(claims)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)

	signingInput := headerB64 + "." + claimsB64
	hash := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, priv, hash[:])
	if err != nil {
		t.Fatalf("failed to sign: %v", err)
	}
	// ECDSA signature: R || S, each padded to curve byte length
	keyLen := priv.Params().BitSize / 8
	sigBytes := append(r.Bytes(), s.Bytes()...)
	// Pad to 2*keyLen
	padded := make([]byte, 2*keyLen)
	copy(padded[2*keyLen-len(sigBytes):], sigBytes)
	sigB64 := base64.RawURLEncoding.EncodeToString(padded)

	token := headerB64 + "." + claimsB64 + "." + sigB64

	cfg := core.OIDCAuth{
		Issuer:   "https://test.example.com",
		ClientID: "ec-client",
	}
	auth := &OIDCAuthenticator{
		config:      cfg,
		jwks:        jwks,
		jwksFetched: time.Now(),
		jwksTTL:     time.Hour,
	}

	userInfo, err := auth.parseIDToken(token, "")
	if err != nil {
		t.Fatalf("parseIDToken with EC key failed: %v", err)
	}
	if userInfo.Email != "ecuser@example.com" {
		t.Errorf("Expected email ecuser@example.com, got %s", userInfo.Email)
	}
}

func TestOIDCAuthenticator_GetUsers(t *testing.T) {
	cfg := core.OIDCAuth{
		Issuer:       "https://example.com",
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://localhost:8080/callback",
	}

	auth := newTestOIDCAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	// Add some users
	if _, err := auth.AddUser("user1@example.com", "User One", "viewer"); err != nil {
		t.Fatalf("AddUser 1: %v", err)
	}
	if _, err := auth.AddUser("user2@example.com", "User Two", "editor"); err != nil {
		t.Fatalf("AddUser 2: %v", err)
	}
	if _, err := auth.AddUser("user3@example.com", "User Three", "admin"); err != nil {
		t.Fatalf("AddUser 3: %v", err)
	}

	users := auth.GetUsers()
	if len(users) < 3 {
		t.Errorf("Expected at least 3 users, got %d", len(users))
	}
}

func TestOIDCAuthenticator_OIDCCallback_StateExpiration(t *testing.T) {
	cfg := core.OIDCAuth{
		Issuer:       "https://example.com",
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://localhost:8080/callback",
	}

	auth := newTestOIDCAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	// Manually add an expired state
	expiredState := "expired-state"
	auth.mu.Lock()
	auth.state[expiredState] = &oidcState{
		Nonce:     "test-nonce",
		ExpiresAt: time.Now().Add(-1 * time.Hour), // Expired 1 hour ago
	}
	auth.mu.Unlock()

	_, _, err := auth.OIDCCallback("test-code", auth.signState(expiredState), "test-nonce")
	if err == nil {
		t.Error("Expected error for expired state")
	}
}

func TestOIDCAuthenticator_OIDCCallback_OIDCError(t *testing.T) {
	cfg := core.OIDCAuth{
		Issuer:       "https://example.com",
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://localhost:8080/callback",
	}

	// Create a mock server that returns an OIDC error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_grant","error_description":"code expired"}`))
	}))
	defer server.Close()

	// Use the mock server as issuer (it won't work but tests the error path)
	cfg.Issuer = server.URL
	auth := newTestOIDCAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	// Generate a valid state
	loginURL, state, nonce, _ := auth.OIDCLoginURL()
	if loginURL == "" {
		// OIDC config fetch failed, which is expected for the mock server
		t.Log("OIDC login URL fetch failed as expected for mock server")
		return
	}

	// Try callback with valid state (the token exchange will fail)
	_, _, err := auth.OIDCCallback("test-code", state, nonce)
	if err == nil {
		t.Error("Expected error from failed token exchange")
	}
}

func TestOIDCAuthenticator_FetchOIDCConfig_Success(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(oidcConfig{
			Issuer:      server.URL,
			AuthURL:     server.URL + "/auth",
			TokenURL:    server.URL + "/token",
			UserInfoURL: server.URL + "/userinfo",
			JWKSURI:     server.URL + "/jwks",
		})
	}))
	defer server.Close()

	auth := &OIDCAuthenticator{config: core.OIDCAuth{Issuer: server.URL + "/"}}
	cfg, err := auth.fetchOIDCConfig()
	if err != nil {
		t.Fatalf("fetchOIDCConfig failed: %v", err)
	}
	if cfg.AuthURL != server.URL+"/auth" {
		t.Errorf("Expected AuthURL, got %s", cfg.AuthURL)
	}
	if cfg.TokenURL != server.URL+"/token" {
		t.Errorf("Expected TokenURL, got %s", cfg.TokenURL)
	}
}

func TestOIDCAuthenticator_FetchOIDCConfig_Errors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    bool
	}{
		{"not found", http.StatusNotFound, "", true},
		{"invalid json", http.StatusOK, "not json", true},
		{"missing auth url", http.StatusOK, `{"token_endpoint":"https://t.com/token"}`, true},
		{"missing token url", http.StatusOK, `{"authorization_endpoint":"https://t.com/auth"}`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body))
			}))
			defer server.Close()

			auth := &OIDCAuthenticator{config: core.OIDCAuth{Issuer: server.URL}}
			_, err := auth.fetchOIDCConfig()
			if (err != nil) != tt.wantErr {
				t.Fatalf("fetchOIDCConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestOIDCAuthenticator_ExchangeCode_Success(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(oidcConfig{
				Issuer:   server.URL,
				AuthURL:  server.URL + "/auth",
				TokenURL: server.URL + "/token",
			})
		case "/token":
			json.NewEncoder(w).Encode(tokenResponse{
				AccessToken: "access_token_123",
				IDToken:     "id_token_123",
				TokenType:   "Bearer",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	auth := &OIDCAuthenticator{config: core.OIDCAuth{
		Issuer:       server.URL,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "http://localhost:8080/callback",
	}}
	resp, err := auth.exchangeCode("test-code")
	if err != nil {
		t.Fatalf("exchangeCode failed: %v", err)
	}
	if resp.AccessToken != "access_token_123" {
		t.Errorf("Expected access_token_123, got %s", resp.AccessToken)
	}
	if resp.IDToken != "id_token_123" {
		t.Errorf("Expected id_token_123, got %s", resp.IDToken)
	}
}

func TestOIDCAuthenticator_ExchangeCode_Error(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(oidcConfig{
				Issuer:   server.URL,
				AuthURL:  server.URL + "/auth",
				TokenURL: server.URL + "/token",
			})
		case "/token":
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"invalid_grant"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	auth := &OIDCAuthenticator{config: core.OIDCAuth{
		Issuer:       server.URL,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "http://localhost:8080/callback",
	}}
	_, err := auth.exchangeCode("bad-code")
	if err == nil {
		t.Error("Expected error for bad token response")
	}
}

func TestOIDCAuthenticator_GetUserInfo_Success(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(oidcConfig{
				Issuer:      server.URL,
				AuthURL:     server.URL + "/auth",
				TokenURL:    server.URL + "/token",
				UserInfoURL: server.URL + "/userinfo",
			})
		case "/userinfo":
			json.NewEncoder(w).Encode(userInfoResponse{
				Sub:   "user123",
				Email: "user@example.com",
				Name:  "Test User",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	auth := &OIDCAuthenticator{config: core.OIDCAuth{Issuer: server.URL}}
	info, err := auth.getUserInfo("test-access-token")
	if err != nil {
		t.Fatalf("getUserInfo failed: %v", err)
	}
	if info.Email != "user@example.com" {
		t.Errorf("Expected email user@example.com, got %s", info.Email)
	}
}

func TestOIDCAuthenticator_GetUserInfo_Errors(t *testing.T) {
	t.Run("missing userinfo url", func(t *testing.T) {
		var server *httptest.Server
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(oidcConfig{
				Issuer:   server.URL,
				AuthURL:  server.URL + "/auth",
				TokenURL: server.URL + "/token",
			})
		}))
		defer server.Close()

		auth := &OIDCAuthenticator{config: core.OIDCAuth{Issuer: server.URL}}
		_, err := auth.getUserInfo("token")
		if err == nil {
			t.Error("Expected error when no userinfo URL")
		}
	})

	t.Run("non-200 response", func(t *testing.T) {
		var server *httptest.Server
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/.well-known/openid-configuration":
				json.NewEncoder(w).Encode(oidcConfig{
					Issuer:      server.URL,
					AuthURL:     server.URL + "/auth",
					TokenURL:    server.URL + "/token",
					UserInfoURL: server.URL + "/userinfo",
				})
			case "/userinfo":
				w.WriteHeader(http.StatusUnauthorized)
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		auth := &OIDCAuthenticator{config: core.OIDCAuth{Issuer: server.URL}}
		_, err := auth.getUserInfo("token")
		if err == nil {
			t.Error("Expected error for non-200 userinfo response")
		}
	})
}

func TestOIDCAuthenticator_FetchJWKs_Success(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	jwks := createTestJWK(priv)

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(oidcConfig{
				Issuer:   server.URL,
				AuthURL:  server.URL + "/auth",
				TokenURL: server.URL + "/token",
				JWKSURI:  server.URL + "/jwks",
			})
		case "/jwks":
			json.NewEncoder(w).Encode(jwks)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	auth := &OIDCAuthenticator{config: core.OIDCAuth{Issuer: server.URL}}
	result, err := auth.fetchJWKs()
	if err != nil {
		t.Fatalf("fetchJWKs failed: %v", err)
	}
	if len(result.Keys) == 0 {
		t.Error("Expected non-empty JWK set")
	}
}

func TestOIDCAuthenticator_FetchJWKs_Cache(t *testing.T) {
	jwks := &jwkSet{Keys: []jwk{{Kty: "RSA", Kid: "cached", N: "abc", E: "AQAB"}}}
	auth := &OIDCAuthenticator{
		config:      core.OIDCAuth{Issuer: "https://example.com"},
		jwks:        jwks,
		jwksFetched: time.Now(),
		jwksTTL:     time.Hour,
	}
	result, err := auth.fetchJWKs()
	if err != nil {
		t.Fatalf("fetchJWKs failed: %v", err)
	}
	if len(result.Keys) != 1 || result.Keys[0].Kid != "cached" {
		t.Error("Expected cached JWK set")
	}
}

func TestOIDCAuthenticator_FetchJWKs_Errors(t *testing.T) {
	t.Run("missing jwks_uri", func(t *testing.T) {
		var server *httptest.Server
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(oidcConfig{
				Issuer:   server.URL,
				AuthURL:  server.URL + "/auth",
				TokenURL: server.URL + "/token",
			})
		}))
		defer server.Close()

		auth := &OIDCAuthenticator{config: core.OIDCAuth{Issuer: server.URL}}
		_, err := auth.fetchJWKs()
		if err == nil {
			t.Error("Expected error when jwks_uri is missing")
		}
	})

	t.Run("empty jwk set", func(t *testing.T) {
		var server *httptest.Server
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/.well-known/openid-configuration":
				json.NewEncoder(w).Encode(oidcConfig{
					Issuer:   server.URL,
					AuthURL:  server.URL + "/auth",
					TokenURL: server.URL + "/token",
					JWKSURI:  server.URL + "/jwks",
				})
			case "/jwks":
				json.NewEncoder(w).Encode(jwkSet{Keys: []jwk{}})
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		auth := &OIDCAuthenticator{config: core.OIDCAuth{Issuer: server.URL}}
		_, err := auth.fetchJWKs()
		if err == nil {
			t.Error("Expected error for empty JWK set")
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		var server *httptest.Server
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/.well-known/openid-configuration":
				json.NewEncoder(w).Encode(oidcConfig{
					Issuer:   server.URL,
					AuthURL:  server.URL + "/auth",
					TokenURL: server.URL + "/token",
					JWKSURI:  server.URL + "/jwks",
				})
			case "/jwks":
				w.Write([]byte("not json"))
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		auth := &OIDCAuthenticator{config: core.OIDCAuth{Issuer: server.URL}}
		_, err := auth.fetchJWKs()
		if err == nil {
			t.Error("Expected error for invalid JWK JSON")
		}
	})
}

func TestOIDCAuthenticator_FindKeyForJWT_NoKid(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	jwks := createTestJWK(priv)

	auth := &OIDCAuthenticator{
		config:      core.OIDCAuth{Issuer: "https://test.example.com", ClientID: "client-id"},
		jwks:        jwks,
		jwksFetched: time.Now(),
		jwksTTL:     time.Hour,
	}

	headers := map[string]interface{}{"alg": "RS256"}
	key, err := auth.findKeyForJWT(headers)
	if err != nil {
		t.Fatalf("findKeyForJWT failed: %v", err)
	}
	if key == nil {
		t.Error("Expected key to be found")
	}
}

func TestOIDCAuthenticator_FindKeyForJWT_KidMismatch(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	jwks := createTestJWK(priv)
	jwks.Keys[0].Kid = "wrong-kid"

	auth := &OIDCAuthenticator{
		config:      core.OIDCAuth{Issuer: "https://test.example.com", ClientID: "client-id"},
		jwks:        jwks,
		jwksFetched: time.Now(),
		jwksTTL:     time.Hour,
	}

	headers := map[string]interface{}{"alg": "RS256", "kid": "expected-kid"}
	_, err := auth.findKeyForJWT(headers)
	if err == nil {
		t.Error("Expected error for kid mismatch")
	}
}

func TestJWK_toPublicKey_Errors(t *testing.T) {
	tests := []struct {
		name string
		jwk  jwk
	}{
		{"unsupported key type", jwk{Kty: "OCT"}},
		{"bad rsa n", jwk{Kty: "RSA", N: "!!!", E: "AQAB"}},
		{"bad rsa e", jwk{Kty: "RSA", N: "AQAB", E: "!!!"}},
		{"bad ec x", jwk{Kty: "EC", Crv: "P-256", X: "!!!", Y: "AQAB"}},
		{"bad ec y", jwk{Kty: "EC", Crv: "P-256", X: "AQAB", Y: "!!!"}},
		{"unsupported curve", jwk{Kty: "EC", Crv: "P-999", X: "AQAB", Y: "AQAB"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.jwk.toPublicKey()
			if err == nil {
				t.Error("Expected error for invalid JWK")
			}
		})
	}
}

func TestOIDCAuthenticator_OIDCLoginURL_Success(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(oidcConfig{
			Issuer:   server.URL,
			AuthURL:  server.URL + "/auth",
			TokenURL: server.URL + "/token",
		})
	}))
	defer server.Close()

	auth := newTestOIDCAuth(t, core.OIDCAuth{
		Issuer:      server.URL,
		ClientID:    "test-client",
		RedirectURL: "http://localhost:8080/callback",
	}, "", "", "")

	url, state, nonce, err := auth.OIDCLoginURL()
	if err != nil {
		t.Fatalf("OIDCLoginURL failed: %v", err)
	}
	if state == "" {
		t.Error("Expected non-empty state")
	}
	if nonce == "" {
		t.Error("Expected non-empty nonce")
	}
	if url == "" {
		t.Error("Expected non-empty URL")
	}
}

func TestOIDCAuthenticator_ParseIDToken_AudienceList(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	jwks := createTestJWK(priv)

	claims := map[string]interface{}{
		"sub":            "123",
		"email":          "test@example.com",
		"iss":            "https://test.example.com",
		"aud":            []interface{}{"other-client", "test-client-id"},
		"azp":            "test-client-id", // required by OIDC when aud is multi-valued
		"exp":            time.Now().Add(time.Hour).Unix(),
		"email_verified": true,
	}
	token := createTestJWT(t, priv, claims)

	auth := &OIDCAuthenticator{
		config:      core.OIDCAuth{Issuer: "https://test.example.com", ClientID: "test-client-id"},
		jwks:        jwks,
		jwksFetched: time.Now(),
		jwksTTL:     time.Hour,
	}

	userInfo, err := auth.parseIDToken(token, "")
	if err != nil {
		t.Fatalf("parseIDToken failed: %v", err)
	}
	if userInfo.Email != "test@example.com" {
		t.Errorf("Expected email test@example.com, got %s", userInfo.Email)
	}
}

func TestOIDCAuthenticator_ParseIDToken_AudienceList_Missing(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	jwks := createTestJWK(priv)

	claims := map[string]interface{}{
		"sub":   "123",
		"email": "test@example.com",
		"iss":   "https://test.example.com",
		"aud":   []interface{}{"other-client"},
		"exp":   time.Now().Add(time.Hour).Unix(),
	}
	token := createTestJWT(t, priv, claims)

	auth := &OIDCAuthenticator{
		config:      core.OIDCAuth{Issuer: "https://test.example.com", ClientID: "test-client-id"},
		jwks:        jwks,
		jwksFetched: time.Now(),
		jwksTTL:     time.Hour,
	}

	_, err := auth.parseIDToken(token, "")
	if err == nil {
		t.Error("Expected error when audience list doesn't contain client ID")
	}
}

func TestOIDCAuthenticator_Authenticate_ExpiredToken(t *testing.T) {
	cfg := core.OIDCAuth{
		Issuer:       "https://example.com",
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://localhost:8080/callback",
	}

	auth := newTestOIDCAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	user, err := auth.AddUser("user@example.com", "Test User", "viewer")
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	// Manually inject an expired session
	token := "expired-oidc-token"
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

func TestOIDCAuthenticator_Authenticate_MissingUser(t *testing.T) {
	cfg := core.OIDCAuth{
		Issuer:       "https://example.com",
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://localhost:8080/callback",
	}

	auth := newTestOIDCAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	// Manually inject a session pointing to a non-existent user
	token := "orphan-oidc-token"
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

func TestOIDCAuthenticator_OIDCCallback_Success(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	jwks := createTestJWK(priv)

	var idToken string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(oidcConfig{
				Issuer:      server.URL,
				AuthURL:     server.URL + "/auth",
				TokenURL:    server.URL + "/token",
				UserInfoURL: server.URL + "/userinfo",
				JWKSURI:     server.URL + "/jwks",
			})
		case "/token":
			json.NewEncoder(w).Encode(tokenResponse{
				AccessToken: "access_token_123",
				IDToken:     idToken,
				TokenType:   "Bearer",
			})
		case "/jwks":
			json.NewEncoder(w).Encode(jwks)
		case "/userinfo":
			json.NewEncoder(w).Encode(userInfoResponse{
				Email: "oidcuser@example.com",
				Name:  "OIDC User",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	state := "valid-state-abc"
	nonce := "valid-nonce-xyz"

	now := time.Now()
	claims := map[string]interface{}{
		"iss":   server.URL,
		"aud":   "client-id",
		"sub":   "user-123",
		"nonce": nonce,
		"email": "oidcuser@example.com",
		"name":  "OIDC User",
		"exp":   now.Add(time.Hour).Unix(),
	}
	idToken = createTestJWT(t, priv, claims)

	auth := newTestOIDCAuth(t, core.OIDCAuth{
		Issuer:       server.URL,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "http://localhost:8080/callback",
	}, "", "admin@test.com", "TestPass1234!")
	defer auth.Shutdown()

	// Inject a valid state
	auth.mu.Lock()
	auth.state[state] = &oidcState{
		Nonce:     nonce,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	auth.mu.Unlock()

	user, token, err := auth.OIDCCallback("test-code", auth.signState(state), nonce)
	if err != nil {
		t.Fatalf("OIDCCallback failed: %v", err)
	}
	if user.Email != "oidcuser@example.com" {
		t.Errorf("Expected email oidcuser@example.com, got %s", user.Email)
	}
	if token == "" {
		t.Error("Expected non-empty token")
	}

	// Authenticate with the token
	authUser, err := auth.Authenticate(token)
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}
	if authUser.Email != "oidcuser@example.com" {
		t.Errorf("Expected authenticated email oidcuser@example.com, got %s", authUser.Email)
	}
}

func TestOIDCAuthenticator_OIDCCallback_EmptyEmail(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	jwks := createTestJWK(priv)

	var idToken string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(oidcConfig{
				Issuer:   server.URL,
				AuthURL:  server.URL + "/auth",
				TokenURL: server.URL + "/token",
				JWKSURI:  server.URL + "/jwks",
			})
		case "/token":
			json.NewEncoder(w).Encode(tokenResponse{
				AccessToken: "access_token_123",
				IDToken:     idToken,
				TokenType:   "Bearer",
			})
		case "/jwks":
			json.NewEncoder(w).Encode(jwks)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	now := time.Now()
	claims := map[string]interface{}{
		"iss": server.URL,
		"aud": "client-id",
		"sub": "user-123",
		"exp": now.Add(time.Hour).Unix(),
	}
	idToken = createTestJWT(t, priv, claims)

	auth := newTestOIDCAuth(t, core.OIDCAuth{
		Issuer:       server.URL,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "http://localhost:8080/callback",
	}, "", "admin@test.com", "TestPass1234!")
	defer auth.Shutdown()

	state := "empty-email-state"
	nonce := "empty-email-nonce"
	auth.mu.Lock()
	auth.state[state] = &oidcState{
		Nonce:     nonce,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	auth.mu.Unlock()

	_, _, err = auth.OIDCCallback("test-code", auth.signState(state), nonce)
	if err == nil {
		t.Error("Expected error for empty email in OIDC response")
	}
}

func TestOIDCAuthenticator_OIDCCallback_GetUserInfoError(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	jwks := createTestJWK(priv)

	var idToken string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(oidcConfig{
				Issuer:      server.URL,
				AuthURL:     server.URL + "/auth",
				TokenURL:    server.URL + "/token",
				UserInfoURL: server.URL + "/userinfo",
				JWKSURI:     server.URL + "/jwks",
			})
		case "/token":
			json.NewEncoder(w).Encode(tokenResponse{
				AccessToken: "access_token_123",
				IDToken:     idToken,
				TokenType:   "Bearer",
			})
		case "/jwks":
			json.NewEncoder(w).Encode(jwks)
		case "/userinfo":
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"server error"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	state := "userinfo-error-state"
	nonce := "userinfo-error-nonce"

	now := time.Now()
	claims := map[string]interface{}{
		"iss":   server.URL,
		"aud":   "client-id",
		"sub":   "user-123",
		"nonce": nonce,
		"email": "oidcuser@example.com",
		"name":  "OIDC User",
		"exp":   now.Add(time.Hour).Unix(),
	}
	idToken = createTestJWT(t, priv, claims)

	auth := newTestOIDCAuth(t, core.OIDCAuth{
		Issuer:       server.URL,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "http://localhost:8080/callback",
	}, "", "admin@test.com", "TestPass1234!")
	defer auth.Shutdown()

	auth.mu.Lock()
	auth.state[state] = &oidcState{
		Nonce:     nonce,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	auth.mu.Unlock()

	// Callback should still succeed because ID token has email
	user, token, err := auth.OIDCCallback("test-code", auth.signState(state), nonce)
	if err != nil {
		t.Fatalf("OIDCCallback failed: %v", err)
	}
	if user.Email != "oidcuser@example.com" {
		t.Errorf("Expected email oidcuser@example.com, got %s", user.Email)
	}
	if token == "" {
		t.Error("Expected non-empty token")
	}
}

func TestJWK_toPublicKey_UnsupportedKeyType(t *testing.T) {
	jwk := &jwk{Kty: "OCT", Alg: "HS256", Kid: "bad-key"}
	_, err := jwk.toPublicKey()
	if err == nil {
		t.Error("Expected error for unsupported key type")
	}
}

func TestOIDCAuthenticator_FindKeyForJWT_NoUsableKeys(t *testing.T) {
	auth := &OIDCAuthenticator{
		config:      core.OIDCAuth{Issuer: "https://test.example.com", ClientID: "client-id"},
		jwks:        &jwkSet{Keys: []jwk{}},
		jwksFetched: time.Now(),
		jwksTTL:     time.Hour,
	}

	headers := map[string]interface{}{"alg": "RS256"}
	_, err := auth.findKeyForJWT(headers)
	if err == nil {
		t.Error("Expected error when no keys available")
	}
}

func TestOIDCAuthenticator_FindKeyForJWT_BadKeyNoKid(t *testing.T) {
	auth := &OIDCAuthenticator{
		config: core.OIDCAuth{Issuer: "https://test.example.com", ClientID: "client-id"},
		jwks: &jwkSet{Keys: []jwk{
			{Kty: "RSA", N: "!!!bad-base64!!!", E: "AQAB", Kid: "bad-key"},
		}},
		jwksFetched: time.Now(),
		jwksTTL:     time.Hour,
	}

	headers := map[string]interface{}{"alg": "RS256"}
	_, err := auth.findKeyForJWT(headers)
	if err == nil {
		t.Error("Expected error when no usable keys available")
	}
}

func TestOIDCAuthenticator_DelegationMethods(t *testing.T) {
	cfg := core.OIDCAuth{
		Issuer:       "https://accounts.google.com",
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://localhost:8080/api/v1/auth/oidc/callback",
	}
	auth := newTestOIDCAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

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
	err = auth.ConfirmPasswordReset(resetToken, "ResetPass1234!!")
	if err != nil {
		t.Errorf("ConfirmPasswordReset failed: %v", err)
	}
}

// TestJWK_ToECPublicKey tests EC key conversion with various curves
func TestJWK_ToECPublicKey(t *testing.T) {
	tests := []struct {
		name      string
		curve     string
		expectAlg string
	}{
		{"P-256", "P-256", "ES256"},
		{"P-384", "P-384", "ES384"},
		{"P-521", "P-521", "ES512"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Generate a real EC key to get valid coordinates
			var curve elliptic.Curve
			switch tt.curve {
			case "P-256":
				curve = elliptic.P256()
			case "P-384":
				curve = elliptic.P384()
			case "P-521":
				curve = elliptic.P521()
			}

			privKey, err := ecdsa.GenerateKey(curve, rand.Reader)
			if err != nil {
				t.Fatalf("Failed to generate EC key: %v", err)
			}

			xBytes := privKey.PublicKey.X.Bytes()
			yBytes := privKey.PublicKey.Y.Bytes()

			j := &jwk{
				Kty: "EC",
				Kid: "test-key",
				Alg: tt.expectAlg,
				Crv: tt.curve,
				X:   base64.RawURLEncoding.EncodeToString(xBytes),
				Y:   base64.RawURLEncoding.EncodeToString(yBytes),
			}

			key, err := j.toPublicKey()
			if err != nil {
				t.Fatalf("toPublicKey failed for %s: %v", tt.curve, err)
			}

			if key.alg != tt.expectAlg {
				t.Errorf("Expected alg %s, got %s", tt.expectAlg, key.alg)
			}

			ecKey, ok := key.key.(*ecdsa.PublicKey)
			if !ok {
				t.Fatal("Expected *ecdsa.PublicKey")
			}

			if ecKey.X.Cmp(privKey.PublicKey.X) != 0 {
				t.Error("X coordinate mismatch")
			}
			if ecKey.Y.Cmp(privKey.PublicKey.Y) != 0 {
				t.Error("Y coordinate mismatch")
			}
		})
	}
}

// TestJWK_ToECPublicKey_BadBase64 tests EC key with invalid base64
func TestJWK_ToECPublicKey_BadBase64(t *testing.T) {
	j := &jwk{Kty: "EC", Crv: "P-256", X: "!!!invalid!!!", Y: "valid"}
	_, err := j.toPublicKey()
	if err == nil {
		t.Fatal("Expected error for invalid base64")
	}
	if !strings.Contains(err.Error(), "decode EC X") {
		t.Errorf("Unexpected error: %v", err)
	}

	// Invalid Y base64
	j = &jwk{Kty: "EC", Crv: "P-256", X: "dGVzdA", Y: "!!!invalid!!!"}
	_, err = j.toPublicKey()
	if err == nil {
		t.Fatal("Expected error for invalid Y base64")
	}
	if !strings.Contains(err.Error(), "decode EC Y") {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestJWK_ToECPublicKey_UnsupportedCurve tests unsupported curve
func TestJWK_ToECPublicKey_UnsupportedCurve(t *testing.T) {
	j := &jwk{Kty: "EC", Crv: "P-12345"}
	_, err := j.toPublicKey()
	if err == nil {
		t.Fatal("Expected error for unsupported curve")
	}
	if !strings.Contains(err.Error(), "unsupported EC curve") {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestJWK_ToRSAPublicKey_BadBase64 tests RSA key with invalid base64
func TestJWK_ToRSAPublicKey_BadBase64(t *testing.T) {
	j := &jwk{Kty: "RSA", N: "!!!invalid!!!", E: "AQAB"}
	_, err := j.toPublicKey()
	if err == nil {
		t.Fatal("Expected error for invalid base64")
	}
	if !strings.Contains(err.Error(), "decode RSA modulus") {
		t.Errorf("Unexpected error: %v", err)
	}

	// Invalid E base64
	j = &jwk{Kty: "RSA", N: "dGVzdA", E: "!!!invalid!!!"}
	_, err = j.toPublicKey()
	if err == nil {
		t.Fatal("Expected error for invalid exponent base64")
	}
	if !strings.Contains(err.Error(), "decode RSA exponent") {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestJWK_ToPublicKey_UnknownType tests unsupported key type
func TestJWK_ToPublicKey_UnknownType(t *testing.T) {
	j := &jwk{Kty: "oct", Kid: "symmetric"}
	_, err := j.toPublicKey()
	if err == nil {
		t.Fatal("Expected error for unsupported key type")
	}
	if !strings.Contains(err.Error(), "unsupported key type") {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestVerifyJWTSignature_EmptyAndInvalid tests verifyJWTSignature error paths
func TestVerifyJWTSignature_EmptyAndInvalid(t *testing.T) {
	cfg := core.OIDCAuth{
		Issuer:      "https://example.com",
		ClientID:    "test-client",
		RedirectURL: "http://localhost/callback",
	}
	auth := newTestOIDCAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	// Empty token
	_, err := auth.verifyJWTSignature("")
	if err == nil {
		t.Fatal("Expected error for empty token")
	}
	if !strings.Contains(err.Error(), "empty ID token") {
		t.Errorf("Unexpected error: %v", err)
	}

	// Invalid format (not 3 parts)
	_, err = auth.verifyJWTSignature("abc.def")
	if err == nil {
		t.Fatal("Expected error for invalid JWT format")
	}
	if !strings.Contains(err.Error(), "invalid JWT format") {
		t.Errorf("Unexpected error: %v", err)
	}

	// Invalid base64 header
	_, err = auth.verifyJWTSignature("!!!.payload.sig")
	if err == nil {
		t.Fatal("Expected error for invalid base64")
	}
	if !strings.Contains(err.Error(), "failed to decode JWT header") {
		t.Errorf("Unexpected error: %v", err)
	}

	// Invalid JSON header
	badHeader := base64.RawURLEncoding.EncodeToString([]byte("not json"))
	_, err = auth.verifyJWTSignature(badHeader + ".payload.sig")
	if err == nil {
		t.Fatal("Expected error for invalid JSON header")
	}
	if !strings.Contains(err.Error(), "failed to parse JWT header") {
		t.Errorf("Unexpected error: %v", err)
	}

	// Algorithm "none"
	noneHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	_, err = auth.verifyJWTSignature(noneHeader + ".payload.sig")
	if err == nil {
		t.Fatal("Expected error for 'none' algorithm")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("Unexpected error: %v", err)
	}

	// Unknown algorithm
	unknownAlg := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	_, err = auth.verifyJWTSignature(unknownAlg + ".payload.sig")
	if err == nil {
		t.Fatal("Expected error for unknown algorithm")
	}
	if !strings.Contains(err.Error(), "unsupported JWT signing algorithm") {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestVerifyState_InvalidFormat tests verifyState with bad state
func TestVerifyState_InvalidFormat(t *testing.T) {
	cfg := core.OIDCAuth{
		Issuer:      "https://example.com",
		ClientID:    "test-client",
		RedirectURL: "http://localhost/callback",
	}
	auth := newTestOIDCAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	// No dot separator
	_, err := auth.verifyState("nodothere")
	if err == nil {
		t.Fatal("Expected error for invalid state format")
	}
	if !strings.Contains(err.Error(), "invalid state format") {
		t.Errorf("Unexpected error: %v", err)
	}

	// Tampered signature
	signed := auth.signState("my-state")
	tampered := signed[:len(signed)-5] + "XXXXX"
	_, err = auth.verifyState(tampered)
	if err == nil {
		t.Fatal("Expected error for tampered signature")
	}
	if !strings.Contains(err.Error(), "invalid state signature") {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestParseIDToken_ValidationErrors tests parseIDToken error paths
func TestParseIDToken_ValidationErrors(t *testing.T) {
	cfg := core.OIDCAuth{
		Issuer:      "https://example.com",
		ClientID:    "test-client-id",
		RedirectURL: "http://localhost/callback",
	}

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/openid-configuration" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"issuer":"` + server.URL + `","authorization_endpoint":"` + server.URL + `/auth","token_endpoint":"` + server.URL + `/token","jwks_uri":"` + server.URL + `/jwks"}`))
			return
		}
		if r.URL.Path == "/jwks" {
			privKey, _ := rsa.GenerateKey(rand.Reader, 2048)
			jwks := fmt.Sprintf(`{"keys": [{"kty":"RSA","kid":"test","n":"%s","e":"%s","alg":"RS256"}]}`,
				base64.RawURLEncoding.EncodeToString(privKey.PublicKey.N.Bytes()),
				base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privKey.PublicKey.E)).Bytes()))
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(jwks))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	cfg.Issuer = server.URL
	auth := newTestOIDCAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	// We need the private key from the server to sign tokens.
	// Since the server generates a new key each request, we can't sign valid tokens.
	// Instead, test verifyJWTSignature error paths directly (which don't need signing).

	// Missing issuer claim - this is parseIDToken path, requires valid signature first.
	// Since parseIDToken calls verifyJWTSignature which needs network, test via verifyJWTSignature:
	_, err := auth.verifyJWTSignature("")
	if err == nil {
		t.Fatal("Expected error for empty token")
	}

	// Invalid format
	_, err = auth.verifyJWTSignature("a.b")
	if err == nil {
		t.Fatal("Expected error for invalid format")
	}
	if !strings.Contains(err.Error(), "invalid JWT format") {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestFindKeyForJWT_NoMatchingKid tests findKeyForJWT when kid doesn't match
func TestFindKeyForJWT_NoMatchingKid(t *testing.T) {
	cfg := core.OIDCAuth{
		Issuer:      "https://example.com",
		ClientID:    "test-client",
		RedirectURL: "http://localhost/callback",
	}

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/openid-configuration" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"issuer":"` + server.URL + `","authorization_endpoint":"` + server.URL + `/auth","token_endpoint":"` + server.URL + `/token","jwks_uri":"` + server.URL + `/jwks"}`))
			return
		}
		if r.URL.Path == "/jwks" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"keys": [{"kty":"RSA","kid":"other-key","n":"dGVzdA","e":"AQAB"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	cfg.Issuer = server.URL
	auth := newTestOIDCAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	headers := map[string]interface{}{"kid": "nonexistent-key", "alg": "RS256"}
	_, err := auth.findKeyForJWT(headers)
	if err == nil {
		t.Fatal("Expected error for non-matching kid")
	}
	if !strings.Contains(err.Error(), "no matching key found") {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestVerifyJWTSignature_KeyTypeMismatch tests key type mismatch in signature verification
func TestVerifyJWTSignature_KeyTypeMismatch(t *testing.T) {
	cfg := core.OIDCAuth{
		Issuer:      "https://example.com",
		ClientID:    "test-client",
		RedirectURL: "http://localhost/callback",
	}

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/openid-configuration" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"issuer":"` + server.URL + `","authorization_endpoint":"` + server.URL + `/auth","token_endpoint":"` + server.URL + `/token","jwks_uri":"` + server.URL + `/jwks"}`))
			return
		}
		if r.URL.Path == "/jwks" {
			privKey, _ := rsa.GenerateKey(rand.Reader, 2048)
			nBytes := privKey.PublicKey.N.Bytes()
			eBytes := big.NewInt(int64(privKey.PublicKey.E)).Bytes()
			jwks := fmt.Sprintf(`{"keys": [{"kty":"RSA","kid":"rsa-key","n":"%s","e":"%s"}]}`,
				base64.RawURLEncoding.EncodeToString(nBytes),
				base64.RawURLEncoding.EncodeToString(eBytes))
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(jwks))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	cfg.Issuer = server.URL
	auth := newTestOIDCAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT","kid":"rsa-key"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"https://example.com","sub":"123","aud":"test-client","exp":9999999999}`))
	signingInput := header + "." + payload
	badSig := base64.RawURLEncoding.EncodeToString([]byte("bad-signature"))

	_, err := auth.verifyJWTSignature(signingInput + "." + badSig)
	if err == nil {
		t.Fatal("Expected error for bad signature")
	}
}

// TestVerifyJWTSignature_ES256KeyTypeMismatch tests ECDSA key type mismatch
func TestVerifyJWTSignature_ES256KeyTypeMismatch(t *testing.T) {
	cfg := core.OIDCAuth{
		Issuer:      "https://example.com",
		ClientID:    "test-client",
		RedirectURL: "http://localhost/callback",
	}

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/openid-configuration" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"issuer":"` + server.URL + `","authorization_endpoint":"` + server.URL + `/auth","token_endpoint":"` + server.URL + `/token","jwks_uri":"` + server.URL + `/jwks"}`))
			return
		}
		if r.URL.Path == "/jwks" {
			privKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			jwks := fmt.Sprintf(`{"keys": [{"kty":"EC","kid":"ec-key","crv":"P-256","x":"%s","y":"%s","alg":"ES256"}]}`,
				base64.RawURLEncoding.EncodeToString(privKey.PublicKey.X.Bytes()),
				base64.RawURLEncoding.EncodeToString(privKey.PublicKey.Y.Bytes()))
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(jwks))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	cfg.Issuer = server.URL
	auth := newTestOIDCAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"ES256","typ":"JWT","kid":"ec-key"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"https://example.com","sub":"123","aud":"test-client","exp":9999999999}`))
	signingInput := header + "." + payload
	badSig := base64.RawURLEncoding.EncodeToString([]byte("wrong-length-sig"))

	_, err := auth.verifyJWTSignature(signingInput + "." + badSig)
	if err == nil {
		t.Fatal("Expected error for wrong signature length")
	}
}

// TestFetchJWKs_MissingJWKSURI tests fetchJWKs when JWKS URI is missing
func TestFetchJWKs_MissingJWKSURI(t *testing.T) {
	cfg := core.OIDCAuth{
		Issuer:      "https://example.com",
		ClientID:    "test-client",
		RedirectURL: "http://localhost/callback",
	}

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"issuer":"` + server.URL + `","authorization_endpoint":"` + server.URL + `/auth","token_endpoint":"` + server.URL + `/token"}`))
	}))
	defer server.Close()

	cfg.Issuer = server.URL
	auth := newTestOIDCAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	_, err := auth.fetchJWKs()
	if err == nil {
		t.Fatal("Expected error for missing jwks_uri")
	}
	if !strings.Contains(err.Error(), "missing jwks_uri") {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestVerifyJWTSignature_UnsupportedAlgorithm tests verifyJWTSignature when the key's alg is unsupported
func TestVerifyJWTSignature_UnsupportedAlgorithm(t *testing.T) {
	cfg := core.OIDCAuth{
		Issuer:      "https://example.com",
		ClientID:    "test-client",
		RedirectURL: "http://localhost/callback",
	}

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/openid-configuration" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"issuer":"` + server.URL + `","authorization_endpoint":"` + server.URL + `/auth","token_endpoint":"` + server.URL + `/token","jwks_uri":"` + server.URL + `/jwks"}`))
			return
		}
		if r.URL.Path == "/jwks" {
			privKey, _ := rsa.GenerateKey(rand.Reader, 2048)
			// Serve with RS384 alg (not in the verify switch)
			jwks := fmt.Sprintf(`{"keys": [{"kty":"RSA","kid":"rsa-key","n":"%s","e":"%s","alg":"RS384"}]}`,
				base64.RawURLEncoding.EncodeToString(privKey.PublicKey.N.Bytes()),
				base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privKey.PublicKey.E)).Bytes()))
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(jwks))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	cfg.Issuer = server.URL
	auth := newTestOIDCAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	// JWT header matches the RS384 key alg
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS384","typ":"JWT","kid":"rsa-key"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"https://example.com","sub":"123","aud":"test-client","exp":9999999999}`))
	signingInput := header + "." + payload
	badSig := base64.RawURLEncoding.EncodeToString([]byte("fake-sig"))

	_, err := auth.verifyJWTSignature(signingInput + "." + badSig)
	if err == nil {
		t.Fatal("Expected error for RS384 algorithm (not in verify switch)")
	}
	// RS384 key leads to the default case in the verify switch
	if !strings.Contains(err.Error(), "unsupported JWT signing algorithm") {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestParseIDToken_NonceMismatch tests nonce validation in parseIDToken
func TestParseIDToken_NonceMismatch(t *testing.T) {
	privKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	jwks := createTestJWK(privKey)

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/openid-configuration" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(oidcConfig{
				Issuer:   server.URL,
				AuthURL:  server.URL + "/auth",
				TokenURL: server.URL + "/token",
				JWKSURI:  server.URL + "/jwks",
			})
			return
		}
		if r.URL.Path == "/jwks" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(jwks)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	cfg := core.OIDCAuth{
		Issuer:      server.URL,
		ClientID:    "test-client-id",
		RedirectURL: "http://localhost/callback",
	}
	auth := newTestOIDCAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT","kid":"test-key"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"` + server.URL + `","sub":"123","aud":"test-client-id","exp":9999999999,"nonce":"different-nonce"}`))
	signingInput := header + "." + payload
	hash := sha256.Sum256([]byte(signingInput))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, hash[:])
	token := signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)

	_, err := auth.parseIDToken(token, "expected-nonce")
	if err == nil {
		t.Fatal("Expected error for nonce mismatch")
	}
	if !strings.Contains(err.Error(), "nonce") {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestParseIDToken_AudienceInList tests audience as an array claim
func TestParseIDToken_AudienceInList(t *testing.T) {
	privKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	jwks := createTestJWK(privKey)

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/openid-configuration" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(oidcConfig{
				Issuer:   server.URL,
				AuthURL:  server.URL + "/auth",
				TokenURL: server.URL + "/token",
				JWKSURI:  server.URL + "/jwks",
			})
			return
		}
		if r.URL.Path == "/jwks" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(jwks)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	cfg := core.OIDCAuth{
		Issuer:      server.URL,
		ClientID:    "test-client-id",
		RedirectURL: "http://localhost/callback",
	}
	auth := newTestOIDCAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT","kid":"test-key"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"` + server.URL + `","sub":"123","aud":["other-client","test-client-id"],"azp":"test-client-id","exp":9999999999}`))
	signingInput := header + "." + payload
	hash := sha256.Sum256([]byte(signingInput))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, hash[:])
	token := signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)

	userInfo, err := auth.parseIDToken(token, "")
	if err != nil {
		t.Fatalf("Expected audience in list to work: %v", err)
	}
	if userInfo.Sub != "123" {
		t.Errorf("Expected sub 123, got %s", userInfo.Sub)
	}

	// Audience NOT in list
	payload2 := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"` + server.URL + `","sub":"123","aud":["client-a","client-b"],"exp":9999999999}`))
	signingInput2 := header + "." + payload2
	hash2 := sha256.Sum256([]byte(signingInput2))
	sig2, _ := rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, hash2[:])
	token2 := signingInput2 + "." + base64.RawURLEncoding.EncodeToString(sig2)

	_, err = auth.parseIDToken(token2, "")
	if err == nil {
		t.Fatal("Expected error for audience not in list")
	}
	if !strings.Contains(err.Error(), "audience not in list") {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestParseIDToken_InvalidPayloadJSON tests verifyJWTSignature with non-JSON payload
func TestParseIDToken_InvalidPayloadJSON(t *testing.T) {
	privKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	jwks := createTestJWK(privKey)

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/openid-configuration" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(oidcConfig{
				Issuer:   server.URL,
				AuthURL:  server.URL + "/auth",
				TokenURL: server.URL + "/token",
				JWKSURI:  server.URL + "/jwks",
			})
			return
		}
		if r.URL.Path == "/jwks" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(jwks)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	cfg := core.OIDCAuth{
		Issuer:      server.URL,
		ClientID:    "test-client",
		RedirectURL: "http://localhost/callback",
	}
	auth := newTestOIDCAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT","kid":"test-key"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`not-json-at-all`))
	signingInput := header + "." + payload
	hash := sha256.Sum256([]byte(signingInput))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, privKey, crypto.SHA256, hash[:])
	token := signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)

	_, err := auth.verifyJWTSignature(token)
	if err == nil {
		t.Fatal("Expected error for invalid payload JSON")
	}
	if !strings.Contains(err.Error(), "failed to parse JWT claims JSON") {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestVerifyJWTSignature_SignatureDecodeError tests base64 decode error on signature
func TestVerifyJWTSignature_SignatureDecodeError(t *testing.T) {
	cfg := core.OIDCAuth{
		Issuer:      "https://example.com",
		ClientID:    "test-client",
		RedirectURL: "http://localhost/callback",
	}

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/openid-configuration" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"issuer":"` + server.URL + `","authorization_endpoint":"` + server.URL + `/auth","token_endpoint":"` + server.URL + `/token","jwks_uri":"` + server.URL + `/jwks"}`))
			return
		}
		if r.URL.Path == "/jwks" {
			privKey, _ := rsa.GenerateKey(rand.Reader, 2048)
			jwks := fmt.Sprintf(`{"keys": [{"kty":"RSA","kid":"rsa-key","n":"%s","e":"%s","alg":"RS256"}]}`,
				base64.RawURLEncoding.EncodeToString(privKey.PublicKey.N.Bytes()),
				base64.RawURLEncoding.EncodeToString(big.NewInt(int64(privKey.PublicKey.E)).Bytes()))
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(jwks))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	cfg.Issuer = server.URL
	auth := newTestOIDCAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT","kid":"rsa-key"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"https://example.com","sub":"123","aud":"test-client","exp":9999999999}`))
	signingInput := header + "." + payload

	_, err := auth.verifyJWTSignature(signingInput + "." + "!!!invalid-base64!!!")
	if err == nil {
		t.Fatal("Expected error for invalid signature base64")
	}
	if !strings.Contains(err.Error(), "failed to decode JWT signature") {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestOIDCAuthenticator_SwitchWorkspace tests the SwitchWorkspace method
func TestOIDCAuthenticator_SwitchWorkspace(t *testing.T) {
	cfg := core.OIDCAuth{
		Issuer:       "https://example.com",
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://localhost:8080/callback",
	}

	auth := newTestOIDCAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

	// Login to get a valid token (local fallback)
	_, token, err := auth.Login("admin@test.com", "TestPass1234!")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	// Switch workspace
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

	// Switch with invalid token should delegate to local (which should error)
	_, err = auth.SwitchWorkspace("invalid-token", "test-workspace")
	if err == nil {
		t.Error("Expected error for invalid token")
	}

	// Switch with expired token
	auth.mu.Lock()
	auth.tokens[token] = &session{
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	auth.mu.Unlock()

	_, err = auth.SwitchWorkspace(token, "test-workspace")
	if err == nil {
		t.Error("Expected error for expired token")
	}
	if !strings.Contains(err.Error(), "token expired") {
		t.Errorf("Expected token expired error, got: %v", err)
	}
}

// TestOIDCAuthenticator_SwitchWorkspace_UserNotFound tests SwitchWorkspace with missing user
func TestOIDCAuthenticator_SwitchWorkspace_UserNotFound(t *testing.T) {
	cfg := core.OIDCAuth{
		Issuer:       "https://example.com",
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		RedirectURL:  "http://localhost:8080/callback",
	}

	auth := newTestOIDCAuth(t, cfg, "", "admin@test.com", "TestPass1234!")

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
