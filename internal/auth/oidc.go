package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// OIDCAuthenticator implements OIDC authentication with local fallback
type OIDCAuthenticator struct {
	mu        sync.RWMutex
	config    core.OIDCAuth
	local     *LocalAuthenticator
	state     map[string]*oidcState // CSRF state tracking
	users     map[string]*api.User  // email -> user
	tokens    map[string]*session   // token -> session
	stopCh    chan struct{}
	stopOnce  sync.Once
	stateHMAC []byte // HMAC key for signing state (cluster-compatible)

	// JWK cache
	jwksMu      sync.RWMutex
	jwks        *jwkSet
	jwksFetched time.Time
	jwksTTL     time.Duration
}

type oidcState struct {
	Email     string    `json:"email"`
	Nonce     string    `json:"nonce"` // CSRF protection: binds state to session
	ExpiresAt time.Time `json:"expires_at"`
}

type oidcConfig struct {
	Issuer      string   `json:"issuer"`
	AuthURL     string   `json:"authorization_endpoint"`
	TokenURL    string   `json:"token_endpoint"`
	UserInfoURL string   `json:"userinfo_endpoint"`
	JWKSURI     string   `json:"jwks_uri"`
	Scopes      []string `json:"scopes_supported"`
}

// JWK types for signature verification
type jwkSet struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	N   string `json:"n"`   // RSA modulus
	E   string `json:"e"`   // RSA exponent
	Crv string `json:"crv"` // EC curve
	X   string `json:"x"`   // EC x-coordinate
	Y   string `json:"y"`   // EC y-coordinate
}

// cryptoKey represents a parsed public key ready for verification
type cryptoKey struct {
	key crypto.PublicKey
	alg string // RS256, ES256, etc.
	kid string
}

// base64url decode helper (handles both padded and unpadded)
func base64Decode(s string) ([]byte, error) {
	// RawURLEncoding handles unpadded input natively
	return base64.RawURLEncoding.DecodeString(s)
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

type userInfoResponse struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	Name          string `json:"name"`
	EmailVerified bool   `json:"email_verified"`
}

const oidcMaxResponseBody = 1 << 20 // 1 MiB

func normalizedIssuer(raw string) (*url.URL, string, error) {
	issuer, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, "", fmt.Errorf("invalid OIDC issuer: %w", err)
	}
	if issuer.Host == "" || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" {
		return nil, "", fmt.Errorf("invalid OIDC issuer URL")
	}
	if issuer.Scheme != "https" && !(issuer.Scheme == "http" && issuerLoopback(issuer)) {
		return nil, "", fmt.Errorf("OIDC issuer must use HTTPS (HTTP is only allowed for loopback)")
	}
	issuer.Path = strings.TrimRight(issuer.Path, "/")
	issuer.RawPath = ""
	return issuer, issuer.String(), nil
}

func issuerLoopback(u *url.URL) bool {
	host := strings.ToLower(u.Hostname())
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if u.Scheme == "https" {
		return "443"
	}
	if u.Scheme == "http" {
		return "80"
	}
	return ""
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) &&
		strings.EqualFold(a.Hostname(), b.Hostname()) &&
		effectivePort(a) == effectivePort(b)
}

func validateOIDCEndpoint(raw, name string, issuer *url.URL) (*url.URL, error) {
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" {
		return nil, fmt.Errorf("invalid OIDC %s endpoint", name)
	}
	if endpoint.Scheme != "https" {
		// Plain HTTP is a development-only exception and must stay on a loopback
		// origin. OIDC permits legitimate endpoint origins to differ from the
		// issuer, so production trust is pinned by exact issuer metadata plus TLS.
		if issuer.Scheme != "http" || !issuerLoopback(issuer) || endpoint.Scheme != "http" || !issuerLoopback(endpoint) {
			return nil, fmt.Errorf("OIDC %s endpoint must use HTTPS", name)
		}
	}
	return endpoint, nil
}

