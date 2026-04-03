package storage

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// StatusPageRepository implements status page data access
type StatusPageRepository struct {
	storage *CobaltDB
}

// NewStatusPageRepository creates a new status page repository
func NewStatusPageRepository(storage *CobaltDB) *StatusPageRepository {
	return &StatusPageRepository{storage: storage}
}

// GetStatusPageByDomain retrieves a status page by custom domain
func (r *StatusPageRepository) GetStatusPageByDomain(domain string) (*core.StatusPage, error) {
	key := "statuspage/domain/" + domain
	data, err := r.storage.Get(key)
	if err != nil {
		return nil, &core.NotFoundError{Entity: "status_page", ID: domain}
	}

	var page core.StatusPage
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, err
	}

	return &page, nil
}

// GetStatusPageBySlug retrieves a status page by slug
func (r *StatusPageRepository) GetStatusPageBySlug(slug string) (*core.StatusPage, error) {
	key := "statuspage/slug/" + slug
	data, err := r.storage.Get(key)
	if err != nil {
		return nil, &core.NotFoundError{Entity: "status_page", ID: slug}
	}

	var page core.StatusPage
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, err
	}

	return &page, nil
}

// GetStatusPage retrieves a status page by ID
func (r *StatusPageRepository) GetStatusPage(id string) (*core.StatusPage, error) {
	key := "statuspage/" + id
	data, err := r.storage.Get(key)
	if err != nil {
		return nil, &core.NotFoundError{Entity: "status_page", ID: id}
	}

	var page core.StatusPage
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, err
	}

	return &page, nil
}

// SaveStatusPage saves a status page
func (r *StatusPageRepository) SaveStatusPage(page *core.StatusPage) error {
	if page.ID == "" {
		page.ID = core.GenerateID()
	}
	page.UpdatedAt = time.Now().UTC()

	// Save main record
	key := "statuspage/" + page.ID
	data, err := json.Marshal(page)
	if err != nil {
		return err
	}

	if err := r.storage.Put(key, data); err != nil {
		return err
	}

	// Save slug index
	if page.Slug != "" {
		slugKey := "statuspage/slug/" + page.Slug
		if err := r.storage.Put(slugKey, []byte(page.ID)); err != nil {
			return err
		}
	}

	// Save custom domain index
	if page.CustomDomain != "" {
		domainKey := "statuspage/domain/" + page.CustomDomain
		if err := r.storage.Put(domainKey, data); err != nil {
			return err
		}
	}

	return nil
}

// DeleteStatusPage deletes a status page
func (r *StatusPageRepository) DeleteStatusPage(id string) error {
	page, err := r.GetStatusPage(id)
	if err != nil {
		return err
	}

	// Delete slug index
	if page.Slug != "" {
		slugKey := "statuspage/slug/" + page.Slug
		r.storage.Delete(slugKey)
	}

	// Delete domain index
	if page.CustomDomain != "" {
		domainKey := "statuspage/domain/" + page.CustomDomain
		r.storage.Delete(domainKey)
	}

	// Delete main record
	key := "statuspage/" + id
	return r.storage.Delete(key)
}

// ListStatusPages lists all status pages for a workspace
func (r *StatusPageRepository) ListStatusPages(workspaceID string) ([]*core.StatusPage, error) {
	prefix := "statuspage/"
	prefixData, err := r.storage.PrefixScan(prefix)
	if err != nil {
		return nil, err
	}

	pages := make([]*core.StatusPage, 0)
	for key, data := range prefixData {
		// Skip index keys
		if strings.Contains(key, "/slug/") || strings.Contains(key, "/domain/") {
			continue
		}

		var page core.StatusPage
		if err := json.Unmarshal(data, &page); err != nil {
			continue
		}

		if page.WorkspaceID == workspaceID {
			pages = append(pages, &page)
		}
	}

	return pages, nil
}

// GetSoul retrieves a soul by ID
func (r *StatusPageRepository) GetSoul(id string) (*core.Soul, error) {
	key := "souls/" + id
	data, err := r.storage.Get(key)
	if err != nil {
		return nil, &core.NotFoundError{Entity: "soul", ID: id}
	}

	var soul core.Soul
	if err := json.Unmarshal(data, &soul); err != nil {
		return nil, err
	}

	return &soul, nil
}

// GetSoulJudgments retrieves recent judgments for a soul
func (r *StatusPageRepository) GetSoulJudgments(soulID string, limit int) ([]core.Judgment, error) {
	// Use judgments storage instead of direct feather access
	return nil, fmt.Errorf("not implemented - use judgment storage directly")
}

// GetIncidentsByPage retrieves incidents for a status page
func (r *StatusPageRepository) GetIncidentsByPage(pageID string) ([]core.StatusIncident, error) {
	page, err := r.GetStatusPage(pageID)
	if err != nil {
		return nil, err
	}

	// Return incidents from page data
	return page.Incidents, nil
}

// GetUptimeHistory retrieves uptime history for a soul
func (r *StatusPageRepository) GetUptimeHistory(soulID string, days int) ([]core.UptimeDay, error) {
	// Generate placeholder data for now
	history := make([]core.UptimeDay, 0, days)

	for i := 0; i < days; i++ {
		date := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		history = append(history, core.UptimeDay{
			Date:   date,
			Status: "operational",
			Uptime: 100.0,
		})
	}

	return history, nil
}

// SaveUptimeDay saves a day's uptime record
func (r *StatusPageRepository) SaveUptimeDay(soulID string, day core.UptimeDay) error {
	key := fmt.Sprintf("uptime/%s/%s", soulID, day.Date)
	data, err := json.Marshal(day)
	if err != nil {
		return err
	}
	return r.storage.Put(key, data)
}

// GetWorkspace retrieves a workspace by ID
func (r *StatusPageRepository) GetWorkspace(id string) (*core.Workspace, error) {
	key := "workspaces/" + id
	data, err := r.storage.Get(key)
	if err != nil {
		return nil, &core.NotFoundError{Entity: "workspace", ID: id}
	}

	var workspace core.Workspace
	if err := json.Unmarshal(data, &workspace); err != nil {
		return nil, err
	}

	return &workspace, nil
}
