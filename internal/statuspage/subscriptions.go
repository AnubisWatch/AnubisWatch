package statuspage

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"mime"
	"net"
	"net/http"
	netmail "net/mail"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/mail"
)

// Subscription paths. They live under /status/ so the REST router hands them
// to this handler; the slugs "subscribe" and "subscriptions" are reserved.
const (
	subscribePath   = "/status/subscribe"
	confirmPath     = "/status/subscriptions/confirm"
	unsubscribePath = "/status/subscriptions/unsubscribe"
)

// Abuse limits for the public subscribe endpoint.
const (
	confirmTokenTTL          = 48 * time.Hour
	ipSubscribeLimit         = 10
	ipSubscribeWindow        = 10 * time.Minute
	addressConfirmLimit      = 3
	addressConfirmWindow     = 24 * time.Hour
	pageConfirmLimit         = 100
	pageConfirmWindow        = time.Hour
	maxPendingPerPage        = 1000
	maxEmailLength           = 254
	notifyRecencyWindow      = 15 * time.Minute
	maxTrackedIncidentStates = 10000
)

// Mailer queues an email for asynchronous delivery.
type Mailer interface {
	Enqueue(msg mail.Message) bool
}

// PageByIDSource is optionally implemented by a Repository so a subscription
// can name its page by ID. It must return only published pages.
type PageByIDSource interface {
	GetStatusPageByID(id string) (*core.StatusPage, error)
}

// PageLister is optionally implemented by a Repository; incident fan-out
// needs it to find the pages that show an incident's soul.
type PageLister interface {
	ListStatusPages() ([]*core.StatusPage, error)
}

// SubscriptionOptions enables email subscriptions on a Handler.
type SubscriptionOptions struct {
	// Mailer delivers the emails. Required.
	Mailer Mailer
	// PublicURL is the absolute base URL links point at (server.public_url).
	PublicURL string
	// ClientIP resolves the client address for rate limiting; RemoteAddr when nil.
	ClientIP func(*http.Request) string
	// ShouldNotify reports whether this node sends incident notifications.
	// In a cluster only the leader does; nil means always (single node).
	ShouldNotify func() bool
	Logger       *slog.Logger
}

type subscriptions struct {
	repo         Repository
	mailer       Mailer
	publicURL    string
	clientIP     func(*http.Request) string
	shouldNotify func() bool
	logger       *slog.Logger
	now          func() time.Time

	ipLimiter   *windowLimiter
	addrLimiter *windowLimiter
	// pageLimiter caps confirmation emails per page per hour, whatever the
	// addresses: the per-address limit alone lets one requester spray
	// many distinct (or plus-tagged) addresses from many IPs.
	pageLimiter *windowLimiter

	// writeMu serialises subscription read-modify-write cycles.
	writeMu sync.Mutex

	seenMu sync.Mutex
	seen   map[string]string // alert incident ID -> last status notified/observed

	jobs chan func()
}

// EnableSubscriptions turns on email subscriptions. Without it the subscribe
// endpoint answers 503 and pages show no form.
func (h *Handler) EnableSubscriptions(opts SubscriptionOptions) *Handler {
	if opts.Mailer == nil || strings.TrimSpace(opts.PublicURL) == "" {
		return h
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	clientIP := opts.ClientIP
	if clientIP == nil {
		clientIP = func(r *http.Request) string {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				return r.RemoteAddr
			}
			return host
		}
	}
	s := &subscriptions{
		repo:         h.repository,
		mailer:       opts.Mailer,
		publicURL:    strings.TrimRight(strings.TrimSpace(opts.PublicURL), "/"),
		clientIP:     clientIP,
		shouldNotify: opts.ShouldNotify,
		logger:       logger,
		now:          time.Now,
		ipLimiter:    newWindowLimiter(ipSubscribeLimit, ipSubscribeWindow),
		addrLimiter:  newWindowLimiter(addressConfirmLimit, addressConfirmWindow),
		pageLimiter:  newWindowLimiter(pageConfirmLimit, pageConfirmWindow),
		seen:         make(map[string]string),
		jobs:         make(chan func(), 256),
	}
	go func() {
		for job := range s.jobs {
			job()
		}
	}()
	h.subs = s
	return h
}

// SubscriptionsAvailable reports whether visitors can subscribe by email.
func (h *Handler) SubscriptionsAvailable() bool {
	return h != nil && h.subs != nil
}

// windowLimiter is a fixed-window counter per key.
type windowLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string]*windowCount
}

type windowCount struct {
	start time.Time
	count int
}

