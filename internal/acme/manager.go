package acme

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

// Manager handles ACME certificate operations
// The Divine Scribe - records certificates in the sacred tablets
//
//go:generate echo "The Judgment Never Sleeps"
type Manager struct {
	storage          *storage.CobaltDB
	accountKey       crypto.PrivateKey
	accountEmail     string
	provider         Provider
	httpClient       *http.Client
	certCache        map[string]*CachedCertificate
	cacheMu          sync.RWMutex
	directoryURL     string
	certPath         string
	challengeHandler *ChallengeHandler
}

// Provider represents the ACME provider type
type Provider string

const (
	ProviderLetsEncrypt        Provider = "letsencrypt"         // Let's Encrypt production
	ProviderLetsEncryptStaging Provider = "letsencrypt_staging" // Let's Encrypt staging
	ProviderZeroSSL            Provider = "zerossl"             // ZeroSSL
	ProviderCustom             Provider = "custom"              // Custom ACME server
)

// Directory URLs for known providers
var providerDirectories = map[Provider]string{
	ProviderLetsEncrypt:        "https://acme-v02.api.letsencrypt.org/directory",
	ProviderLetsEncryptStaging: "https://acme-staging-v02.api.letsencrypt.org/directory",
}

// CachedCertificate represents a stored certificate
type CachedCertificate struct {
	Domain      string    `json:"domain" yaml:"domain"`
	Certificate []byte    `json:"certificate" yaml:"certificate"`
	PrivateKey  []byte    `json:"private_key" yaml:"private_key"`
	IssuedAt    time.Time `json:"issued_at" yaml:"issued_at"`
	ExpiresAt   time.Time `json:"expires_at" yaml:"expires_at"`
	Issuer      string    `json:"issuer" yaml:"issuer"`
}

// Config holds ACME configuration
type Config struct {
	Enabled      bool     `json:"enabled" yaml:"enabled"`
	Provider     Provider `json:"provider" yaml:"provider"`
	Email        string   `json:"email" yaml:"email"`
	AcceptTOS    bool     `json:"accept_tos" yaml:"accept_tos"` // Terms of Service acceptance
	CustomDirURL string   `json:"custom_directory_url,omitempty" yaml:"custom_directory_url,omitempty"`
	CertPath     string   `json:"cert_path" yaml:"cert_path"`
}

// ChallengeHandler handles ACME HTTP-01 challenges
type ChallengeHandler struct {
	tokens map[string]string // token -> key authorization
	mu     sync.RWMutex
}

// NewManager creates a new ACME manager
func NewManager(storage *storage.CobaltDB, config Config) (*Manager, error) {
	if !config.AcceptTOS {
		return nil, &core.ConfigError{Message: "Terms of Service must be accepted"}
	}

	directoryURL := config.CustomDirURL
	if directoryURL == "" && config.Provider != ProviderZeroSSL {
		directoryURL = providerDirectories[config.Provider]
	}
	if directoryURL == "" {
		return nil, &core.ConfigError{Message: "ACME directory URL required"}
	}

	// Load or create account key
	accountKey, err := loadOrCreateAccountKey(storage)
	if err != nil {
		return nil, fmt.Errorf("failed to load account key: %w", err)
	}

	challengeHandler := &ChallengeHandler{
		tokens: make(map[string]string),
	}

	m := &Manager{
		storage:          storage,
		accountKey:       accountKey,
		accountEmail:     config.Email,
		provider:         config.Provider,
		httpClient:       &http.Client{Timeout: 30 * time.Second},
		certCache:        make(map[string]*CachedCertificate),
		directoryURL:     directoryURL,
		certPath:         config.CertPath,
		challengeHandler: challengeHandler,
	}

	// Load cached certificates from storage
	if err := m.loadCertificates(); err != nil {
		return nil, fmt.Errorf("failed to load certificates: %w", err)
	}

	return m, nil
}

// loadOrCreateAccountKey loads or generates the ACME account private key
func loadOrCreateAccountKey(db *storage.CobaltDB) (crypto.PrivateKey, error) {
	keyData, err := db.Get("acme/account_key")
	if err == nil && len(keyData) > 0 {
		// Parse existing key
		block, _ := pem.Decode(keyData)
		if block != nil {
			key, err := x509.ParseECPrivateKey(block.Bytes)
			if err == nil {
				return key, nil
			}
		}
	}

	// Generate new key
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	// Save key
	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}

	block := &pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	}
	pemData := pem.EncodeToMemory(block)

	err = db.Put("acme/account_key", pemData)
	if err != nil {
		return nil, err
	}

	return key, nil
}

