package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// Status page incidents and scheduled-maintenance announcements are edited
// through dedicated endpoints instead of whole-page PUTs. Each write is a
// read-modify-write of the stored page, so it runs under a per-page lock:
// two editors posting updates at once both land, in order, and the save hook
// (restStorageAdapter.onStatusPageSave) compares each save with the page it
// replaced, so every posted update notifies subscribers exactly once.
//
// Text is plain text. It is stored verbatim (minus surrounding whitespace)
// and escaped wherever it is rendered; the public page additionally turns
// http(s) URLs into links. Raw HTML is never interpreted.

const (
	maxIncidentTitleLen       = 200
	maxIncidentMessageLen     = 5000
	maxIncidentsPerPage       = 500
	maxUpdatesPerIncident     = 200
	maxAnnouncementsPerPage   = 200
	maxAffectedSoulsPerRecord = 200

	// maxStatusPageRecordBytes bounds one serialized status page. The page is
	// one storage record: in cluster mode its JSON is base64-wrapped in the
	// FSM command, again in the Raft log entry and again in the WAL entry
	// (about 2.4x in all), and a WAL entry may not exceed 1 MiB
	// (storage.maxWALEntryBytes). 256 KiB keeps the largest page near 600 KiB
	// on disk, and keeps re-marshalling and rendering it cheap.
	maxStatusPageRecordBytes = 256 * 1024
)

// statusPageLocks serialises read-modify-write cycles on one status page. The
// stripes are shared by every RESTServer in the process, which only ever
// serialises more than strictly necessary.
var statusPageLocks [64]sync.Mutex

func lockStatusPage(id string) func() {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	mu := &statusPageLocks[h.Sum32()%uint32(len(statusPageLocks))]
	mu.Lock()
	return mu.Unlock
}

// cleanStatusText trims v and checks it is valid, printable plain text of at
// most max runes. Newlines and tabs are kept when multiline is set.
func cleanStatusText(field, v string, maxRunes int, required, multiline bool) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		if required {
			return "", fmt.Errorf("%s is required", field)
		}
		return "", nil
	}
	if !utf8.ValidString(v) {
		return "", fmt.Errorf("%s must be valid UTF-8", field)
	}
	if utf8.RuneCountInString(v) > maxRunes {
		return "", fmt.Errorf("%s must be at most %d characters", field, maxRunes)
	}
	for _, r := range v {
		if r == '\n' || r == '\t' || r == '\r' {
			if !multiline {
				return "", fmt.Errorf("%s must be a single line", field)
			}
			continue
		}
		if unicode.IsControl(r) {
			return "", fmt.Errorf("%s contains control characters", field)
		}
	}
	return strings.ReplaceAll(v, "\r\n", "\n"), nil
}

// pageSoulSet returns every soul the page shows.
func pageSoulSet(page *core.StatusPage) map[string]bool {
	set := make(map[string]bool, len(page.Souls))
	for _, id := range page.Souls {
		set[id] = true
	}
	for _, g := range page.Groups {
		for _, id := range g.SoulIDs {
			set[id] = true
		}
	}
	return set
}