func oidcHTTPClient(trustedOrigin *url.URL) *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many OIDC redirects")
			}
			if req.URL.Scheme != "https" && !(trustedOrigin.Scheme == "http" && issuerLoopback(trustedOrigin) && req.URL.Scheme == "http" && issuerLoopback(req.URL)) {
				return fmt.Errorf("OIDC redirect must use HTTPS")
			}
			if !sameOrigin(trustedOrigin, req.URL) {
				return fmt.Errorf("OIDC redirect changed trusted origin")
			}
			return nil
		},
	}
}

func decodeOIDCJSON(body io.Reader, dst interface{}) error {
	data, err := io.ReadAll(io.LimitReader(body, oidcMaxResponseBody+1))
	if err != nil {
		return err
	}
	if len(data) > oidcMaxResponseBody {
		return fmt.Errorf("OIDC response body exceeds %d bytes", oidcMaxResponseBody)
	}
	return json.Unmarshal(data, dst)
}

// NewOIDCAuthenticator creates a new OIDC authenticator with local fallback.
// Returns an error if the embedded local authenticator or HMAC key
// generation fails (K6: previously panicked).
func NewOIDCAuthenticator(cfg core.OIDCAuth, localPath, adminEmail, adminPassword string) (*OIDCAuthenticator, error) {
	localAuth, err := NewLocalAuthenticator(localPath, adminEmail, adminPassword)
	if err != nil {
		return nil, fmt.Errorf("local authenticator init: %w", err)
	}
	// Generate random HMAC key for state signing (cluster-compatible).
	// A failure here is fatal — without a per-process HMAC key, OIDC
	// state tokens are forgeable, which is a security regression. We
	// still return an error rather than panicking so the caller can
	// decide whether to retry, fail startup, or run in degraded mode.
	stateHMAC := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, stateHMAC); err != nil {
		return nil, fmt.Errorf("failed to generate state HMAC key: %w", err)
	}
	return &OIDCAuthenticator{
		config:    cfg,
		local:     localAuth,
		state:     make(map[string]*oidcState),
		users:     make(map[string]*api.User),
		tokens:    make(map[string]*session),
		stopCh:    make(chan struct{}),
		stateHMAC: stateHMAC,
		jwksTTL:   24 * time.Hour, // Cache JWKs for 24 hours
	}, nil
}

// signState creates an HMAC-signed state token for cluster compatibility.
// Format: state.hmac_hex(state)
func (o *OIDCAuthenticator) signState(state string) string {
	mac := hmac.New(sha256.New, o.stateHMAC)
	mac.Write([]byte(state))
	return fmt.Sprintf("%s.%x", state, mac.Sum(nil))
}

// verifyState checks that the state parameter has a valid HMAC signature.
// Returns the raw state (without the HMAC suffix) or an error.
func (o *OIDCAuthenticator) verifyState(signedState string) (string, error) {
	parts := strings.SplitN(signedState, ".", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid state format: missing HMAC signature")
	}
	rawState, expectedMAC := parts[0], parts[1]

	mac := hmac.New(sha256.New, o.stateHMAC)
	mac.Write([]byte(rawState))
	actualMAC := fmt.Sprintf("%x", mac.Sum(nil))

	if !hmac.Equal([]byte(actualMAC), []byte(expectedMAC)) {
		return "", fmt.Errorf("invalid state signature")
	}
	return rawState, nil
}

