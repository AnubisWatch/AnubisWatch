package statuspage

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Operator-posted incidents and scheduled maintenance announcements on the
// public page. All of their text is plain text: it is HTML-escaped here and
// http(s) URLs become links (rel=nofollow noopener), so nothing an editor
// writes is ever interpreted as markup. The JSON view relies on
// encoding/json, which escapes <, > and & as well.

// announcementsWithWindows returns the page's announcements with the schedule
// of a linked one-off maintenance window, so moving the window moves the
// announcement too.
func (h *Handler) announcementsWithWindows(page *core.StatusPage) []core.StatusAnnouncement {
	out := append([]core.StatusAnnouncement(nil), page.Announcements...)
	linked := false
	for _, a := range out {
		if a.MaintenanceWindowID != "" {
			linked = true
			break
		}
	}
	if !linked {
		return out
	}
	source, ok := h.repository.(MaintenanceSource)
	if !ok {
		return out
	}
	windows, err := source.ListMaintenanceWindows()
	if err != nil {
		return out
	}
	byID := make(map[string]*core.MaintenanceWindow, len(windows))
	for _, w := range windows {
		if w != nil && w.Recurring == "" && sameWorkspace(w.WorkspaceID, page.WorkspaceID) && w.EndTime.After(w.StartTime) {
			byID[w.ID] = w
		}
	}
	for i := range out {
		if w, ok := byID[out[i].MaintenanceWindowID]; ok {
			out[i].StartsAt, out[i].EndsAt = w.StartTime, w.EndTime
		}
	}
	return out
}

// applyIncidentImpact makes component status reflect the operator's word:
// souls named by in-progress maintenance show as under maintenance, and souls
// named by an active incident show at least the incident's impact (minor:
// degraded; major or critical: down). Only incidents with an explicit impact
// change component status.
func applyIncidentImpact(souls []core.SoulStatusInfo, incidents []core.StatusIncident, announcements []core.StatusAnnouncement, now time.Time) {
	maintenance := make(map[string]bool)
	for _, a := range announcements {
		if a.PhaseAt(now) == core.AnnouncementInProgress {
			for _, id := range a.AffectedSouls {
				maintenance[id] = true
			}
		}
	}
	worst := make(map[string]core.IncidentImpact)
	for _, inc := range incidents {
		if !inc.IsActive() || inc.Impact == "" {
			continue
		}
		for _, id := range inc.AffectedSouls {
			if core.ImpactRank(inc.Impact) > core.ImpactRank(worst[id]) {
				worst[id] = inc.Impact
			}
		}
	}
	for i := range souls {
		s := &souls[i]
		if maintenance[s.ID] && (s.Status == string(core.SoulAlive) || s.Status == "unknown") {
			s.Status = string(core.SoulEmbalmed)
		}
		impact, ok := worst[s.ID]
		if !ok {
			continue
		}
		s.IncidentImpact = string(impact)
		switch impact {
		case core.ImpactMajor, core.ImpactCritical:
			s.Status = string(core.SoulDead)
		case core.ImpactMinor:
			if s.Status != string(core.SoulDead) {
				s.Status = string(core.SoulDegraded)
			}
		}
	}
}

// overallWithIncidents raises the overall status to the worst explicit impact
// of an active incident, so an incident that names no component still shows.
func overallWithIncidents(overall core.OverallStatus, incidents []core.StatusIncident) core.OverallStatus {
	worst := core.ImpactNone
	for _, inc := range incidents {
		if inc.IsActive() && core.ImpactRank(inc.Impact) > core.ImpactRank(worst) {
			worst = inc.Impact
		}
	}
	switch {
	case core.ImpactRank(worst) >= core.ImpactRank(core.ImpactMajor) && overall.Status != "major_outage":
		overall.Status = "major_outage"
		overall.Title = "Major Outage"
		overall.Description = "We are working on an active incident"
	case worst == core.ImpactMinor && overall.Status == "operational":
		overall.Status = "degraded"
		overall.Title = "Degraded Performance"
		overall.Description = "We are working on an active incident"
	}
	return overall
}

var plainTextURLPattern = regexp.MustCompile(`https?://[^\s<>"'` + "`" + `]+`)

