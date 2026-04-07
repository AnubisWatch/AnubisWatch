package api

import (
	"net/http"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// handleStatusPage returns public status page data (no auth required)
func (s *RESTServer) handleStatusPage(ctx *Context) error {
	// Get all souls
	souls, err := s.store.ListSoulsNoCtx("default", 0, 10000)
	if err != nil {
		return ctx.Error(http.StatusInternalServerError, "failed to fetch status")
	}

	// Build status page response
	response := StatusPageData{
		Name:        "System Status",
		Description: "AnubisWatch Monitoring Status Page",
		UpdatedAt:   time.Now().UTC(),
		Components:  make([]StatusComponent, 0, len(souls)),
	}

	// Calculate overall status
	overallStatus := "operational"
	operationalCount := 0

	for _, soul := range souls {
		// Get latest judgment
		judgments, _ := s.store.ListJudgmentsNoCtx(soul.ID, time.Now().Add(-24*time.Hour), time.Now(), 1)

		status := "unknown"
		if len(judgments) > 0 {
			switch judgments[0].Status {
			case core.SoulAlive:
				status = "operational"
				operationalCount++
			case core.SoulDead:
				status = "major_outage"
				overallStatus = "major_outage"
			case core.SoulDegraded:
				status = "degraded_performance"
				if overallStatus == "operational" {
					overallStatus = "degraded_performance"
				}
			}
		}

		component := StatusComponent{
			ID:          soul.ID,
			Name:        soul.Name,
			Status:      status,
			Type:        string(soul.Type),
			Target:      soul.Target,
			UpdatedAt:   soul.UpdatedAt,
			Description: soul.Target,
		}

		if len(judgments) > 0 {
			component.LastChecked = judgments[0].Timestamp
			component.Latency = judgments[0].Duration.String()
		}

		response.Components = append(response.Components, component)
	}

	response.Status = overallStatus
	response.OperationalCount = operationalCount
	response.TotalCount = len(souls)

	if len(souls) > 0 {
		response.UptimePercentage = float64(operationalCount) / float64(len(souls)) * 100
	}

	return ctx.JSON(http.StatusOK, response)
}

// StatusPageData represents the public status page response
type StatusPageData struct {
	Name             string            `json:"name"`
	Description      string            `json:"description"`
	Status           string            `json:"status"`
	UpdatedAt        time.Time         `json:"updated_at"`
	UptimePercentage float64           `json:"uptime_percentage"`
	OperationalCount int               `json:"operational_count"`
	TotalCount       int               `json:"total_count"`
	Components       []StatusComponent `json:"components"`
}

// StatusComponent represents a single component's status
type StatusComponent struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Type        string    `json:"type"`
	Target      string    `json:"target"`
	Description string    `json:"description"`
	UpdatedAt   time.Time `json:"updated_at"`
	LastChecked time.Time `json:"last_checked,omitempty"`
	Latency     string    `json:"latency,omitempty"`
}

// handlePublicStatus returns simple status for public access (no auth)
func (s *RESTServer) handlePublicStatus(ctx *Context) error {
	// Count souls by status
	souls, err := s.store.ListSoulsNoCtx("default", 0, 10000)
	if err != nil {
		return ctx.JSON(http.StatusOK, map[string]interface{}{
			"status":  "unknown",
			"message": "Unable to fetch status",
		})
	}

	operational := 0
	degraded := 0
	outage := 0

	for _, soul := range souls {
		judgments, _ := s.store.ListJudgmentsNoCtx(soul.ID, time.Now().Add(-1*time.Hour), time.Now(), 1)
		if len(judgments) == 0 {
			continue
		}

		switch judgments[0].Status {
		case core.SoulAlive:
			operational++
		case core.SoulDegraded:
			degraded++
		case core.SoulDead:
			outage++
		}
	}

	status := "operational"
	if outage > 0 {
		status = "major_outage"
	} else if degraded > 0 {
		status = "degraded"
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"status":      status,
		"operational": operational,
		"degraded":    degraded,
		"outage":      outage,
		"total":       len(souls),
		"updated_at":  time.Now().UTC(),
	})
}

