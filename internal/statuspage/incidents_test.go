package statuspage

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// incidentPageRepo serves one public page with operator-posted incidents.
type incidentPageRepo struct {
	mockRepository
	page    *core.StatusPage
	windows []*core.MaintenanceWindow
}

func (r *incidentPageRepo) GetStatusPageBySlug(slug string) (*core.StatusPage, error) {
	cp := *r.page
	return &cp, nil
}

func (r *incidentPageRepo) GetStatusPageByDomain(string) (*core.StatusPage, error) {
	return nil, errNotFoundForTest
}

func (r *incidentPageRepo) ListMaintenanceWindows() ([]*core.MaintenanceWindow, error) {
	return r.windows, nil
}

type notFoundErr struct{}

func (notFoundErr) Error() string { return "not found" }

var errNotFoundForTest = notFoundErr{}

const xssPayload = `<script>alert("x")</script><img src=x onerror=alert(1)>`

func incidentPage(now time.Time) *core.StatusPage {
	resolvedAt := now.Add(-48 * time.Hour)
	return &core.StatusPage{
		ID: "page-1", Slug: "acme", Name: "Acme", WorkspaceID: "default", Enabled: true,
		Visibility: core.VisibilityPublic, Souls: []string{"soul-1", "soul-2", "soul-3"},
		Theme: core.GetDefaultTheme(), UptimeDays: 30,
		Incidents: []core.StatusIncident{
			{
				ID: "inc-active", Title: "API down " + xssPayload, Status: core.StatusIdentified, Impact: core.ImpactCritical,
				AffectedSouls: []string{"soul-1"}, StartedAt: now.Add(-time.Hour),
				Updates: []core.IncidentUpdate{
					{ID: "u1", Message: "Investigating " + xssPayload, Status: "investigating", UpdatedAt: now.Add(-time.Hour)},
					{ID: "u2", Message: "See https://example.com/postmortem?a=1&b=2. Thanks", Status: "identified", UpdatedAt: now.Add(-30 * time.Minute)},
				},
			},
			{
				ID: "inc-minor", Title: "Slow web", Status: core.StatusMonitoring, Impact: core.ImpactMinor,
				AffectedSouls: []string{"soul-2"}, StartedAt: now.Add(-2 * time.Hour),
			},
			{
				ID: "inc-old", Title: "Resolved last week", Status: core.StatusResolved, Impact: core.ImpactMajor,
				AffectedSouls: []string{"soul-3"}, StartedAt: now.Add(-72 * time.Hour), ResolvedAt: &resolvedAt,
			},
			{
				ID: "inc-ancient", Title: "Ancient history", Status: core.StatusResolved, Impact: core.ImpactMajor,
				StartedAt: now.Add(-200 * 24 * time.Hour),
			},
		},
		Announcements: []core.StatusAnnouncement{
			{ID: "a-up", Title: "Upgrade " + xssPayload, Message: "Planned " + xssPayload, StartsAt: now.Add(24 * time.Hour), EndsAt: now.Add(25 * time.Hour), AffectedSouls: []string{"soul-3"}},
			{ID: "a-now", Title: "Network work", StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour), AffectedSouls: []string{"soul-3"}},
			{ID: "a-done", Title: "Done work", StartsAt: now.Add(-10 * time.Hour), EndsAt: now.Add(-9 * time.Hour)},
		},
	}
}

func TestStatusPageIncidentData(t *testing.T) {
	now := time.Now().UTC()
	h := NewHandler(&incidentPageRepo{page: incidentPage(now)})
	data, err := h.buildStatusPageData(incidentPage(now))
	if err != nil {
		t.Fatal(err)
	}
	status := map[string]core.SoulStatusInfo{}
	for _, s := range data.Souls {
		status[s.ID] = s
	}
	if s := status["soul-1"]; s.Status != "dead" || s.IncidentImpact != "critical" {
		t.Errorf("critical incident soul = %+v", s)
	}
	if s := status["soul-2"]; s.Status != "degraded" || s.IncidentImpact != "minor" {
		t.Errorf("minor incident soul = %+v", s)
	}
	if s := status["soul-3"]; s.Status != string(core.SoulEmbalmed) {
		t.Errorf("in-maintenance soul = %+v (resolved incidents must not count)", s)
	}
	if data.Status.Status != "major_outage" {
		t.Errorf("overall = %s", data.Status.Status)
	}
	if len(data.Incidents) != 2 {
		t.Errorf("active incidents = %d", len(data.Incidents))
	}
	if len(data.IncidentHistory) != 3 || data.IncidentHistory[0].ID != "inc-active" {
		t.Errorf("history = %+v", data.IncidentHistory)
	}
	phases := map[string]string{}
	for _, a := range data.Maintenance {
		phases[a.ID] = a.Phase
	}
	if phases["a-up"] != core.AnnouncementUpcoming || phases["a-now"] != core.AnnouncementInProgress || phases["a-done"] != core.AnnouncementCompleted {
		t.Errorf("phases = %v", phases)
	}
}

