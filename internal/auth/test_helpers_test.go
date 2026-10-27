package auth

// Wrappers only tests use: production callers always pass the client IP
// (LoginFrom) and a PKCE verifier.

// checkBruteForceProtection checks the lockout of an account for a caller
// with no known client address.
func (a *LocalAuthenticator) checkBruteForceProtection(email string) error {
	return a.checkBruteForceProtectionFrom(email, "")
}

// recordFailedAttempt records a failed login attempt from an unknown address.
func (a *LocalAuthenticator) recordFailedAttempt(email string) {
	a.recordFailedAttemptFrom(email, "")
}

// clearFailedAttempts clears failed login attempts for an unknown address.
func (a *LocalAuthenticator) clearFailedAttempts(email string) {
	a.clearFailedAttemptsFrom(email, "")
}

// exchangeCode exchanges an authorization code for tokens without PKCE.
func (o *OIDCAuthenticator) exchangeCode(code string) (*tokenResponse, error) {
	return o.exchangeCodePKCE(code, "")
}
