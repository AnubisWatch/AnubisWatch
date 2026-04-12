package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// OIDCAuthenticator implements OIDC authentication with local fallback
type OIDCAuthenticator struct {
	mu     sync.RWMutex
	config core.OIDCAuth
	local  *LocalAuthenticator
	state  map[string]*oidcState // CSRF state tracking
	users  map[string]*api.User  // email -> user
	tokens map[string]*session   // token -> session
	stopCh chan struct{}

	// JWK cache
	jwksMu      sync.RWMutex
	jwks        *jwkSet
	jwksFetched time.Time
	jwksTTL     time.Duration
}

type oidcState struct {
	Email     string    `json:"email"`
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

// NewOIDCAuthenticator creates a new OIDC authenticator with local fallback
func NewOIDCAuthenticator(cfg core.OIDCAuth, localPath, adminEmail, adminPassword string) *OIDCAuthenticator {
	localAuth := NewLocalAuthenticator(localPath, adminEmail, adminPassword)
	return &OIDCAuthenticator{
		config:  cfg,
		local:   localAuth,
		state:   make(map[string]*oidcState),
		users:   make(map[string]*api.User),
		tokens:  make(map[string]*session),
		stopCh:  make(chan struct{}),
		jwksTTL: 24 * time.Hour, // Cache JWKs for 24 hours
	}
}

// OIDCLoginURL returns the OIDC authorization URL for redirect
func (o *OIDCAuthenticator) OIDCLoginURL() (string, string, error) {
	// Fetch OIDC configuration from issuer
	cfg, err := o.fetchOIDCConfig()
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch OIDC config: %w", err)
	}

	// Generate CSRF state
	state := generateToken()
	expiresAt := time.Now().Add(10 * time.Minute)

	o.mu.Lock()
	o.state[state] = &oidcState{ExpiresAt: expiresAt}
	o.mu.Unlock()

	// Build authorization URL
	redirectURL := cfg.AuthURL + "?" +
		"response_type=code" +
		"&client_id=" + o.config.ClientID +
		"&redirect_uri=" + o.config.RedirectURL +
		"&scope=openid+profile+email" +
		"&state=" + state

	return redirectURL, state, nil
}

// OIDCCallback handles the OIDC callback
func (o *OIDCAuthenticator) OIDCCallback(code, state string) (*api.User, string, error) {
	// Verify state (CSRF protection)
	o.mu.Lock()
	csrfState, exists := o.state[state]
	if !exists {
		o.mu.Unlock()
		return nil, "", fmt.Errorf("invalid state")
	}
	if time.Now().After(csrfState.ExpiresAt) {
		delete(o.state, state)
		o.mu.Unlock()
		return nil, "", fmt.Errorf("state expired")
	}
	delete(o.state, state)
	o.mu.Unlock()

	// Exchange code for token
	tokenResp, err := o.exchangeCode(code)
	if err != nil {
		return nil, "", fmt.Errorf("failed to exchange code: %w", err)
	}

	// Verify ID token signature and claims (always, even if we also fetch userinfo)
	userInfo, err := o.parseIDToken(tokenResp.IDToken)
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
		user = &api.User{
			ID:        generateID(),
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
	token := generateToken()
	o.mu.Lock()
	o.tokens[token] = &session{
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	o.mu.Unlock()

	return user, token, nil
}

// fetchOIDCConfig fetches the OIDC discovery document
func (o *OIDCAuthenticator) fetchOIDCConfig() (*oidcConfig, error) {
	// Normalize issuer URL (remove trailing slash)
	issuer := strings.TrimSuffix(o.config.Issuer, "/")

	// Try well-known endpoint
	wellKnownURL := issuer + "/.well-known/openid-configuration"

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(wellKnownURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch OIDC config from %s: %w", wellKnownURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OIDC config endpoint returned %d", resp.StatusCode)
	}

	var cfg oidcConfig
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse OIDC config: %w", err)
	}

	if cfg.AuthURL == "" || cfg.TokenURL == "" {
		return nil, fmt.Errorf("OIDC config missing authorization or token endpoint")
	}

	return &cfg, nil
}

// exchangeCode exchanges an authorization code for tokens
func (o *OIDCAuthenticator) exchangeCode(code string) (*tokenResponse, error) {
	cfg, err := o.fetchOIDCConfig()
	if err != nil {
		return nil, err
	}

	// Build token request
	data := fmt.Sprintf(
		"grant_type=authorization_code&code=%s&redirect_uri=%s&client_id=%s&client_secret=%s",
		code, o.config.RedirectURL, o.config.ClientID, o.config.ClientSecret,
	)

	resp, err := http.Post(cfg.TokenURL, "application/x-www-form-urlencoded", strings.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
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

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("userinfo request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo endpoint returned %d", resp.StatusCode)
	}

	var userInfo userInfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
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

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(cfg.JWKSURI)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKs from %s: %w", cfg.JWKSURI, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKs endpoint returned %d", resp.StatusCode)
	}

	var jwks jwkSet
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
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
func (o *OIDCAuthenticator) parseIDToken(idToken string) (*userInfoResponse, error) {
	// Verify signature and get claims
	claims, err := o.verifyJWTSignature(idToken)
	if err != nil {
		return nil, err
	}

	// Validate required claims
	if iss, ok := claims["iss"].(string); ok && iss != "" {
		issuer := strings.TrimSuffix(o.config.Issuer, "/")
		if iss != issuer {
			return nil, fmt.Errorf("issuer mismatch: expected %s, got %s", issuer, iss)
		}
	}

	// Validate audience
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

	userInfo := &userInfoResponse{}
	if sub, ok := claims["sub"].(string); ok {
		userInfo.Sub = sub
	}
	if email, ok := claims["email"].(string); ok {
		userInfo.Email = email
	}
	if name, ok := claims["name"].(string); ok {
		userInfo.Name = name
	}
	if verified, ok := claims["email_verified"].(bool); ok {
		userInfo.EmailVerified = verified
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

// Shutdown gracefully stops the authenticator
func (o *OIDCAuthenticator) Shutdown() {
	o.local.Shutdown()
	close(o.stopCh)
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

// AddUser adds a user (for admin management)
func (o *OIDCAuthenticator) AddUser(email, name, role string) *api.User {
	o.mu.Lock()
	defer o.mu.Unlock()

	user := &api.User{
		ID:        generateID(),
		Email:     email,
		Name:      name,
		Role:      role,
		Workspace: "default",
		CreatedAt: time.Now(),
	}
	o.users[user.ID] = user
	o.users[user.Email] = user
	return user
}