func TestStatusPageIncidentImpactRaisesOverallWithoutComponents(t *testing.T) {
	now := time.Now().UTC()
	page := &core.StatusPage{ID: "p", Souls: []string{"soul-1"}, UptimeDays: 30, Incidents: []core.StatusIncident{
		{ID: "i", Title: "Billing delays", Status: core.StatusInvestigating, Impact: core.ImpactMinor, StartedAt: now},
	}}
	data, err := NewHandler(&mockRepository{}).buildStatusPageData(page)
	if err != nil {
		t.Fatal(err)
	}
	if data.Status.Status != "degraded" || data.Souls[0].Status != "alive" {
		t.Fatalf("overall = %s, soul = %s", data.Status.Status, data.Souls[0].Status)
	}
}

func TestStatusPageLinkedMaintenanceWindowMovesAnnouncement(t *testing.T) {
	now := time.Now().UTC()
	page := incidentPage(now)
	page.Announcements = []core.StatusAnnouncement{{ID: "a", Title: "Linked", MaintenanceWindowID: "mw",
		StartsAt: now.Add(24 * time.Hour), EndsAt: now.Add(25 * time.Hour)}}
	repo := &incidentPageRepo{page: page, windows: []*core.MaintenanceWindow{{ID: "mw", WorkspaceID: "default",
		StartTime: now.Add(-time.Minute), EndTime: now.Add(time.Hour), Enabled: true}}}
	data, err := NewHandler(repo).buildStatusPageData(page)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Maintenance) != 1 || data.Maintenance[0].Phase != core.AnnouncementInProgress {
		t.Fatalf("linked announcement = %+v", data.Maintenance)
	}
}

func servePage(t *testing.T, h *Handler, accept string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/status/acme", nil)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /status/acme = %d", w.Code)
	}
	return w.Body.String()
}

func TestStatusPageIncidentHTMLIsEscapedAndLinkified(t *testing.T) {
	now := time.Now().UTC()
	h := NewHandler(&incidentPageRepo{page: incidentPage(now)})
	body := servePage(t, h, "")

	for _, bad := range []string{"<script>alert", "<img src=x", `onerror=alert(1)>`} {
		if strings.Contains(body, bad) {
			t.Fatalf("unescaped markup %q in HTML", bad)
		}
	}
	for _, want := range []string{
		"&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt;",
		`<a href="https://example.com/postmortem?a=1&amp;b=2" rel="nofollow noopener noreferrer" target="_blank">https://example.com/postmortem?a=1&amp;b=2</a>. Thanks`,
		"Active Incidents", `active-incidents impact-critical`, "Scheduled Maintenance", "In progress", "Upcoming",
		"Incident History", "Resolved last week", "Done work",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("HTML lacks %q", want)
		}
	}
	if strings.Contains(body, "Ancient history") {
		t.Error("incident older than 90 days rendered")
	}
	// Newest update first in the timeline.
	if strings.Index(body, "postmortem") > strings.Index(body, "Investigating &lt;script") {
		t.Error("timeline is not newest-first")
	}
}

func TestStatusPageIncidentJSONIsEscaped(t *testing.T) {
	now := time.Now().UTC()
	h := NewHandler(&incidentPageRepo{page: incidentPage(now)})
	body := servePage(t, h, "application/json")
	if strings.Contains(body, "<script>") || strings.Contains(body, "<img") {
		t.Fatalf("raw markup in JSON: %s", body)
	}
	var data core.StatusPageData
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(data.Incidents[0].Title, "<script>") || len(data.IncidentHistory) != 3 || len(data.Maintenance) != 3 {
		t.Fatalf("JSON lost data: %+v", data)
	}
}

func TestStatusPageIncidentRSSIsEscaped(t *testing.T) {
	now := time.Now().UTC()
	h := NewHandler(&incidentPageRepo{page: incidentPage(now)})
	req := httptest.NewRequest(http.MethodGet, "/status/acme/feed.xml", nil)
	w := httptest.NewRecorder()
	h.RSSFeedHandler(w, req)
	body := w.Body.String()
	if strings.Contains(body, "<script>") || strings.Contains(body, "<img") {
		t.Fatalf("raw markup in RSS: %s", body)
	}
	for _, want := range []string{"API down &lt;script&gt;", "incident-inc-active-2", "Scheduled maintenance: Upgrade &lt;script&gt;", "maintenance-a-up"} {
		if !strings.Contains(body, want) {
			t.Errorf("RSS lacks %q", want)
		}
	}
	if strings.Contains(body, "Ancient history") {
		t.Error("RSS lists an incident older than 90 days")
	}
}

