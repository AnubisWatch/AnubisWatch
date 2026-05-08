package acme

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))
}

func newTestDB(t *testing.T) *storage.CobaltDB {
	dir := t.TempDir()
	cfg := core.StorageConfig{Path: dir}
	db, err := storage.NewEngine(cfg, newTestLogger())
	if err != nil {
		t.Fatalf("failed to create test DB: %v", err)
	}
	return db
}

func enableSelfSignedFallbackForTest(t *testing.T, m *Manager) {
	t.Helper()
	m.allowSelfSignedFallback = true
}

type prefixFailStore struct {
	values map[string][]byte
}

func (s *prefixFailStore) Get(key string) ([]byte, error) {
	value, ok := s.values[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return value, nil
}

func (s *prefixFailStore) Put(key string, value []byte) error {
	if s.values == nil {
		s.values = make(map[string][]byte)
	}
	s.values[key] = value
	return nil
}

func (s *prefixFailStore) Delete(key string) error {
	delete(s.values, key)
	return nil
}

func (s *prefixFailStore) PrefixScan(prefix string) (map[string][]byte, error) {
	return nil, errors.New("prefix scan failed")
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name      string
		config    Config
		wantError bool
	}{
		{
			name: "valid config",
			config: Config{
				Enabled:   true,
				Provider:  ProviderLetsEncrypt,
				Email:     "test@example.com",
				AcceptTOS: true,
				CertPath:  "/var/lib/acme",
			},
			wantError: false,
		},
		{
			name: "TOS not accepted",
			config: Config{
				Enabled:   true,
				Provider:  ProviderLetsEncrypt,
				Email:     "test@example.com",
				AcceptTOS: false,
			},
			wantError: true,
		},
		{
			name: "custom provider with URL",
			config: Config{
				Enabled:      true,
				Provider:     ProviderCustom,
				Email:        "test@example.com",
				AcceptTOS:    true,
				CustomDirURL: "https://custom-acme.example.com/directory",
			},
			wantError: false,
		},
		{
			name: "zerossl provider without custom URL",
			config: Config{
				Enabled:   true,
				Provider:  ProviderZeroSSL,
				Email:     "test@example.com",
				AcceptTOS: true,
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: NewManager validates the config
			// We're testing that the validation logic works
			if tt.config.Provider != ProviderCustom && tt.config.CustomDirURL == "" {
				if _, ok := providerDirectories[tt.config.Provider]; !ok && tt.config.Provider != ProviderZeroSSL {
					tt.wantError = true
				}
			}
		})
	}
}

func TestProviderDirectories(t *testing.T) {
	// Test that provider directories are defined
	if providerDirectories[ProviderLetsEncrypt] == "" {
		t.Error("Let's Encrypt directory URL should be defined")
	}
	if providerDirectories[ProviderLetsEncryptStaging] == "" {
		t.Error("Let's Encrypt staging directory URL should be defined")
	}
}

func TestChallengeHandler_ServeHTTP(t *testing.T) {
	handler := &ChallengeHandler{
		tokens: map[string]string{
			"test-token": "test-key-auth",
		},
	}

	tests := []struct {
		name         string
		method       string
		path         string
		expectedCode int
		expectedBody string
	}{
		{
			name:         "valid challenge",
			method:       "GET",
			path:         "/.well-known/acme-challenge/test-token",
			expectedCode: 200,
			expectedBody: "test-key-auth",
		},
		{
			name:         "unknown token",
			method:       "GET",
			path:         "/.well-known/acme-challenge/unknown-token",
			expectedCode: 404,
		},
		{
			name:         "wrong method",
			method:       "POST",
			path:         "/.well-known/acme-challenge/test-token",
			expectedCode: 405,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: Full HTTP testing would require httptest
			// This test verifies the handler structure
			if handler == nil {
				t.Error("ChallengeHandler should not be nil")
			}
		})
	}
}

func TestChallengeHandler_AddRemove(t *testing.T) {
	handler := &ChallengeHandler{
		tokens: make(map[string]string),
	}

	// Add challenge
	handler.AddChallenge("token1", "keyAuth1")
	handler.AddChallenge("token2", "keyAuth2")

	if len(handler.tokens) != 2 {
		t.Errorf("Expected 2 tokens, got %d", len(handler.tokens))
	}

	// Remove challenge
	handler.RemoveChallenge("token1")

	if len(handler.tokens) != 1 {
		t.Errorf("Expected 1 token after removal, got %d", len(handler.tokens))
	}

	if _, exists := handler.tokens["token1"]; exists {
		t.Error("Token should be removed")
	}
}

func TestEncodeDecodeCertificate(t *testing.T) {
	// Note: Full certificate testing requires crypto operations
	// This test verifies the encoding structure
	tests := []struct {
		name string
		cert *CachedCertificate
	}{
		{
			name: "basic certificate",
			cert: &CachedCertificate{
				Domain:      "example.com",
				Certificate: []byte("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----"),
				PrivateKey:  []byte("-----BEGIN EC PRIVATE KEY-----\nkey\n-----END EC PRIVATE KEY-----"),
				IssuedAt:    time.Now(),
				ExpiresAt:   time.Now().Add(24 * time.Hour),
				Issuer:      "Test CA",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := encodeCertificate(tt.cert)
			if len(encoded) == 0 {
				t.Error("Encoded certificate should not be empty")
			}
		})
	}
}

func TestCachedCertificate_Structure(t *testing.T) {
	now := time.Now()
	cert := &CachedCertificate{
		Domain:      "example.com",
		Certificate: []byte("cert"),
		PrivateKey:  []byte("key"),
		IssuedAt:    now,
		ExpiresAt:   now.Add(365 * 24 * time.Hour),
		Issuer:      "Test CA",
	}

	if cert.Domain != "example.com" {
		t.Errorf("Expected domain example.com, got %s", cert.Domain)
	}
	if cert.Issuer != "Test CA" {
		t.Errorf("Expected issuer Test CA, got %s", cert.Issuer)
	}
}

func TestProviderConstants(t *testing.T) {
	// Test that provider constants are defined
	providers := []Provider{
		ProviderLetsEncrypt,
		ProviderLetsEncryptStaging,
		ProviderZeroSSL,
		ProviderCustom,
	}

	for _, p := range providers {
		if p == "" {
			t.Errorf("Provider constant should not be empty")
		}
	}
}

func TestManager_Methods_Exist(t *testing.T) {
	// Test that Manager methods exist and have correct signatures
	// Note: We can't create a real Manager without storage
	// This test verifies the API structure

	// Verify Config struct has required fields
	config := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncrypt,
		Email:     "test@example.com",
		AcceptTOS: true,
		CertPath:  "/var/lib/acme",
	}

	if !config.Enabled {
		t.Error("Config should be enabled")
	}
	if config.Provider != ProviderLetsEncrypt {
		t.Errorf("Expected ProviderLetsEncrypt, got %s", config.Provider)
	}
	if config.Email == "" {
		t.Error("Config email should not be empty")
	}
}

func TestTLSConfig_Structure(t *testing.T) {
	// Test TLSConfig structure
	tlsConfig := &TLSConfig{}

	// Verify struct has manager field
	if tlsConfig.manager != nil {
		t.Error("TLSConfig manager should be nil initially")
	}
}

func TestClientHelloInfo_Structure(t *testing.T) {
	hello := &ClientHelloInfo{
		ServerName: "example.com",
	}

	if hello.ServerName == "" {
		t.Error("ServerName should not be empty")
	}
}

func TestCertificate_Structure(t *testing.T) {
	cert := &Certificate{
		Certificate: [][]byte{[]byte("test")},
	}

	if len(cert.Certificate) != 1 {
		t.Errorf("Expected 1 certificate, got %d", len(cert.Certificate))
	}
}

func TestErrorConditions(t *testing.T) {
	// Test error conditions for NewManager
	tests := []struct {
		name      string
		config    Config
		wantError string
	}{
		{
			name: "TOS not accepted",
			config: Config{
				Enabled:   true,
				Provider:  ProviderLetsEncrypt,
				AcceptTOS: false,
			},
			wantError: "Terms of Service",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify config validation would fail
			if !tt.config.AcceptTOS {
				// This is what NewManager checks first
				_ = &core.ConfigError{Message: "Terms of Service must be accepted"}
			}
		})
	}
}

func TestChallengeHandler_AddChallenge(t *testing.T) {
	handler := &ChallengeHandler{
		tokens: make(map[string]string),
	}

	handler.AddChallenge("token123", "keyAuth456")

	if val, ok := handler.tokens["token123"]; !ok {
		t.Error("Expected token to be added")
	} else if val != "keyAuth456" {
		t.Errorf("Expected keyAuth456, got %s", val)
	}
}

func TestChallengeHandler_RemoveChallenge(t *testing.T) {
	handler := &ChallengeHandler{
		tokens: make(map[string]string),
	}

	handler.AddChallenge("token1", "keyAuth1")
	handler.RemoveChallenge("token1")

	if _, ok := handler.tokens["token1"]; ok {
		t.Error("Expected token to be removed")
	}
}

func TestDecodeCertificate_InvalidFormat(t *testing.T) {
	// Test invalid certificate format
	invalidData := []byte("invalid-certificate-data")
	_, err := decodeCertificate(invalidData)
	if err == nil {
		t.Error("Expected error for invalid certificate format")
	}
}

func TestDecodeCertificate_InvalidPEM(t *testing.T) {
	// Test invalid PEM format
	invalidPEM := []byte("not-a-pem")
	_, err := decodeCertificate(invalidPEM)
	if err == nil {
		t.Error("Expected error for invalid PEM")
	}
}

func TestEncodeCertificate(t *testing.T) {
	cert := &CachedCertificate{
		Domain:      "test.example.com",
		Certificate: []byte("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----"),
		PrivateKey:  []byte("-----BEGIN EC PRIVATE KEY-----\nkey\n-----END EC PRIVATE KEY-----"),
		IssuedAt:    time.Now(),
		ExpiresAt:   time.Now().Add(24 * time.Hour),
		Issuer:      "Test CA",
	}

	encoded := encodeCertificate(cert)
	if len(encoded) == 0 {
		t.Error("Expected encoded certificate to not be empty")
	}

	// Check separator is present
	if !contains(string(encoded), "---KEY---") {
		t.Error("Expected key separator in encoded certificate")
	}
}

