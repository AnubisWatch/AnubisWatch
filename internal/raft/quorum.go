package raft

import (
	"fmt"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Leadership validity: check-quorum, leader verification and read barriers.
//
// A leader learns that a voter accepts its leadership when the voter answers
// an AppendEntries (or InstallSnapshot) request of the leader's term without
// a higher term. markPeerAck records, per peer, the send time of the newest
// such request. From that:
//
//   - check-quorum: a leader that has not had its term acknowledged by a voter
//     majority within an election timeout steps down, so a leader cut off in
//     a minority partition stops acting as leader instead of serving stale
//     state and running leader-only work next to the majority's new leader;
//   - VerifyLeader confirms with a fresh round that a majority still accepts
//     this leader after the call started (the ReadIndex check);
//   - ReadBarrier is a linearizable read point: once it returns, local reads
//     observe every write committed before it was called.

// markPeerAck records that peerID accepted this node's leadership in term
// for a request sent at sentAt.
func (n *Node) markPeerAck(peerID string, term uint64, sentAt time.Time) {
	n.peerMu.Lock()
	defer n.peerMu.Unlock()
	p, ok := n.peers[peerID]
	if !ok {
		return
	}
	if p.ackTerm != term || sentAt.After(p.ackSentAt) {
		p.ackTerm, p.ackSentAt = term, sentAt
	}
}

// quorumAckedSinceLocked reports whether a voter majority (this node
// included) acknowledged term with a request sent no earlier than since.
// Caller holds n.mu (read or write).
func (n *Node) quorumAckedSinceLocked(term uint64, since time.Time) bool {
	votes, voters := n.countVoters(true, func(p *Peer) bool {
		return p.ackTerm == term && !p.ackSentAt.Before(since)
	})
	return voters > 0 && votes > voters/2
}

// checkQuorum steps the leader down when no voter majority has acknowledged
// its term within the last election timeout.
func (n *Node) checkQuorum() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.state.Load().(core.RaftState) != core.StateLeader {
		return
	}
	window := n.electionTimeout
	if window <= 0 || time.Since(n.leaderSince) < window {
		return
	}
	if n.quorumAckedSinceLocked(n.currentTerm, time.Now().Add(-window)) {
		return
	}
	n.logger.Warn("Stepping down: a majority of voters has not acknowledged this leader within the election timeout",
		"term", n.currentTerm, "timeout", window)
	n.stepDownLocked()
}

// stepDownLocked turns a leader into a follower in the same term. Every
// proposal still waiting for commit fails with ErrLeadershipLost: whether it
// survives is now up to the next leader. Caller holds n.mu.
func (n *Node) stepDownLocked() {
	if n.state.Load().(core.RaftState) != core.StateLeader {
		return
	}
	n.state.Store(core.StateFollower)
	n.leaderID = ""
	n.lastContact = time.Now()
	n.stats.ElectionsLost++
	n.failPendingFutures(0, "leadership lost before the entry committed")
}

// failPendingFutures fails every proposal waiting at an index >= from with
// ErrLeadershipLost.
func (n *Node) failPendingFutures(from uint64, reason string) {
	n.applyWaiters.Range(func(key, _ any) bool {
		index, ok := key.(uint64)
		if !ok || index < from {
			return true
		}
		if v, loaded := n.applyWaiters.LoadAndDelete(key); loaded {
			f := v.(*applyFuture)
			f.err = &core.RaftError{Code: core.ErrLeadershipLost, Message: fmt.Sprintf("%s (index %d)", reason, index)}
			close(f.done)
		}
		return true
	})
}

