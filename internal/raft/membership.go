package raft

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Membership changes: one server at a time (Raft dissertation §4.1).
//
// A configuration is the set of members with their roles. Every membership
// change adds or removes exactly one server, and the configuration a node uses
// is the latest one in its log -- it takes effect when the entry is appended,
// committed or not. Because two configurations that differ by one server
// always have overlapping majorities, no joint phase is needed, provided that
//
//   - only one change is in flight: a leader refuses a new change while the
//     latest configuration entry in its log is uncommitted, and
//   - a new leader first commits an entry of its own term (its no-op) before
//     proposing a change, so a change appended by a previous leader that the
//     new leader's log does not hold cannot overlap with a new one.
//
// The configuration is derived from the log, never kept on the side: the
// base is the latest committed configuration (from the static peers, the
// persisted membership, a joined cluster or an installed snapshot) and every
// membership entry in the log is folded over it. A truncated entry therefore
// takes its configuration with it, and a restarted node, or a new leader,
// picks up a pending change simply by reading its log.
//
// Commit and election quorums are a majority of the configuration's voters;
// non-voters receive the log but never count.

const (
	// membershipPhaseSingle marks an entry written by this protocol. Older
	// versions wrote "joint" and "final" entries without Members; those are
	// folded as single-server deltas.
	membershipPhaseSingle = "single"

	// ErrCodeMembershipChangePending refuses a membership change while
	// another one has not committed yet. The caller may retry shortly.
	ErrCodeMembershipChangePending = "MEMBERSHIP_CHANGE_PENDING"

	// membershipChangeTimeout bounds how long AddPeer/RemovePeer wait for
	// the change to commit.
	membershipChangeTimeout = 30 * time.Second
)

// membershipEntry is the payload of a LogMembershipChange entry. It extends
// core.MembershipChange with Members, the complete configuration that takes
// effect once the entry is appended.
type membershipEntry struct {
	core.MembershipChange
	Members []core.RaftPeer `json:"members,omitempty"`
}

func decodeMembershipEntry(data []byte) (membershipEntry, error) {
	var e membershipEntry
	err := json.Unmarshal(data, &e)
	return e, err
}

func cloneMembers(members []core.RaftPeer) []core.RaftPeer {
	return append([]core.RaftPeer(nil), members...)
}

func memberPosition(members []core.RaftPeer, id string) int {
	for i, m := range members {
		if m.ID == id {
			return i
		}
	}
	return -1
}

func configurationIDs(members []core.RaftPeer) []string {
	ids := make([]string, len(members))
	for i, m := range members {
		ids[i] = m.ID
	}
	return ids
}