// handleStatusPageHTML returns HTML status page
func (s *RESTServer) handleStatusPageHTML(ctx *Context) error {
	// Get status data
	souls, _ := s.store.ListSoulsNoCtx("default", 0, 10000)

	html := `<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>System Status - AnubisWatch</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: linear-gradient(135deg, #0a0a0a 0%, #1a1a2e 100%);
            color: #fff;
            min-height: 100vh;
            padding: 40px 20px;
        }
        .container {
            max-width: 900px;
            margin: 0 auto;
        }
        .header {
            text-align: center;
            margin-bottom: 40px;
        }
        .header h1 {
            font-size: 2.5em;
            margin-bottom: 10px;
            background: linear-gradient(90deg, #D4AF37, #F4D03F);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
        }
        .status-badge {
            display: inline-block;
            padding: 12px 30px;
            border-radius: 50px;
            font-size: 1.2em;
            font-weight: bold;
            margin: 20px 0;
        }
        .status-operational { background: #22C55E; }
        .status-degraded { background: #F59E0B; }
        .status-outage { background: #EF4444; }
        .components {
            background: rgba(255,255,255,0.05);
            border-radius: 16px;
            padding: 30px;
            backdrop-filter: blur(10px);
        }
        .component {
            display: flex;
            justify-content: space-between;
            align-items: center;
            padding: 20px;
            border-bottom: 1px solid rgba(255,255,255,0.1);
        }
        .component:last-child { border-bottom: none; }
        .component-name { font-size: 1.1em; font-weight: 500; }
        .component-target { font-size: 0.85em; color: #888; margin-top: 5px; }
        .component-status {
            display: flex;
            align-items: center;
            gap: 10px;
        }
        .status-dot {
            width: 12px;
            height: 12px;
            border-radius: 50%;
        }
        .dot-operational { background: #22C55E; box-shadow: 0 0 10px #22C55E; }
        .dot-degraded { background: #F59E0B; }
        .dot-outage { background: #EF4444; }
        .footer {
            text-align: center;
            margin-top: 40px;
            color: #666;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>⚖️ System Status</h1>`

	// Calculate overall status
	operational := 0
	outage := 0
	for _, soul := range souls {
		judgments, _ := s.store.ListJudgmentsNoCtx(soul.ID, time.Now().Add(-1*time.Hour), time.Now(), 1)
		if len(judgments) > 0 {
			if judgments[0].Status == core.SoulAlive {
				operational++
			} else if judgments[0].Status == core.SoulDead {
				outage++
			}
		}
	}

	statusClass := "status-operational"
	statusText := "All Systems Operational"
	if outage > 0 {
		statusClass = "status-outage"
		statusText = "Major System Outage"
	} else if operational < len(souls) {
		statusClass = "status-degraded"
		statusText = "Degraded Performance"
	}

	html += `<div class="status-badge ` + statusClass + `">` + statusText + `</div>
        </div>
        <div class="components">`

	for _, soul := range souls {
		judgments, _ := s.store.ListJudgmentsNoCtx(soul.ID, time.Now().Add(-24*time.Hour), time.Now(), 1)

		dotClass := "dot-outage"
		statusText := "Unknown"
		if len(judgments) > 0 {
			switch judgments[0].Status {
			case core.SoulAlive:
				dotClass = "dot-operational"
				statusText = "Operational"
			case core.SoulDegraded:
				dotClass = "dot-degraded"
				statusText = "Degraded"
			case core.SoulDead:
				dotClass = "dot-outage"
				statusText = "Major Outage"
			}
		}

		html += `<div class="component">
            <div>
                <div class="component-name">` + soul.Name + `</div>
                <div class="component-target">` + soul.Target + `</div>
            </div>
            <div class="component-status">
                <div class="status-dot ` + dotClass + `"></div>
                <span>` + statusText + `</span>
            </div>
        </div>`
	}

	html += `</div>
        <div class="footer">
            <p>Powered by AnubisWatch - The Judgment Never Sleeps</p>
            <p>Last updated: ` + time.Now().Format(time.RFC3339) + `</p>
        </div>
    </div>
</body>
</html>`

	ctx.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
	ctx.Response.Write([]byte(html))
	return nil
}
