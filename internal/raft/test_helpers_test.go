package raft

import "github.com/AnubisWatch/anubiswatch/internal/core"

// Helpers only tests use; production code applies membership through
// applyMembershipEntry.

// applyMembershipChange applies a committed membership change written by an
// older version (or a test). See applyMembershipEntry.
func (n *Node) applyMembershipChange(change core.MembershipChange, index uint64) {
	n.applyMembershipEntry(membershipEntry{MembershipChange: change}, index)
}

// containsString checks if a string is in a slice
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
