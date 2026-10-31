package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// With necropolis.distribution.redundancy above 1 several members probe each
// soul. Only the primary (first of the soul's owners) speaks for it: it folds
// the other probers' latest results into one judgment whose status is the
// quorum's, so storage, alert rules, the API and every other reader keep
// seeing a single judgment stream per soul. A secondary's result is only a
// vote, replicated as one small record per (soul, prober) that the next
// primary judgment reads.

// voteFreshness is how many intervals a vote counts for. Past it the prober is
// treated as silent and left out of the quorum, so a dead secondary cannot
// keep a soul degraded.
const voteFreshness = 2

// vote is a secondary prober's latest result for one soul.
type vote struct {
	JackalID  string          `json:"jackal_id"`
	Status    core.SoulStatus `json:"status"`
	Timestamp time.Time       `json:"timestamp"`
}

func voteKey(workspaceID, soulID, jackalID string) string {
	return fmt.Sprintf("%s/votes/%s/%s", workspaceOrDefault(workspaceID), soulID, jackalID)
}

// quorumStatus combines the primary's own status with the fresh votes of the
// other probers. A strict majority of dead is dead; some dead (or degraded)
// short of a majority is degraded; otherwise the soul is alive. With a single
// voter the result is that voter's status, so a primary whose peers are all
// silent behaves as it does without redundancy.
func quorumStatus(own core.SoulStatus, others []core.SoulStatus) core.SoulStatus {
	voters := 1 + len(others)
	dead, degraded := 0, 0
	for _, status := range append([]core.SoulStatus{own}, others...) {
		switch status {
		case core.SoulDead:
			dead++
		case core.SoulDegraded:
			degraded++
		}
	}
	switch {
	case dead*2 > voters:
		return core.SoulDead
	case dead > 0 || degraded > 0:
		return core.SoulDegraded
	}
	// Alive probers outvoting nothing keep the primary's own non-failing
	// status (alive, unknown, embalmed).
	return own
}

// voteBoard is what the consolidator needs from the cluster: who probes a
// soul, who this node is, and the replicated vote records.
type voteBoard interface {
	ownersOf(soulID string) []string
	selfID() string
	publishVote(key string, v vote) error
	readVote(key string) ([]byte, error)
}

// quorumConsolidator is the probe engine's consolidator in a redundant cluster.
type quorumConsolidator struct {
	board  voteBoard
	logger *slog.Logger
	now    func() time.Time
}

func newQuorumConsolidator(w *clusterWork) *quorumConsolidator {
	return &quorumConsolidator{board: w, logger: w.logger, now: time.Now}
}

// Consolidate implements probe.Consolidator.
func (c *quorumConsolidator) Consolidate(soul *core.Soul, j *core.Judgment) bool {
	owners := c.board.ownersOf(soul.ID)
	if len(owners) <= 1 {
		return true
	}
	self := c.board.selfID()
	if owners[0] != self {
		c.publishVote(soul, j)
		return false
	}

	freshFor := time.Duration(voteFreshness) * soul.Weight.Duration
	others := make([]core.SoulStatus, 0, len(owners)-1)
	for _, member := range owners[1:] {
		if v, ok := c.readVote(soul, member); ok && c.now().Sub(v.Timestamp) <= freshFor {
			others = append(others, v.Status)
		}
	}
	if status := quorumStatus(j.Status, others); status != j.Status {
		dead := 0
		for _, s := range others {
			if s == core.SoulDead {
				dead++
			}
		}
		if j.Status == core.SoulDead {
			dead++
		}
		j.Message = fmt.Sprintf("%s (%d of %d probers report dead)", j.Message, dead, 1+len(others))
		j.Status = status
	}
	return true
}

func (c *quorumConsolidator) publishVote(soul *core.Soul, j *core.Judgment) {
	v := vote{JackalID: c.board.selfID(), Status: j.Status, Timestamp: j.Timestamp}
	if err := c.board.publishVote(voteKey(soul.WorkspaceID, soul.ID, v.JackalID), v); err != nil {
		c.logger.Warn("failed to publish probe vote", "soul", soul.ID, "err", err)
	}
}

func (c *quorumConsolidator) readVote(soul *core.Soul, member string) (vote, bool) {
	data, err := c.board.readVote(voteKey(soul.WorkspaceID, soul.ID, member))
	if err != nil || len(data) == 0 {
		return vote{}, false
	}
	var v vote
	if json.Unmarshal(data, &v) != nil {
		return vote{}, false
	}
	return v, true
}

func (w *clusterWork) selfID() string { return w.mgr.NodeID() }

func (w *clusterWork) publishVote(key string, v vote) error { return w.replicateAsync(key, v) }

func (w *clusterWork) readVote(key string) ([]byte, error) { return w.store.Get(key) }

// ownersOf returns every member probing soulID at the last ownership pass,
// primary first, or nil when the soul is not planned.
func (w *clusterWork) ownersOf(soulID string) []string {
	w.ownedMu.RLock()
	defer w.ownedMu.RUnlock()
	return slices.Clone(w.owners[soulID])
}
