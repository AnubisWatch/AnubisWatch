package api

import (
	"context"
	"net/http"
	"sort"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// ProbeVoteSource reads the secondary probers' votes for a soul. It is only
// available where redundant probing can run; without it the locations answer
// lists the latest judgment alone.
type ProbeVoteSource interface {
	ListProbeVotes(ctx context.Context, workspaceID, soulID string) ([]core.ProbeVote, error)
}

// SetProbeVotes enables the per-location breakdown of redundant probing.
func (s *RESTServer) SetProbeVotes(src ProbeVoteSource) { s.probeVotes = src }

// voteStaleAfterIntervals mirrors cmd/anubis voteFreshness: a vote older than
// this many check intervals no longer counts towards the quorum.
const voteStaleAfterIntervals = 2

// SoulLocation is one prober's latest result for a soul.
type SoulLocation struct {
	JackalID  string          `json:"jackal_id"`
	Region    string          `json:"region,omitempty"`
	Status    core.SoulStatus `json:"status"`
	LatencyMs int64           `json:"latency_ms"`
	CheckedAt time.Time       `json:"checked_at"`
	// Primary marks the prober whose judgment is stored (the quorum result).
	Primary bool `json:"primary"`
	// Stale means the result is older than two check intervals, so the
	// quorum no longer counts it.
	Stale bool `json:"stale"`
}

// SoulLocationsResponse is the body of GET /api/v1/souls/:id/locations.
type SoulLocationsResponse struct {
	SoulID    string         `json:"soul_id"`
	Locations []SoulLocation `json:"locations"`
}

// handleSoulLocations lists every prober's latest result for a soul, primary
// first. With a single prober it holds that prober alone.
func (s *RESTServer) handleSoulLocations(ctx *Context) error {
	soul, err := s.store.GetSoulNoCtx(ctx.Params["id"])
	if err != nil || soul == nil {
		return ctx.Error(http.StatusNotFound, "soul not found")
	}
	if soul.WorkspaceID != ctx.Workspace {
		return ctx.Error(http.StatusForbidden, "access denied: soul belongs to another workspace")
	}

	now := time.Now()
	staleAfter := time.Duration(voteStaleAfterIntervals) * soul.Weight.Duration
	stale := func(at time.Time) bool { return staleAfter > 0 && now.Sub(at) > staleAfter }

	locations := []SoulLocation{}
	seen := map[string]bool{}
	judgments, err := s.store.ListJudgmentsNoCtx(soul.ID, time.Time{}, now.Add(time.Hour), 1)
	if err == nil && len(judgments) > 0 && judgments[0] != nil {
		j := judgments[0]
		seen[j.JackalID] = true
		locations = append(locations, SoulLocation{
			JackalID:  j.JackalID,
			Region:    j.Region,
			Status:    j.Status,
			LatencyMs: j.Duration.Milliseconds(),
			CheckedAt: j.Timestamp,
			Primary:   true,
			Stale:     stale(j.Timestamp),
		})
	}

	if s.probeVotes != nil {
		votes, err := s.probeVotes.ListProbeVotes(ctx.Request.Context(), soul.WorkspaceID, soul.ID)
		if err != nil {
			return s.internalError(ctx, err, "failed to read probe votes")
		}
		sort.Slice(votes, func(a, b int) bool { return votes[a].JackalID < votes[b].JackalID })
		for _, v := range votes {
			if seen[v.JackalID] {
				continue
			}
			seen[v.JackalID] = true
			locations = append(locations, SoulLocation{
				JackalID:  v.JackalID,
				Region:    v.Region,
				Status:    v.Status,
				LatencyMs: v.Latency.Milliseconds(),
				CheckedAt: v.Timestamp,
				Stale:     stale(v.Timestamp),
			})
		}
	}
	return ctx.JSON(http.StatusOK, SoulLocationsResponse{SoulID: soul.ID, Locations: locations})
}
