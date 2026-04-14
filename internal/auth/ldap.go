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
	mu     sync.RWMutex
	cfg    core.LDAPAuth
	local  *LocalAuthenticator
	users  map[string]*api.User // ID -> user
	tokens map[string]*session  // token -> session
	stopCh chan struct{}
}

// NewLDAPAuthenticator creates a new LDAP authenticator with local fallback
func NewLDAPAuthenticator(cfg core.LDAPAuth, localPath, adminEmail, adminPassword string) *LDAPAuthenticator {
	localAuth := NewLocalAuthenticator(localPath, adminEmail, adminPassword)
	return &LDAPAuthenticator{
		cfg:    cfg,
		local:  localAuth,
		users:  make(map[string]*api.User),
		tokens: make(map[string]*session),
		stopCh: make(chan struct{}),
	}
}

// Login validates credentials against LDAP and creates a session
func (l *LDAPAuthenticator) Login(email, password string) (*api.User, string, error) {
	// Try LDAP first
	user, err := l.ldapLogin(email, password)
	if err != nil {
		// Fallback to local
		return l.local.Login(email, password)
	}

	// Create session
	token := generateToken()
	l.mu.Lock()
	l.tokens[token] = &session{
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	l.mu.Unlock()

	return user, token, nil
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
		if err := conn.StartTLS(&tls.Config{ServerName: hostname}); err != nil {
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

	// Find or create user
	l.mu.Lock()
	var existing *api.User
	for _, u := range l.users {
		if u.Email == email {
			existing = u
			break
		}
	}

	if existing != nil {
		l.mu.Unlock()
		return existing, nil
	}

	user := &api.User{
		ID:        generateID(),
		Email:     email,
		Name:      name,
		Role:      "viewer",
		Workspace: "default",
		CreatedAt: time.Now(),
	}
	l.users[user.ID] = user
	l.mu.Unlock()

	return user, nil
}

// buildUserDN constructs the DN for a user from email
func (l *LDAPAuthenticator) buildUserDN(email string) string {
	// If email contains @, try to use it as UPN for AD
	if strings.Contains(email, "@") {
		// Extract username part for constructing DN
		username := strings.Split(email, "@")[0]
		// If BindDN pattern uses {{mail}}, return email as-is (for AD UPN bind)
		if strings.Contains(l.cfg.BindDN, "{{mail}}") {
			return strings.ReplaceAll(l.cfg.BindDN, "{{mail}}", email)
		}
		// Try to construct DN from base DN
		return fmt.Sprintf("CN=%s,%s", username, l.cfg.BaseDN)
	}
	// Direct DN
	return email
}

// extractHostnameFromURL extracts the hostname from an LDAP URL for SNI
func extractHostnameFromURL(rawURL string) string {
	// ldap://hostname:port/... or ldaps://hostname:port/...
	rawURL = strings.TrimPrefix(rawURL, "ldap://")
	rawURL = strings.TrimPrefix(rawURL, "ldaps://")
	// Remove path
	if idx := strings.Index(rawURL, "/"); idx >= 0 {
		rawURL = rawURL[:idx]
	}
	// Remove port
	if idx := strings.LastIndex(rawURL, ":"); idx >= 0 {
		rawURL = rawURL[:idx]
	}
	return rawURL
}

// Authenticate validates a token and returns the user
func (l *LDAPAuthenticator) Authenticate(token string) (*api.User, error) {
	// Check LDAP tokens first
	l.mu.RLock()
	sess, ok := l.tokens[token]
	if ok {
		if time.Now().After(sess.ExpiresAt) {
			l.mu.RUnlock()
			return nil, fmt.Errorf("token expired")
		}
		user := l.users[sess.UserID]
		l.mu.RUnlock()
		if user == nil {
			return nil, fmt.Errorf("user not found")
		}
		return user, nil
	}
	l.mu.RUnlock()

	// Fallback to local auth
	return l.local.Authenticate(token)
}

// Logout invalidates a token
func (l *LDAPAuthenticator) Logout(token string) error {
	l.mu.Lock()
	delete(l.tokens, token)
	l.mu.Unlock()
	return l.local.Logout(token)
}

// Shutdown gracefully stops the authenticator
func (l *LDAPAuthenticator) Shutdown() {
	l.local.Shutdown()
	close(l.stopCh)
}

// GetUsers returns all LDAP users
func (l *LDAPAuthenticator) GetUsers() []*api.User {
	l.mu.RLock()
	defer l.mu.RUnlock()

	users := make([]*api.User, 0, len(l.users))
	for _, user := range l.users {
		users = append(users, user)
	}
	return users
}

// AddUser adds a user (for admin management)
func (l *LDAPAuthenticator) AddUser(email, name, role string) *api.User {
	l.mu.Lock()
	defer l.mu.Unlock()

	user := &api.User{
		ID:        generateID(),
		Email:     email,
		Name:      name,
		Role:      role,
		Workspace: "default",
		CreatedAt: time.Now(),
	}
	l.users[user.ID] = user
	return user
}