// loadCertificates loads cached certificates from storage
func (m *Manager) loadCertificates() error {
	// Get all certificate keys
	prefix := "acme/cert/"
	data, err := m.storage.PrefixScan(prefix)
	if err != nil {
		return err
	}

	for key, value := range data {
		cert, err := decodeCertificate(value)
		if err != nil {
			continue
		}

		domain := strings.TrimPrefix(key, prefix)
		m.certCache[domain] = cert
	}

	return nil
}

// encodeCertificate encodes a certificate to bytes
func encodeCertificate(cert *CachedCertificate) []byte {
	// Simple encoding: PEM cert + separator + PEM key
	result := append(cert.Certificate, []byte("\n---KEY---\n")...)
	result = append(result, cert.PrivateKey...)
	return result
}

// decodeCertificate decodes certificate from bytes
func decodeCertificate(data []byte) (*CachedCertificate, error) {
	parts := strings.Split(string(data), "\n---KEY---\n")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid certificate format")
	}

	certPEM := []byte(parts[0])
	keyPEM := []byte(parts[1])

	// Parse certificate to get metadata
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode certificate")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}

	return &CachedCertificate{
		Certificate: certPEM,
		PrivateKey:  keyPEM,
		Domain:      cert.Subject.CommonName,
		IssuedAt:    cert.NotBefore,
		ExpiresAt:   cert.NotAfter,
		Issuer:      cert.Issuer.CommonName,
	}, nil
}

// GetCertificate returns a certificate for a domain (from cache or issues new)
func (m *Manager) GetCertificate(domain string) (*CachedCertificate, error) {
	// Check cache first
	m.cacheMu.RLock()
	cert, exists := m.certCache[domain]
	m.cacheMu.RUnlock()

	if exists && cert.ExpiresAt.After(time.Now().Add(7*24*time.Hour)) {
		// Certificate exists and is valid for more than 7 days
		return cert, nil
	}

	// Need to obtain new certificate
	return m.ObtainCertificate(domain)
}

// ObtainCertificate obtains a new certificate for a domain
func (m *Manager) ObtainCertificate(domain string) (*CachedCertificate, error) {
	// Generate private key for the certificate
	certKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate certificate key: %w", err)
	}

	// Encode private key
	keyBytes, err := x509.MarshalECPrivateKey(certKey)
	if err != nil {
		return nil, err
	}

	keyBlock := &pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	}
	keyPEM := pem.EncodeToMemory(keyBlock)

	// Create CSR (Certificate Signing Request)
	csrTemplate := &x509.CertificateRequest{
		DNSNames: []string{domain},
	}

	csrBytes, err := x509.CreateCertificateRequest(rand.Reader, csrTemplate, certKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create CSR: %w", err)
	}

	// Execute ACME protocol (simplified implementation)
	certPEM, err := m.executeACMEProtocol(domain, csrBytes)
	if err != nil {
		return nil, fmt.Errorf("ACME protocol failed: %w", err)
	}

	// Parse certificate to get expiry
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, fmt.Errorf("failed to decode obtained certificate")
	}

	parsedCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}

	cert := &CachedCertificate{
		Domain:      domain,
		Certificate: certPEM,
		PrivateKey:  keyPEM,
		IssuedAt:    parsedCert.NotBefore,
		ExpiresAt:   parsedCert.NotAfter,
		Issuer:      parsedCert.Issuer.CommonName,
	}

	// Save to cache
	m.cacheMu.Lock()
	m.certCache[domain] = cert
	m.cacheMu.Unlock()

	// Persist to storage
	certData := encodeCertificate(cert)
	key := "acme/cert/" + domain
	if err := m.storage.Put(key, certData); err != nil {
		return nil, fmt.Errorf("failed to save certificate: %w", err)
	}

	return cert, nil
}

// executeACMEProtocol executes the ACME protocol for certificate issuance
// This is a simplified implementation focusing on HTTP-01 challenge
func (m *Manager) executeACMEProtocol(domain string, csr []byte) ([]byte, error) {
	// Note: Full ACME implementation is complex and would require
	// implementing the full RFC 8555 protocol. For production use,
	// consider using golang.org/x/crypto/acme/autocert

	// This is a placeholder that returns an error indicating
	// the full ACME implementation should be used
	return nil, fmt.Errorf("ACME protocol execution requires golang.org/x/crypto/acme/autocert integration")
}

// ChallengeHandler returns the HTTP handler for ACME challenges
func (m *Manager) ChallengeHandler() http.Handler {
	return m.challengeHandler
}

