package statuspage

import (
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/mail"
)

// update is one notification-worthy change, rendered per subscriber.
type update struct {
	subject string
	lines   []string // plain-text paragraphs; HTML-escaped for the HTML part
}

// NotifyIncident is the alert manager's incident hook. It emails the
// confirmed subscribers of every published page that shows the incident's
// soul when the incident opens, is acknowledged or resolves.
//
// It never blocks: the fan-out runs on a background worker and delivery on
// the mail queue. The hook fires on every node (locally, or as the incident
// replicates), so in a cluster only the node ShouldNotify selects (the
// leader) sends; every node still records the status so a new leader does
// not repeat a notification already sent. Escalations and appended events
// keep the status and send nothing, and a status older than the recency
// window (log replay after a restart) is never mailed.
func (h *Handler) NotifyIncident(incident *core.Incident) {
	s := h.subs
	if s == nil || incident == nil || incident.ID == "" {
		return
	}
	status := string(incident.Status)
	s.seenMu.Lock()
	prev, seen := s.seen[incident.ID]
	if len(s.seen) >= maxTrackedIncidentStates {
		for id, st := range s.seen {
			if st == string(core.IncidentResolved) {
				delete(s.seen, id)
			}
		}
	}
	s.seen[incident.ID] = status
	s.seenMu.Unlock()
	if seen && prev == status {
		return
	}

	var at time.Time
	var u update
	soul := incident.SoulName
	if soul == "" {
		soul = "A monitored service"
	}
	switch incident.Status {
	case core.IncidentOpen:
		at = incident.StartedAt
		u = update{
			subject: soul + " is experiencing issues",
			lines:   []string{soul + " is experiencing issues. We are looking into it.", "Started: " + at.UTC().Format(time.RFC1123)},
		}
	case core.IncidentAcked:
		if incident.AckedAt != nil {
			at = *incident.AckedAt
		}
		u = update{
			subject: "Investigating: " + soul,
			lines:   []string{"The team is investigating the issue affecting " + soul + "."},
		}
	case core.IncidentResolved:
		if incident.ResolvedAt != nil {
			at = *incident.ResolvedAt
		}
		u = update{
			subject: "Resolved: " + soul,
			lines:   []string{"The issue affecting " + soul + " has been resolved.", "Resolved: " + at.UTC().Format(time.RFC1123)},
		}
	default:
		return
	}
	if at.IsZero() || s.now().Sub(at) > notifyRecencyWindow {
		return
	}
	if s.shouldNotify != nil && !s.shouldNotify() {
		return
	}
	soulID, workspace := incident.SoulID, incident.WorkspaceID
	s.submit(func() {
		lister, ok := h.repository.(PageLister)
		if !ok {
			return
		}
		pages, err := lister.ListStatusPages()
		if err != nil {
			s.logger.Warn("incident notification: list status pages failed", "error", err)
			return
		}
		for _, page := range pages {
			if page == nil || !page.Enabled || !sameWorkspace(page.WorkspaceID, workspace) || !pageShowsSoul(page, soulID) {
				continue
			}
			s.sendToSubscribers(page, u)
		}
	})
}

