package core

import (
	"fmt"
	"time"
)

// ProbeVote is a secondary prober's latest result for one soul in a redundant
// cluster (necropolis.distribution.redundancy > 1). One record per (soul,
// prober), overwritten on every probe.
type ProbeVote struct {
	JackalID  string     `json:"jackal_id"`
	Status    SoulStatus `json:"status"`
	Timestamp time.Time  `json:"timestamp"`
	// Latency and Region describe the secondary's own probe. Both are
	// optional: votes written by an older build lack them and decode with
	// zero values.
	Latency time.Duration `json:"latency,omitempty"`
	Region  string        `json:"region,omitempty"`
}

// ProbeVoteKey is the storage key of one prober's vote for a soul.
func ProbeVoteKey(workspaceID, soulID, jackalID string) string {
	return ProbeVotePrefix(workspaceID, soulID) + jackalID
}

// ProbeVotePrefix is the key prefix shared by every vote of a soul. It ends in
// "/" so one soul's prefix never matches another soul whose ID extends it.
func ProbeVotePrefix(workspaceID, soulID string) string {
	if workspaceID == "" {
		workspaceID = "default"
	}
	return fmt.Sprintf("%s/votes/%s/", workspaceID, soulID)
}