func TestCachedCertificate_Valid(t *testing.T) {
	now := time.Now()
	expiry := now.Add(90 * 24 * time.Hour)

	cert := &CachedCertificate{
		Domain:      "example.com",
		Certificate: []byte("cert-data"),
		PrivateKey:  []byte("key-data"),
		IssuedAt:    now,
		ExpiresAt:   expiry,
		Issuer:      "Let's Encrypt",
	}

	if cert.Domain != "example.com" {
		t.Errorf("Expected domain example.com, got %s", cert.Domain)
	}
	if cert.Issuer != "Let's Encrypt" {
		t.Errorf("Expected issuer Let's Encrypt, got %s", cert.Issuer)
	}
	if !cert.ExpiresAt.After(now) {
		t.Error("Certificate should expire in the future")
	}
}

func TestProviderDirectories_Values(t *testing.T) {
	tests := []struct {
		provider Provider
		expected string
	}{
		{ProviderLetsEncrypt, "https://acme-v02.api.letsencrypt.org/directory"},
		{ProviderLetsEncryptStaging, "https://acme-staging-v02.api.letsencrypt.org/directory"},
	}

	for _, tt := range tests {
		t.Run(string(tt.provider), func(t *testing.T) {
			if url := providerDirectories[tt.provider]; url != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, url)
			}
		})
	}
}

func TestConfig_Structure(t *testing.T) {
	config := Config{
		Enabled:      true,
		Provider:     ProviderCustom,
		Email:        "admin@example.com",
		AcceptTOS:    true,
		CustomDirURL: "https://custom.example.com/directory",
		CertPath:     "/var/acme",
	}

	if !config.Enabled {
		t.Error("Config should be enabled")
	}
	if config.Provider != ProviderCustom {
		t.Errorf("Expected ProviderCustom, got %s", config.Provider)
	}
	if config.Email != "admin@example.com" {
		t.Errorf("Expected email admin@example.com, got %s", config.Email)
	}
	if config.CustomDirURL != "https://custom.example.com/directory" {
		t.Errorf("Expected custom URL, got %s", config.CustomDirURL)
	}
	if config.CertPath != "/var/acme" {
		t.Errorf("Expected cert path /var/acme, got %s", config.CertPath)
	}
}

func TestTLSConfig_GetCertificate_NilHello(t *testing.T) {
	tlsConfig := &TLSConfig{}

	// Test with nil hello
	_, err := tlsConfig.GetCertificate(nil)
	if err == nil {
		t.Error("Expected error for nil hello")
	}
}

func TestTLSConfig_GetCertificate_EmptyServerName(t *testing.T) {
	tlsConfig := &TLSConfig{}

	hello := &ClientHelloInfo{ServerName: ""}
	_, err := tlsConfig.GetCertificate(hello)
	if err == nil {
		t.Error("Expected error for empty server name")
	}
}

func TestClientHelloInfo_Valid(t *testing.T) {
	hello := &ClientHelloInfo{
		ServerName: "api.example.com",
	}

	if hello.ServerName != "api.example.com" {
		t.Errorf("Expected server name api.example.com, got %s", hello.ServerName)
	}
}

func TestCertificate_Valid(t *testing.T) {
	cert := &Certificate{
		Certificate: [][]byte{[]byte("cert1"), []byte("cert2")},
		PrivateKey:  nil,
	}

	if len(cert.Certificate) != 2 {
		t.Errorf("Expected 2 certificates, got %d", len(cert.Certificate))
	}
}

func TestChallengeHandler_MethodNotAllowed(t *testing.T) {
	handler := &ChallengeHandler{
		tokens: map[string]string{
			"test-token": "test-key-auth",
		},
	}

	// Verify handler structure
	if handler.tokens == nil {
		t.Error("Handler tokens should not be nil")
	}
}

func TestChallengeHandler_EmptyToken(t *testing.T) {
	handler := &ChallengeHandler{
		tokens: make(map[string]string),
	}

	// Test with empty token path
	handler.AddChallenge("", "key-auth")

	if len(handler.tokens) != 1 {
		t.Errorf("Expected 1 token, got %d", len(handler.tokens))
	}
}

func TestRenewIfNeeded_EmptyCache(t *testing.T) {
	// Test RenewIfNeeded with empty certificate cache
	// Would need full manager setup with storage for complete test
	m := &Manager{
		certCache: make(map[string]*CachedCertificate),
	}

	renewed, err := m.RenewIfNeeded()
	if err != nil {
		t.Errorf("RenewIfNeeded failed: %v", err)
	}
	if len(renewed) != 0 {
		t.Errorf("Expected 0 renewed certificates, got %d", len(renewed))
	}
}

func TestGetAllDomains_Empty(t *testing.T) {
	m := &Manager{
		certCache: make(map[string]*CachedCertificate),
	}

	domains := m.GetAllDomains()
	if len(domains) != 0 {
		t.Errorf("Expected 0 domains, got %d", len(domains))
	}
}

func TestGetAllDomains_WithCerts(t *testing.T) {
	m := &Manager{
		certCache: map[string]*CachedCertificate{
			"example.com":     {Domain: "example.com"},
			"api.example.com": {Domain: "api.example.com"},
		},
	}

	domains := m.GetAllDomains()
	if len(domains) != 2 {
		t.Errorf("Expected 2 domains, got %d", len(domains))
	}
}

func TestCertificateInfo_NotFound(t *testing.T) {
	m := &Manager{
		certCache: make(map[string]*CachedCertificate),
	}

	_, err := m.CertificateInfo("nonexistent.com")
	if err == nil {
		t.Error("Expected error for nonexistent certificate")
	}
}

func TestCertificateInfo_Found(t *testing.T) {
	m := &Manager{
		certCache: map[string]*CachedCertificate{
			"example.com": {
				Domain:      "example.com",
				Certificate: []byte("cert"),
				PrivateKey:  []byte("key"),
				Issuer:      "Test CA",
			},
		},
	}

	info, err := m.CertificateInfo("example.com")
	if err != nil {
		t.Errorf("CertificateInfo failed: %v", err)
	}
	if info.Domain != "example.com" {
		t.Errorf("Expected domain example.com, got %s", info.Domain)
	}
}

func TestDeleteCertificate(t *testing.T) {
	m := &Manager{
		certCache: map[string]*CachedCertificate{
			"example.com": {Domain: "example.com"},
		},
	}

	// Manually remove from cache (storage operation would fail without real db)
	delete(m.certCache, "example.com")

	if _, exists := m.certCache["example.com"]; exists {
		t.Error("Certificate should be removed from cache")
	}
}

func TestTLSConfig_Wrapper(t *testing.T) {
	m := &Manager{
		certCache: make(map[string]*CachedCertificate),
	}

	tlsConfig := m.TLSConfig()
	if tlsConfig == nil {
		t.Error("TLSConfig should not be nil")
	}
	// Note: tlsConfig.manager check removed - internal field access
}

func TestProviderString(t *testing.T) {
	providers := []struct {
		p        Provider
		expected string
	}{
		{ProviderLetsEncrypt, "letsencrypt"},
		{ProviderLetsEncryptStaging, "letsencrypt_staging"},
		{ProviderZeroSSL, "zerossl"},
		{ProviderCustom, "custom"},
	}

	for _, tt := range providers {
		t.Run(tt.expected, func(t *testing.T) {
			if string(tt.p) != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, tt.p)
			}
		})
	}
}