func newWindowLimiter(limit int, window time.Duration) *windowLimiter {
	return &windowLimiter{limit: limit, window: window, hits: make(map[string]*windowCount)}
}

func (l *windowLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.hits) > 50000 {
		for k, c := range l.hits {
			if now.Sub(c.start) >= l.window {
				delete(l.hits, k)
			}
		}
	}
	c, ok := l.hits[key]
	if !ok || now.Sub(c.start) >= l.window {
		l.hits[key] = &windowCount{start: now, count: 1}
		return true
	}
	if c.count >= l.limit {
		return false
	}
	c.count++
	return true
}

// serveSubscriptionRoute handles the subscription paths and reports whether
// path was one of them.
func (h *Handler) serveSubscriptionRoute(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case subscribePath:
		h.SubscribeHandler(w, r)
	case confirmPath:
		h.ConfirmSubscriptionHandler(w, r)
	case unsubscribePath:
		h.UnsubscribeHandler(w, r)
	default:
		return false
	}
	return true
}

func newToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// normalizeEmail returns the bare, lower-cased address, or "" when raw is not
// a single plain address.
func normalizeEmail(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxEmailLength || strings.ContainsAny(raw, "\r\n<>,; ") {
		return ""
	}
	addr, err := netmail.ParseAddress(raw)
	if err != nil || addr.Name != "" || addr.Address != raw {
		return ""
	}
	at := strings.LastIndex(addr.Address, "@")
	if at <= 0 || at == len(addr.Address)-1 || !strings.Contains(addr.Address[at+1:], ".") {
		return ""
	}
	return strings.ToLower(addr.Address)
}

// mailboxKey names the mailbox an address delivers to, for rate limiting:
// lower-cased, with a "+tag" suffix of the local part removed, so
// "Ops+1@x" and "ops+2@x" share one limit. The address stored and mailed
// stays as given.
func mailboxKey(email string) string {
	email = strings.ToLower(email)
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		return email
	}
	local, domain := email[:at], email[at:]
	if plus := strings.Index(local, "+"); plus > 0 {
		local = local[:plus]
	}
	return local + domain
}

func wantsHTML(r *http.Request) bool {
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return ct == "application/x-www-form-urlencoded" || ct == "multipart/form-data"
}

// subscribeReply answers JSON clients with a JSON object and form posts with
// a small HTML page.
func subscribeReply(w http.ResponseWriter, r *http.Request, status int, message string) {
	w.Header().Set("Cache-Control", "no-store")
	if wantsHTML(r) || r.Method == http.MethodGet {
		writeNoticePage(w, status, "Status updates", message, "")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]any{"success": status < 300, "message": message}
	if status >= 300 {
		body["error"] = message
	} else {
		body["confirmation_required"] = true
	}
	_ = json.NewEncoder(w).Encode(body)
}