// OIDCLoginURL returns the OIDC authorization URL for redirect
func (o *OIDCAuthenticator) OIDCLoginURL() (string, string, string, error) {
	// Fetch OIDC configuration from issuer
	cfg, err := o.fetchOIDCConfig()
	if err != nil {
		return "", "", "", fmt.Errorf("failed to fetch OIDC config: %w", err)
	}

	// Generate CSRF state and nonce
	state, err := generateToken()
	if err != nil {
		return "", "", "", err
	}
	nonce, err := generateToken() // Used to bind state to session (CSRF protection)
	if err != nil {
		return "", "", "", err
	}
	expiresAt := time.Now().Add(10 * time.Minute)

	o.mu.Lock()
	o.state[state] = &oidcState{
		Nonce:     nonce,
		ExpiresAt: expiresAt,
	}
	o.mu.Unlock()

	// Sign state for cluster compatibility
	signedState := o.signState(state)

	// Build authorization URL
	redirectURL := cfg.AuthURL + "?" +
		"response_type=code" +
		"&client_id=" + o.config.ClientID +
		"&redirect_uri=" + o.config.RedirectURL +
		"&scope=openid+profile+email" +
		"&state=" + url.QueryEscape(signedState)

	return redirectURL, signedState, nonce, nil
}

// OIDCCallback handles the OIDC callback
func (o *OIDCAuthenticator) OIDCCallback(code, state, nonce string) (*api.User, string, error) {
	// Verify HMAC signature on state (cluster-compatible)
	rawState, err := o.verifyState(state)
	if err != nil {
		slog.Warn("OIDC callback state HMAC verification failed", "error", err)
		return nil, "", fmt.Errorf("invalid state: %w", err)
	}

	// Verify state (CSRF protection)
	o.mu.Lock()
	csrfState, exists := o.state[rawState]
	if !exists {
		o.mu.Unlock()
		return nil, "", fmt.Errorf("invalid state")
	}
	if time.Now().After(csrfState.ExpiresAt) {
		delete(o.state, rawState)
		o.mu.Unlock()
		return nil, "", fmt.Errorf("state expired")
	}
	// Verify nonce matches (binds callback to original session)
	expectedNonce := csrfState.Nonce
	if csrfState.Nonce != nonce {
		delete(o.state, rawState)
		o.mu.Unlock()
		slog.Warn("OIDC callback nonce mismatch - possible CSRF attack", "state", rawState)
		return nil, "", fmt.Errorf("invalid nonce: possible CSRF attack")
	}
	delete(o.state, rawState)
	o.mu.Unlock()

	// Exchange code for token
	tokenResp, err := o.exchangeCode(code)
	if err != nil {
		return nil, "", fmt.Errorf("failed to exchange code: %w", err)
	}

	// Verify ID token signature and claims (always, even if we also fetch userinfo)
	userInfo, err := o.parseIDToken(tokenResp.IDToken, expectedNonce)
	if err != nil {
		return nil, "", fmt.Errorf("invalid ID token: %w", err)
	}

	// Optionally refresh from userinfo endpoint if available
	if info, err := o.getUserInfo(tokenResp.AccessToken); err == nil && info.Email != "" {
		if userInfo.Email == "" || info.EmailVerified {
			userInfo = info
		}
	}

	if userInfo.Email == "" {
		return nil, "", fmt.Errorf("no email in OIDC response")
	}

	// Create or find user
	o.mu.Lock()
	user, exists := o.users[userInfo.Email]
	if !exists {
		newID, err := generateID()
		if err != nil {
			o.mu.Unlock()
			return nil, "", err
		}
		user = &api.User{
			ID:        newID,
			Email:     userInfo.Email,
			Name:      userInfo.Name,
			Role:      "viewer", // Default role for OIDC users
			Workspace: "default",
			CreatedAt: time.Now(),
		}
		o.users[user.ID] = user
		o.users[user.Email] = user // Index by email for lookup
	}
	o.mu.Unlock()

	// Generate session token
	token, err := generateToken()
	if err != nil {
		return nil, "", err
	}
	o.mu.Lock()
	o.tokens[token] = &session{
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	o.mu.Unlock()

	return user, token, nil
}

// fetchOIDCConfig fetches and validates the OIDC discovery document.
func (o *OIDCAuthenticator) fetchOIDCConfig() (*oidcConfig, error) {
	issuerURL, issuer, err := normalizedIssuer(o.config.Issuer)
	if err != nil {
		return nil, err
	}
	wellKnownURL := issuer + "/.well-known/openid-configuration"

	resp, err := oidcHTTPClient(issuerURL).Get(wellKnownURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch OIDC config from %s: %w", wellKnownURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OIDC config endpoint returned %d", resp.StatusCode)
	}

	var cfg oidcConfig
	if err := decodeOIDCJSON(resp.Body, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse OIDC config: %w", err)
	}
	returnedIssuer, normalizedReturnedIssuer, err := normalizedIssuer(cfg.Issuer)
	if err != nil || normalizedReturnedIssuer != issuer {
		return nil, fmt.Errorf("OIDC discovery issuer does not match configured issuer")
	}
	if cfg.AuthURL == "" || cfg.TokenURL == "" {
		return nil, fmt.Errorf("OIDC config missing authorization or token endpoint")
	}

	endpoints := []struct {
		raw      string
		name     string
		required bool
	}{
		{cfg.AuthURL, "authorization", true},
		{cfg.TokenURL, "token", true},
		{cfg.UserInfoURL, "userinfo", false},
		{cfg.JWKSURI, "JWKS", false},
	}
	for _, endpoint := range endpoints {
		if endpoint.raw == "" && !endpoint.required {
			continue
		}
		if _, err := validateOIDCEndpoint(endpoint.raw, endpoint.name, returnedIssuer); err != nil {
			return nil, err
		}
	}

	return &cfg, nil
}