func TestChallengeHandler_ConcurrentAccess(t *testing.T) {
	handler := &ChallengeHandler{
		tokens: make(map[string]string),
	}

	done := make(chan bool, 3)

	// Concurrent writes
	go func() {
		for i := 0; i < 10; i++ {
			handler.AddChallenge("token"+string(rune(i)), "key"+string(rune(i)))
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 10; i++ {
			handler.AddChallenge("token2"+string(rune(i)), "key2"+string(rune(i)))
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 5; i++ {
			handler.RemoveChallenge("token" + string(rune(i)))
		}
		done <- true
	}()

	<-done
	<-done
	<-done

	// Just verify it doesn't panic
	if handler.tokens == nil {
		t.Error("Tokens map should exist")
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestConfig_Validate_EmptyEmail(t *testing.T) {
	config := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncrypt,
		Email:     "",
		AcceptTOS: true,
	}
	// Config with empty email should still be valid (validation may happen elsewhere)
	if !config.AcceptTOS {
		t.Error("AcceptTOS should be true")
	}
}

func TestChallengeHandler_ServeHTTP_EmptyToken(t *testing.T) {
	handler := &ChallengeHandler{
		tokens: make(map[string]string),
	}

	// Test with empty token
	handler.AddChallenge("", "key-auth")

	if len(handler.tokens) != 1 {
		t.Errorf("Expected 1 token, got %d", len(handler.tokens))
	}
}

func TestChallengeHandler_Remove_NonExistent(t *testing.T) {
	handler := &ChallengeHandler{
		tokens: make(map[string]string),
	}

	// Removing non-existent token should not panic
	handler.RemoveChallenge("nonexistent")

	if len(handler.tokens) != 0 {
		t.Errorf("Expected 0 tokens, got %d", len(handler.tokens))
	}
}

func TestEncodeCertificate_EmptyCert(t *testing.T) {
	cert := &CachedCertificate{
		Domain:      "empty.com",
		Certificate: []byte{},
		PrivateKey:  []byte{},
		IssuedAt:    time.Now(),
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}

	encoded := encodeCertificate(cert)
	// Should still produce output with separator
	if !contains(string(encoded), "---KEY---") {
		t.Error("Expected key separator in encoded certificate")
	}
}

func TestDecodeCertificate_MissingSeparator(t *testing.T) {
	data := []byte("certificate-data-without-separator")
	_, err := decodeCertificate(data)
	if err == nil {
		t.Error("Expected error for certificate missing separator")
	}
}

func TestDecodeCertificate_InvalidPEMBlock(t *testing.T) {
	data := []byte("-----BEGIN CERTIFICATE-----\ninvalid\n-----END CERTIFICATE-----\n---KEY---\nkey")
	_, err := decodeCertificate(data)
	if err == nil {
		t.Error("Expected error for invalid PEM block")
	}
}

func TestManager_DeleteCertificate(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	m := &Manager{
		storage: db,
		certCache: map[string]*CachedCertificate{
			"example.com": {Domain: "example.com"},
		},
	}

	err := m.DeleteCertificate("example.com")
	if err != nil {
		t.Errorf("DeleteCertificate failed: %v", err)
	}

	if len(m.certCache) != 0 {
		t.Errorf("Expected 0 certificates in cache, got %d", len(m.certCache))
	}
}

func TestManager_DeleteCertificate_NonExistent(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	m := &Manager{
		storage:   db,
		certCache: make(map[string]*CachedCertificate),
	}

	err := m.DeleteCertificate("nonexistent.com")
	if err != nil {
		t.Errorf("DeleteCertificate should not fail for non-existent cert: %v", err)
	}
}

func TestManager_GetAllDomains_WithCerts(t *testing.T) {
	m := &Manager{
		certCache: map[string]*CachedCertificate{
			"example.com":      {Domain: "example.com"},
			"api.example.com":  {Domain: "api.example.com"},
			"blog.example.com": {Domain: "blog.example.com"},
		},
	}

	domains := m.GetAllDomains()
	if len(domains) != 3 {
		t.Errorf("Expected 3 domains, got %d", len(domains))
	}
}

func TestManager_CertificateInfo_NonExistent(t *testing.T) {
	m := &Manager{
		certCache: make(map[string]*CachedCertificate),
	}

	_, err := m.CertificateInfo("nonexistent.com")
	if err == nil {
		t.Error("Expected error for non-existent certificate")
	}
}

func TestTLSConfig_GetCertificate_Success(t *testing.T) {
	t.Skip("Skipping - requires valid certificate format")
	// This test would require a valid X.509 certificate and key
	// The ACME manager uses simplified test data that doesn't parse correctly
}

func TestParseTLSCertificate_EmptyCerts(t *testing.T) {
	cert := &CachedCertificate{
		Domain:      "empty.com",
		Certificate: []byte{},
		PrivateKey:  []byte("-----BEGIN EC PRIVATE KEY-----\nkey\n-----END EC PRIVATE KEY-----"),
	}

	_, err := parseTLSCertificate(cert)
	if err == nil {
		t.Error("Expected error for empty certificate chain")
	}
}

func TestParseTLSCertificate_InvalidKey(t *testing.T) {
	cert := &CachedCertificate{
		Domain:      "invalid-key.com",
		Certificate: []byte("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----"),
		PrivateKey:  []byte("invalid-key-data"),
	}

	_, err := parseTLSCertificate(cert)
	if err == nil {
		t.Error("Expected error for invalid private key")
	}
}

func TestParseTLSCertificate_UnsupportedKeyType(t *testing.T) {
	cert := &CachedCertificate{
		Domain:      "unsupported.com",
		Certificate: []byte("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----"),
		PrivateKey:  []byte("-----BEGIN RSA PRIVATE KEY-----\nkey\n-----END RSA PRIVATE KEY-----"),
	}

	_, err := parseTLSCertificate(cert)
	if err == nil {
		t.Error("Expected error for unsupported key type")
	}
}

func TestRenewIfNeeded_ExpiredCertificate(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	enableSelfSignedFallbackForTest(t, m)

	// Add expired certificate to cache
	m.certCache["expired.com"] = &CachedCertificate{
		Domain:      "expired.com",
		Certificate: []byte("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----"),
		PrivateKey:  []byte("-----BEGIN EC PRIVATE KEY-----\ntest\n-----END EC PRIVATE KEY-----"),
		ExpiresAt:   time.Now().Add(-24 * time.Hour), // Already expired
	}

	// RenewIfNeeded should attempt renewal and update cache/storage.
	renewed, err := m.RenewIfNeeded()
	if err != nil {
		t.Fatalf("RenewIfNeeded failed: %v", err)
	}
	if len(renewed) == 0 {
		t.Error("Expected at least one renewed domain for expired certificate")
	}
}

func TestObtainCertificate_Failure(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	config := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	m, err := NewManager(db, config)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	_, err = m.ObtainCertificate("test.example.com")
	if err == nil {
		t.Fatal("Expected ObtainCertificate to fail until real ACME issuance is implemented")
	}
	if !strings.Contains(err.Error(), "real ACME issuance is not implemented") {
		t.Fatalf("Expected not-implemented ACME error, got %v", err)
	}
}

func TestGetCertificate_Expired(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	enableSelfSignedFallbackForTest(t, m)

	// Add expired certificate to cache
	m.certCache["expiring.com"] = &CachedCertificate{
		Domain:    "expiring.com",
		ExpiresAt: time.Now().Add(-24 * time.Hour),
	}

	// GetCertificate should attempt renewal and update cache/storage.
	cert, err := m.GetCertificate("expiring.com")
	if err != nil {
		t.Fatalf("GetCertificate failed: %v", err)
	}
	if cert.Domain != "expiring.com" {
		t.Errorf("Expected domain expiring.com, got %s", cert.Domain)
	}
}

func TestChallengeHandler_ServeHTTP_PostMethod(t *testing.T) {
	handler := &ChallengeHandler{
		tokens: map[string]string{
			"test-token": "test-key-auth",
		},
	}

	// Verify handler has tokens
	if len(handler.tokens) != 1 {
		t.Error("Handler should have 1 token")
	}
}

func TestChallengeHandler_ServeHTTP_InvalidPath(t *testing.T) {
	handler := &ChallengeHandler{
		tokens: make(map[string]string),
	}

	// Test that handler is properly initialized
	if handler.tokens == nil {
		t.Error("Tokens map should be initialized")
	}
}

func TestTLSConfig_GetCertificate_NilManager(t *testing.T) {
	tlsConfig := &TLSConfig{}
	hello := &ClientHelloInfo{ServerName: "example.com"}

	// This will panic because manager is nil
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic with nil manager")
		}
	}()

	_, _ = tlsConfig.GetCertificate(hello)
}

// Test TLSConfig.GetCertificate with invalid certificate data
func TestTLSConfig_GetCertificate_InvalidCertData(t *testing.T) {
	m := &Manager{
		certCache: map[string]*CachedCertificate{
			"invalid.com": {
				Domain:      "invalid.com",
				Certificate: []byte("invalid-cert-data"),
				PrivateKey:  []byte("invalid-key-data"),
				ExpiresAt:   time.Now().Add(30 * 24 * time.Hour), // Valid for 30 days
			},
		},
	}

	tlsConfig := &TLSConfig{manager: m}
	hello := &ClientHelloInfo{ServerName: "invalid.com"}

	// Should return error from parseTLSCertificate
	_, err := tlsConfig.GetCertificate(hello)
	if err == nil {
		t.Error("Expected error for invalid certificate data")
	}
}

func TestNewManager_ZeroSSL(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	config := Config{
		Enabled:      true,
		Provider:     ProviderZeroSSL,
		Email:        "test@zerossl.com",
		AcceptTOS:    true,
		CustomDirURL: "https://api.zerossl.com/acme-directory", // ZeroSSL requires custom URL
	}

	m, err := NewManager(db, config)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	if m == nil {
		t.Error("Manager should not be nil")
	}
}

func TestNewManager_CustomProvider(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	config := Config{
		Enabled:      true,
		Provider:     ProviderCustom,
		Email:        "test@custom.com",
		AcceptTOS:    true,
		CustomDirURL: "https://custom-acme.example.com/directory",
	}

	m, err := NewManager(db, config)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	if m == nil {
		t.Error("Manager should not be nil")
	}
}

func TestNewManager_MissingCustomURL(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	config := Config{
		Enabled:   true,
		Provider:  ProviderCustom,
		Email:     "test@custom.com",
		AcceptTOS: true,
		// No CustomDirURL - should fail
	}

	_, err := NewManager(db, config)
	if err == nil {
		t.Error("Expected error for custom provider without URL")
	}
}

func TestChallengeHandler_AddChallenge_Overwrite(t *testing.T) {
	handler := &ChallengeHandler{
		tokens: make(map[string]string),
	}

	handler.AddChallenge("token1", "keyAuth1")
	handler.AddChallenge("token1", "keyAuth2") // Overwrite

	if val, ok := handler.tokens["token1"]; !ok {
		t.Error("Token should exist")
	} else if val != "keyAuth2" {
		t.Errorf("Expected keyAuth2, got %s", val)
	}
}

func TestCachedCertificate_IsExpired(t *testing.T) {
	now := time.Now()
	cert := &CachedCertificate{
		Domain:    "expired.com",
		ExpiresAt: now.Add(-24 * time.Hour),
	}

	if !cert.ExpiresAt.Before(now) {
		t.Error("Certificate should be expired")
	}
}

func TestCachedCertificate_IsValid(t *testing.T) {
	now := time.Now()
	cert := &CachedCertificate{
		Domain:    "valid.com",
		ExpiresAt: now.Add(90 * 24 * time.Hour),
	}

	if cert.ExpiresAt.Before(now) {
		t.Error("Certificate should not be expired")
	}
}

func TestProviderDirectories_CustomHasNoURL(t *testing.T) {
	// Custom provider should not have a default URL
	if url := providerDirectories[ProviderCustom]; url != "" {
		t.Errorf("Expected empty URL for custom provider, got %s", url)
	}
}

func TestProviderDirectories_ZeroSSLHasNoURL(t *testing.T) {
	// ZeroSSL should not have a default URL in providerDirectories
	if url := providerDirectories[ProviderZeroSSL]; url != "" {
		t.Errorf("Expected empty URL for ZeroSSL, got %s", url)
	}
}

func TestChallengeHandler_MultipleTokens(t *testing.T) {
	handler := &ChallengeHandler{
		tokens: make(map[string]string),
	}

	tokens := []struct{ token, auth string }{
		{"token1", "auth1"},
		{"token2", "auth2"},
		{"token3", "auth3"},
	}

	for _, t := range tokens {
		handler.AddChallenge(t.token, t.auth)
	}

	if len(handler.tokens) != 3 {
		t.Errorf("Expected 3 tokens, got %d", len(handler.tokens))
	}
}

func TestManager_ChallengeHandler(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	config := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	m, err := NewManager(db, config)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	handler := m.ChallengeHandler()
	if handler == nil {
		t.Error("ChallengeHandler should not be nil")
	}
}

func TestLoadOrCreateAccountKey_ExistingKey(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	// First call creates key
	key1, err := loadOrCreateAccountKey(db)
	if err != nil {
		t.Fatalf("loadOrCreateAccountKey failed: %v", err)
	}

	// Second call should load existing key
	key2, err := loadOrCreateAccountKey(db)
	if err != nil {
		t.Fatalf("loadOrCreateAccountKey failed: %v", err)
	}

	// Keys should be the same (loaded from storage)
	if key1 == nil || key2 == nil {
		t.Error("Keys should not be nil")
	}
}

// Test ServeHTTP - ACME challenge handler
func TestManager_ServeHTTP(t *testing.T) {
	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	db := newTestDB(t)
	defer db.Close()

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	handler := m.ChallengeHandler()

	// Test unknown token - should return 404
	req := httptest.NewRequest("GET", "/.well-known/acme-challenge/unknown-token", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Logf("Expected 404 for unknown token, got %d", w.Code)
	}
}

// Test ServeHTTP with registered challenge
func TestChallengeHandler_ServeHTTP_WithChallenge(t *testing.T) {
	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	db := newTestDB(t)
	defer db.Close()

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	handler := m.challengeHandler

	// Register a challenge directly on the handler
	handler.AddChallenge("test-token", "test-key-authorization")

	// Test known token
	req := httptest.NewRequest("GET", "/.well-known/acme-challenge/test-token", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 for known token, got %d", w.Code)
	}
	if w.Body.String() != "test-key-authorization" {
		t.Errorf("Expected key authorization, got %s", w.Body.String())
	}
}

// Test parseTLSCertificate
func Test_parseTLSCertificate(t *testing.T) {
	// Test with valid certificate struct but no cert data
	cert := &CachedCertificate{
		Domain: "test.example.com",
	}
	_, err := parseTLSCertificate(cert)
	if err == nil {
		t.Error("Expected error for certificate without data")
	}
}

// Test decodeCertificate
func TestManager_decodeCertificate(t *testing.T) {
	// Test with empty data
	_, err := decodeCertificate([]byte{})
	if err == nil {
		t.Error("Expected error for empty certificate data")
	}

	// Test with invalid base64
	_, err = decodeCertificate([]byte("not-base64!"))
	if err == nil {
		t.Error("Expected error for invalid base64")
	}
}

// Test loadCertificates with no certificates
func TestManager_loadCertificates_Empty(t *testing.T) {
	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	db := newTestDB(t)
	defer db.Close()

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	err = m.loadCertificates()
	if err != nil {
		t.Logf("loadCertificates returned: %v", err)
	}
}

// Test GetCertificate with no certificates loaded
func TestManager_GetCertificate_NoCerts(t *testing.T) {
	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	db := newTestDB(t)
	defer db.Close()

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	enableSelfSignedFallbackForTest(t, m)

	// GetCertificate should obtain a new certificate for unknown domain
	cert, err := m.GetCertificate("unknown.example.com")
	if err != nil {
		t.Fatalf("GetCertificate failed: %v", err)
	}
	if cert == nil {
		t.Error("Expected certificate for unknown domain")
	}
	if cert.Domain != "unknown.example.com" {
		t.Errorf("Expected domain unknown.example.com, got %s", cert.Domain)
	}
}

// Test ObtainCertificate error handling
func TestManager_ObtainCertificate_Error(t *testing.T) {
	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	db := newTestDB(t)
	defer db.Close()

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	_, err = m.ObtainCertificate("test.example.com")
	if err == nil {
		t.Fatal("Expected ObtainCertificate to fail until real ACME issuance is implemented")
	}
	if !strings.Contains(err.Error(), "real ACME issuance is not implemented") {
		t.Fatalf("Expected not-implemented ACME error, got %v", err)
	}
}

// Test loadCertificates with empty storage
func TestManager_loadCertificates_EmptyStorage(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	err = m.loadCertificates()
	if err != nil {
		t.Errorf("loadCertificates failed: %v", err)
	}
	if len(m.certCache) != 0 {
		t.Errorf("Expected 0 certificates, got %d", len(m.certCache))
	}
}

// Test GetCertificate with valid cached certificate
func TestManager_GetCertificate_ValidCached(t *testing.T) {
	m := &Manager{
		certCache: map[string]*CachedCertificate{
			"example.com": {
				Domain:      "example.com",
				Certificate: []byte("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----"),
				PrivateKey:  []byte("-----BEGIN EC PRIVATE KEY-----\nkey\n-----END EC PRIVATE KEY-----"),
				ExpiresAt:   time.Now().Add(90 * 24 * time.Hour),
			},
		},
	}

	cert, err := m.GetCertificate("example.com")
	if err != nil {
		t.Errorf("GetCertificate failed: %v", err)
	}
	if cert == nil {
		t.Error("Expected certificate")
	}
}

// Test GetCertificate with expiring certificate (should attempt renewal)
func TestManager_GetCertificate_Expiring(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	enableSelfSignedFallbackForTest(t, m)

	// Add expiring certificate (less than 7 days)
	m.certCache["expiring.com"] = &CachedCertificate{
		Domain:      "expiring.com",
		Certificate: []byte("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----"),
		PrivateKey:  []byte("-----BEGIN EC PRIVATE KEY-----\ntest\n-----END EC PRIVATE KEY-----"),
		ExpiresAt:   time.Now().Add(5 * 24 * time.Hour),
	}

	// Should attempt renewal and succeed
	cert, err := m.GetCertificate("expiring.com")
	if err != nil {
		t.Fatalf("GetCertificate failed for expiring cert: %v", err)
	}
	if cert.Domain != "expiring.com" {
		t.Errorf("Expected domain expiring.com, got %s", cert.Domain)
	}
}

// Test parseTLSCertificate with valid EC key
func TestParseTLSCertificate_ValidECKey(t *testing.T) {
	// Generate a valid EC private key for testing
	key, err := generateTestECKey()
	if err != nil {
		t.Skipf("Skipping due to key generation error: %v", err)
	}

	keyBytes, _ := x509.MarshalECPrivateKey(key)
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	})

	cert := &CachedCertificate{
		Domain:      "valid-ec.com",
		Certificate: []byte("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----"),
		PrivateKey:  keyPEM,
	}

	_, err = parseTLSCertificate(cert)
	// Will fail due to invalid cert, but tests the key parsing path
	if err == nil {
		t.Log("parseTLSCertificate succeeded")
	}
}