// linkify escapes plain text for HTML, turns http(s) URLs into links and line
// breaks into <br>.
func linkify(text string) string {
	var b strings.Builder
	last := 0
	for _, m := range plainTextURLPattern.FindAllStringIndex(text, -1) {
		start, end := m[0], m[1]
		// Leave sentence punctuation after a URL outside the link.
		for end > start && strings.ContainsRune(".,;:!?)]}", rune(text[end-1])) {
			end--
		}
		raw := text[start:end]
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			continue
		}
		b.WriteString(html.EscapeString(text[last:start]))
		escaped := html.EscapeString(raw)
		b.WriteString(`<a href="` + escaped + `" rel="nofollow noopener noreferrer" target="_blank">` + escaped + `</a>`)
		last = end
	}
	b.WriteString(html.EscapeString(text[last:]))
	return strings.ReplaceAll(b.String(), "\n", "<br>")
}

func formatPageTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("Jan 2, 2006 15:04 UTC")
}

var impactLabels = map[core.IncidentImpact]string{
	core.ImpactNone:     "No impact",
	core.ImpactMinor:    "Minor",
	core.ImpactMajor:    "Major",
	core.ImpactCritical: "Critical",
}

var statusLabels = map[string]string{
	string(core.StatusInvestigating): "Investigating",
	string(core.StatusIdentified):    "Identified",
	string(core.StatusMonitoring):    "Monitoring",
	string(core.StatusResolved):      "Resolved",
	string(core.StatusOngoing):       "Ongoing",
}

func statusLabel(status string) string {
	if l, ok := statusLabels[status]; ok {
		return l
	}
	return status
}

func soulNameMap(data *core.StatusPageData) map[string]string {
	names := make(map[string]string, len(data.Souls))
	for _, s := range data.Souls {
		names[s.ID] = s.Name
	}
	return names
}

func affectedHTML(ids []string, names map[string]string) string {
	list := make([]string, 0, len(ids))
	for _, id := range ids {
		if name, ok := names[id]; ok {
			list = append(list, html.EscapeString(name))
		}
	}
	if len(list) == 0 {
		return ""
	}
	return `<div class="incident-affected">Affected: ` + strings.Join(list, ", ") + `</div>`
}

