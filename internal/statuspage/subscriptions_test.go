package statuspage

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/mail"
	"github.com/AnubisWatch/anubiswatch/internal/mail/mailtest"
	"github.com/AnubisWatch/anubiswatch/internal/probe"
)

const testPublicURL = "https://status.example.com"

// recordingMailer captures queued messages.
type recordingMailer struct {
	mu     sync.Mutex
	msgs   []mail.Message
	notify chan struct{}
}

func newRecordingMailer() *recordingMailer {
	return &recordingMailer{notify: make(chan struct{}, 100)}
}

func (m *recordingMailer) Enqueue(msg mail.Message) bool {
	m.mu.Lock()
	m.msgs = append(m.msgs, msg)
	m.mu.Unlock()
	select {
	case m.notify <- struct{}{}:
	default:
	}
	return true
}

func (m *recordingMailer) all() []mail.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]mail.Message(nil), m.msgs...)
}

func (m *recordingMailer) waitFor(n int) []mail.Message {
	deadline := time.After(2 * time.Second)
	for {
		if msgs := m.all(); len(msgs) >= n {
			return msgs
		}
		select {
		case <-m.notify:
		case <-deadline:
			return m.all()
		}
	}
}

func enableTestSubscriptions(h *Handler) *Handler {
	return h.EnableSubscriptions(SubscriptionOptions{
		Mailer:    newRecordingMailer(),
		PublicURL: testPublicURL,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

// memRepo is an in-memory repository with one published page.
type memRepo struct {
	mockRepository
	mu    sync.Mutex
	page  *core.StatusPage
	subs  map[string]*core.StatusPageSubscription
	other *core.StatusPage
}

func newMemRepo() *memRepo {
	return &memRepo{
		page: &core.StatusPage{
			ID: "page-1", Slug: "acme", Name: "Acme", WorkspaceID: "default",
			Visibility: core.VisibilityPublic, Enabled: true, Souls: []string{"soul-1"},
			Theme: core.GetDefaultTheme(), UptimeDays: 30,
		},
		other: &core.StatusPage{
			ID: "page-2", Slug: "other", Name: "Other", WorkspaceID: "default",
			Visibility: core.VisibilityPublic, Enabled: true, Souls: []string{"soul-9"},
		},
		subs: make(map[string]*core.StatusPageSubscription),
	}
}

func (r *memRepo) GetStatusPageBySlug(slug string) (*core.StatusPage, error) {
	if slug == r.page.Slug {
		cp := *r.page
		return &cp, nil
	}
	return nil, fmt.Errorf("not found")
}

func (r *memRepo) GetStatusPageByDomain(string) (*core.StatusPage, error) {
	return nil, fmt.Errorf("not found")
}

func (r *memRepo) GetStatusPageByID(id string) (*core.StatusPage, error) {
	if id == r.page.ID {
		cp := *r.page
		return &cp, nil
	}
	return nil, fmt.Errorf("not found")
}

func (r *memRepo) ListStatusPages() ([]*core.StatusPage, error) {
	a, b := *r.page, *r.other
	return []*core.StatusPage{&a, &b}, nil
}

func (r *memRepo) SaveSubscription(sub *core.StatusPageSubscription) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *sub
	r.subs[sub.ID] = &cp
	return nil
}

func (r *memRepo) GetSubscriptionsByPage(pageID string) ([]*core.StatusPageSubscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*core.StatusPageSubscription
	for _, sub := range r.subs {
		if sub.PageID == pageID {
			cp := *sub
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *memRepo) DeleteSubscription(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.subs, id)
	return nil
}

func (r *memRepo) byEmail(email string) *core.StatusPageSubscription {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, sub := range r.subs {
		if sub.Email == email {
			cp := *sub
			return &cp
		}
	}
	return nil
}

func newSubscriptionHandler(t *testing.T) (*Handler, *memRepo, *recordingMailer) {
	t.Helper()
	repo := newMemRepo()
	mailer := newRecordingMailer()
	h := NewHandler(repo).EnableSubscriptions(SubscriptionOptions{
		Mailer:    mailer,
		PublicURL: testPublicURL + "/",
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return h, repo, mailer
}

func subscribe(h *Handler, email, ip string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"page_id": "page-1", "email": email})
	req := httptest.NewRequest(http.MethodPost, "/status/subscribe", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = ip + ":1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

var linkPattern = regexp.MustCompile(`https://status\.example\.com/status/subscriptions/(confirm|unsubscribe)\?[^\s"<>]+`)

func findLink(t *testing.T, text, kind string) *url.URL {
	t.Helper()
	for _, m := range linkPattern.FindAllStringSubmatch(text, -1) {
		if m[1] == kind {
			u, err := url.Parse(m[0])
			if err != nil {
				t.Fatal(err)
			}
			return u
		}
	}
	t.Fatalf("no %s link in:\n%s", kind, text)
	return nil
}

// follow requests a link's path and query (and, for POST, the same params).
func follow(h *Handler, method string, u *url.URL) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, u.RequestURI(), nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestSubscriptionDoubleOptIn(t *testing.T) {
	h, repo, mailer := newSubscriptionHandler(t)

	w := subscribe(h, "User@Example.com", "203.0.113.1")
	if w.Code != http.StatusAccepted {
		t.Fatalf("subscribe = %d %s", w.Code, w.Body.String())
	}
	sub := repo.byEmail("user@example.com")
	if sub == nil || sub.Confirmed || sub.ConfirmTokenHash == "" || sub.UnsubscribeToken == "" {
		t.Fatalf("stored subscription = %+v", sub)
	}
	msgs := mailer.all()
	if len(msgs) != 1 || msgs[0].To != "user@example.com" || msgs[0].Kind != "subscription_confirm" {
		t.Fatalf("confirmation mail = %+v", msgs)
	}
	if msgs[0].Headers["List-Unsubscribe"] == "" || msgs[0].Headers["List-Unsubscribe-Post"] != "List-Unsubscribe=One-Click" {
		t.Errorf("confirmation lacks List-Unsubscribe headers: %v", msgs[0].Headers)
	}
	confirm := findLink(t, msgs[0].Text, "confirm")
	token := confirm.Query().Get("token")
	if strings.Contains(sub.ConfirmTokenHash, token) || len(token) < 40 {
		t.Fatalf("token stored in the clear or too short (%d chars)", len(token))
	}

	// GET only renders a button: link scanners must not confirm.
	if w := follow(h, http.MethodGet, confirm); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "<form") {
		t.Fatalf("GET confirm = %d %s", w.Code, w.Body.String())
	}
	if repo.byEmail("user@example.com").Confirmed {
		t.Fatal("GET confirmed the subscription")
	}

	// A wrong token is refused.
	bad := *confirm
	q := bad.Query()
	q.Set("token", token[:len(token)-2]+"xx")
	bad.RawQuery = q.Encode()
	if w := follow(h, http.MethodPost, &bad); w.Code != http.StatusBadRequest {
		t.Fatalf("wrong token = %d", w.Code)
	}

	if w := follow(h, http.MethodPost, confirm); w.Code != http.StatusOK {
		t.Fatalf("POST confirm = %d %s", w.Code, w.Body.String())
	}
	got := repo.byEmail("user@example.com")
	if !got.Confirmed || got.ConfirmedAt == nil || got.ConfirmTokenHash != "" {
		t.Fatalf("after confirm = %+v", got)
	}
	// Single use.
	if w := follow(h, http.MethodPost, confirm); w.Code != http.StatusBadRequest {
		t.Fatalf("reused token = %d", w.Code)
	}

}

func TestSubscriptionResponsesDoNotRevealMembership(t *testing.T) {
	h, _, mailer := newSubscriptionHandler(t)
	first := subscribe(h, "a@example.com", "198.51.100.1")
	confirm := findLink(t, mailer.all()[0].Text, "confirm")
	follow(h, http.MethodPost, confirm)

	existing := subscribe(h, "a@example.com", "198.51.100.2")
	fresh := subscribe(h, "b@example.com", "198.51.100.3")
	if existing.Code != fresh.Code || existing.Body.String() != fresh.Body.String() || first.Body.String() != fresh.Body.String() {
		t.Fatalf("responses differ:\n%d %s\n%d %s", existing.Code, existing.Body, fresh.Code, fresh.Body)
	}
	for _, m := range mailer.all()[1:] {
		if m.To == "a@example.com" {
			t.Fatal("confirmed address was mailed again")
		}
	}
}

func TestSubscriptionExpiredToken(t *testing.T) {
	h, repo, mailer := newSubscriptionHandler(t)
	subscribe(h, "late@example.com", "192.0.2.1")
	confirm := findLink(t, mailer.all()[0].Text, "confirm")
	h.subs.now = func() time.Time { return time.Now().Add(confirmTokenTTL + time.Hour) }
	if w := follow(h, http.MethodPost, confirm); w.Code != http.StatusBadRequest {
		t.Fatalf("expired token = %d", w.Code)
	}
	if repo.byEmail("late@example.com").Confirmed {
		t.Fatal("expired token confirmed")
	}
	// A later subscribe drops the expired request and starts over.
	subscribe(h, "other@example.com", "192.0.2.2")
	if repo.byEmail("late@example.com") != nil {
		t.Fatal("expired pending subscription was not cleaned up")
	}
}

func TestSubscriptionRateLimits(t *testing.T) {
	h, _, mailer := newSubscriptionHandler(t)
	for i := 0; i < addressConfirmLimit+2; i++ {
		if w := subscribe(h, "spam@example.com", fmt.Sprintf("192.0.2.%d", i+1)); w.Code != http.StatusAccepted {
			t.Fatalf("request %d = %d", i, w.Code)
		}
	}
	if n := len(mailer.all()); n != addressConfirmLimit {
		t.Fatalf("address got %d confirmation emails, want %d", n, addressConfirmLimit)
	}
	var last *httptest.ResponseRecorder
	for i := 0; i < ipSubscribeLimit+1; i++ {
		last = subscribe(h, fmt.Sprintf("u%d@example.com", i), "198.51.100.77")
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("IP limit not enforced: %d", last.Code)
	}
}

func TestSubscribeValidation(t *testing.T) {
	h, repo, _ := newSubscriptionHandler(t)
	for _, email := range []string{"", "not-an-email", "a@example.com\r\nBcc: x@example.com", "Name <a@example.com>", "a@b", strings.Repeat("a", 250) + "@example.com"} {
		if w := subscribe(h, email, "192.0.2.50"); w.Code != http.StatusBadRequest {
			t.Errorf("email %q = %d", email, w.Code)
		}
	}
	body := `{"page_id":"missing","email":"a@example.com"}`
	req := httptest.NewRequest(http.MethodPost, "/status/subscribe", strings.NewReader(body))
	req.RemoteAddr = "192.0.2.60:1"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown page = %d", w.Code)
	}
	if len(repo.subs) != 0 {
		t.Fatalf("invalid requests stored %d subscriptions", len(repo.subs))
	}
}

func TestSubscribeFormPostRendersHTML(t *testing.T) {
	h, repo, _ := newSubscriptionHandler(t)
	form := url.Values{"page_id": {"page-1"}, "email": {"form@example.com"}}
	req := httptest.NewRequest(http.MethodPost, "/status/subscribe", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "192.0.2.70:1"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted || !strings.Contains(w.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("form subscribe = %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	if repo.byEmail("form@example.com") == nil {
		t.Fatal("form subscription not stored")
	}
}

func TestUnsubscribe(t *testing.T) {
	h, repo, mailer := newSubscriptionHandler(t)
	subscribe(h, "bye@example.com", "192.0.2.80")
	msg := mailer.all()[0]
	follow(h, http.MethodPost, findLink(t, msg.Text, "confirm"))
	header := msg.Headers["List-Unsubscribe"]
	unsub, err := url.Parse(strings.Trim(header, "<>"))
	if err != nil || unsub.Path != unsubscribePath {
		t.Fatalf("List-Unsubscribe = %q", header)
	}
	if body := findLink(t, msg.Text, "unsubscribe"); body.String() != unsub.String() {
		t.Fatalf("body link %s != header link %s", body, unsub)
	}

	// Wrong token: refused, nothing removed.
	bad := *unsub
	q := bad.Query()
	q.Set("token", "forged")
	bad.RawQuery = q.Encode()
	if w := follow(h, http.MethodPost, &bad); w.Code != http.StatusBadRequest {
		t.Fatalf("forged unsubscribe = %d", w.Code)
	}
	// GET renders a button only.
	if w := follow(h, http.MethodGet, unsub); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "<form") {
		t.Fatalf("GET unsubscribe = %d", w.Code)
	}
	if repo.byEmail("bye@example.com") == nil {
		t.Fatal("removed before POST")
	}
	// RFC 8058 one-click POST.
	req := httptest.NewRequest(http.MethodPost, unsub.RequestURI(), strings.NewReader("List-Unsubscribe=One-Click"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK || repo.byEmail("bye@example.com") != nil {
		t.Fatalf("one-click unsubscribe = %d, still stored: %v", w.Code, repo.byEmail("bye@example.com") != nil)
	}
	// Idempotent.
	if w := follow(h, http.MethodPost, unsub); w.Code != http.StatusOK {
		t.Fatalf("repeat unsubscribe = %d", w.Code)
	}
}

func confirmedSubscriber(t *testing.T, h *Handler, mailer *recordingMailer, email string) {
	t.Helper()
	before := len(mailer.all())
	subscribe(h, email, "192.0.2.200")
	msgs := mailer.all()
	if len(msgs) != before+1 {
		t.Fatalf("no confirmation for %s", email)
	}
	if w := follow(h, http.MethodPost, findLink(t, msgs[len(msgs)-1].Text, "confirm")); w.Code != http.StatusOK {
		t.Fatalf("confirm %s = %d", email, w.Code)
	}
}

func TestIncidentFanOutReachesConfirmedSubscribersOnly(t *testing.T) {
	h, _, mailer := newSubscriptionHandler(t)
	confirmedSubscriber(t, h, mailer, "confirmed@example.com")
	subscribe(h, "pending@example.com", "192.0.2.201")
	base := len(mailer.all())

	now := time.Now().UTC()
	inc := &core.Incident{ID: "inc-1", SoulID: "soul-1", SoulName: "API", WorkspaceID: "default", Status: core.IncidentOpen, StartedAt: now}
	h.NotifyIncident(inc)
	msgs := mailer.waitFor(base + 1)
	if len(msgs) != base+1 {
		t.Fatalf("got %d notifications, want 1", len(msgs)-base)
	}
	got := msgs[base]
	if got.To != "confirmed@example.com" || !strings.Contains(got.Subject, "API") || got.Headers["List-Unsubscribe"] == "" {
		t.Fatalf("notification = %+v", got)
	}
	if !strings.Contains(got.Text, testPublicURL+"/status/acme") || !strings.Contains(got.Text, "/status/subscriptions/unsubscribe?") {
		t.Errorf("notification lacks page or unsubscribe link:\n%s", got.Text)
	}

	// Same status again (escalation, appended event, replication): nothing.
	h.NotifyIncident(inc)
	// Soul not on the page, another workspace, or a stale status: nothing.
	h.NotifyIncident(&core.Incident{ID: "inc-2", SoulID: "soul-x", Status: core.IncidentOpen, StartedAt: now})
	h.NotifyIncident(&core.Incident{ID: "inc-3", SoulID: "soul-1", WorkspaceID: "other", Status: core.IncidentOpen, StartedAt: now})
	h.NotifyIncident(&core.Incident{ID: "inc-4", SoulID: "soul-1", Status: core.IncidentOpen, StartedAt: now.Add(-time.Hour)})

	resolved := *inc
	resolved.Status = core.IncidentResolved
	resolved.ResolvedAt = &now
	h.NotifyIncident(&resolved)
	msgs = mailer.waitFor(base + 2)
	time.Sleep(50 * time.Millisecond)
	msgs = mailer.all()
	if len(msgs) != base+2 || !strings.HasPrefix(strings.SplitN(msgs[base+1].Subject, "] ", 2)[1], "Resolved") {
		var subjects []string
		for _, m := range msgs[base:] {
			subjects = append(subjects, m.Subject)
		}
		t.Fatalf("notifications after resolve = %v", subjects)
	}
}

func TestIncidentFanOutOnlyOnSendingNode(t *testing.T) {
	repo := newMemRepo()
	mailer := newRecordingMailer()
	leader := false
	var mu sync.Mutex
	h := NewHandler(repo).EnableSubscriptions(SubscriptionOptions{
		Mailer: mailer, PublicURL: testPublicURL, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		ShouldNotify: func() bool { mu.Lock(); defer mu.Unlock(); return leader },
	})
	confirmedSubscriber(t, h, mailer, "c@example.com")
	base := len(mailer.all())
	now := time.Now()
	h.NotifyIncident(&core.Incident{ID: "i", SoulID: "soul-1", Status: core.IncidentOpen, StartedAt: now})
	time.Sleep(50 * time.Millisecond)
	if n := len(mailer.all()) - base; n != 0 {
		t.Fatalf("follower sent %d notifications", n)
	}
	// After a leader change the new leader does not repeat the open notice.
	mu.Lock()
	leader = true
	mu.Unlock()
	h.NotifyIncident(&core.Incident{ID: "i", SoulID: "soul-1", Status: core.IncidentOpen, StartedAt: now})
	time.Sleep(50 * time.Millisecond)
	if n := len(mailer.all()) - base; n != 0 {
		t.Fatalf("new leader repeated a notification (%d)", n)
	}
}

func TestStatusPageIncidentNotifications(t *testing.T) {
	h, repo, mailer := newSubscriptionHandler(t)
	confirmedSubscriber(t, h, mailer, "ops@example.com")
	base := len(mailer.all())

	before := *repo.page
	after := *repo.page
	after.Incidents = []core.StatusIncident{{ID: "si-1", Title: "Degraded API", Description: "Slow responses", Status: core.StatusInvestigating}}
	h.NotifyStatusPageChange(&before, &after)
	msgs := mailer.waitFor(base + 1)
	if len(msgs) != base+1 || !strings.Contains(msgs[base].Subject, "Incident: Degraded API") || !strings.Contains(msgs[base].Text, "Slow responses") {
		t.Fatalf("created notification = %+v", msgs[base:])
	}

	// No change: nothing.
	h.NotifyStatusPageChange(&after, &after)
	resolved := after
	resolved.Incidents = []core.StatusIncident{{ID: "si-1", Title: "Degraded API", Status: core.StatusResolved,
		Updates: []core.IncidentUpdate{{ID: "u1", Message: "Fixed by rollback", Status: "resolved"}}}}
	h.NotifyStatusPageChange(&after, &resolved)
	msgs = mailer.waitFor(base + 2)
	time.Sleep(50 * time.Millisecond)
	msgs = mailer.all()
	if len(msgs) != base+2 || !strings.Contains(msgs[base+1].Subject, "Resolved: Degraded API") || !strings.Contains(msgs[base+1].Text, "Fixed by rollback") {
		t.Fatalf("resolve notification = %+v", msgs[base:])
	}
}

func TestPageNameCannotInjectHeaders(t *testing.T) {
	h, repo, mailer := newSubscriptionHandler(t)
	repo.page.Name = "Acme\r\nBcc: attacker@example.com"
	subscribe(h, "x@example.com", "192.0.2.99")
	msgs := mailer.all()
	if len(msgs) != 1 || strings.ContainsAny(msgs[0].Subject, "\r\n") {
		t.Fatalf("subject = %q", msgs[0].Subject)
	}
}

func TestSubscriptionsUnavailableWithoutMail(t *testing.T) {
	repo := newMemRepo()
	h := NewHandler(repo)

	w := subscribe(h, "a@example.com", "192.0.2.1")
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "subscriptions unavailable") {
		t.Fatalf("subscribe without mail = %d %s", w.Code, w.Body.String())
	}
	if len(repo.subs) != 0 {
		t.Fatal("subscription stored without mail")
	}
	page := httptest.NewRecorder()
	h.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/status/acme", nil))
	if strings.Contains(page.Body.String(), "subscribe-section") {
		t.Fatal("page shows a subscribe form without mail")
	}
	js := httptest.NewRecorder()
	h.ServeHTTP(js, httptest.NewRequest(http.MethodGet, "/status/acme?format=json", nil))
	var data core.StatusPageData
	if err := json.Unmarshal(js.Body.Bytes(), &data); err != nil || data.SubscriptionsAvailable {
		t.Fatalf("json subscriptions_available = %v (%v)", data.SubscriptionsAvailable, err)
	}
	if w := follow(h, http.MethodGet, &url.URL{Path: confirmPath}); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("confirm without mail = %d", w.Code)
	}

	// With mail the page offers the form and the JSON says so.
	h2, _, _ := newSubscriptionHandler(t)
	page = httptest.NewRecorder()
	h2.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/status/acme", nil))
	if !strings.Contains(page.Body.String(), `action="/status/subscribe"`) || !strings.Contains(page.Body.String(), `value="page-1"`) {
		t.Fatal("page lacks the subscribe form")
	}
	js = httptest.NewRecorder()
	h2.ServeHTTP(js, httptest.NewRequest(http.MethodGet, "/status/acme?format=json", nil))
	if err := json.Unmarshal(js.Body.Bytes(), &data); err != nil || !data.SubscriptionsAvailable {
		t.Fatalf("json subscriptions_available = %v (%v)", data.SubscriptionsAvailable, err)
	}
}

// TestSubscriptionOverFakeSMTP runs the flow through the real mailer and
// delivery queue against an SMTP server on 127.0.0.1.
func TestSubscriptionOverFakeSMTP(t *testing.T) {
	previous := probe.DefaultValidator.Load()
	t.Cleanup(func() { probe.DefaultValidator.Store(previous) })
	if err := probe.ConfigureDefaultValidator(true, nil); err != nil {
		t.Fatal(err)
	}
	srv := mailtest.NewServer(t)
	mailer, err := mail.New(core.MailConfig{Host: srv.Host(), Port: srv.Port(), From: "Status <status@example.com>", TLS: core.MailTLSNone})
	if err != nil {
		t.Fatal(err)
	}
	queue := mail.NewQueue(mailer, slog.New(slog.NewTextHandler(io.Discard, nil)), mail.QueueOptions{RetryDelays: []time.Duration{10 * time.Millisecond}})
	defer queue.Close()
	repo := newMemRepo()
	h := NewHandler(repo).EnableSubscriptions(SubscriptionOptions{Mailer: queue, PublicURL: testPublicURL, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})

	srv.FailNext(1) // first delivery attempt fails transiently and is retried
	if w := subscribe(h, "smtp@example.com", "192.0.2.10"); w.Code != http.StatusAccepted {
		t.Fatalf("subscribe = %d", w.Code)
	}
	msgs := srv.WaitFor(1, 3*time.Second)
	if len(msgs) != 1 || msgs[0].To[0] != "smtp@example.com" {
		t.Fatalf("relay got %d messages", len(msgs))
	}
	if msgs[0].Header("List-Unsubscribe") == "" {
		t.Error("confirmation lacks List-Unsubscribe")
	}
	confirm := findLink(t, msgs[0].Body(), "confirm")
	if w := follow(h, http.MethodPost, confirm); w.Code != http.StatusOK {
		t.Fatalf("confirm = %d", w.Code)
	}
	h.NotifyIncident(&core.Incident{ID: "e2e", SoulID: "soul-1", SoulName: "Web", Status: core.IncidentOpen, StartedAt: time.Now()})
	msgs = srv.WaitFor(2, 3*time.Second)
	if len(msgs) != 2 || !strings.Contains(msgs[1].Header("Subject"), "Web") {
		t.Fatalf("incident mail not delivered: %d", len(msgs))
	}
}
