package storage

import (
	"encoding/json"
	"fmt"
	"log/slog"
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

	// Slug index stores the page ID as raw bytes
	pageID := string(data)
	return r.GetStatusPage(pageID)
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
		if err := r.storage.Delete(slugKey); err != nil {
			slog.Warn("failed to delete statuspage slug index", "key", slugKey, "err", err)
		}
	}

	// Delete domain index
	if page.CustomDomain != "" {
		domainKey := "statuspage/domain/" + page.CustomDomain
		if err := r.storage.Delete(domainKey); err != nil {
			slog.Warn("failed to delete statuspage domain index", "key", domainKey, "err", err)
		}
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
	return r.storage.GetSoulNoCtx(id)
}

// GetSoulJudgments retrieves recent judgments for a soul
func (r *StatusPageRepository) GetSoulJudgments(soulID string, limit int) ([]core.Judgment, error) {
	return r.storage.GetSoulJudgments(soulID, limit)
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
	return r.storage.GetUptimeHistory(soulID, days)
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

// SaveSubscription saves a status page subscription
func (r *StatusPageRepository) SaveSubscription(sub *core.StatusPageSubscription) error {
	key := fmt.Sprintf("statuspage/subscriptions/%s/%s", sub.PageID, sub.ID)
	data, err := json.Marshal(sub)
	if err != nil {
		return err
	}
	return r.storage.Put(key, data)
}

// GetSubscriptionsByPage retrieves all subscriptions for a status page
func (r *StatusPageRepository) GetSubscriptionsByPage(pageID string) ([]*core.StatusPageSubscription, error) {
	prefix := fmt.Sprintf("statuspage/subscriptions/%s/", pageID)
	results, err := r.storage.PrefixScan(prefix)
	if err != nil {
		return nil, err
	}

	subs := make([]*core.StatusPageSubscription, 0, len(results))
	for _, data := range results {
		var sub core.StatusPageSubscription
		if err := json.Unmarshal(data, &sub); err != nil {
			continue
		}
		subs = append(subs, &sub)
	}

	return subs, nil
}

// DeleteSubscription removes a subscription
func (r *StatusPageRepository) DeleteSubscription(subscriptionID string) error {
	// Find the subscription first
	prefix := "statuspage/subscriptions/"
	results, err := r.storage.PrefixScan(prefix)
	if err != nil {
		return err
	}

	for key, data := range results {
		var sub core.StatusPageSubscription
		if err := json.Unmarshal(data, &sub); err != nil {
			continue
		}
		if sub.ID == subscriptionID {
			return r.storage.Delete(key)
		}
	}

	return &core.NotFoundError{Entity: "subscription", ID: subscriptionID}
}

// AddIncident adds an incident to a status page
func (r *StatusPageRepository) AddIncident(pageID string, incident core.StatusIncident) error {
	page, err := r.GetStatusPage(pageID)
	if err != nil {
		return err
	}

	// Add incident to the list
	page.Incidents = append(page.Incidents, incident)

	// Save updated page
	return r.SaveStatusPage(page)
}

// UpdateIncident updates an incident on a status page
func (r *StatusPageRepository) UpdateIncident(pageID string, incidentID string, updates core.StatusIncident) error {
	page, err := r.GetStatusPage(pageID)
	if err != nil {
		return err
	}

	// Find and update the incident
	for i, inc := range page.Incidents {
		if inc.ID == incidentID {
			page.Incidents[i] = updates
			return r.SaveStatusPage(page)
		}
	}

	return &core.NotFoundError{Entity: "incident", ID: incidentID}
}