// cleanAffectedSouls deduplicates ids and requires each to be a soul the page
// shows: a page can only report on its own components, and the page's souls
// were already checked to be in its workspace.
func cleanAffectedSouls(page *core.StatusPage, ids []string) ([]string, error) {
	if len(ids) > maxAffectedSoulsPerRecord {
		return nil, fmt.Errorf("at most %d affected souls", maxAffectedSoulsPerRecord)
	}
	onPage := pageSoulSet(page)
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		if !onPage[id] {
			return nil, fmt.Errorf("soul %s is not shown on this status page", id)
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

// severityForImpact keeps the legacy severity field meaningful for readers
// that predate impact.
func severityForImpact(impact core.IncidentImpact) core.Severity {
	switch impact {
	case core.ImpactCritical, core.ImpactMajor:
		return core.SeverityCritical
	case core.ImpactMinor:
		return core.SeverityWarning
	}
	return core.SeverityInfo
}

// errStatusPageNotFound is returned by mutateStatusPage for a page that does
// not exist or belongs to another workspace; the two are indistinguishable to
// the caller.
var errStatusPageNotFound = errors.New("status page not found")

// statusPageHTTPError carries a client error out of a mutation.
type statusPageHTTPError struct {
	code int
	msg  string
}

func (e *statusPageHTTPError) Error() string { return e.msg }

func badStatusPageRequest(err error) error {
	return &statusPageHTTPError{code: http.StatusBadRequest, msg: err.Error()}
}

func statusPageNotFound(what string) error {
	return &statusPageHTTPError{code: http.StatusNotFound, msg: what + " not found"}
}

// mutateStatusPage loads the caller's page under its lock, applies fn and
// saves the result through the store (and so through cluster replication and
// the notification hook). fn returns the value to send back.
func (s *RESTServer) mutateStatusPage(ctx *Context, fn func(page *core.StatusPage, now time.Time) (any, error)) (any, error) {
	id := ctx.Params["id"]
	unlock := lockStatusPage(id)
	defer unlock()
	page, err := s.store.GetStatusPageNoCtx(id)
	if err != nil || page == nil || !sameWorkspace(page.WorkspaceID, contextWorkspace(ctx)) {
		return nil, errStatusPageNotFound
	}
	// Work on a copy: the store may hand out its cached object, and the save
	// hook must see the stored page as it was.
	copied := *page
	copied.Incidents = append([]core.StatusIncident(nil), page.Incidents...)
	for i := range copied.Incidents {
		copied.Incidents[i].Updates = append([]core.IncidentUpdate(nil), page.Incidents[i].Updates...)
		copied.Incidents[i].AffectedSouls = append([]string(nil), page.Incidents[i].AffectedSouls...)
	}
	copied.Announcements = append([]core.StatusAnnouncement(nil), page.Announcements...)
	now := time.Now().UTC()
	// Age out what the public page no longer shows before applying the
	// change, so the record only holds the 90-day history.
	core.CompactStatusPage(&copied, now)
	out, err := fn(&copied, now)
	if err != nil {
		return nil, err
	}
	copied.UpdatedAt = now
	copied.PasswordSet = false
	if err := checkStatusPageBudget(page, &copied); err != nil {
		return nil, err
	}
	if err := s.store.SaveStatusPageNoCtx(&copied); err != nil {
		return nil, err
	}
	return out, nil
}

// checkStatusPageBudget refuses a change that leaves the page record larger
// than maxStatusPageRecordBytes. A change that does not grow the record (a
// delete, a shorter edit) is always allowed, so an oversized legacy page can
// still be trimmed.
func checkStatusPageBudget(before, after *core.StatusPage) error {
	newRaw, err := json.Marshal(after)
	if err != nil {
		return err
	}
	if len(newRaw) <= maxStatusPageRecordBytes {
		return nil
	}
	if oldRaw, err := json.Marshal(before); err == nil && len(newRaw) <= len(oldRaw) {
		return nil
	}
	return &statusPageHTTPError{code: http.StatusRequestEntityTooLarge, msg: fmt.Sprintf(
		"status page would exceed its %d KiB limit; delete or shorten old incidents or announcements", maxStatusPageRecordBytes/1024)}
}

// writeStatusPageMutation maps a mutateStatusPage result onto the response.
func (s *RESTServer) writeStatusPageMutation(ctx *Context, code int, out any, err error) error {
	if err != nil {
		var httpErr *statusPageHTTPError
		switch {
		case errors.Is(err, errStatusPageNotFound):
			return ctx.Error(http.StatusNotFound, "status page not found")
		case errors.As(err, &httpErr):
			return ctx.Error(httpErr.code, httpErr.msg)
		default:
			return s.internalError(ctx, err, "internal server error")
		}
	}
	if code == http.StatusNoContent {
		return ctx.JSON(http.StatusNoContent, nil)
	}
	return ctx.JSON(code, out)
}

func findIncident(page *core.StatusPage, id string) int {
	for i := range page.Incidents {
		if page.Incidents[i].ID == id {
			return i
		}
	}
	return -1
}

func findAnnouncement(page *core.StatusPage, id string) int {
	for i := range page.Announcements {
		if page.Announcements[i].ID == id {
			return i
		}
	}
	return -1
}

// applyIncidentStatus moves inc to status at now, keeping ResolvedAt in step.
func applyIncidentStatus(inc *core.StatusIncident, status core.IncidentStatus, now time.Time) {
	inc.Status = status
	if status == core.StatusResolved {
		if inc.ResolvedAt == nil {
			at := now
			inc.ResolvedAt = &at
		}
	} else {
		inc.ResolvedAt = nil
	}
}

func (s *RESTServer) handleListStatusPageIncidents(ctx *Context) error {
	page, err := s.store.GetStatusPageNoCtx(ctx.Params["id"])
	if err != nil || page == nil || !sameWorkspace(page.WorkspaceID, contextWorkspace(ctx)) {
		return ctx.Error(http.StatusNotFound, "status page not found")
	}
	incidents := page.Incidents
	if incidents == nil {
		incidents = []core.StatusIncident{}
	}
	return ctx.JSON(http.StatusOK, incidents)
}

type statusIncidentCreateRequest struct {
	Title         string              `json:"title"`
	Impact        core.IncidentImpact `json:"impact"`
	Status        core.IncidentStatus `json:"status"`
	Message       string              `json:"message"`
	AffectedSouls []string            `json:"affected_souls"`
}

func (s *RESTServer) handleCreateStatusPageIncident(ctx *Context) error {
	var req statusIncidentCreateRequest
	if err := ctx.Bind(&req); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid incident data")
	}
	title, err := cleanStatusText("title", req.Title, maxIncidentTitleLen, true, false)
	if err != nil {
		return ctx.Error(http.StatusBadRequest, err.Error())
	}
	message, err := cleanStatusText("message", req.Message, maxIncidentMessageLen, true, true)
	if err != nil {
		return ctx.Error(http.StatusBadRequest, err.Error())
	}
	if req.Impact == "" {
		req.Impact = core.ImpactMinor
	}
	if !core.ValidIncidentImpact(req.Impact) {
		return ctx.Error(http.StatusBadRequest, "impact must be one of none, minor, major, critical")
	}
	if req.Status == "" {
		req.Status = core.StatusInvestigating
	}
	if !core.ValidStatusPageIncidentStatus(req.Status) {
		return ctx.Error(http.StatusBadRequest, "status must be one of investigating, identified, monitoring, resolved")
	}
	out, err := s.mutateStatusPage(ctx, func(page *core.StatusPage, now time.Time) (any, error) {
		if len(page.Incidents) >= maxIncidentsPerPage {
			return nil, badStatusPageRequest(fmt.Errorf("a status page holds at most %d incidents; delete old ones first", maxIncidentsPerPage))
		}
		souls, err := cleanAffectedSouls(page, req.AffectedSouls)
		if err != nil {
			return nil, badStatusPageRequest(err)
		}
		inc := core.StatusIncident{
			ID:            core.GenerateID(),
			Title:         title,
			Description:   message,
			Impact:        req.Impact,
			Severity:      severityForImpact(req.Impact),
			AffectedSouls: souls,
			StartedAt:     now,
			UpdatedAt:     now,
			Updates: []core.IncidentUpdate{{
				ID: core.GenerateID(), Message: message, Status: string(req.Status), UpdatedAt: now,
			}},
		}
		applyIncidentStatus(&inc, req.Status, now)
		page.Incidents = append(page.Incidents, inc)
		return inc, nil
	})
	return s.writeStatusPageMutation(ctx, http.StatusCreated, out, err)
}

type statusIncidentPatchRequest struct {
	Title         *string              `json:"title"`
	Impact        *core.IncidentImpact `json:"impact"`
	AffectedSouls *[]string            `json:"affected_souls"`
	Status        *core.IncidentStatus `json:"status"`
}

// handlePatchStatusPageIncident edits an incident's title, impact or affected
// components. Status changes go through a posted update so the timeline (and
// the subscriber emails) record them.
func (s *RESTServer) handlePatchStatusPageIncident(ctx *Context) error {
	var req statusIncidentPatchRequest
	if err := ctx.Bind(&req); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid incident data")
	}
	if req.Status != nil {
		return ctx.Error(http.StatusBadRequest, "post an update to change the incident status")
	}
	var title string
	if req.Title != nil {
		var err error
		if title, err = cleanStatusText("title", *req.Title, maxIncidentTitleLen, true, false); err != nil {
			return ctx.Error(http.StatusBadRequest, err.Error())
		}
	}
	if req.Impact != nil && !core.ValidIncidentImpact(*req.Impact) {
		return ctx.Error(http.StatusBadRequest, "impact must be one of none, minor, major, critical")
	}
	incidentID := ctx.Params["incidentId"]
	out, err := s.mutateStatusPage(ctx, func(page *core.StatusPage, now time.Time) (any, error) {
		i := findIncident(page, incidentID)
		if i < 0 {
			return nil, statusPageNotFound("incident")
		}
		inc := &page.Incidents[i]
		if req.Title != nil {
			inc.Title = title
		}
		if req.Impact != nil {
			inc.Impact = *req.Impact
			inc.Severity = severityForImpact(*req.Impact)
		}
		if req.AffectedSouls != nil {
			souls, err := cleanAffectedSouls(page, *req.AffectedSouls)
			if err != nil {
				return nil, badStatusPageRequest(err)
			}
			inc.AffectedSouls = souls
		}
		inc.UpdatedAt = now
		return *inc, nil
	})
	return s.writeStatusPageMutation(ctx, http.StatusOK, out, err)
}