// normalizeMembers drops entries without an ID, keeps the first entry of a
// duplicated ID, defaults the role to voter and sorts by ID. It returns the
// IDs that were listed more than once.
func normalizeMembers(members []core.RaftPeer) ([]core.RaftPeer, []string) {
	out := make([]core.RaftPeer, 0, len(members))
	seen := make(map[string]bool, len(members))
	var dups []string
	for _, m := range members {
		if m.ID == "" {
			continue
		}
		if seen[m.ID] {
			dups = append(dups, m.ID)
			continue
		}
		seen[m.ID] = true
		if m.Role == "" {
			m.Role = core.RoleVoter
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, dups
}

// foldMembership returns the configuration that results from applying e to
// cur. An entry with Members replaces the configuration; an entry from an
// older version is a delta on its Peer.
func foldMembership(cur []core.RaftPeer, e membershipEntry) []core.RaftPeer {
	if len(e.Members) > 0 {
		out, _ := normalizeMembers(e.Members)
		return out
	}
	out := cloneMembers(cur)
	remove := func(id string) {
		if i := memberPosition(out, id); i >= 0 {
			out = append(out[:i], out[i+1:]...)
		}
	}
	switch e.Type {
	case core.MembershipAddPeer:
		remove(e.Peer.ID)
		out = append(out, e.Peer)
	case core.MembershipRemovePeer:
		remove(e.Peer.ID)
	case core.MembershipReplacePeer:
		if e.OldPeer != nil {
			remove(e.OldPeer.ID)
		}
		remove(e.Peer.ID)
		out = append(out, e.Peer)
	}
	out, _ = normalizeMembers(out)
	return out
}

// initialMembers builds the bootstrap configuration from the static config:
// this node plus raft.peers. Listing this node among its own peers, or one
// peer twice, used to count it twice in the quorum; such entries are ignored.
func initialMembers(config core.RaftConfig, logger *slog.Logger) []core.RaftPeer {
	members := []core.RaftPeer{{
		ID:      config.NodeID,
		Address: firstNonEmptyString(config.AdvertiseAddr, config.BindAddr),
		Region:  config.Region,
		Role:    core.RoleVoter,
	}}
	for _, p := range config.Peers {
		if p.ID == config.NodeID {
			logger.Warn("raft peers list this node itself; ignoring that entry", "node_id", p.ID)
			continue
		}
		members = append(members, p)
	}
	out, dups := normalizeMembers(members)
	if len(dups) > 0 {
		logger.Warn("raft peers list a node more than once; using the first entry", "duplicates", dups)
	}
	return out
}

// refreshMembershipLocked recomputes the effective configuration: the base
// with every membership entry in the log folded over it. Caller holds n.mu.
func (n *Node) refreshMembershipLocked() {
	n.membership.mu.Lock()
	latest, latestIndex := cloneMembers(n.membership.base), uint64(0)
	for _, entry := range n.log {
		if entry.Index <= n.logBase || entry.Type != core.LogMembershipChange {
			continue
		}
		e, err := decodeMembershipEntry(entry.Data)
		if err != nil {
			continue
		}
		latest, latestIndex = foldMembership(latest, e), entry.Index
	}
	n.membership.latest, n.membership.latestIndex = latest, latestIndex
	base := cloneMembers(n.membership.base)
	n.membership.mu.Unlock()
	n.syncPeersLocked(base, latest)
}

// logHasMembershipLocked reports whether the in-memory log holds a
// membership entry. Caller holds n.mu.
func (n *Node) logHasMembershipLocked() bool {
	for _, entry := range n.log {
		if entry.Index > n.logBase && entry.Type == core.LogMembershipChange {
			return true
		}
	}
	return false
}

// syncPeersLocked makes the peer map match the configuration. A member of
// the latest configuration is a full peer. A member only of the committed
// base -- one whose removal has not committed yet -- keeps receiving the log
// so it can learn of its removal, but is "leaving" and no longer counts in any
// quorum. Progress of existing peers is preserved. Caller holds n.mu.
func (n *Node) syncPeersLocked(base, latest []core.RaftPeer) {
	selfVoter, selfPresent := false, false
	union := make(map[string]core.RaftPeer, len(base)+len(latest))
	inLatest := make(map[string]bool, len(latest))
	for _, m := range latest {
		if m.ID == n.nodeID {
			selfVoter, selfPresent = m.Role != core.RoleNonVoter, true
			continue
		}
		inLatest[m.ID] = true
		union[m.ID] = m
	}
	for _, m := range base {
		if _, ok := union[m.ID]; !ok && m.ID != n.nodeID {
			union[m.ID] = m
		}
	}
	n.selfNonVoter.Store(!selfVoter)
	n.selfAbsent.Store(!selfPresent)

	isLeader := n.state.Load().(core.RaftState) == core.StateLeader
	next := n.lastLogIndexLocked() + 1
	var added []core.RaftPeer
	var dropped []string
	n.peerMu.Lock()
	for id, m := range union {
		p, ok := n.peers[id]
		if !ok {
			p = &Peer{ID: id}
			if isLeader {
				p.nextIndex = next
				n.nextIndex[id] = next
				n.matchIndex[id] = 0
			}
			n.peers[id] = p
			added = append(added, m)
		}
		if dialableAddr(m.Address) || p.Address == "" {
			p.Address = m.Address
		}
		p.Region, p.Role = m.Region, m.Role
		p.leaving = !inLatest[id]
	}
	for id := range n.peers {
		if _, ok := union[id]; !ok {
			delete(n.peers, id)
			delete(n.nextIndex, id)
			delete(n.matchIndex, id)
			dropped = append(dropped, id)
		}
	}
	n.peerMu.Unlock()

	if tt, ok := n.transport.(*TCPTransport); ok {
		for _, m := range added {
			if dialableAddr(m.Address) {
				tt.RegisterPeer(m.ID, m.Address)
			}
		}
		for _, id := range dropped {
			tt.UnregisterPeer(id)
		}
	}
}

// setMembershipBaseLocked replaces the committed configuration base and
// recomputes the effective one. Caller holds n.mu.
func (n *Node) setMembershipBaseLocked(base []core.RaftPeer) {
	n.membership.mu.Lock()
	n.membership.base = cloneMembers(base)
	n.membership.mu.Unlock()
	n.refreshMembershipLocked()
}

// membershipBase returns a copy of the committed configuration.
func (n *Node) membershipBase() []core.RaftPeer {
	n.membership.mu.RLock()
	defer n.membership.mu.RUnlock()
	return cloneMembers(n.membership.base)
}

// Configuration returns the effective configuration (every member, this node
// included) and the log index of the entry that defines it (0 when it is the
// committed base).
func (n *Node) Configuration() ([]core.RaftPeer, uint64) {
	n.membership.mu.RLock()
	defer n.membership.mu.RUnlock()
	return cloneMembers(n.membership.latest), n.membership.latestIndex
}

// countVoters returns the number of voters in the configuration and how many
// of them satisfy ok; this node counts as satisfying when selfOK. Peers that
// are leaving or non-voters are ignored.
func (n *Node) countVoters(selfOK bool, ok func(p *Peer) bool) (votes, voters int) {
	if !n.selfNonVoter.Load() {
		voters++
		if selfOK {
			votes++
		}
	}
	n.peerMu.RLock()
	for _, p := range n.peers {
		if p.Role == core.RoleNonVoter || p.leaving {
			continue
		}
		voters++
		if ok(p) {
			votes++
		}
	}
	n.peerMu.RUnlock()
	return votes, voters
}

// votingPeers returns the peers whose votes count in an election.
func (n *Node) votingPeers() []*Peer {
	n.peerMu.RLock()
	defer n.peerMu.RUnlock()
	peers := make([]*Peer, 0, len(n.peers))
	for _, p := range n.peers {
		if p.Role != core.RoleNonVoter && !p.leaving {
			peers = append(peers, p)
		}
	}
	return peers
}

// persistMembers records the committed configuration so it survives a
// restart. The format is the peer list older versions wrote (they skip the
// entry for the node itself).
func (n *Node) persistMembers(members []core.RaftPeer) {
	if n.stable == nil {
		return
	}
	data, err := json.Marshal(members)
	if err != nil {
		return
	}
	if err := n.stable.Set(stablePeers, data); err != nil {
		n.logger.Error("Failed to persist membership", "error", err)
	}
}

// restorePeers replaces the configured peers with the membership persisted by
// the last applied membership change, if there is one.
func (n *Node) restorePeers() error {
	if n.stable == nil {
		return nil
	}
	data, err := n.stable.Get(stablePeers)
	if err != nil {
		var notFound *core.NotFoundError
		if errors.As(err, &notFound) {
			return nil
		}
		return err
	}
	var peers []core.RaftPeer
	if err := json.Unmarshal(data, &peers); err != nil {
		return err
	}
	self := core.RaftPeer{ID: n.nodeID, Address: firstNonEmptyString(n.advertiseAddr, n.bindAddr), Region: n.config.Region, Role: core.RoleVoter}
	base := []core.RaftPeer{self}
	for _, p := range peers {
		if p.ID == n.nodeID {
			if p.Role != "" {
				base[0].Role = p.Role
			}
			continue
		}
		base = append(base, p)
	}
	base, _ = normalizeMembers(base)
	n.mu.Lock()
	n.setMembershipBaseLocked(base)
	n.mu.Unlock()
	return nil
}

// applyMembershipEntry runs when a membership entry commits: its
// configuration becomes the committed base, which is persisted. The
// effective configuration already changed when the entry was appended.
func (n *Node) applyMembershipEntry(e membershipEntry, index uint64) {
	n.mu.Lock()
	n.membership.mu.Lock()
	n.membership.base = foldMembership(n.membership.base, e)
	base := cloneMembers(n.membership.base)
	n.membership.mu.Unlock()
	n.refreshMembershipLocked()
	n.mu.Unlock()

	selfIn := memberPosition(base, n.nodeID) >= 0
	switch {
	case !selfIn && e.Type == core.MembershipRemovePeer && e.Peer.ID == n.nodeID:
		n.setRemoved(true)
		n.logger.Warn("This node was removed from the cluster; it will no longer campaign")
	case selfIn && n.removed.Load():
		n.setRemoved(false)
		n.logger.Info("This node was added back to the cluster")
	}
	n.persistMembers(base)
	n.logger.Info("Membership change committed",
		"index", index, "type", e.Type, "peer", e.Peer.ID, "members", configurationIDs(base))
}

// AddPeer adds a member through a single-server membership change and waits
// for it to commit. Only the leader can add members.
func (n *Node) AddPeer(peer core.RaftPeer) error {
	if !n.IsLeader() {
		return &core.RaftError{Code: core.ErrNotLeader, Message: "only leader can add peers", NodeID: n.Leader()}
	}
	if peer.ID == n.nodeID {
		return fmt.Errorf("cannot add self as peer")
	}
	if peer.ID == "" {
		return fmt.Errorf("cannot add a peer without an ID")
	}
	if peer.Role == "" {
		peer.Role = core.RoleVoter
	}
	n.peerMu.RLock()
	_, exists := n.peers[peer.ID]
	n.peerMu.RUnlock()
	if exists {
		return fmt.Errorf("peer %s already exists", peer.ID)
	}
	if err := n.proposeMembershipChange(core.MembershipChange{Type: core.MembershipAddPeer, Peer: peer}); err != nil {
		return fmt.Errorf("failed to propose membership change: %w", err)
	}
	n.logger.Info("Peer added", "peer_id", peer.ID, "address", peer.Address)
	return nil
}

// RemovePeer removes a member through a single-server membership change and
// waits for it to commit. Only the leader can remove members.
func (n *Node) RemovePeer(peerID string) error {
	if !n.IsLeader() {
		return &core.RaftError{Code: core.ErrNotLeader, Message: "only leader can remove peers", NodeID: n.Leader()}
	}
	if peerID == n.nodeID {
		return fmt.Errorf("cannot remove self")
	}
	n.peerMu.RLock()
	peer, exists := n.peers[peerID]
	var target core.RaftPeer
	if exists {
		target = core.RaftPeer{ID: peer.ID, Address: peer.Address, Region: peer.Region, Role: peer.Role}
	}
	n.peerMu.RUnlock()
	if !exists {
		return fmt.Errorf("peer %s not found", peerID)
	}
	if err := n.proposeMembershipChange(core.MembershipChange{Type: core.MembershipRemovePeer, Peer: target}); err != nil {
		return fmt.Errorf("failed to propose membership change: %w", err)
	}
	n.logger.Info("Peer removed", "peer_id", peerID)
	return nil
}

// proposeMembershipChange appends a single-server membership change on the
// leader, puts it into effect, and waits for it to commit. It refuses while
// another change is uncommitted, and waits for the leader's own no-op to
// commit first.
func (n *Node) proposeMembershipChange(change core.MembershipChange) error {
	deadline := time.Now().Add(membershipChangeTimeout)
	for {
		n.mu.Lock()
		if n.state.Load().(core.RaftState) != core.StateLeader {
			leader := n.leaderID
			n.mu.Unlock()
			return &core.RaftError{Code: core.ErrNotLeader, Message: "not leader", NodeID: leader}
		}
		if n.commitIndex >= n.leaderNoopIndex {
			break // n.mu stays held
		}
		n.mu.Unlock()
		if time.Now().After(deadline) {
			return &core.RaftError{Code: core.ErrTimeout, Message: "timeout waiting for the new leader to commit its first entry"}
		}
		select {
		case <-time.After(10 * time.Millisecond):
		case <-n.shutdownCh:
			return &core.RaftError{Code: core.ErrShutdown, Message: "node is shutting down"}
		}
	}

	n.membership.mu.RLock()
	pendingIndex := n.membership.latestIndex
	current := cloneMembers(n.membership.latest)
	n.membership.mu.RUnlock()
	if pendingIndex > n.commitIndex {
		n.mu.Unlock()
		return &core.RaftError{Code: ErrCodeMembershipChangePending,
			Message: fmt.Sprintf("membership change at index %d has not committed yet; retry once it has", pendingIndex)}
	}

	entry := membershipEntry{MembershipChange: change}
	entry.Phase = membershipPhaseSingle
	entry.Members = foldMembership(current, membershipEntry{MembershipChange: change})
	entry.OldConfig = configurationIDs(current)
	entry.NewConfig = configurationIDs(entry.Members)
	if len(entry.Members) == 0 {
		n.mu.Unlock()
		return fmt.Errorf("membership change would leave no members")
	}
	data, err := json.Marshal(entry)
	if err != nil {
		n.mu.Unlock()
		return fmt.Errorf("encode membership change: %w", err)
	}
	if err := n.appendEntry(core.RaftLogEntry{Term: n.currentTerm, Type: core.LogMembershipChange, Data: data}); err != nil {
		n.mu.Unlock()
		return fmt.Errorf("persist membership change: %w", err)
	}
	appended := n.log[len(n.log)-1]
	future := &applyFuture{index: appended.Index, term: appended.Term, done: make(chan struct{})}
	n.applyWaiters.Store(appended.Index, future)
	// The new configuration is in effect from now on, for this entry too.
	n.refreshMembershipLocked()
	n.mu.Unlock()

	n.logger.Info("Proposed membership change",
		"index", appended.Index, "type", change.Type, "peer", change.Peer.ID, "members", entry.NewConfig)
	n.replicateLog()

	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case <-future.done:
		return future.err
	case <-timer.C:
		return &core.RaftError{Code: core.ErrTimeout, Message: "timeout waiting for membership change to commit"}
	case <-n.shutdownCh:
		return &core.RaftError{Code: core.ErrShutdown, Message: "node is shutting down"}
	}
}
