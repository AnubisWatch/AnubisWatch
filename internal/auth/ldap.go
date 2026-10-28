package auth

import (
	"crypto/tls"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/go-ldap/ldap/v3"
)

// LDAPAuthenticator implements LDAP/Active Directory authentication
type LDAPAuthenticator struct {
	mu       sync.RWMutex
	cfg      core.LDAPAuth
	local    *LocalAuthenticator
	users    map[string]*api.User // ID -> user
	tokens   map[string]*session  // token -> session
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewLDAPAuthenticator creates a new LDAP authenticator with local fallback.
// Returns an error if the embedded local authenticator fails to initialize
// (e.g. weak admin password — see K6).
func NewLDAPAuthenticator(cfg core.LDAPAuth, localPath, adminEmail, adminPassword string) (*LDAPAuthenticator, error) {
	localAuth, err := NewLocalAuthenticator(localPath, adminEmail, adminPassword)
	if err != nil {
		return nil, fmt.Errorf("local authenticator init: %w", err)
	}
	l := &LDAPAuthenticator{
		cfg:    cfg,
		local:  localAuth,
		users:  make(map[string]*api.User),
		tokens: make(map[string]*session),
		stopCh: make(chan struct{}),
	}
	localAuth.mu.Lock()
	localAuth.onRevokeUser = l.revokeUserTokens
	localAuth.mu.Unlock()
	return l, nil
}

// revokeUserTokens ends the LDAP sessions of userID. The local directory
// calls it (holding its own lock) when the account is disabled, deleted,
// removed or downgraded.
func (l *LDAPAuthenticator) revokeUserTokens(userID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for token, sess := range l.tokens {
		if sess != nil && sess.UserID == userID {
			delete(l.tokens, token)
		}
	}
}

// UserDirectory exposes the persisted directory LDAP accounts are
// provisioned into, for the members API.
func (l *LDAPAuthenticator) UserDirectory() api.UserDirectory { return l.local }

// SetMemberQuota installs the max_team_members check.
func (l *LDAPAuthenticator) SetMemberQuota(check func(workspace string, current int) error) {
	l.local.SetMemberQuota(check)
}

// Login validates credentials against LDAP and creates a session
func (l *LDAPAuthenticator) Login(email, password string) (*api.User, string, error) {
	return l.LoginFrom(email, password, "")
}

// LoginFrom is Login with the caller's address. The directory enforces its
// own lockout policy; the address keys the local fallback's brute-force
// protection.
func (l *LDAPAuthenticator) LoginFrom(email, password, clientIP string) (*api.User, string, error) {
	// Try LDAP first
	user, err := l.ldapLogin(email, password)
	if err != nil {
		// Only fall back to local auth for unreachable LDAP server.
		// Authentication failures (invalid credentials, account locked) do NOT
		// fall back — this prevents a misconfigured LDAP server (e.g. anonymous
		// bind) from silently delegating to the local account.
		if l.isConnectionFailure(err) {
			return l.local.LoginFrom(email, password, clientIP)
		}
		return nil, "", err
	}

	// Create session — ldapLogin already populated l.users with the LDAP user
	token, err := generateToken()
	if err != nil {
		return nil, "", err
	}
	now := time.Now()
	l.mu.Lock()
	// Reclaim expired sessions on insert (same discipline as the OIDC state
	// store): entries are otherwise deleted only when their own token is
	// presented again, so logins that are never repeated grew this map for
	// the process lifetime.
	l.sweepExpiredTokensLocked(now)
	l.tokens[token] = &session{
		UserID:    user.ID,
		ExpiresAt: now.Add(24 * time.Hour),
	}
	l.mu.Unlock()

	return copyUser(user), token, nil
}

// sweepExpiredTokensLocked drops expired sessions. Caller must hold l.mu for
// writing.
func (l *LDAPAuthenticator) sweepExpiredTokensLocked(now time.Time) {
	for token, sess := range l.tokens {
		if sess == nil || now.After(sess.ExpiresAt) {
			delete(l.tokens, token)
		}
	}
}

// ldapLogin authenticates against LDAP server
func (l *LDAPAuthenticator) ldapLogin(email, password string) (*api.User, error) {
	// Connect to LDAP server
	conn, err := ldap.DialURL(l.cfg.URL, ldap.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to LDAP: %w", err)
	}
	defer conn.Close()

	// Start TLS if not already using ldaps
	if !strings.HasPrefix(l.cfg.URL, "ldaps://") {
		hostname := extractHostnameFromURL(l.cfg.URL)
		if err := conn.StartTLS(&tls.Config{ServerName: hostname, MinVersion: tls.VersionTLS12}); err != nil {
			return nil, fmt.Errorf("failed to start TLS: %w", err)
		}
	}

	// Build bind DN for authentication
	bindDN := l.buildUserDN(email)

	// Attempt to bind with user credentials
	if err := conn.Bind(bindDN, password); err != nil {
		return nil, fmt.Errorf("LDAP bind failed: %w", err)
	}

	// Search for user attributes if bind DN is configured
	var name string
	if l.cfg.BindDN != "" {
		// Re-bind with service account to search
		if err := conn.Bind(l.cfg.BindDN, l.cfg.BindPassword); err != nil {
			return nil, fmt.Errorf("LDAP service bind failed: %w", err)
		}

		// Search for user to get display name
		filter := l.cfg.UserFilter
		if filter == "" {
			filter = "(mail={{mail}})"
		}
		filter = strings.ReplaceAll(filter, "{{mail}}", ldap.EscapeFilter(email))

		searchReq := ldap.NewSearchRequest(
			l.cfg.BaseDN,
			ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
			filter,
			[]string{"cn", "displayName", "name", "mail"},
			nil,
		)

		searchResult, err := conn.Search(searchReq)
		if err != nil {
			return nil, fmt.Errorf("LDAP search failed: %w", err)
		}

		if len(searchResult.Entries) > 0 {
			entry := searchResult.Entries[0]
			// Try to get display name from attributes
			name = entry.GetAttributeValue("displayName")
			if name == "" {
				name = entry.GetAttributeValue("cn")
			}
			if name == "" {
				name = entry.GetAttributeValue("name")
			}
		}
	}

	if name == "" {
		name = email
	}

	// Find or create the account in the persisted directory: a first sign-in
	// is a viewer of the default workspace, later ones keep the roles an
	// administrator gave it, and a disabled account is refused.
	provisioned, err := l.local.ProvisionExternalUser(email, name, SourceLDAP)
	if err != nil {
		return nil, fmt.Errorf("LDAP account: %w", err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if user, ok := l.users[provisioned.ID]; ok {
		user.Email, user.Name = provisioned.Email, provisioned.Name
		return user, nil
	}
	l.users[provisioned.ID] = provisioned
	return provisioned, nil
}

// buildUserDN constructs the DN for a user from email
func (l *LDAPAuthenticator) buildUserDN(email string) string {
	// If email contains @, try to use it as UPN for AD
	if strings.Contains(email, "@") {
		// Extract username part for constructing DN
		username := strings.Split(email, "@")[0]
		// If BindDN pattern uses {{mail}}, escape DN special chars and substitute
		if strings.Contains(l.cfg.BindDN, "{{mail}}") {
			return strings.ReplaceAll(l.cfg.BindDN, "{{mail}}", ldap.EscapeDN(email))
		}
		// Escape username for DN construction
		return fmt.Sprintf("CN=%s,%s", ldap.EscapeDN(username), l.cfg.BaseDN)
	}
	// Direct DN - return as-is (already a valid DN)
	return email
}

// extractHostnameFromURL extracts the hostname from an LDAP URL for SNI.
// The result must be a bare host: it is used directly as tls.Config.ServerName,
// where an IPv6 literal is only usable without its brackets.
func extractHostnameFromURL(rawURL string) string {
	// ldap://hostname:port/... or ldaps://hostname:port/...
	rawURL = strings.TrimPrefix(rawURL, "ldap://")
	rawURL = strings.TrimPrefix(rawURL, "ldaps://")
	// Remove path
	if idx := strings.Index(rawURL, "/"); idx >= 0 {
		rawURL = rawURL[:idx]
	}
	// SplitHostPort understands a bracketed IPv6 literal and its port; the
	// fallback covers a URL with no port, where a bracketed literal still needs
	// unwrapping.
	if host, _, err := net.SplitHostPort(rawURL); err == nil {
		return host
	}
	return strings.Trim(rawURL, "[]")
}

// Authenticate validates a token and returns the user
func (l *LDAPAuthenticator) Authenticate(token string) (*api.User, error) {
	// Check LDAP tokens first
	l.mu.RLock()
	sess, ok := l.tokens[token]
	if ok {
		if time.Now().After(sess.ExpiresAt) {
			l.mu.RUnlock()
			// Reclaim the rejected entry: nothing else deletes expired
			// tokens, so leaving it here grew the map for the process
			// lifetime (LocalAuthenticator and the OIDC state store both
			// reclaim their expired entries).
			l.mu.Lock()
			if s, ok := l.tokens[token]; ok && time.Now().After(s.ExpiresAt) {
				delete(l.tokens, token)
			}
			l.mu.Unlock()
			return nil, fmt.Errorf("token expired")
		}
		user := copyUser(l.users[sess.UserID])
		l.mu.RUnlock()
		if user == nil {
			return nil, fmt.Errorf("user not found")
		}
		// The directory decides the role and whether the account may still
		// sign in.
		return l.local.ResolveExternalUser(user)
	}
	l.mu.RUnlock()

	// Fallback to local auth
	return l.local.Authenticate(token)
}

// isConnectionFailure returns true when the LDAP error indicates the server
// is unreachable, not that authentication failed. This distinction determines
// whether local fallback is safe to apply.
func (l *LDAPAuthenticator) isConnectionFailure(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// ldap.ErrCouldNotStartTLS, ldap.ErrInvalidCredentials, and bind failures
	// all contain specific markers. Connection/dial failures do not.
	connectionMarkers := []string{
		"failed to connect",
		"connection refused",
		"timeout",
		"no such host",
		"network unreachable",
		"i/o timeout",
		"connection reset",
		"use of closed",
	}
	for _, m := range connectionMarkers {
		if strings.Contains(strings.ToLower(msg), m) {
			return true
		}
	}
	// Invalid credentials / bind failure = auth error, not connection error
	if strings.Contains(msg, "LDAP bind failed") ||
		strings.Contains(msg, "invalid credentials") ||
		strings.Contains(msg, "server bind failed") {
		return false
	}
	// When in doubt, treat it as an auth failure — do not fall back
	return false
}

// Logout invalidates a token
func (l *LDAPAuthenticator) Logout(token string) error {
	l.mu.Lock()
	delete(l.tokens, token)
	l.mu.Unlock()
	return l.local.Logout(token)
}

// SwitchWorkspace updates the active workspace for LDAP-backed sessions and
// delegates local fallback sessions to the local authenticator.
func (l *LDAPAuthenticator) SwitchWorkspace(token, workspace string) (*api.User, error) {
	if workspace == "" {
		return nil, fmt.Errorf("workspace is required")
	}

	l.mu.Lock()
	sess, ok := l.tokens[token]
	if ok {
		if time.Now().After(sess.ExpiresAt) {
			delete(l.tokens, token)
			l.mu.Unlock()
			return nil, fmt.Errorf("token expired")
		}
		user := l.users[sess.UserID]
		if user == nil {
			l.mu.Unlock()
			return nil, fmt.Errorf("user not found")
		}
		user.Workspace = workspace
		l.mu.Unlock()
		return copyUser(user), nil
	}
	l.mu.Unlock()

	return l.local.SwitchWorkspace(token, workspace)
}

// Shutdown gracefully stops the authenticator. Idempotent — t.Cleanup
// hooks plus deferred Shutdown() calls in tests can both fire safely.
func (l *LDAPAuthenticator) Shutdown() {
	l.local.Shutdown()
	l.stopOnce.Do(func() { close(l.stopCh) })
}

// ChangePassword delegates to local authenticator (HIGH-04)
func (l *LDAPAuthenticator) ChangePassword(token, currentPassword, newPassword string) error {
	return l.local.ChangePassword(token, currentPassword, newPassword)
}

// RequestPasswordReset delegates to local authenticator (HIGH-04)
func (l *LDAPAuthenticator) RequestPasswordReset(email string) (string, error) {
	return l.local.RequestPasswordReset(email)
}

// ConfirmPasswordReset delegates to local authenticator (HIGH-04)
func (l *LDAPAuthenticator) ConfirmPasswordReset(token, newPassword string) error {
	return l.local.ConfirmPasswordReset(token, newPassword)
}

// GetUsers returns all LDAP users
func (l *LDAPAuthenticator) GetUsers() []*api.User {
	l.mu.RLock()
	defer l.mu.RUnlock()

	users := make([]*api.User, 0, len(l.users))
	for _, user := range l.users {
		users = append(users, copyUser(user))
	}
	return users
}

// AddUser adds a user (for admin management). Returns the new user or
// an error if the system CSPRNG is unavailable (K6: previously panicked).
func (l *LDAPAuthenticator) AddUser(email, name, role string) (*api.User, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

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
	l.users[user.ID] = user
	return copyUser(user), nil
}

// CheckKeyOwner checks an API key's owner against the local directory.
func (l *LDAPAuthenticator) CheckKeyOwner(ownerID, workspace string, role core.MemberRole) error {
	return l.local.CheckKeyOwner(ownerID, workspace, role)
}

// RequestPasswordResetTo delegates to the local authenticator.
func (l *LDAPAuthenticator) RequestPasswordResetTo(email string) (string, string, error) {
	return l.local.RequestPasswordResetTo(email)
}