// HandleChallenge handles ACME challenge requests
func (ch *ChallengeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract token from path: /.well-known/acme-challenge/<token>
	token := path.Base(r.URL.Path)
	if token == "" || token == "." {
		http.NotFound(w, r)
		return
	}

	ch.mu.RLock()
	keyAuth, exists := ch.tokens[token]
	ch.mu.RUnlock()

	if !exists {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(keyAuth))
}

// AddChallenge adds a challenge token for validation
func (ch *ChallengeHandler) AddChallenge(token, keyAuth string) {
	ch.mu.Lock()
	ch.tokens[token] = keyAuth
	ch.mu.Unlock()
}

// RemoveChallenge removes a challenge token
func (ch *ChallengeHandler) RemoveChallenge(token string) {
	ch.mu.Lock()
	delete(ch.tokens, token)
	ch.mu.Unlock()
}

// RenewIfNeeded checks and renews certificates that are expiring soon
func (m *Manager) RenewIfNeeded() ([]string, error) {
	renewed := []string{}
	threshold := time.Now().Add(7 * 24 * time.Hour) // Renew 7 days before expiry

	m.cacheMu.RLock()
	certs := make(map[string]*CachedCertificate)
	for domain, cert := range m.certCache {
		certs[domain] = cert
	}
	m.cacheMu.RUnlock()

	for domain, cert := range certs {
		if cert.ExpiresAt.Before(threshold) {
			if _, err := m.ObtainCertificate(domain); err == nil {
				renewed = append(renewed, domain)
			}
		}
	}

	return renewed, nil
}

// DeleteCertificate removes a certificate from cache and storage
func (m *Manager) DeleteCertificate(domain string) error {
	m.cacheMu.Lock()
	delete(m.certCache, domain)
	m.cacheMu.Unlock()

	key := "acme/cert/" + domain
	return m.storage.Delete(key)
}

// GetAllDomains returns all domains with certificates
func (m *Manager) GetAllDomains() []string {
	m.cacheMu.RLock()
	domains := make([]string, 0, len(m.certCache))
	for domain := range m.certCache {
		domains = append(domains, domain)
	}
	m.cacheMu.RUnlock()
	return domains
}

// CertificateInfo returns certificate information for a domain
func (m *Manager) CertificateInfo(domain string) (*CachedCertificate, error) {
	m.cacheMu.RLock()
	cert, exists := m.certCache[domain]
	m.cacheMu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("certificate not found for domain: %s", domain)
	}

	return cert, nil
}

// TLSConfig creates a TLS configuration using ACME certificates
func (m *Manager) TLSConfig() *TLSConfig {
	return &TLSConfig{
		manager: m,
	}
}

// TLSConfig wraps the ACME manager for TLS certificate retrieval
type TLSConfig struct {
	manager *Manager
}

// GetCertificate implements the tls.Config.GetCertificate callback
func (tc *TLSConfig) GetCertificate(hello *ClientHelloInfo) (*Certificate, error) {
	if hello == nil || hello.ServerName == "" {
		return nil, fmt.Errorf("no SNI provided")
	}

	cert, err := tc.manager.GetCertificate(hello.ServerName)
	if err != nil {
		return nil, err
	}

	// Parse certificate for TLS
	tlsCert, err := parseTLSCertificate(cert)
	if err != nil {
		return nil, err
	}

	return tlsCert, nil
}

// Minimal types for TLS integration (to avoid crypto/tls import issues)
type ClientHelloInfo struct {
	ServerName string
}

type Certificate struct {
	Certificate [][]byte
	PrivateKey  crypto.PrivateKey
}

// parseTLSCertificate converts cached certificate to TLS certificate format
func parseTLSCertificate(cert *CachedCertificate) (*Certificate, error) {
	// Parse certificate chain
	var certs [][]byte
	block, rest := pem.Decode(cert.Certificate)
	for block != nil {
		certs = append(certs, block.Bytes)
		block, rest = pem.Decode(rest)
	}

	if len(certs) == 0 {
		return nil, fmt.Errorf("no certificates found")
	}

	// Parse private key
	keyBlock, _ := pem.Decode(cert.PrivateKey)
	if keyBlock == nil {
		return nil, fmt.Errorf("failed to decode private key")
	}

	var privateKey crypto.PrivateKey
	if keyBlock.Type == "EC PRIVATE KEY" {
		key, err := x509.ParseECPrivateKey(keyBlock.Bytes)
		if err != nil {
			return nil, err
		}
		privateKey = key
	} else {
		return nil, fmt.Errorf("unsupported key type: %s", keyBlock.Type)
	}

	return &Certificate{
		Certificate: certs,
		PrivateKey:  privateKey,
	}, nil
}