// writeNoticePage renders a minimal standalone page. form, when set, is
// trusted HTML built by this package.
func writeNoticePage(w http.ResponseWriter, status int, title, message, form string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(status)
	// #nosec G705 -- title and message are escaped here; form is built only by
	// hiddenForm, which escapes every value it interpolates.
	_, _ = fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>%s</title>
<style>body{font-family:system-ui,-apple-system,sans-serif;background:#0f172a;color:#f8fafc;display:flex;min-height:100vh;align-items:center;justify-content:center;margin:0;padding:1rem}
main{max-width:28rem;text-align:center}h1{color:#fbbf24;font-size:1.5rem}button{margin-top:1rem;padding:.75rem 1.5rem;border:0;border-radius:.5rem;background:#fbbf24;color:#0f172a;font-weight:600;cursor:pointer}</style>
</head><body><main><h1>%s</h1><p>%s</p>%s</main></body></html>`,
		html.EscapeString(title), html.EscapeString(title), html.EscapeString(message), form)
}

const subscribeAcceptedMessage = "If this address can receive email, a confirmation link is on its way. Follow it to start receiving updates."

// SubscribeHandler handles POST /status/subscribe (JSON or form). Only email
// subscriptions exist; each must be confirmed through an emailed link.
//
// The response is the same whether the address is new, pending or already
// confirmed, so it reveals nothing about who is subscribed.
func (h *Handler) SubscribeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s := h.subs
	if s == nil {
		subscribeReply(w, r, http.StatusServiceUnavailable, "subscriptions unavailable: this server has no outgoing mail configured")
		return
	}
	if !s.ipLimiter.allow(s.clientIP(r), s.now()) {
		w.Header().Set("Retry-After", "600")
		subscribeReply(w, r, http.StatusTooManyRequests, "too many subscription requests; try again later")
		return
	}

	var req struct {
		PageID string `json:"page_id"`
		Slug   string `json:"slug"`
		Email  string `json:"email"`
		Type   string `json:"type"`
	}
	// This endpoint is public and unauthenticated: bound the body so one
	// request cannot make the server buffer and store an unbounded value.
	r.Body = http.MaxBytesReader(w, r.Body, maxSubscribeBodyBytes)
	if wantsHTML(r) {
		if err := r.ParseForm(); err != nil {
			subscribeReply(w, r, http.StatusBadRequest, "invalid request")
			return
		}
		req.PageID = r.PostForm.Get("page_id")
		req.Slug = r.PostForm.Get("slug")
		req.Email = r.PostForm.Get("email")
		req.Type = r.PostForm.Get("type")
	} else if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		subscribeReply(w, r, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Type != "" && req.Type != "email" {
		subscribeReply(w, r, http.StatusBadRequest, "only email subscriptions are supported")
		return
	}
	if req.PageID == "" && req.Slug == "" {
		subscribeReply(w, r, http.StatusBadRequest, "page_id is required")
		return
	}
	email := normalizeEmail(req.Email)
	if email == "" {
		subscribeReply(w, r, http.StatusBadRequest, "a valid email address is required")
		return
	}
	page := h.lookupSubscribablePage(req.PageID, req.Slug)
	if page == nil || !h.pageAccessAllowed(r, page) {
		subscribeReply(w, r, http.StatusNotFound, "status page not found")
		return
	}

	status, msg := s.subscribe(page, email)
	subscribeReply(w, r, status, msg)
}

func (h *Handler) lookupSubscribablePage(pageID, slug string) *core.StatusPage {
	if pageID != "" {
		if src, ok := h.repository.(PageByIDSource); ok {
			if page, err := src.GetStatusPageByID(pageID); err == nil && page != nil {
				return page
			}
			return nil
		}
		slug = pageID
	}
	page, err := h.repository.GetStatusPageBySlug(slug)
	if err != nil {
		return nil
	}
	return page
}

func (s *subscriptions) subscribe(page *core.StatusPage, email string) (int, string) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	repo := s.repo
	if repo == nil {
		return http.StatusServiceUnavailable, "subscriptions unavailable"
	}
	now := s.now().UTC()
	existing, err := repo.GetSubscriptionsByPage(page.ID)
	if err != nil {
		s.logger.Error("list subscriptions failed", "page_id", page.ID, "error", err)
		return http.StatusInternalServerError, "failed to save subscription"
	}
	var match *core.StatusPageSubscription
	pending := 0
	for _, sub := range existing {
		if sub == nil {
			continue
		}
		if !sub.Confirmed && !sub.ConfirmExpiresAt.IsZero() && now.After(sub.ConfirmExpiresAt) {
			// Lazily drop expired, never-confirmed requests.
			if err := repo.DeleteSubscription(sub.ID); err != nil {
				s.logger.Warn("delete expired subscription failed", "error", err)
			}
			continue
		}
		if strings.EqualFold(sub.Email, email) {
			match = sub
		}
		if !sub.Confirmed {
			pending++
		}
	}
	if match != nil && match.Confirmed {
		return http.StatusAccepted, subscribeAcceptedMessage
	}
	if match == nil && pending >= maxPendingPerPage {
		return http.StatusTooManyRequests, "too many pending subscriptions for this page; try again later"
	}
	if !s.addrLimiter.allow(mailboxKey(email), now) {
		// Same answer as success: the address limit must not be observable.
		return http.StatusAccepted, subscribeAcceptedMessage
	}
	if !s.pageLimiter.allow(page.ID, now) {
		return http.StatusTooManyRequests, "too many subscription requests for this page; try again later"
	}

	token, err := newToken()
	if err != nil {
		return http.StatusInternalServerError, "failed to save subscription"
	}
	sub := match
	if sub == nil {
		unsub, err := newToken()
		if err != nil {
			return http.StatusInternalServerError, "failed to save subscription"
		}
		sub = &core.StatusPageSubscription{
			ID:               core.GenerateID(),
			PageID:           page.ID,
			Email:            email,
			Type:             "email",
			SubscribedAt:     now,
			UnsubscribeToken: unsub,
		}
	}
	sub.ConfirmTokenHash = hashToken(token)
	sub.ConfirmExpiresAt = now.Add(confirmTokenTTL)
	sub.ConfirmSentAt = now
	if err := repo.SaveSubscription(sub); err != nil {
		s.logger.Error("save subscription failed", "page_id", page.ID, "error", err)
		return http.StatusInternalServerError, "failed to save subscription"
	}
	s.mailer.Enqueue(s.confirmationEmail(page, sub, token))
	return http.StatusAccepted, subscribeAcceptedMessage
}

func (s *subscriptions) link(path string, sub *core.StatusPageSubscription, token string) string {
	q := url.Values{}
	q.Set("page", sub.PageID)
	q.Set("id", sub.ID)
	q.Set("token", token)
	return s.publicURL + path + "?" + q.Encode()
}

func (s *subscriptions) pageURL(page *core.StatusPage) string {
	return s.publicURL + "/status/" + url.PathEscape(page.Slug)
}

func (s *subscriptions) withUnsubscribe(msg mail.Message, sub *core.StatusPageSubscription) mail.Message {
	unsub := s.link(unsubscribePath, sub, sub.UnsubscribeToken)
	msg.Headers = map[string]string{
		"List-Unsubscribe":      "<" + unsub + ">",
		"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
	}
	msg.Text += "\n\n--\nUnsubscribe: " + unsub + "\n"
	msg.HTML = `<!DOCTYPE html><html><body style="font-family:Arial,sans-serif;line-height:1.6;color:#333">` +
		msg.HTML +
		`<hr><p style="font-size:12px;color:#666">You receive this because this address subscribed to status updates. <a href="` +
		html.EscapeString(unsub) + `">Unsubscribe</a></p></body></html>`
	return msg
}

func (s *subscriptions) confirmationEmail(page *core.StatusPage, sub *core.StatusPageSubscription, token string) mail.Message {
	confirm := s.link(confirmPath, sub, token)
	name := page.Name
	msg := mail.Message{
		To:      sub.Email,
		Subject: oneLine("Confirm your subscription to " + name + " status updates"),
		Kind:    "subscription_confirm",
		Text: "Someone (hopefully you) asked to receive status updates for " + name + " at this address.\n\n" +
			"Confirm the subscription within 48 hours:\n" + confirm + "\n\n" +
			"If you did not ask for this, ignore this email and nothing will be sent.",
		HTML: "<p>Someone (hopefully you) asked to receive status updates for <strong>" + html.EscapeString(name) +
			"</strong> at this address.</p><p><a href=\"" + html.EscapeString(confirm) +
			"\">Confirm the subscription</a> within 48 hours.</p><p>If you did not ask for this, ignore this email and nothing will be sent.</p>",
	}
	return s.withUnsubscribe(msg, sub)
}

// findSubscription loads the subscription a link names and checks its token.
func (h *Handler) findSubscription(pageID, id string) *core.StatusPageSubscription {
	if pageID == "" || id == "" {
		return nil
	}
	subs, err := h.repository.GetSubscriptionsByPage(pageID)
	if err != nil {
		return nil
	}
	for _, sub := range subs {
		if sub != nil && sub.ID == id {
			return sub
		}
	}
	return nil
}

// linkParams reads page, id and token from the query, then the form.
func linkParams(r *http.Request) (pageID, id, token string) {
	q := r.URL.Query()
	pageID, id, token = q.Get("page"), q.Get("id"), q.Get("token")
	if r.Method == http.MethodPost && (pageID == "" || id == "" || token == "") {
		r.Body = http.MaxBytesReader(nil, r.Body, maxSubscribeBodyBytes)
		if err := r.ParseForm(); err == nil {
			pageID, id, token = r.PostForm.Get("page"), r.PostForm.Get("id"), r.PostForm.Get("token")
		}
	}
	return pageID, id, token
}

func hiddenForm(action, pageID, id, token, button string) string {
	return `<form method="POST" action="` + html.EscapeString(action) + `">` +
		`<input type="hidden" name="page" value="` + html.EscapeString(pageID) + `">` +
		`<input type="hidden" name="id" value="` + html.EscapeString(id) + `">` +
		`<input type="hidden" name="token" value="` + html.EscapeString(token) + `">` +
		`<button type="submit">` + html.EscapeString(button) + `</button></form>`
}

const invalidLinkMessage = "This link is invalid or has expired. Subscribe again from the status page to get a new one."

// ConfirmSubscriptionHandler serves /status/subscriptions/confirm. GET shows a
// confirmation button, so link scanners that prefetch email links cannot
// confirm on the recipient's behalf; POST confirms. The token is single use.
func (h *Handler) ConfirmSubscriptionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s := h.subs
	if s == nil {
		writeNoticePage(w, http.StatusServiceUnavailable, "Subscriptions unavailable", "This server does not send email updates.", "")
		return
	}
	pageID, id, token := linkParams(r)

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	sub := h.findSubscription(pageID, id)
	if sub == nil || token == "" || sub.Confirmed || sub.ConfirmTokenHash == "" ||
		subtle.ConstantTimeCompare([]byte(hashToken(token)), []byte(sub.ConfirmTokenHash)) != 1 ||
		s.now().After(sub.ConfirmExpiresAt) {
		if sub != nil && sub.Confirmed && r.Method == http.MethodGet {
			writeNoticePage(w, http.StatusOK, "Already confirmed", "This subscription is already active.", "")
			return
		}
		writeNoticePage(w, http.StatusBadRequest, "Link expired", invalidLinkMessage, "")
		return
	}
	if r.Method == http.MethodGet {
		writeNoticePage(w, http.StatusOK, "Confirm subscription", "Confirm that "+sub.Email+" should receive status updates.",
			hiddenForm(confirmPath, pageID, id, token, "Confirm subscription"))
		return
	}
	now := s.now().UTC()
	sub.Confirmed = true
	sub.ConfirmedAt = &now
	sub.ConfirmTokenHash = ""
	sub.ConfirmExpiresAt = time.Time{}
	if err := h.repository.SaveSubscription(sub); err != nil {
		s.logger.Error("confirm subscription failed", "error", err)
		writeNoticePage(w, http.StatusInternalServerError, "Something went wrong", "The subscription could not be confirmed. Try again later.", "")
		return
	}
	writeNoticePage(w, http.StatusOK, "Subscription confirmed", "You will now receive status updates by email. Every email has an unsubscribe link.", "")
}

// UnsubscribeHandler serves /status/subscriptions/unsubscribe. POST removes
// the subscription; that is also the RFC 8058 one-click request mail clients
// send for the List-Unsubscribe-Post header. GET shows a one-button page.
func (h *Handler) UnsubscribeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pageID, id, token := linkParams(r)
	var mu *sync.Mutex
	if h.subs != nil {
		mu = &h.subs.writeMu
		mu.Lock()
		defer mu.Unlock()
	}
	sub := h.findSubscription(pageID, id)
	if sub == nil {
		// Already gone: unsubscribing is idempotent.
		writeNoticePage(w, http.StatusOK, "Unsubscribed", "This address no longer receives status updates.", "")
		return
	}
	if token == "" || sub.UnsubscribeToken == "" ||
		subtle.ConstantTimeCompare([]byte(token), []byte(sub.UnsubscribeToken)) != 1 {
		writeNoticePage(w, http.StatusBadRequest, "Link invalid", "This unsubscribe link is not valid.", "")
		return
	}
	if r.Method == http.MethodGet {
		writeNoticePage(w, http.StatusOK, "Unsubscribe", "Stop sending status updates to "+sub.Email+"?",
			hiddenForm(unsubscribePath, pageID, id, token, "Unsubscribe"))
		return
	}
	if err := h.repository.DeleteSubscription(sub.ID); err != nil {
		writeNoticePage(w, http.StatusInternalServerError, "Something went wrong", "The subscription could not be removed. Try again later.", "")
		return
	}
	writeNoticePage(w, http.StatusOK, "Unsubscribed", "This address no longer receives status updates.", "")
}