// exchangeCode exchanges an authorization code for tokens
func (o *OIDCAuthenticator) exchangeCode(code string) (*tokenResponse, error) {
	cfg, err := o.fetchOIDCConfig()
	if err != nil {
		return nil, err
	}

	// Build token request with proper URL encoding
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", o.config.RedirectURL)
	data.Set("client_id", o.config.ClientID)
	data.Set("client_secret", o.config.ClientSecret)

	issuerURL, _, err := normalizedIssuer(o.config.Issuer)
	if err != nil {
		return nil, err
	}
	tokenURL, err := validateOIDCEndpoint(cfg.TokenURL, "token", issuerURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, cfg.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := oidcHTTPClient(tokenURL).Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp tokenResponse
	if err := decodeOIDCJSON(resp.Body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	return &tokenResp, nil
}

// getUserInfo fetches user info from OIDC provider
func (o *OIDCAuthenticator) getUserInfo(accessToken string) (*userInfoResponse, error) {
	cfg, err := o.fetchOIDCConfig()
	if err != nil {
		return nil, err
	}

	if cfg.UserInfoURL == "" {
		return nil, fmt.Errorf("no userinfo endpoint in OIDC config")
	}

	req, err := http.NewRequest("GET", cfg.UserInfoURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	issuerURL, _, err := normalizedIssuer(o.config.Issuer)
	if err != nil {
		return nil, err
	}
	userInfoURL, err := validateOIDCEndpoint(cfg.UserInfoURL, "userinfo", issuerURL)
	if err != nil {
		return nil, err
	}
	resp, err := oidcHTTPClient(userInfoURL).Do(req)
	if err != nil {
		return nil, fmt.Errorf("userinfo request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo endpoint returned %d", resp.StatusCode)
	}

	var userInfo userInfoResponse
	if err := decodeOIDCJSON(resp.Body, &userInfo); err != nil {
		return nil, fmt.Errorf("failed to parse userinfo: %w", err)
	}

	return &userInfo, nil
}

// fetchJWKs fetches the JWK set from the OIDC provider
func (o *OIDCAuthenticator) fetchJWKs() (*jwkSet, error) {
	o.jwksMu.RLock()
	if o.jwks != nil && time.Since(o.jwksFetched) < o.jwksTTL {
		jwks := o.jwks
		o.jwksMu.RUnlock()
		return jwks, nil
	}
	o.jwksMu.RUnlock()

	o.jwksMu.Lock()
	defer o.jwksMu.Unlock()

	// Double-check after write lock
	if o.jwks != nil && time.Since(o.jwksFetched) < o.jwksTTL {
		return o.jwks, nil
	}

	cfg, err := o.fetchOIDCConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch OIDC config for JWKs: %w", err)
	}
	if cfg.JWKSURI == "" {
		return nil, fmt.Errorf("OIDC config missing jwks_uri")
	}

	issuerURL, _, err := normalizedIssuer(o.config.Issuer)
	if err != nil {
		return nil, err
	}
	jwksURL, err := validateOIDCEndpoint(cfg.JWKSURI, "JWKS", issuerURL)
	if err != nil {
		return nil, err
	}
	resp, err := oidcHTTPClient(jwksURL).Get(cfg.JWKSURI)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKs from %s: %w", cfg.JWKSURI, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKs endpoint returned %d", resp.StatusCode)
	}

	var jwks jwkSet
	if err := decodeOIDCJSON(resp.Body, &jwks); err != nil {
		return nil, fmt.Errorf("failed to parse JWK set: %w", err)
	}

	if len(jwks.Keys) == 0 {
		return nil, fmt.Errorf("JWK set is empty")
	}

	o.jwks = &jwks
	o.jwksFetched = time.Now()
	slog.Info("JWK set refreshed", "jwks_uri", cfg.JWKSURI, "key_count", len(jwks.Keys))
	return &jwks, nil
}