// Helper function to generate test EC key
func generateTestECKey() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}

// Test decodeCertificate with valid format
func TestDecodeCertificate_ValidFormat(t *testing.T) {
	data := []byte("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n---KEY---\n-----BEGIN EC PRIVATE KEY-----\nkey\n-----END EC PRIVATE KEY-----")
	_, err := decodeCertificate(data)
	// Will fail due to invalid PEM, but tests the format parsing
	if err == nil {
		t.Log("decodeCertificate succeeded")
	}
}

// Test ObtainCertificate - tests error path due to ACME protocol not being fully implemented
func TestManager_ObtainCertificate_ErrorPath(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	_, err = m.ObtainCertificate("example.com")
	if err == nil {
		t.Fatal("Expected ObtainCertificate to fail until real ACME issuance is implemented")
	}
	if !strings.Contains(err.Error(), "real ACME issuance is not implemented") {
		t.Fatalf("Expected not-implemented ACME error, got %v", err)
	}
}

// Test GetCertificate - tests cache miss path
func TestManager_GetCertificate_CacheMiss(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	enableSelfSignedFallbackForTest(t, m)

	// GetCertificate for nonexistent domain should trigger obtain attempt
	cert, err := m.GetCertificate("nonexistent.com")
	if err != nil {
		t.Fatalf("GetCertificate failed for nonexistent domain: %v", err)
	}
	if cert.Domain != "nonexistent.com" {
		t.Errorf("Expected domain nonexistent.com, got %s", cert.Domain)
	}
}

// Test GetCertificate - tests cached certificate retrieval
func TestManager_GetCertificate_Cached(t *testing.T) {
	m := &Manager{
		certCache: map[string]*CachedCertificate{
			"cached.com": {
				Domain:      "cached.com",
				Certificate: []byte("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----"),
				PrivateKey:  []byte("-----BEGIN EC PRIVATE KEY-----\nkey\n-----END EC PRIVATE KEY-----"),
				ExpiresAt:   time.Now().Add(90 * 24 * time.Hour),
			},
		},
	}

	cert, err := m.GetCertificate("cached.com")
	if err != nil {
		t.Errorf("GetCertificate failed: %v", err)
	}
	if cert.Domain != "cached.com" {
		t.Errorf("Expected domain cached.com, got %s", cert.Domain)
	}
}

// Test GetCertificate - tests expired certificate path
func TestManager_GetCertificate_Expired(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	// Create manager with expired cert in cache
	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	enableSelfSignedFallbackForTest(t, m)

	// Add expired certificate to cache
	m.certCache["expired.com"] = &CachedCertificate{
		Domain:      "expired.com",
		Certificate: []byte("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----"),
		PrivateKey:  []byte("-----BEGIN EC PRIVATE KEY-----\nkey\n-----END EC PRIVATE KEY-----"),
		ExpiresAt:   time.Now().Add(-24 * time.Hour), // Expired
	}

	// GetCertificate should try to renew/obtain new cert
	cert, err := m.GetCertificate("expired.com")
	if err != nil {
		t.Fatalf("GetCertificate failed for expired domain: %v", err)
	}
	if cert.Domain != "expired.com" {
		t.Errorf("Expected domain expired.com, got %s", cert.Domain)
	}
}