// subscribeFormHTML is the form a page shows when subscriptions are available.
func subscribeFormHTML(page *core.StatusPage) string {
	return `
	<div class="subscribe-section" style="margin:2rem 0;padding:1.5rem;border:1px solid var(--border);border-radius:1rem;background:var(--card-bg);text-align:center">
		<h2 style="color:var(--primary);margin-bottom:.5rem">Get updates by email</h2>
		<p style="opacity:.7;margin-bottom:1rem">We will email you when an incident starts, changes or is resolved.</p>
		<form method="POST" action="` + subscribePath + `" style="display:flex;gap:.5rem;flex-wrap:wrap;justify-content:center">
			<input type="hidden" name="page_id" value="` + html.EscapeString(page.ID) + `">
			<label for="subscribe-email" style="position:absolute;left:-9999px">Email address</label>
			<input id="subscribe-email" type="email" name="email" required maxlength="254" placeholder="you@example.com" style="padding:.6rem;border-radius:.5rem;border:1px solid var(--accent);background:rgba(255,255,255,0.05);color:var(--text);min-width:16rem">
			<button type="submit" style="padding:.6rem 1.2rem;border:0;border-radius:.5rem;background:var(--primary);color:var(--bg);font-weight:600;cursor:pointer">Subscribe</button>
		</form>
	</div>`
}