type statusIncidentUpdateRequest struct {
	Message string              `json:"message"`
	Status  core.IncidentStatus `json:"status"`
}

// postIncidentUpdate appends one timeline entry and moves the incident to its
// status. The save hook sees one more update and sends one notification.
func (s *RESTServer) postIncidentUpdate(ctx *Context, req statusIncidentUpdateRequest, defaultMessage string) error {
	message, err := cleanStatusText("message", req.Message, maxIncidentMessageLen, defaultMessage == "", true)
	if err != nil {
		return ctx.Error(http.StatusBadRequest, err.Error())
	}
	if message == "" {
		message = defaultMessage
	}
	if !core.ValidStatusPageIncidentStatus(req.Status) {
		return ctx.Error(http.StatusBadRequest, "status must be one of investigating, identified, monitoring, resolved")
	}
	incidentID := ctx.Params["incidentId"]
	out, err := s.mutateStatusPage(ctx, func(page *core.StatusPage, now time.Time) (any, error) {
		i := findIncident(page, incidentID)
		if i < 0 {
			return nil, statusPageNotFound("incident")
		}
		inc := &page.Incidents[i]
		if len(inc.Updates) >= maxUpdatesPerIncident {
			return nil, badStatusPageRequest(fmt.Errorf("an incident holds at most %d updates", maxUpdatesPerIncident))
		}
		inc.Updates = append(inc.Updates, core.IncidentUpdate{
			ID: core.GenerateID(), Message: message, Status: string(req.Status), UpdatedAt: now,
		})
		applyIncidentStatus(inc, req.Status, now)
		inc.UpdatedAt = now
		return *inc, nil
	})
	return s.writeStatusPageMutation(ctx, http.StatusCreated, out, err)
}