// HasQuorumContact reports whether this node is in contact with a working
// majority and its replicated state is current:
//
//   - as leader, a voter majority acknowledged it within the election
//     timeout, and it has applied its own term's first entry;
//   - as follower, it heard from a leader within two election timeouts (a
//     leader that lost its majority steps down within one) and has applied
//     everything that leader had committed when contact was established, so
//     decisions made from replicated state (soul ownership) are not stale.
//
// It is false while this node is not in its own latest configuration (it was
// removed, or is not added yet). Work that must run on one node only -- soul
// ownership, alerting -- should stop when this turns false, because the
// majority side will hand that work to someone else.
func (n *Node) HasQuorumContact() bool {
	if n.removed.Load() || n.awaitingJoin.Load() || n.shutdown.Load() || n.selfAbsent.Load() {
		return false
	}
	n.mu.RLock()
	defer n.mu.RUnlock()
	window := n.electionTimeout
	if window <= 0 {
		window = time.Second
	}
	switch n.state.Load().(core.RaftState) {
	case core.StateLeader:
		if n.lastApplied < n.leaderNoopIndex {
			return false
		}
		if time.Since(n.leaderSince) < window {
			return true
		}
		return n.quorumAckedSinceLocked(n.currentTerm, time.Now().Add(-window))
	default:
		return n.leaderID != "" && n.leaderID != n.nodeID &&
			time.Since(n.lastContact) < 2*window &&
			n.lastApplied >= n.catchUpTarget
	}
}

// VerifyLeader confirms that this node is still the leader: a voter majority
// must acknowledge its current term with requests sent after the call began.
func (n *Node) VerifyLeader(timeout time.Duration) error {
	start := time.Now()
	deadline := start.Add(timeout)
	n.mu.RLock()
	term, leader, commitIndex := n.currentTerm, n.state.Load().(core.RaftState) == core.StateLeader, n.commitIndex
	n.mu.RUnlock()
	if !leader {
		return &core.RaftError{Code: core.ErrNotLeader, Message: "not leader", NodeID: n.Leader()}
	}
	poll := n.heartbeatTimeout / 4
	if poll <= 0 || poll > 50*time.Millisecond {
		poll = 5 * time.Millisecond
	}
	lastSend := time.Time{}
	for {
		n.mu.RLock()
		stillLeader := n.state.Load().(core.RaftState) == core.StateLeader && n.currentTerm == term
		confirmed := stillLeader && n.quorumAckedSinceLocked(term, start)
		n.mu.RUnlock()
		if !stillLeader {
			return &core.RaftError{Code: core.ErrLeadershipLost, Message: "leadership lost while verifying it", NodeID: n.Leader()}
		}
		if confirmed {
			return nil
		}
		if n.shutdown.Load() {
			return &core.RaftError{Code: core.ErrShutdown, Message: "node is shutting down"}
		}
		if time.Now().After(deadline) {
			return &core.RaftError{Code: core.ErrTimeout, Message: "timeout verifying leadership with a majority"}
		}
		if time.Since(lastSend) >= n.heartbeatTimeout/2 {
			n.sendHeartbeats(commitIndex)
			lastSend = time.Now()
		}
		time.Sleep(poll)
	}
}

// ReadBarrier returns once local state reflects every write committed before
// it was called, so a read that follows it is linearizable. It runs on the
// leader only (ReadIndex): wait for the leader's own no-op to commit, take
// the commit index, confirm leadership with a majority, then wait for the
// commit index to apply locally. It returns the read index.
func (n *Node) ReadBarrier(timeout time.Duration) (uint64, error) {
	deadline := time.Now().Add(timeout)
	var readIndex uint64
	for {
		n.mu.RLock()
		leader := n.state.Load().(core.RaftState) == core.StateLeader
		ready := n.commitIndex >= n.leaderNoopIndex
		readIndex = n.commitIndex
		n.mu.RUnlock()
		if !leader {
			return 0, &core.RaftError{Code: core.ErrNotLeader, Message: "not leader", NodeID: n.Leader()}
		}
		if ready {
			break
		}
		if time.Now().After(deadline) {
			return 0, &core.RaftError{Code: core.ErrTimeout, Message: "timeout waiting for the leader to commit its first entry"}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := n.VerifyLeader(time.Until(deadline)); err != nil {
		return 0, err
	}
	if err := n.WaitForApplied(readIndex, time.Until(deadline)); err != nil {
		return 0, err
	}
	return readIndex, nil
}