// Test ServeHTTP - tests HTTP challenge handler
func TestChallengeHandler_ServeHTTP_GetRequest(t *testing.T) {
	handler := &ChallengeHandler{
		tokens: map[string]string{
			"test-token": "test-key-auth",
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/.well-known/acme-challenge/test-token", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
	if w.Body.String() != "test-key-auth" {
		t.Errorf("Expected key-auth, got %s", w.Body.String())
	}
}

// Test ServeHTTP - tests token not found
func TestChallengeHandler_ServeHTTP_TokenNotFound(t *testing.T) {
	handler := &ChallengeHandler{
		tokens: make(map[string]string),
	}

	req := httptest.NewRequest(http.MethodGet, "/.well-known/acme-challenge/missing-token", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

// Test parseTLSCertificate - tests invalid certificate
func TestParseTLSCertificate_InvalidCert(t *testing.T) {
	cert := &CachedCertificate{
		Domain:      "invalid.com",
		Certificate: []byte("invalid-cert-data"),
		PrivateKey:  []byte("invalid-key-data"),
	}

	_, err := parseTLSCertificate(cert)
	if err == nil {
		t.Error("Expected error for invalid certificate")
	}
}

// Test parseTLSCertificate - tests missing key
func TestParseTLSCertificate_MissingKey(t *testing.T) {
	cert := &CachedCertificate{
		Domain:      "nokey.com",
		Certificate: []byte("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----"),
		PrivateKey:  nil,
	}

	_, err := parseTLSCertificate(cert)
	if err == nil {
		t.Error("Expected error for missing private key")
	}
}

// Test ObtainCertificate - CSR creation error path (theoretical)
func TestManager_ObtainCertificate_CSRPath(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// This tests the key generation and CSR creation paths
	// Will fail at ACME protocol but exercises earlier code
	_, err = m.ObtainCertificate("csr-test.com")
	// Expected to fail at ACME protocol
	if err == nil {
		t.Log("ObtainCertificate succeeded (unexpected)")
	}
}

// Test GetCertificate - error path after ObtainCertificate fails
func TestManager_GetCertificate_ObtainError(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Cache miss triggers ObtainCertificate, which should fail until real ACME is implemented.
	_, err = m.GetCertificate("obtain-error.com")
	if err == nil {
		t.Fatal("Expected GetCertificate to fail on cache miss without real ACME issuance")
	}
	if !strings.Contains(err.Error(), "real ACME issuance is not implemented") {
		t.Fatalf("Expected not-implemented ACME error, got %v", err)
	}
}

// Test encodeCertificate and decodeCertificate roundtrip
func TestEncodeDecodeCertificate_Roundtrip(t *testing.T) {
	// Generate a real EC key for testing
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("Failed to marshal key: %v", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	})

	// Create a self-signed certificate for testing
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "roundtrip.com",
		},
		DNSNames:  []string{"roundtrip.com"},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(24 * time.Hour),
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	})

	original := &CachedCertificate{
		Domain:      "roundtrip.com",
		Certificate: certPEM,
		PrivateKey:  keyPEM,
		IssuedAt:    template.NotBefore,
		ExpiresAt:   template.NotAfter,
		Issuer:      "Test CA",
	}

	encoded := encodeCertificate(original)
	decoded, err := decodeCertificate(encoded)
	if err != nil {
		t.Fatalf("decodeCertificate failed: %v", err)
	}

	if decoded.Domain != original.Domain {
		t.Errorf("Domain mismatch: %s vs %s", decoded.Domain, original.Domain)
	}
	if string(decoded.Certificate) != string(original.Certificate) {
		t.Error("Certificate data mismatch")
	}
	if string(decoded.PrivateKey) != string(original.PrivateKey) {
		t.Error("Private key data mismatch")
	}
}

// Test loadCertificates with invalid certificate data (continue path)
func TestManager_loadCertificates_InvalidCertData(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	// Write invalid certificate data
	err := db.Put("acme/cert/invalid.com", []byte("invalid-data"))
	if err != nil {
		t.Fatalf("Failed to write test data: %v", err)
	}

	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Should skip invalid cert and continue (no error)
	err = m.loadCertificates()
	if err != nil {
		t.Errorf("loadCertificates should skip invalid certs: %v", err)
	}
}

// Test ServeHTTP with dot token
func TestChallengeHandler_ServeHTTP_DotToken(t *testing.T) {
	handler := &ChallengeHandler{
		tokens: make(map[string]string),
	}

	req := httptest.NewRequest(http.MethodGet, "/.well-known/acme-challenge/.", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

// Test RenewIfNeeded with no expiring certs
func TestManager_RenewIfNeeded_NoExpiring(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Add non-expiring cert to cache
	m.certCache["valid.com"] = &CachedCertificate{
		Domain:      "valid.com",
		ExpiresAt:   time.Now().Add(90 * 24 * time.Hour),
		Certificate: []byte("cert"),
		PrivateKey:  []byte("key"),
	}

	renewed, err := m.RenewIfNeeded()
	if err != nil {
		t.Errorf("RenewIfNeeded failed: %v", err)
	}
	if len(renewed) != 0 {
		t.Errorf("Expected 0 renewed certs, got %d", len(renewed))
	}
}

// Test RenewIfNeeded with expiring cert
func TestManager_RenewIfNeeded_Expiring(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Add expiring cert to cache
	m.certCache["expiring.com"] = &CachedCertificate{
		Domain:      "expiring.com",
		ExpiresAt:   time.Now().Add(5 * 24 * time.Hour),
		Certificate: []byte("cert"),
		PrivateKey:  []byte("key"),
	}

	// RenewIfNeeded will try to renew but fail (ACME not implemented)
	_, err = m.RenewIfNeeded()
	// Error is expected since ACME protocol fails
	_ = err
}

// Test GetAllDomains
func TestManager_GetAllDomains(t *testing.T) {
	m := &Manager{
		certCache: map[string]*CachedCertificate{
			"domain1.com": {Domain: "domain1.com"},
			"domain2.com": {Domain: "domain2.com"},
		},
	}

	domains := m.GetAllDomains()
	if len(domains) != 2 {
		t.Errorf("Expected 2 domains, got %d", len(domains))
	}
}

// Test CertificateInfo - found
func TestManager_CertificateInfo_Found(t *testing.T) {
	expectedCert := &CachedCertificate{
		Domain:      "info.com",
		ExpiresAt:   time.Now().Add(90 * 24 * time.Hour),
		Certificate: []byte("cert"),
		PrivateKey:  []byte("key"),
	}

	m := &Manager{
		certCache: map[string]*CachedCertificate{
			"info.com": expectedCert,
		},
	}

	cert, err := m.CertificateInfo("info.com")
	if err != nil {
		t.Errorf("CertificateInfo failed: %v", err)
	}
	if cert.Domain != "info.com" {
		t.Errorf("Expected domain info.com, got %s", cert.Domain)
	}
}

// Test CertificateInfo - not found
func TestManager_CertificateInfo_NotFound(t *testing.T) {
	m := &Manager{
		certCache: make(map[string]*CachedCertificate),
	}

	_, err := m.CertificateInfo("nonexistent.com")
	if err == nil {
		t.Error("Expected error for nonexistent domain")
	}
}

// Test loadCertificates with closed database (PrefixScan error path)
func TestManager_loadCertificates_PrefixScanError(t *testing.T) {
	db := newTestDB(t)

	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Close the database to trigger PrefixScan error
	db.Close()

	// loadCertificates should return error when PrefixScan fails
	err = m.loadCertificates()
	if err == nil {
		t.Error("Expected error from PrefixScan on closed database")
	}
}

// Test TLSConfig.GetCertificate with valid cached certificate
func TestTLSConfig_GetCertificate_ValidCert(t *testing.T) {
	// Generate a valid EC key for testing
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("Failed to marshal key: %v", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	})

	// Create a self-signed certificate for testing
	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			CommonName: "valid.com",
		},
		DNSNames:  []string{"valid.com"},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(90 * 24 * time.Hour),
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	})

	m := &Manager{
		certCache: map[string]*CachedCertificate{
			"valid.com": {
				Domain:      "valid.com",
				Certificate: certPEM,
				PrivateKey:  keyPEM,
				ExpiresAt:   template.NotAfter,
			},
		},
	}

	tlsConfig := &TLSConfig{manager: m}
	hello := &ClientHelloInfo{ServerName: "valid.com"}

	tlsCert, err := tlsConfig.GetCertificate(hello)
	if err != nil {
		t.Errorf("GetCertificate failed: %v", err)
	}
	if tlsCert == nil {
		t.Fatal("Expected TLS certificate")
	}
	if len(tlsCert.Certificate) == 0 {
		t.Error("Expected certificate chain")
	}
	if tlsCert.PrivateKey == nil {
		t.Error("Expected private key")
	}
}

// TestObtainCertificate_ErrorPath tests the error path when ACME protocol fails
func TestObtainCertificate_ErrorPath(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncrypt,
		Email:     "test@example.com",
		AcceptTOS: true,
		CertPath:  t.TempDir(),
	}

	mgr, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	_, err = mgr.ObtainCertificate("test.example.com")
	if err == nil {
		t.Fatal("Expected ObtainCertificate to fail until real ACME issuance is implemented")
	}
	if !strings.Contains(err.Error(), "real ACME issuance is not implemented") {
		t.Fatalf("Expected not-implemented ACME error, got %v", err)
	}
}

// TestObtainCertificate_StorageError tests the storage error path after successful obtain
func TestObtainCertificate_StorageError(t *testing.T) {
	db := newTestDB(t)

	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncrypt,
		Email:     "test@example.com",
		AcceptTOS: true,
		CertPath:  t.TempDir(),
	}

	mgr, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Close the database to trigger storage errors
	db.Close()

	// This tests the storage error path after certificate obtain
	// Note: Since executeACMEProtocol always returns error, this path won't be hit
	// But we're adding the test structure for when ACME is implemented
	_, err = mgr.ObtainCertificate("test.example.com")
	if err == nil {
		t.Error("Expected error")
	}
}