func (s *RESTServer) handlePostStatusPageIncidentUpdate(ctx *Context) error {
	var req statusIncidentUpdateRequest
	if err := ctx.Bind(&req); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid update data")
	}
	return s.postIncidentUpdate(ctx, req, "")
}

func (s *RESTServer) handleResolveStatusPageIncident(ctx *Context) error {
	var req statusIncidentUpdateRequest
	// The body is optional: an empty one posts the default message.
	if err := ctx.Bind(&req); err != nil && !errors.Is(err, io.EOF) {
		return ctx.Error(http.StatusBadRequest, "invalid update data")
	}
	req.Status = core.StatusResolved
	return s.postIncidentUpdate(ctx, req, "This incident has been resolved.")
}

func (s *RESTServer) handleDeleteStatusPageIncident(ctx *Context) error {
	incidentID := ctx.Params["incidentId"]
	out, err := s.mutateStatusPage(ctx, func(page *core.StatusPage, now time.Time) (any, error) {
		i := findIncident(page, incidentID)
		if i < 0 {
			return nil, statusPageNotFound("incident")
		}
		page.Incidents = append(page.Incidents[:i], page.Incidents[i+1:]...)
		return nil, nil
	})
	return s.writeStatusPageMutation(ctx, http.StatusNoContent, out, err)
}