// findKeyForJWT finds the matching public key for a JWT based on the kid header
func (o *OIDCAuthenticator) findKeyForJWT(headers map[string]interface{}) (*cryptoKey, error) {
	kid, _ := headers["kid"].(string)

	// Fetch all JWKs (from cache or network)
	jwks, err := o.fetchJWKs()
	if err != nil {
		return nil, err
	}

	for _, jwk := range jwks.Keys {
		// Match by kid if present in JWT
		if kid != "" && jwk.Kid != kid {
			continue
		}

		key, err := jwk.toPublicKey()
		if err != nil {
			continue
		}
		return key, nil
	}

	if kid != "" {
		return nil, fmt.Errorf("no matching key found for kid=%s", kid)
	}
	// If no kid in JWT, return first usable key
	for _, jwk := range jwks.Keys {
		key, err := jwk.toPublicKey()
		if err == nil {
			return key, nil
		}
	}
	return nil, fmt.Errorf("no usable keys found in JWK set")
}

// toPublicKey converts a JWK to a crypto.PublicKey
func (j *jwk) toPublicKey() (*cryptoKey, error) {
	switch j.Kty {
	case "RSA":
		return j.toRSAPublicKey()
	case "EC":
		return j.toECPublicKey()
	default:
		return nil, fmt.Errorf("unsupported key type: %s", j.Kty)
	}
}