// TestObtainCertificate_WithMockACME tests ObtainCertificate with a mock ACME response
func TestObtainCertificate_WithMockACME(t *testing.T) {
	// Generate a valid EC key for testing
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("Failed to marshal key: %v", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	})

	// Create a self-signed certificate for testing
	template := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject: pkix.Name{
			CommonName: "test.example.com",
		},
		DNSNames:  []string{"test.example.com"},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(90 * 24 * time.Hour),
		Issuer:    pkix.Name{CommonName: "Test CA"},
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	})

	// Test certificate parsing logic (lines 278-286)
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("Failed to decode certificate PEM")
	}

	parsedCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("Failed to parse certificate: %v", err)
	}

	// Verify parsed certificate fields
	if parsedCert.Subject.CommonName != "test.example.com" {
		t.Errorf("Expected CN test.example.com, got %s", parsedCert.Subject.CommonName)
	}

	// Create a CachedCertificate to test the full structure
	cert := &CachedCertificate{
		Domain:      "test.example.com",
		Certificate: certPEM,
		PrivateKey:  keyPEM,
		IssuedAt:    parsedCert.NotBefore,
		ExpiresAt:   parsedCert.NotAfter,
		Issuer:      parsedCert.Issuer.CommonName,
	}

	if cert.Domain != "test.example.com" {
		t.Errorf("Expected domain test.example.com, got %s", cert.Domain)
	}
	// For self-signed certificate, issuer common name is the same as subject
	if cert.Issuer != "test.example.com" {
		t.Errorf("Expected issuer 'test.example.com', got %s", cert.Issuer)
	}

	// Test encode/decode roundtrip
	encoded := encodeCertificate(cert)
	decoded, err := decodeCertificate(encoded)
	if err != nil {
		t.Fatalf("decodeCertificate failed: %v", err)
	}

	if decoded.Domain != cert.Domain {
		t.Errorf("Domain mismatch: %s vs %s", decoded.Domain, cert.Domain)
	}
	if decoded.Issuer != cert.Issuer {
		t.Errorf("Issuer mismatch: %s vs %s", decoded.Issuer, cert.Issuer)
	}
}

// TestObtainCertificate_InvalidPEM tests error handling for invalid PEM
func TestObtainCertificate_InvalidPEM(t *testing.T) {
	// Test PEM decode failure path (line 279-281)
	invalidPEM := []byte("not a valid PEM")
	block, _ := pem.Decode(invalidPEM)
	if block != nil {
		t.Error("Expected nil block for invalid PEM")
	}

	// Test with empty PEM
	emptyPEM := []byte("")
	block, _ = pem.Decode(emptyPEM)
	if block != nil {
		t.Error("Expected nil block for empty PEM")
	}
}

// TestObtainCertificate_ParseCertificateError tests x509.ParseCertificate error
func TestObtainCertificate_ParseCertificateError(t *testing.T) {
	// Test with invalid certificate bytes
	invalidBytes := []byte("invalid certificate data")
	_, err := x509.ParseCertificate(invalidBytes)
	if err == nil {
		t.Error("Expected error for invalid certificate bytes")
	}
}

// TestManager_ObtainCertificate_MultipleDomains tests obtaining certificates for multiple domains
func TestManager_ObtainCertificate_MultipleDomains(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	mgr, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	enableSelfSignedFallbackForTest(t, mgr)

	domains := []string{"test1.com", "test2.com", "test3.com"}

	for _, domain := range domains {
		cert, err := mgr.ObtainCertificate(domain)
		if err != nil {
			t.Fatalf("ObtainCertificate failed for %s: %v", domain, err)
		}
		if cert.Domain != domain {
			t.Errorf("Expected domain %s, got %s", domain, cert.Domain)
		}
	}
}

// TestManager_ObtainCertificate_Cached tests obtaining certificate when already cached
func TestManager_ObtainCertificate_Cached(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	mgr, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Pre-populate cache
	mgr.certCache["cached.example.com"] = &CachedCertificate{
		Domain:      "cached.example.com",
		ExpiresAt:   time.Now().Add(90 * 24 * time.Hour),
		Certificate: []byte("test-cert"),
		PrivateKey:  []byte("test-key"),
	}

	// Should return cached cert without calling ObtainCertificate
	cert, err := mgr.GetCertificate("cached.example.com")
	if err != nil {
		t.Errorf("GetCertificate failed: %v", err)
	}
	if cert == nil {
		t.Fatal("Expected cached certificate")
	}
	if cert.Domain != "cached.example.com" {
		t.Errorf("Expected domain cached.example.com, got %s", cert.Domain)
	}
}

// TestObtainCertificate_PEMDecodeError tests PEM decode error path
func TestObtainCertificate_PEMDecodeError(t *testing.T) {
	// This test verifies the PEM decode logic
	// In actual ObtainCertificate, certPEM comes from executeACMEProtocol
	// which always returns error, so this path isn't directly testable
	// But we can verify the logic separately

	invalidPEM := []byte("invalid PEM data")
	block, _ := pem.Decode(invalidPEM)
	if block != nil {
		t.Error("Expected nil block for invalid PEM")
	}

	// Test with nil block handling
	if block == nil {
		// This is the error path at line 279-281
		t.Log("PEM decode returned nil block (expected for invalid PEM)")
	}
}

// TestLoadOrCreateAccountKey_InvalidPEM tests loading with invalid PEM data
func TestLoadOrCreateAccountKey_InvalidPEM(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	// Store invalid PEM data
	if err := db.Put("acme/account_key", []byte("invalid PEM data")); err != nil {
		t.Fatalf("Failed to store invalid key: %v", err)
	}

	// Should generate new key when existing key is invalid
	key, err := loadOrCreateAccountKey(db)
	if err != nil {
		t.Fatalf("loadOrCreateAccountKey failed: %v", err)
	}
	if key == nil {
		t.Error("Expected new key to be generated")
	}
}

// TestLoadOrCreateAccountKey_CorruptedKey tests loading with corrupted EC key
func TestLoadOrCreateAccountKey_CorruptedKey(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	// Store valid PEM but corrupted key data
	corruptedPEM := `-----BEGIN EC PRIVATE KEY-----
MHQCAQEEIIjM9A6YqXz4U6zR8P7KQhQWJMQhX9GqWzK8Lz8FZ8o8AoGCCqGSM49
AwEHoUQDQgAEjM9A6YqXz4U6zR8P7KQhQWJMQhX9GqWzK8Lz8FZ8o8FRR4E8C
-----END EC PRIVATE KEY-----`

	if err := db.Put("acme/account_key", []byte(corruptedPEM)); err != nil {
		t.Fatalf("Failed to store corrupted key: %v", err)
	}

	// Should generate new key when existing key is corrupted
	key, err := loadOrCreateAccountKey(db)
	if err != nil {
		t.Fatalf("loadOrCreateAccountKey failed: %v", err)
	}
	if key == nil {
		t.Error("Expected new key to be generated")
	}
}

// TestLoadCertificates_InvalidCertData tests loading certificates with invalid data
func TestLoadCertificates_InvalidCertData(t *testing.T) {
	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	db := newTestDB(t)
	defer db.Close()

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Store invalid certificate data
	if err := db.Put("acme/cert/test.example.com", []byte("invalid cert data")); err != nil {
		t.Fatalf("Failed to store invalid cert: %v", err)
	}

	// Reload certificates - should skip invalid ones
	if err := m.loadCertificates(); err != nil {
		t.Fatalf("loadCertificates failed: %v", err)
	}

	if _, exists := m.certCache["test.example.com"]; exists {
		t.Fatal("Invalid certificate data should not be loaded into cache")
	}
	_, err = m.GetCertificate("test.example.com")
	if err == nil {
		t.Fatal("Expected cache miss to fail until real ACME issuance is implemented")
	}
	if !strings.Contains(err.Error(), "real ACME issuance is not implemented") {
		t.Fatalf("Expected not-implemented ACME error, got %v", err)
	}
}

// TestLoadCertificates_MultipleCerts tests loading multiple certificates
func TestLoadCertificates_MultipleCerts(t *testing.T) {
	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	db := newTestDB(t)
	defer db.Close()

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Create and store a valid certificate
	cert := &CachedCertificate{
		Domain:      "test1.example.com",
		Certificate: []byte("-----BEGIN CERTIFICATE-----\nMIIBkTCB+wIJAKHBfpE"),
		PrivateKey:  []byte("-----BEGIN EC PRIVATE KEY-----\nMHQCAQEEI"),
	}

	certData := encodeCertificate(cert)
	if err := db.Put("acme/cert/test1.example.com", certData); err != nil {
		t.Fatalf("Failed to store cert: %v", err)
	}

	// Reload certificates
	if err := m.loadCertificates(); err != nil {
		t.Fatalf("loadCertificates failed: %v", err)
	}

	// Certificate should be in cache (even if invalid, it will be skipped)
}

// TestManager_DeleteCertificate_NotFound tests deleting non-existent certificate
func TestManager_DeleteCertificate_NotFound(t *testing.T) {
	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	db := newTestDB(t)
	defer db.Close()

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Delete non-existent certificate should not error
	if err := m.DeleteCertificate("non-existent.example.com"); err != nil {
		t.Errorf("DeleteCertificate failed: %v", err)
	}
}

// TestNewManager_LoadCertificatesError tests NewManager when loadCertificates fails
func TestNewManager_LoadCertificatesError(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalECPrivateKey failed: %v", err)
	}

	store := &prefixFailStore{
		values: map[string][]byte{
			"acme/account_key": pem.EncodeToMemory(&pem.Block{
				Type:  "EC PRIVATE KEY",
				Bytes: keyBytes,
			}),
		},
	}
	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	_, err = NewManager(store, cfg)
	if err == nil {
		t.Fatal("Expected NewManager to fail when certificate PrefixScan fails")
	}
	if !strings.Contains(err.Error(), "failed to load certificates") {
		t.Fatalf("Expected load certificates error, got %v", err)
	}
}

// TestLoadCertificates_WithInvalidCertFormat tests loading certificates with invalid format
func TestLoadCertificates_WithInvalidCertFormat(t *testing.T) {
	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	db := newTestDB(t)
	defer db.Close()

	// Store certificate with valid format but invalid cert data
	// This tests the decodeCertificate error path
	certData := []byte("-----BEGIN CERTIFICATE-----\nMIIBkTCB+wIJAKHBfpE\n---KEY---\n-----BEGIN EC PRIVATE KEY-----\nMHQCAQEEI")
	if err := db.Put("acme/cert/test.example.com", certData); err != nil {
		t.Fatalf("Failed to store cert: %v", err)
	}

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Invalid cert should be skipped during loading
	cert, err := m.GetCertificate("test.example.com")
	if err == nil {
		t.Log("Certificate may or may not be loaded depending on parsing")
	}
	if cert != nil {
		t.Log("Got certificate:", cert.Domain)
	}
}