func TestLinkifyOnlyLinksHTTPURLs(t *testing.T) {
	cases := map[string]string{
		"javascript:alert(1)":         "javascript:alert(1)",
		"see http://a.example/x).":    `see <a href="http://a.example/x" rel="nofollow noopener noreferrer" target="_blank">http://a.example/x</a>).`,
		`https://x.example/"onmouse`:  `<a href="https://x.example/" rel="nofollow noopener noreferrer" target="_blank">https://x.example/</a>&#34;onmouse`,
		"line one\nline <two>":        "line one<br>line &lt;two&gt;",
		"data:text/html,<script>":     "data:text/html,&lt;script&gt;",
		"https://":                    "https://",
		"http://ok.example/a'b<c":     `<a href="http://ok.example/a" rel="nofollow noopener noreferrer" target="_blank">http://ok.example/a</a>&#39;b&lt;c`,
		"plain & simple":              "plain &amp; simple",
		"https://user@evil.example/p": `<a href="https://user@evil.example/p" rel="nofollow noopener noreferrer" target="_blank">https://user@evil.example/p</a>`,
	}
	for in, want := range cases {
		if got := linkify(in); got != want {
			t.Errorf("linkify(%q)\n got %s\nwant %s", in, got, want)
		}
	}
}

// Every operator change notifies confirmed subscribers once: creating an
// incident, each posted update, and the resolution; editing metadata or
// deleting sends nothing, and a new announcement is one notice.
func TestStatusPageIncidentNotificationsOncePerUpdate(t *testing.T) {
	h, repo, mailer := newSubscriptionHandler(t)
	confirmedSubscriber(t, h, mailer, "ops@example.com")
	subscribe(h, "pending@example.com", "192.0.2.77") // unconfirmed: never notified
	base := len(mailer.all())
	now := time.Now().UTC()

	p0 := *repo.page
	p1 := p0
	p1.Incidents = []core.StatusIncident{{ID: "i1", Title: "API", Status: core.StatusInvestigating, Impact: core.ImpactMajor,
		Updates: []core.IncidentUpdate{{ID: "u1", Message: "Looking", Status: "investigating", UpdatedAt: now}}}}
	p2 := p1
	p2.Incidents = []core.StatusIncident{p1.Incidents[0]}
	p2.Incidents[0].Updates = append(append([]core.IncidentUpdate(nil), p1.Incidents[0].Updates...),
		core.IncidentUpdate{ID: "u2", Message: "Still looking", Status: "investigating", UpdatedAt: now})
	p3 := p2
	p3.Incidents = []core.StatusIncident{p2.Incidents[0]}
	p3.Incidents[0].Title = "API errors" // metadata edit
	p3.Incidents[0].Impact = core.ImpactMinor
	p4 := p3
	p4.Incidents = []core.StatusIncident{p3.Incidents[0]}
	p4.Incidents[0].Status = core.StatusResolved
	p4.Incidents[0].Updates = append(append([]core.IncidentUpdate(nil), p3.Incidents[0].Updates...),
		core.IncidentUpdate{ID: "u3", Message: "Fixed", Status: "resolved", UpdatedAt: now})
	p5 := p4
	p5.Incidents = nil // deleted
	p6 := p5
	p6.Announcements = []core.StatusAnnouncement{{ID: "a1", Title: "DB upgrade", StartsAt: now.Add(time.Hour), EndsAt: now.Add(2 * time.Hour)}}
	p7 := p6
	p7.Announcements = []core.StatusAnnouncement{p6.Announcements[0]}
	p7.Announcements[0].Message = "edited"

	steps := []struct {
		before, after *core.StatusPage
		subject       string
	}{
		{&p0, &p1, "Incident: API"},
		{&p1, &p2, "Update: API"},
		{&p2, &p3, ""},
		{&p3, &p4, "Resolved: API errors"},
		{&p4, &p5, ""},
		{&p5, &p6, "Scheduled maintenance: DB upgrade"},
		{&p6, &p7, ""},
	}
	want := base
	for i, step := range steps {
		h.NotifyStatusPageChange(step.before, step.after)
		if step.subject != "" {
			want++
		}
		msgs := mailer.waitFor(want)
		time.Sleep(30 * time.Millisecond)
		msgs = mailer.all()
		if len(msgs) != want {
			t.Fatalf("step %d: %d messages, want %d", i, len(msgs)-base, want-base)
		}
		if step.subject != "" {
			last := msgs[len(msgs)-1]
			if last.To != "ops@example.com" || !strings.Contains(last.Subject, step.subject) {
				t.Fatalf("step %d: got %q to %s", i, last.Subject, last.To)
			}
		}
	}
}
