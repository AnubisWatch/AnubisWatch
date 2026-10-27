package api

// revokeUser closes every stream of userID. Production revokes by token
// (logout) or by re-checking tokens (password change); only tests use this.
func (r *liveSessionRegistry) revokeUser(userID string) int {
	if userID == "" {
		return 0
	}
	return r.revoke(func(st liveStream) bool { return st.userID == userID })
}