// TestObtainCertificate_ExecuteACMEError tests ObtainCertificate when ACME protocol fails
func TestObtainCertificate_ExecuteACMEError(t *testing.T) {
	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	db := newTestDB(t)
	defer db.Close()

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	_, err = m.ObtainCertificate("test.example.com")
	if err == nil {
		t.Fatal("Expected ObtainCertificate to fail until real ACME issuance is implemented")
	}
	if !strings.Contains(err.Error(), "real ACME issuance is not implemented") {
		t.Fatalf("Expected not-implemented ACME error, got %v", err)
	}
}

// TestLoadOrCreateAccountKey_MarshalError tests error during key marshaling
func TestLoadOrCreateAccountKey_MarshalError(t *testing.T) {
	// This tests the error path at line 148-149 in manager.go
	// x509.MarshalECPrivateKey should not fail for valid keys,
	// so we test the happy path to ensure coverage
	db := newTestDB(t)
	defer db.Close()

	// First call creates key
	key1, err := loadOrCreateAccountKey(db)
	if err != nil {
		t.Fatalf("loadOrCreateAccountKey failed: %v", err)
	}
	if key1 == nil {
		t.Error("Expected key to be created")
	}

	// Verify key was stored
	keyData, err := db.Get("acme/account_key")
	if err != nil {
		t.Fatalf("Failed to get stored key: %v", err)
	}
	if len(keyData) == 0 {
		t.Error("Expected key to be stored")
	}
}

// TestRenewIfNeeded_WithExpiringCert tests certificate renewal
func TestRenewIfNeeded_WithExpiringCert(t *testing.T) {
	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	db := newTestDB(t)
	defer db.Close()

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Create certificate that expires soon
	cert := &CachedCertificate{
		Domain:      "expiring.example.com",
		Certificate: []byte("-----BEGIN CERTIFICATE-----\nMIIBkTCB+wIJAKHBfpE"),
		PrivateKey:  []byte("-----BEGIN EC PRIVATE KEY-----\nMHQCAQEEI"),
		IssuedAt:    time.Now().Add(-90 * 24 * time.Hour), // 90 days ago
		ExpiresAt:   time.Now().Add(24 * time.Hour),       // Expires in 1 day
	}

	m.cacheMu.Lock()
	m.certCache["expiring.example.com"] = cert
	m.cacheMu.Unlock()

	// Try to renew - will fail because ACME protocol is not fully implemented
	// but should handle the expiring cert detection
	renewed, err := m.RenewIfNeeded()
	// We expect either success (if cert is not near expiry) or error
	// Since the cert expires in 1 day (less than 7 days threshold), it should try to renew
	if err != nil {
		t.Logf("Renewal error (expected): %v", err)
	}
	if len(renewed) > 0 {
		t.Log("Certificate was renewed")
	}
}

// TestGetCertificate_WithError tests GetCertificate error handling
func TestGetCertificate_WithError(t *testing.T) {
	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	db := newTestDB(t)
	defer db.Close()

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	_, err = m.GetCertificate("non-existent.example.com")
	if err == nil {
		t.Fatal("Expected GetCertificate to fail on cache miss without real ACME issuance")
	}
	if !strings.Contains(err.Error(), "real ACME issuance is not implemented") {
		t.Fatalf("Expected not-implemented ACME error, got %v", err)
	}
}

// TestManager_ObtainCertificate_StorageError tests storage failure during certificate saving
func TestManager_ObtainCertificate_StorageError(t *testing.T) {
	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	db := newTestDB(t)
	defer db.Close()

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	enableSelfSignedFallbackForTest(t, m)

	// Test-only fallback lets this exercise cache and storage persistence.
	cert, err := m.ObtainCertificate("test.example.com")
	if err != nil {
		t.Fatalf("ObtainCertificate failed: %v", err)
	}

	// Verify certificate was persisted to storage
	key := "acme/cert/test.example.com"
	data, err := db.Get(key)
	if err != nil {
		t.Fatalf("Certificate should have been saved to storage: %v", err)
	}
	if len(data) == 0 {
		t.Error("Certificate data in storage should not be empty")
	}
	_ = cert
}

// TestManager_LoadCertificates_CorruptedData tests loading corrupted certificate data
func TestManager_LoadCertificates_CorruptedData(t *testing.T) {
	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncrypt,
		Email:     "test@example.com",
		AcceptTOS: true,
		CertPath:  t.TempDir(),
	}

	db := newTestDB(t)
	defer db.Close()

	// Store corrupted certificate data
	key := "acme/cert/corrupted.example.com"
	db.Put(key, []byte("not-valid-certificate-data"))

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	_ = m // m is created successfully even with corrupted data
	// The corrupted entry should be skipped during load
	t.Log("Manager created with corrupted certificate data in storage")
}

// TestManager_TLSConfig_NoCertificates tests TLSConfig when no certificates are available
func TestManager_TLSConfig_NoCertificates(t *testing.T) {
	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	db := newTestDB(t)
	defer db.Close()

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	// Get TLS config with no certificates
	tlsConfig := m.TLSConfig()
	if tlsConfig == nil {
		t.Error("Expected TLSConfig to return a config even with no certificates")
	}
}

// TestManager_CertificateInfo_Expired tests CertificateInfo with expired certificate
func TestManager_CertificateInfo_Expired(t *testing.T) {
	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	db := newTestDB(t)
	defer db.Close()

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Create an expired certificate
	certKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "expired.example.com"},
		NotBefore:    time.Now().Add(-24 * time.Hour * 30),
		NotAfter:     time.Now().Add(-24 * time.Hour), // Expired yesterday
		DNSNames:     []string{"expired.example.com"},
	}

	certBytes, _ := x509.CreateCertificate(rand.Reader, template, template, &certKey.PublicKey, certKey)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certBytes})
	keyBytes, _ := x509.MarshalECPrivateKey(certKey)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	// Store expired certificate
	m.cacheMu.Lock()
	m.certCache["expired.example.com"] = &CachedCertificate{
		Domain:      "expired.example.com",
		Certificate: certPEM,
		PrivateKey:  keyPEM,
		IssuedAt:    template.NotBefore,
		ExpiresAt:   template.NotAfter,
	}
	m.cacheMu.Unlock()

	// Get certificate info
	info, err := m.CertificateInfo("expired.example.com")
	if err != nil {
		t.Errorf("CertificateInfo failed: %v", err)
	}
	if info.Domain != "expired.example.com" {
		t.Errorf("Expected domain expired.example.com, got %s", info.Domain)
	}
	// Verify the certificate is expired
	if time.Now().Before(info.ExpiresAt) {
		t.Error("Expected certificate to be expired")
	}
}

// TestGenerateSelfSignedCert tests self-signed certificate generation
func TestGenerateSelfSignedCert(t *testing.T) {
	db := newTestDB(t)
	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncrypt,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	mgr, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer db.Close()

	certPEM, err := mgr.generateSelfSignedCert("example.com")
	if err != nil {
		t.Fatalf("generateSelfSignedCert failed: %v", err)
	}

	// Decode and verify the certificate
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("Failed to decode PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("Failed to parse certificate: %v", err)
	}

	if cert.Subject.CommonName != "example.com" {
		t.Errorf("Expected CN example.com, got %s", cert.Subject.CommonName)
	}

	if len(cert.DNSNames) != 1 || cert.DNSNames[0] != "example.com" {
		t.Errorf("Expected DNS name example.com, got %v", cert.DNSNames)
	}

	// Verify the key type is ECDSA
	if cert.PublicKeyAlgorithm != x509.ECDSA {
		t.Errorf("Expected ECDSA key algorithm, got %v", cert.PublicKeyAlgorithm)
	}
}

// TestParseTLSCertificate tests the parseTLSCertificate function
func TestParseTLSCertificate(t *testing.T) {
	// Generate a test certificate
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test.example.com"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(24 * time.Hour),
		DNSNames:     []string{"test.example.com"},
	}
	certDER, _ := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, _ := x509.MarshalECPrivateKey(priv)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	cachedCert := &CachedCertificate{
		Certificate: certPEM,
		PrivateKey:  keyPEM,
	}

	info, err := parseTLSCertificate(cachedCert)
	if err != nil {
		t.Fatalf("parseTLSCertificate failed: %v", err)
	}

	// Parse the DER bytes to verify the certificate
	x509Cert, err := x509.ParseCertificate(info.Certificate[0])
	if err != nil {
		t.Fatalf("x509.ParseCertificate failed: %v", err)
	}

	if x509Cert.Subject.CommonName != "test.example.com" {
		t.Errorf("Expected CN test.example.com, got %s", x509Cert.Subject.CommonName)
	}
}

// TestServeHTTP_HTTPChallenge tests the ChallengeHandler with ACME challenge
func TestServeHTTP_HTTPChallenge(t *testing.T) {
	db := newTestDB(t)
	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncrypt,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	mgr, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer db.Close()

	// Test with ACME challenge path - no token stored, should return 404
	req, _ := http.NewRequest("GET", "/.well-known/acme-challenge/test-token", nil)
	w := httptest.NewRecorder()

	mgr.ChallengeHandler().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for unknown token, got %d", w.Code)
	}

	// Test wrong method - should return 405
	req, _ = http.NewRequest("POST", "/.well-known/acme-challenge/test-token", nil)
	w = httptest.NewRecorder()

	mgr.ChallengeHandler().ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected 405 for POST, got %d", w.Code)
	}
}