func (s *RESTServer) handleListStatusPageAnnouncements(ctx *Context) error {
	page, err := s.store.GetStatusPageNoCtx(ctx.Params["id"])
	if err != nil || page == nil || !sameWorkspace(page.WorkspaceID, contextWorkspace(ctx)) {
		return ctx.Error(http.StatusNotFound, "status page not found")
	}
	now := time.Now()
	out := make([]core.StatusAnnouncement, 0, len(page.Announcements))
	for _, a := range page.Announcements {
		a.Phase = a.PhaseAt(now)
		out = append(out, a)
	}
	return ctx.JSON(http.StatusOK, out)
}

type statusAnnouncementRequest struct {
	Title               *string    `json:"title"`
	Message             *string    `json:"message"`
	AffectedSouls       *[]string  `json:"affected_souls"`
	StartsAt            *time.Time `json:"starts_at"`
	EndsAt              *time.Time `json:"ends_at"`
	MaintenanceWindowID *string    `json:"maintenance_window_id"`
}

// applyAnnouncementRequest merges req into a and validates the result. A
// linked maintenance window supplies the schedule, and the souls and text the
// request leaves out; only one-off windows map cleanly onto a single
// announcement, so a recurring window cannot be linked.
func (s *RESTServer) applyAnnouncementRequest(page *core.StatusPage, a *core.StatusAnnouncement, req statusAnnouncementRequest) error {
	if req.MaintenanceWindowID != nil {
		a.MaintenanceWindowID = strings.TrimSpace(*req.MaintenanceWindowID)
	}
	var window *core.MaintenanceWindow
	if a.MaintenanceWindowID != "" && req.MaintenanceWindowID != nil {
		w, err := s.store.GetMaintenanceWindow(a.MaintenanceWindowID)
		if err != nil || w == nil || !sameWorkspace(w.WorkspaceID, page.WorkspaceID) {
			return fmt.Errorf("maintenance window %s not found", a.MaintenanceWindowID)
		}
		if w.Recurring != "" {
			return fmt.Errorf("a recurring maintenance window cannot be linked to an announcement")
		}
		window = w
	}
	if req.Title != nil {
		a.Title = *req.Title
	} else if window != nil && a.Title == "" {
		a.Title = window.Name
	}
	if req.Message != nil {
		a.Message = *req.Message
	} else if window != nil && a.Message == "" {
		a.Message = window.Description
	}
	var err error
	if a.Title, err = cleanStatusText("title", a.Title, maxIncidentTitleLen, true, false); err != nil {
		return err
	}
	if a.Message, err = cleanStatusText("message", a.Message, maxIncidentMessageLen, false, true); err != nil {
		return err
	}
	switch {
	case req.AffectedSouls != nil:
		if a.AffectedSouls, err = cleanAffectedSouls(page, *req.AffectedSouls); err != nil {
			return err
		}
	case window != nil:
		onPage := pageSoulSet(page)
		souls := make([]string, 0, len(window.SoulIDs))
		for _, id := range window.SoulIDs {
			if onPage[id] {
				souls = append(souls, id)
			}
		}
		a.AffectedSouls = souls
	}
	if window != nil {
		a.StartsAt, a.EndsAt = window.StartTime.UTC(), window.EndTime.UTC()
	}
	if req.StartsAt != nil {
		a.StartsAt = req.StartsAt.UTC()
	}
	if req.EndsAt != nil {
		a.EndsAt = req.EndsAt.UTC()
	}
	if a.StartsAt.IsZero() || a.EndsAt.IsZero() {
		return fmt.Errorf("starts_at and ends_at are required")
	}
	if !a.EndsAt.After(a.StartsAt) {
		return fmt.Errorf("ends_at must be after starts_at")
	}
	if a.AffectedSouls == nil {
		a.AffectedSouls = []string{}
	}
	return nil
}