// incidentHTML renders one incident with its timeline, newest update first.
func incidentHTML(inc core.StatusIncident, names map[string]string) string {
	impact := inc.EffectiveImpact()
	var b strings.Builder
	fmt.Fprintf(&b, `
			<div class="incident impact-%s severity-%s">
				<h3>%s <span class="impact-badge">%s</span></h3>
				<div class="incident-meta">
					<span>Status: %s</span>
					<span>Started: %s</span>`,
		html.EscapeString(string(impact)), html.EscapeString(string(inc.Severity)),
		html.EscapeString(inc.Title), html.EscapeString(impactLabels[impact]),
		html.EscapeString(statusLabel(string(inc.Status))), formatPageTime(inc.StartedAt))
	if inc.ResolvedAt != nil {
		fmt.Fprintf(&b, `<span>Resolved: %s</span>`, formatPageTime(*inc.ResolvedAt))
	}
	b.WriteString(`</div>`)
	b.WriteString(affectedHTML(inc.AffectedSouls, names))
	if len(inc.Updates) == 0 {
		if inc.Description != "" {
			b.WriteString(`<p>` + linkify(inc.Description) + `</p>`)
		}
	} else {
		b.WriteString(`<ol class="incident-timeline">`)
		for i := len(inc.Updates) - 1; i >= 0; i-- {
			u := inc.Updates[i]
			fmt.Fprintf(&b, `<li><strong>%s</strong> <time>%s</time><p>%s</p></li>`,
				html.EscapeString(statusLabel(u.Status)), formatPageTime(u.UpdatedAt), linkify(u.Message))
		}
		b.WriteString(`</ol>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

var phaseLabels = map[string]string{
	core.AnnouncementUpcoming:   "Upcoming",
	core.AnnouncementInProgress: "In progress",
	core.AnnouncementCompleted:  "Completed",
}

func announcementHTML(a core.StatusAnnouncement, names map[string]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `
			<div class="announcement phase-%s">
				<h3>%s <span class="phase-badge">%s</span></h3>
				<div class="incident-meta"><span>%s &ndash; %s</span></div>`,
		html.EscapeString(a.Phase), html.EscapeString(a.Title), html.EscapeString(phaseLabels[a.Phase]),
		formatPageTime(a.StartsAt), formatPageTime(a.EndsAt))
	b.WriteString(affectedHTML(a.AffectedSouls, names))
	if a.Message != "" {
		b.WriteString(`<p>` + linkify(a.Message) + `</p>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

// renderActiveSections renders the active-incident banner and the scheduled
// maintenance that has not finished yet; both sit above the components.
func renderActiveSections(data *core.StatusPageData) string {
	names := soulNameMap(data)
	var b strings.Builder
	if len(data.Incidents) > 0 {
		worst := core.ImpactNone
		for _, inc := range data.Incidents {
			if core.ImpactRank(inc.EffectiveImpact()) > core.ImpactRank(worst) {
				worst = inc.EffectiveImpact()
			}
		}
		fmt.Fprintf(&b, `<div class="incidents-section active-incidents impact-%s"><h2>Active Incidents</h2>`, html.EscapeString(string(worst)))
		for _, inc := range data.Incidents {
			b.WriteString(incidentHTML(inc, names))
		}
		b.WriteString(`</div>`)
	}
	pending := make([]core.StatusAnnouncement, 0, len(data.Maintenance))
	for _, a := range data.Maintenance {
		if a.Phase != core.AnnouncementCompleted {
			pending = append(pending, a)
		}
	}
	if len(pending) > 0 {
		b.WriteString(`<div class="incidents-section maintenance-section"><h2>Scheduled Maintenance</h2>`)
		for _, a := range pending {
			b.WriteString(announcementHTML(a, names))
		}
		b.WriteString(`</div>`)
	}
	return b.String()
}

// renderHistorySection renders the last 90 days of incidents and completed
// maintenance.
func renderHistorySection(data *core.StatusPageData) string {
	names := soulNameMap(data)
	var b strings.Builder
	b.WriteString(`<div class="incidents-section history-section"><h2>Incident History</h2><p class="history-window">Last 90 days</p>`)
	if len(data.IncidentHistory) == 0 {
		b.WriteString(`<p class="history-empty">No incidents reported in the last 90 days.</p>`)
	}
	for _, inc := range data.IncidentHistory {
		b.WriteString(incidentHTML(inc, names))
	}
	for _, a := range data.Maintenance {
		if a.Phase == core.AnnouncementCompleted {
			b.WriteString(announcementHTML(a, names))
		}
	}
	b.WriteString(`</div>`)
	return b.String()
}

// rssPageItems renders the page's own incidents and announcements as RSS
// items. Every value is escaped for XML.
func rssPageItems(page *core.StatusPage, announcements []core.StatusAnnouncement, baseURL string, now time.Time) string {
	esc := html.EscapeString
	var b strings.Builder
	for _, inc := range core.PublicIncidentHistory(page.Incidents, now) {
		latest := inc.Description
		pub := inc.StartedAt
		if n := len(inc.Updates); n > 0 {
			latest = inc.Updates[n-1].Message
			pub = inc.Updates[n-1].UpdatedAt
		}
		fmt.Fprintf(&b, `    <item>
      <title>%s - %s</title>
      <description>%s</description>
      <pubDate>%s</pubDate>
      <link>%s</link>
      <guid isPermaLink="false">incident-%s-%d</guid>
    </item>
`, esc(inc.Title), esc(statusLabel(string(inc.Status))), esc(latest), pub.UTC().Format(time.RFC1123Z),
			esc(baseURL), esc(inc.ID), len(inc.Updates))
	}
	for _, a := range core.PublicAnnouncements(announcements, now) {
		desc := fmt.Sprintf("%s: %s - %s. %s", phaseLabels[a.Phase], formatPageTime(a.StartsAt), formatPageTime(a.EndsAt), a.Message)
		pub := a.CreatedAt
		if pub.IsZero() {
			pub = a.StartsAt
		}
		fmt.Fprintf(&b, `    <item>
      <title>Scheduled maintenance: %s</title>
      <description>%s</description>
      <pubDate>%s</pubDate>
      <link>%s</link>
      <guid isPermaLink="false">maintenance-%s</guid>
    </item>
`, esc(a.Title), esc(strings.TrimSpace(desc)), pub.UTC().Format(time.RFC1123Z), esc(baseURL), esc(a.ID))
	}
	return b.String()
}