func (j *jwk) toRSAPublicKey() (*cryptoKey, error) {
	nBytes, err := base64Decode(j.N)
	if err != nil {
		return nil, fmt.Errorf("failed to decode RSA modulus: %w", err)
	}
	eBytes, err := base64Decode(j.E)
	if err != nil {
		return nil, fmt.Errorf("failed to decode RSA exponent: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := int(new(big.Int).SetBytes(eBytes).Uint64())

	pubKey := &rsa.PublicKey{N: n, E: e}
	alg := j.Alg
	if alg == "" {
		alg = "RS256" // Default
	}
	return &cryptoKey{key: pubKey, alg: alg, kid: j.Kid}, nil
}

func (j *jwk) toECPublicKey() (*cryptoKey, error) {
	xBytes, err := base64Decode(j.X)
	if err != nil {
		return nil, fmt.Errorf("failed to decode EC X: %w", err)
	}
	yBytes, err := base64Decode(j.Y)
	if err != nil {
		return nil, fmt.Errorf("failed to decode EC Y: %w", err)
	}

	var curve elliptic.Curve
	switch j.Crv {
	case "P-256":
		curve = elliptic.P256()
	case "P-384":
		curve = elliptic.P384()
	case "P-521":
		curve = elliptic.P521()
	default:
		return nil, fmt.Errorf("unsupported EC curve: %s", j.Crv)
	}

	pubKey := &ecdsa.PublicKey{Curve: curve, X: new(big.Int).SetBytes(xBytes), Y: new(big.Int).SetBytes(yBytes)}
	alg := j.Alg
	if alg == "" {
		switch j.Crv {
		case "P-256":
			alg = "ES256"
		case "P-384":
			alg = "ES384"
		case "P-521":
			alg = "ES512"
		}
	}
	return &cryptoKey{key: pubKey, alg: alg, kid: j.Kid}, nil
}

// verifyJWTSignature verifies the JWT signature using the appropriate JWK
func (o *OIDCAuthenticator) verifyJWTSignature(idToken string) (map[string]interface{}, error) {
	if idToken == "" {
		return nil, fmt.Errorf("empty ID token")
	}

	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT format: expected 3 parts, got %d", len(parts))
	}

	// Decode header
	headerBytes, err := base64Decode(parts[0])
	if err != nil {
		return nil, fmt.Errorf("failed to decode JWT header: %w", err)
	}
	var headers map[string]interface{}
	if err := json.Unmarshal(headerBytes, &headers); err != nil {
		return nil, fmt.Errorf("failed to parse JWT header: %w", err)
	}

	// Validate signing algorithm - reject "none" and only allow known algorithms
	if alg, ok := headers["alg"].(string); ok {
		switch alg {
		case "RS256", "RS384", "RS512", "ES256", "ES384", "ES512":
			// Allowed asymmetric algorithms
		case "none", "":
			return nil, fmt.Errorf("JWT signing algorithm %q not allowed: asymmetric signature required", alg)
		default:
			return nil, fmt.Errorf("unsupported JWT signing algorithm: %s", alg)
		}
	}

	// Find the matching key
	cryptoKey, err := o.findKeyForJWT(headers)
	if err != nil {
		return nil, fmt.Errorf("failed to find signing key: %w", err)
	}

	// Verify signature
	signingInput := []byte(parts[0] + "." + parts[1])
	sigBytes, err := base64Decode(parts[2])
	if err != nil {
		return nil, fmt.Errorf("failed to decode JWT signature: %w", err)
	}

	switch cryptoKey.alg {
	case "RS256":
		hash := sha256.Sum256(signingInput)
		rsaKey, ok := cryptoKey.key.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("key type mismatch for RS256")
		}
		if err := rsa.VerifyPKCS1v15(rsaKey, crypto.SHA256, hash[:], sigBytes); err != nil {
			return nil, fmt.Errorf("JWT signature verification failed (RS256): %w", err)
		}
	case "ES256":
		hash := sha256.Sum256(signingInput)
		ecKey, ok := cryptoKey.key.(*ecdsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("key type mismatch for ES256")
		}
		keyLen := ecKey.Params().BitSize / 8
		if len(sigBytes) != 2*keyLen {
			return nil, fmt.Errorf("invalid ECDSA signature length: expected %d, got %d", 2*keyLen, len(sigBytes))
		}
		r := new(big.Int).SetBytes(sigBytes[:keyLen])
		s := new(big.Int).SetBytes(sigBytes[keyLen:])
		if !ecdsa.Verify(ecKey, hash[:], r, s) {
			return nil, fmt.Errorf("JWT signature verification failed (ES256)")
		}
	default:
		return nil, fmt.Errorf("unsupported JWT signing algorithm: %s", cryptoKey.alg)
	}

	// Decode and return payload
	payloadBytes, err := base64Decode(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode JWT payload: %w", err)
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, fmt.Errorf("failed to parse JWT claims JSON: %w", err)
	}

	return claims, nil
}

