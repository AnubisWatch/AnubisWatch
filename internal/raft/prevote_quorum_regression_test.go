package raft

import "testing"

// A pre-vote quorum is a majority of the voting members, and the candidate is one of
// them: requestPreVotes counts its own vote and its main path requires
// (len(peers)+1)/2 + 1, the same rule startElection uses for the real election. The
// transport-nil path used len(peers)/2 + 1, which omits the candidate's own vote and,
// for an odd peer count, sits below a majority — so a node that could reach no peer
// still passed its own pre-vote, defeating the protection that stops a partitioned
// node from disrupting a healthy cluster.

func preVotePeers(n int) []*Peer {
	peers := make([]*Peer, 0, n)
	for i := 0; i < n; i++ {
		peers = append(peers, &Peer{ID: "peer-" + string(rune('a'+i)), Address: "127.0.0.1:1"})
	}
	return peers
}

// TestPreVoteRequiresAMajorityIncludingTheCandidate pins the rule across cluster sizes
// where the two formulas disagreed (odd peer counts) and where they agreed (even).
func TestPreVoteRequiresAMajorityIncludingTheCandidate(t *testing.T) {
	for _, tc := range []struct {
		peers int
		want  bool // can the candidate's own vote alone satisfy the pre-vote?
	}{
		{peers: 0, want: true},  // standalone: self is the whole cluster
		{peers: 1, want: false}, // 2 nodes: a majority is 2
		{peers: 2, want: false}, // 3 nodes: a majority is 2
		{peers: 3, want: false}, // 4 nodes: a majority is 3
		{peers: 4, want: false}, // 5 nodes: a majority is 3
	} {
		node := createTestNode(t) // transport is nil: the peer RPCs cannot be sent
		got := node.requestPreVotes(node.CurrentTerm()+1, 0, 0, preVotePeers(tc.peers))
		if got != tc.want {
			t.Errorf("with %d peers the candidate's own vote alone %s the pre-vote (want %v): "+
				"a quorum must be a majority of the %d voting members including the candidate",
				tc.peers, map[bool]string{true: "satisfied", false: "did not satisfy"}[got], tc.want, tc.peers+1)
		}
	}
}