func (s *RESTServer) handleCreateStatusPageAnnouncement(ctx *Context) error {
	var req statusAnnouncementRequest
	if err := ctx.Bind(&req); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid announcement data")
	}
	out, err := s.mutateStatusPage(ctx, func(page *core.StatusPage, now time.Time) (any, error) {
		if len(page.Announcements) >= maxAnnouncementsPerPage {
			return nil, badStatusPageRequest(fmt.Errorf("a status page holds at most %d announcements; delete old ones first", maxAnnouncementsPerPage))
		}
		a := core.StatusAnnouncement{ID: core.GenerateID(), CreatedAt: now, UpdatedAt: now}
		if err := s.applyAnnouncementRequest(page, &a, req); err != nil {
			return nil, badStatusPageRequest(err)
		}
		page.Announcements = append(page.Announcements, a)
		a.Phase = a.PhaseAt(now)
		return a, nil
	})
	return s.writeStatusPageMutation(ctx, http.StatusCreated, out, err)
}

func (s *RESTServer) handlePatchStatusPageAnnouncement(ctx *Context) error {
	var req statusAnnouncementRequest
	if err := ctx.Bind(&req); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid announcement data")
	}
	announcementID := ctx.Params["announcementId"]
	out, err := s.mutateStatusPage(ctx, func(page *core.StatusPage, now time.Time) (any, error) {
		i := findAnnouncement(page, announcementID)
		if i < 0 {
			return nil, statusPageNotFound("announcement")
		}
		a := page.Announcements[i]
		a.AffectedSouls = append([]string(nil), a.AffectedSouls...)
		if err := s.applyAnnouncementRequest(page, &a, req); err != nil {
			return nil, badStatusPageRequest(err)
		}
		a.UpdatedAt = now
		page.Announcements[i] = a
		a.Phase = a.PhaseAt(now)
		return a, nil
	})
	return s.writeStatusPageMutation(ctx, http.StatusOK, out, err)
}

func (s *RESTServer) handleDeleteStatusPageAnnouncement(ctx *Context) error {
	announcementID := ctx.Params["announcementId"]
	out, err := s.mutateStatusPage(ctx, func(page *core.StatusPage, now time.Time) (any, error) {
		i := findAnnouncement(page, announcementID)
		if i < 0 {
			return nil, statusPageNotFound("announcement")
		}
		page.Announcements = append(page.Announcements[:i], page.Announcements[i+1:]...)
		return nil, nil
	})
	return s.writeStatusPageMutation(ctx, http.StatusNoContent, out, err)
}
