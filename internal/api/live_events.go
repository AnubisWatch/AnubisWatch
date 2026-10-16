package api

import (
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// judgmentDTO is a redacted judgment annotated with its soul's display name.
// The Judgments page and the live events feed search and label rows by
// soul_name; a judgment only stores the soul ID.
type judgmentDTO struct {
	*core.Judgment
	SoulName string `json:"soul_name,omitempty"`
}

// namedJudgmentsFromCore redacts judgments and attaches soul names from
// names (soul ID -> name), built once per request by the caller.
func namedJudgmentsFromCore(judgments []*core.Judgment, names map[string]string) []judgmentDTO {
	out := make([]judgmentDTO, 0, len(judgments))
	for _, judgment := range judgments {
		if judgment == nil {
			continue
		}
		out = append(out, judgmentDTO{
			Judgment: redactedJudgmentFromCore(judgment),
			SoulName: names[judgment.SoulID],
		})
	}
	return out
}

// soulNames maps soul ID to name for the given souls.
func soulNames(souls []*core.Soul) map[string]string {
	names := make(map[string]string, len(souls))
	for _, soul := range souls {
		if soul != nil {
			names[soul.ID] = soul.Name
		}
	}
	return names
}

// incidentSummary is the incident WebSocket payload. It carries the fields
// the dashboard reads (id, status, severity, soul) and leaves out the event
// history, whose embedded judgments can be large; clients refetch
// /api/v1/incidents for the full record.
type incidentSummary struct {
	ID              string              `json:"id"`
	RuleID          string              `json:"rule_id"`
	SoulID          string              `json:"soul_id"`
	SoulName        string              `json:"soul_name,omitempty"`
	WorkspaceID     string              `json:"workspace_id"`
	Status          core.IncidentStatus `json:"status"`
	Severity        core.Severity       `json:"severity"`
	StartedAt       time.Time           `json:"started_at"`
	AckedAt         *time.Time          `json:"acked_at,omitempty"`
	ResolvedAt      *time.Time          `json:"resolved_at,omitempty"`
	AckedBy         string              `json:"acked_by,omitempty"`
	ResolvedBy      string              `json:"resolved_by,omitempty"`
	EscalationLevel int                 `json:"escalation_level"`
}

func incidentSummaryFromCore(incident *core.Incident) incidentSummary {
	return incidentSummary{
		ID:              incident.ID,
		RuleID:          incident.RuleID,
		SoulID:          incident.SoulID,
		SoulName:        incident.SoulName,
		WorkspaceID:     incident.WorkspaceID,
		Status:          incident.Status,
		Severity:        incident.Severity,
		StartedAt:       incident.StartedAt,
		AckedAt:         incident.AckedAt,
		ResolvedAt:      incident.ResolvedAt,
		AckedBy:         incident.AckedBy,
		ResolvedBy:      incident.ResolvedBy,
		EscalationLevel: incident.EscalationLevel,
	}
}

// OnIncidentCallback returns the alert manager's incident hook: every
// incident lifecycle change is pushed to the incident's workspace as an
// "incident" message.
func (s *RESTServer) OnIncidentCallback() func(*core.Incident) {
	return func(incident *core.Incident) {
		if s.ws == nil || incident == nil {
			return
		}
		s.ws.broadcastTenantEvent(incident.WorkspaceID, "incident", WSMessage{
			Type:      "incident",
			Timestamp: time.Now().UTC(),
			Payload:   incidentSummaryFromCore(incident),
		})
	}
}

// OnAlertCallback returns the alert manager's dispatch hook: every alert
// sent to channels is pushed to its workspace as an "alert" message, with
// the embedded judgment redacted like REST responses.
func (s *RESTServer) OnAlertCallback() func(*core.AlertEvent) {
	return func(event *core.AlertEvent) {
		if s.ws == nil || event == nil {
			return
		}
		redacted := *event
		redacted.Judgment = redactedJudgmentFromCore(event.Judgment)
		s.ws.BroadcastAlert(&redacted)
	}
}

// OnSoulChangeCallback returns a hook for the storage layer: action is
// "created", "updated" or "deleted". Wiring it at storage covers every write
// path (REST, MCP, gRPC) with one call site.
func (s *RESTServer) OnSoulChangeCallback() func(soul *core.Soul, action string) {
	return func(soul *core.Soul, action string) {
		if s.ws == nil || soul == nil {
			return
		}
		view := redactedSoulMonitorResponse(soul, "unknown", nil, 0)
		if action != "deleted" {
			view = s.soulWithLatestJudgment(soul)
		}
		s.ws.broadcastSoulChange(soul.WorkspaceID, soulUpdatePayload{
			soulMonitorDTO: view,
			Action:         action,
		})
	}
}