// NotifyStatusPageChange emails subscribers about incidents an operator
// posted, updated or resolved on a status page. previous is the page before
// the save (nil when it was created). It runs on the node that saved the
// page, so a cluster sends once.
func (h *Handler) NotifyStatusPageChange(previous, current *core.StatusPage) {
	s := h.subs
	if s == nil || current == nil || !current.Enabled {
		return
	}
	old := make(map[string]core.StatusIncident)
	if previous != nil {
		for _, inc := range previous.Incidents {
			old[inc.ID] = inc
		}
	}
	var updates []update
	for _, inc := range current.Incidents {
		before, existed := old[inc.ID]
		title := inc.Title
		if title == "" {
			title = "Incident"
		}
		latest := inc.Description
		if n := len(inc.Updates); n > 0 && inc.Updates[n-1].Message != "" {
			latest = inc.Updates[n-1].Message
		}
		status := string(inc.Status)
		switch {
		case !existed:
			if inc.Status == core.StatusResolved {
				continue
			}
			updates = append(updates, update{
				subject: "Incident: " + title,
				lines:   []string{title, "Status: " + status, latest},
			})
		case before.Status != inc.Status || len(inc.Updates) > len(before.Updates):
			subject := "Update: " + title
			if inc.Status == core.StatusResolved {
				subject = "Resolved: " + title
			}
			updates = append(updates, update{
				subject: subject,
				lines:   []string{title, "Status: " + status, latest},
			})
		}
	}
	// A newly announced maintenance that has not already finished is one
	// notification; later edits to it send nothing.
	announced := make(map[string]bool)
	if previous != nil {
		for _, a := range previous.Announcements {
			announced[a.ID] = true
		}
	}
	for _, a := range current.Announcements {
		if announced[a.ID] || !a.EndsAt.After(s.now()) {
			continue
		}
		lines := []string{a.Title,
			"Scheduled: " + a.StartsAt.UTC().Format(time.RFC1123) + " to " + a.EndsAt.UTC().Format(time.RFC1123),
			a.Message}
		updates = append(updates, update{subject: "Scheduled maintenance: " + a.Title, lines: lines})
	}
	if len(updates) == 0 {
		return
	}
	page := *current
	s.submit(func() {
		for _, u := range updates {
			s.sendToSubscribers(&page, u)
		}
	})
}

// submit queues a fan-out job without blocking the caller.
func (s *subscriptions) submit(job func()) {
	select {
	case s.jobs <- job:
	default:
		s.logger.Warn("status page notification dropped: fan-out queue full")
	}
}

// sendToSubscribers emails u to every confirmed subscriber of page.
func (s *subscriptions) sendToSubscribers(page *core.StatusPage, u update) {
	subs, err := s.repo.GetSubscriptionsByPage(page.ID)
	if err != nil {
		s.logger.Warn("status page notification: list subscriptions failed", "page_id", page.ID, "error", err)
		return
	}
	link := s.pageURL(page)
	sent := 0
	for _, sub := range subs {
		if sub == nil || !sub.Confirmed || sub.Email == "" || (sub.Type != "" && sub.Type != "email") || sub.UnsubscribeToken == "" {
			continue
		}
		var text, body strings.Builder
		for _, line := range u.lines {
			if strings.TrimSpace(line) == "" {
				continue
			}
			text.WriteString(line + "\n\n")
			body.WriteString("<p>" + html.EscapeString(line) + "</p>")
		}
		text.WriteString("Current status: " + link)
		body.WriteString(`<p><a href="` + html.EscapeString(link) + `">View the status page</a></p>`)
		msg := mail.Message{
			To:      sub.Email,
			Subject: oneLine(fmt.Sprintf("[%s] %s", page.Name, u.subject)),
			Text:    text.String(),
			HTML:    body.String(),
			Kind:    "status_update",
		}
		if s.mailer.Enqueue(s.withUnsubscribe(msg, sub)) {
			sent++
		}
	}
	if sent > 0 {
		s.logger.Info("status page update queued", "page_id", page.ID, "recipients", sent)
	}
}

func pageShowsSoul(page *core.StatusPage, soulID string) bool {
	for _, id := range page.Souls {
		if id == soulID {
			return true
		}
	}
	for _, group := range page.Groups {
		for _, id := range group.SoulIDs {
			if id == soulID {
				return true
			}
		}
	}
	return false
}

func sameWorkspace(a, b string) bool {
	if a == "" {
		a = "default"
	}
	if b == "" {
		b = "default"
	}
	return a == b
}

// oneLine folds line breaks in user-supplied names so a subject stays one
// header line.
func oneLine(v string) string {
	return strings.Join(strings.Fields(v), " ")
}