// parseIDToken parses a JWT ID token WITH cryptographic signature verification
func (o *OIDCAuthenticator) parseIDToken(idToken string, expectedNonce string) (*userInfoResponse, error) {
	// Verify signature and get claims
	claims, err := o.verifyJWTSignature(idToken)
	if err != nil {
		return nil, err
	}

	// Validate required issuer claim
	iss, ok := claims["iss"].(string)
	if !ok || iss == "" {
		return nil, fmt.Errorf("missing or invalid issuer claim")
	}
	// Normalize both sides: providers may or may not include a trailing slash
	iss = strings.TrimSuffix(iss, "/")
	issuer := strings.TrimSuffix(o.config.Issuer, "/")
	if iss != issuer {
		return nil, fmt.Errorf("issuer mismatch: expected %s, got %s", issuer, iss)
	}

	// Validate required audience claim
	multipleAudiences := false
	if aud, ok := claims["aud"].(string); ok && aud != "" {
		if aud != o.config.ClientID {
			return nil, fmt.Errorf("audience mismatch: expected %s, got %s", o.config.ClientID, aud)
		}
	} else if audList, ok := claims["aud"].([]interface{}); ok {
		found := false
		for _, a := range audList {
			if s, ok := a.(string); ok && s == o.config.ClientID {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("audience not in list: expected %s", o.config.ClientID)
		}
		multipleAudiences = len(audList) > 1
	} else {
		return nil, fmt.Errorf("missing audience claim")
	}

	// Validate the authorized party (azp). Per OIDC Core §3.1.3.7, azp MUST be
	// present when the ID token has multiple audiences, and when present it MUST
	// equal the client ID. Without this, a token minted for a different client
	// that merely lists our client ID in its aud array would be accepted.
	azp, hasAZP := claims["azp"].(string)
	if hasAZP && azp != o.config.ClientID {
		return nil, fmt.Errorf("azp mismatch: expected %s, got %s", o.config.ClientID, azp)
	}
	if multipleAudiences && !hasAZP {
		return nil, fmt.Errorf("azp claim required when ID token has multiple audiences")
	}

	// Validate required subject claim
	sub, ok := claims["sub"].(string)
	if !ok || sub == "" {
		return nil, fmt.Errorf("missing subject claim")
	}

	// Validate nonce if expected
	if expectedNonce != "" {
		if nonce, ok := claims["nonce"].(string); !ok || nonce != expectedNonce {
			return nil, fmt.Errorf("nonce claim mismatch or missing")
		}
	}

	// Validate expiration
	if exp, ok := claims["exp"].(float64); ok {
		if time.Now().After(time.Unix(int64(exp), 0)) {
			return nil, fmt.Errorf("ID token expired")
		}
	}

	// Validate not-before
	if nbf, ok := claims["nbf"].(float64); ok {
		if time.Now().Before(time.Unix(int64(nbf), 0)) {
			return nil, fmt.Errorf("ID token not yet valid (nbf in future)")
		}
	}

	userInfo := &userInfoResponse{
		Sub: sub, // Already validated above
	}
	if verified, ok := claims["email_verified"].(bool); ok {
		userInfo.EmailVerified = verified
	}
	if email, ok := claims["email"].(string); ok && email != "" {
		// The account is provisioned/looked up by email, so an unverified
		// email must not establish identity: an IdP that permits unverified
		// addresses could otherwise let a user assert a victim's email. Reject
		// only when the provider explicitly marks it unverified (absent claim
		// is left to the provider's policy to avoid breaking IdPs that omit it).
		if v, present := claims["email_verified"].(bool); present && !v {
			return nil, fmt.Errorf("email is not verified by the identity provider")
		}
		userInfo.Email = email
	}
	if name, ok := claims["name"].(string); ok {
		userInfo.Name = name
	}

	return userInfo, nil
}

// Authenticate validates a token and returns the user
func (o *OIDCAuthenticator) Authenticate(token string) (*api.User, error) {
	// Check OIDC tokens first
	o.mu.RLock()
	sess, ok := o.tokens[token]
	if ok {
		if time.Now().After(sess.ExpiresAt) {
			o.mu.RUnlock()
			return nil, fmt.Errorf("token expired")
		}
		user := o.users[sess.UserID]
		o.mu.RUnlock()
		if user == nil {
			return nil, fmt.Errorf("user not found")
		}
		return user, nil
	}
	o.mu.RUnlock()

	// Fallback to local auth
	return o.local.Authenticate(token)
}

// Login validates credentials and creates a session (local fallback)
func (o *OIDCAuthenticator) Login(email, password string) (*api.User, string, error) {
	return o.local.Login(email, password)
}

// Logout invalidates a token
func (o *OIDCAuthenticator) Logout(token string) error {
	o.mu.Lock()
	delete(o.tokens, token)
	o.mu.Unlock()
	return o.local.Logout(token)
}

// SwitchWorkspace updates the active workspace for OIDC-backed sessions and
// delegates local fallback sessions to the local authenticator.
func (o *OIDCAuthenticator) SwitchWorkspace(token, workspace string) (*api.User, error) {
	if workspace == "" {
		return nil, fmt.Errorf("workspace is required")
	}

	o.mu.Lock()
	sess, ok := o.tokens[token]
	if ok {
		if time.Now().After(sess.ExpiresAt) {
			delete(o.tokens, token)
			o.mu.Unlock()
			return nil, fmt.Errorf("token expired")
		}
		user := o.users[sess.UserID]
		if user == nil {
			o.mu.Unlock()
			return nil, fmt.Errorf("user not found")
		}
		user.Workspace = workspace
		o.mu.Unlock()
		return user, nil
	}
	o.mu.Unlock()

	return o.local.SwitchWorkspace(token, workspace)
}

// Shutdown gracefully stops the authenticator. Idempotent — t.Cleanup
// hooks plus deferred Shutdown() calls in tests can both fire safely.
func (o *OIDCAuthenticator) Shutdown() {
	o.local.Shutdown()
	o.stopOnce.Do(func() { close(o.stopCh) })
}

// ChangePassword delegates to local authenticator (HIGH-04)
func (o *OIDCAuthenticator) ChangePassword(token, currentPassword, newPassword string) error {
	return o.local.ChangePassword(token, currentPassword, newPassword)
}

// RequestPasswordReset delegates to local authenticator (HIGH-04)
func (o *OIDCAuthenticator) RequestPasswordReset(email string) (string, error) {
	return o.local.RequestPasswordReset(email)
}

// ConfirmPasswordReset delegates to local authenticator (HIGH-04)
func (o *OIDCAuthenticator) ConfirmPasswordReset(token, newPassword string) error {
	return o.local.ConfirmPasswordReset(token, newPassword)
}

// GetUsers returns all OIDC users
func (o *OIDCAuthenticator) GetUsers() []*api.User {
	o.mu.RLock()
	defer o.mu.RUnlock()

	users := make([]*api.User, 0, len(o.users))
	for _, user := range o.users {
		users = append(users, user)
	}
	return users
}

// AddUser adds a user (for admin management). Returns the new user or
// an error if the system CSPRNG is unavailable (K6: previously panicked).
func (o *OIDCAuthenticator) AddUser(email, name, role string) (*api.User, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	newID, err := generateID()
	if err != nil {
		return nil, err
	}
	user := &api.User{
		ID:        newID,
		Email:     email,
		Name:      name,
		Role:      role,
		Workspace: "default",
		CreatedAt: time.Now(),
	}
	o.users[user.ID] = user
	o.users[user.Email] = user
	return user, nil
}