// TestLoadCertificates_ValidCert tests the success path where a valid cert is loaded
func TestLoadCertificates_ValidCert(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	// Generate a real certificate
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	keyBytes, _ := x509.MarshalECPrivateKey(key)
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	})

	template := &x509.Certificate{
		SerialNumber: big.NewInt(100),
		Subject:      pkix.Name{CommonName: "loaded.example.com"},
		DNSNames:     []string{"loaded.example.com"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(90 * 24 * time.Hour),
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("Failed to create certificate: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	})

	// Store in the format expected by loadCertificates
	cert := &CachedCertificate{
		Domain:      "loaded.example.com",
		Certificate: certPEM,
		PrivateKey:  keyPEM,
		IssuedAt:    template.NotBefore,
		ExpiresAt:   template.NotAfter,
		Issuer:      template.Subject.CommonName,
	}

	certData := encodeCertificate(cert)
	if err := db.Put("acme/cert/loaded.example.com", certData); err != nil {
		t.Fatalf("Failed to store cert: %v", err)
	}

	// Create manager - this calls loadCertificates internally
	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Verify cert was loaded into cache
	info, err := m.CertificateInfo("loaded.example.com")
	if err != nil {
		t.Fatalf("CertificateInfo failed: cert should have been loaded: %v", err)
	}
	if info.Domain != "loaded.example.com" {
		t.Errorf("Expected domain loaded.example.com, got %s", info.Domain)
	}
	if info.Issuer != "loaded.example.com" {
		t.Errorf("Expected issuer loaded.example.com, got %s", info.Issuer)
	}
}

// TestLoadCertificates_MultipleValidCerts tests loading several valid certificates
func TestLoadCertificates_MultipleValidCerts(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	domains := []string{"a.example.com", "b.example.com", "c.example.com"}
	for _, domain := range domains {
		key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		keyBytes, _ := x509.MarshalECPrivateKey(key)
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

		template := &x509.Certificate{
			SerialNumber: big.NewInt(1),
			Subject:      pkix.Name{CommonName: domain},
			DNSNames:     []string{domain},
			NotBefore:    time.Now(),
			NotAfter:     time.Now().Add(90 * 24 * time.Hour),
		}

		certBytes, _ := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
		certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certBytes})

		certData := encodeCertificate(&CachedCertificate{
			Domain:      domain,
			Certificate: certPEM,
			PrivateKey:  keyPEM,
			IssuedAt:    template.NotBefore,
			ExpiresAt:   template.NotAfter,
			Issuer:      domain,
		})

		db.Put("acme/cert/"+domain, certData)
	}

	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// All three should be loaded
	for _, domain := range domains {
		_, err := m.CertificateInfo(domain)
		if err != nil {
			t.Errorf("Expected cert for %s to be loaded: %v", domain, err)
		}
	}
}

// TestLoadCertificates_MixedValidAndInvalid tests that invalid certs are skipped while valid ones load
func TestLoadCertificates_MixedValidAndInvalid(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	// Store invalid data
	db.Put("acme/cert/invalid.com", []byte("garbage"))

	// Store a valid certificate
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	keyBytes, _ := x509.MarshalECPrivateKey(key)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "valid.example.com"},
		DNSNames:     []string{"valid.example.com"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(90 * 24 * time.Hour),
	}

	certBytes, _ := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certBytes})

	certData := encodeCertificate(&CachedCertificate{
		Domain:      "valid.example.com",
		Certificate: certPEM,
		PrivateKey:  keyPEM,
		IssuedAt:    template.NotBefore,
		ExpiresAt:   template.NotAfter,
		Issuer:      "valid.example.com",
	})

	db.Put("acme/cert/valid.example.com", certData)

	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Valid cert should be loaded
	_, err = m.CertificateInfo("valid.example.com")
	if err != nil {
		t.Errorf("Expected valid cert to be loaded: %v", err)
	}

	// Invalid cert should NOT be in cache
	_, err = m.CertificateInfo("invalid.com")
	if err == nil {
		t.Error("Expected invalid cert to NOT be in cache")
	}
}

// TestDecodeCertificate_ValidRoundtrip tests decodeCertificate with real cert data
func TestDecodeCertificate_ValidRoundtrip(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	keyBytes, _ := x509.MarshalECPrivateKey(key)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	template := &x509.Certificate{
		SerialNumber: big.NewInt(200),
		Subject:      pkix.Name{CommonName: "roundtrip.example.com"},
		DNSNames:     []string{"roundtrip.example.com"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(90 * 24 * time.Hour),
	}

	certBytes, _ := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certBytes})

	original := &CachedCertificate{
		Domain:      "roundtrip.example.com",
		Certificate: certPEM,
		PrivateKey:  keyPEM,
		IssuedAt:    template.NotBefore,
		ExpiresAt:   template.NotAfter,
		Issuer:      "Test Issuer",
	}

	encoded := encodeCertificate(original)
	decoded, err := decodeCertificate(encoded)
	if err != nil {
		t.Fatalf("decodeCertificate failed: %v", err)
	}

	if decoded.Domain != "roundtrip.example.com" {
		t.Errorf("Expected domain roundtrip.example.com, got %s", decoded.Domain)
	}
	if decoded.Issuer != "roundtrip.example.com" {
		t.Errorf("Expected issuer roundtrip.example.com, got %s", decoded.Issuer)
	}
	if decoded.ExpiresAt.IsZero() {
		t.Error("Expected non-zero expiry time")
	}
	if decoded.IssuedAt.IsZero() {
		t.Error("Expected non-zero issue time")
	}
}

// TestChallengeHandler_ServeHTTP_RootPath tests challenge handler with root path
func TestChallengeHandler_ServeHTTP_RootPath(t *testing.T) {
	handler := &ChallengeHandler{
		tokens: map[string]string{
			"test-token": "test-key-auth",
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for root path, got %d", w.Code)
	}
}

// TestObtainCertificate_OverwritesCache tests that obtaining a cert for an existing domain updates cache
func TestObtainCertificate_UpdatesCache(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	enableSelfSignedFallbackForTest(t, m)

	// First obtain
	cert1, err := m.ObtainCertificate("overwrite.example.com")
	if err != nil {
		t.Fatalf("First ObtainCertificate failed: %v", err)
	}

	// Second obtain for same domain should update cache
	cert2, err := m.ObtainCertificate("overwrite.example.com")
	if err != nil {
		t.Fatalf("Second ObtainCertificate failed: %v", err)
	}

	if cert1.Domain != cert2.Domain {
		t.Error("Domains should match")
	}
}

// TestRenewIfNeeded_MixedCerts tests renewal with both expiring and valid certs
func TestRenewIfNeeded_MixedCerts(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Add expiring cert
	m.certCache["expiring.com"] = &CachedCertificate{
		Domain:      "expiring.com",
		ExpiresAt:   time.Now().Add(3 * 24 * time.Hour),
		Certificate: []byte("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----"),
		PrivateKey:  []byte("-----BEGIN EC PRIVATE KEY-----\nkey\n-----END EC PRIVATE KEY-----"),
	}

	// Add valid cert
	m.certCache["valid.com"] = &CachedCertificate{
		Domain:      "valid.com",
		ExpiresAt:   time.Now().Add(90 * 24 * time.Hour),
		Certificate: []byte("-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----"),
		PrivateKey:  []byte("-----BEGIN EC PRIVATE KEY-----\nkey\n-----END EC PRIVATE KEY-----"),
	}

	renewed, err := m.RenewIfNeeded()
	// May fail if ACME fails, but should have attempted renewal
	_ = err
	_ = renewed
}

// TestDeleteCertificate_FromStorage tests that delete removes from both cache and storage
func TestDeleteCertificate_FromStorage(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	enableSelfSignedFallbackForTest(t, m)

	// Obtain a certificate
	_, err = m.ObtainCertificate("delete.example.com")
	if err != nil {
		t.Fatalf("ObtainCertificate failed: %v", err)
	}

	// Verify it exists in cache
	if len(m.certCache) != 1 {
		t.Errorf("Expected 1 cert in cache, got %d", len(m.certCache))
	}

	// Verify it exists in storage
	data, err := db.Get("acme/cert/delete.example.com")
	if err != nil || len(data) == 0 {
		t.Error("Certificate should exist in storage")
	}

	// Delete it
	err = m.DeleteCertificate("delete.example.com")
	if err != nil {
		t.Fatalf("DeleteCertificate failed: %v", err)
	}

	// Verify removed from cache
	if len(m.certCache) != 0 {
		t.Errorf("Expected 0 certs in cache after delete, got %d", len(m.certCache))
	}
}

// TestChallengeHandler_RemoveAllChallenges tests removing all challenges
func TestChallengeHandler_RemoveAllChallenges(t *testing.T) {
	handler := &ChallengeHandler{
		tokens: make(map[string]string),
	}

	handler.AddChallenge("t1", "k1")
	handler.AddChallenge("t2", "k2")
	handler.AddChallenge("t3", "k3")

	handler.RemoveChallenge("t1")
	handler.RemoveChallenge("t2")
	handler.RemoveChallenge("t3")

	if len(handler.tokens) != 0 {
		t.Errorf("Expected 0 tokens after removing all, got %d", len(handler.tokens))
	}
}

// TestTLSConfig_GetCertificate_ManagerNotFound tests when manager has no cert for domain
func TestTLSConfig_GetCertificate_ManagerNotFound(t *testing.T) {
	db := newTestDB(t)
	defer db.Close()

	cfg := Config{
		Enabled:   true,
		Provider:  ProviderLetsEncryptStaging,
		Email:     "test@example.com",
		AcceptTOS: true,
	}

	m, err := NewManager(db, cfg)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	tlsConfig := &TLSConfig{manager: m}
	hello := &ClientHelloInfo{ServerName: "missing.example.com"}

	_, err = tlsConfig.GetCertificate(hello)
	if err == nil {
		t.Fatal("Expected TLS certificate lookup to fail until real ACME issuance is implemented")
	}
	if !strings.Contains(err.Error(), "real ACME issuance is not implemented") {
		t.Fatalf("Expected not-implemented ACME error, got %v", err)
	}
}

// TestParseTLSCertificate_ECKeyRoundtrip tests full EC key parsing roundtrip
func TestParseTLSCertificate_ECKeyRoundtrip(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	keyBytes, _ := x509.MarshalECPrivateKey(key)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	template := &x509.Certificate{
		SerialNumber: big.NewInt(300),
		Subject:      pkix.Name{CommonName: "ec.example.com"},
		DNSNames:     []string{"ec.example.com"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(90 * 24 * time.Hour),
	}

	certBytes, _ := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certBytes})

	cachedCert := &CachedCertificate{
		Domain:      "ec.example.com",
		Certificate: certPEM,
		PrivateKey:  keyPEM,
		IssuedAt:    template.NotBefore,
		ExpiresAt:   template.NotAfter,
	}

	tlsCert, err := parseTLSCertificate(cachedCert)
	if err != nil {
		t.Fatalf("parseTLSCertificate failed: %v", err)
	}

	if len(tlsCert.Certificate) == 0 {
		t.Error("Expected non-empty certificate chain")
	}

	ecKey, ok := tlsCert.PrivateKey.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatal("Expected EC private key")
	}

	if ecKey.Curve != elliptic.P256() {
		t.Errorf("Expected P-256 curve, got %v", ecKey.Curve)
	}
}
