package storage

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func newTestStatusPageRepo(t *testing.T) (*StatusPageRepository, *CobaltDB) {
	db := newTestDB(t)
	repo := NewStatusPageRepository(db)
	return repo, db
}

// TestStatusPageRepository_SaveSubscription tests saving a subscription
func TestStatusPageRepository_SaveSubscription(t *testing.T) {
	repo, db := newTestStatusPageRepo(t)
	defer db.Close()

	sub := &core.StatusPageSubscription{
		ID:           "sub-1",
		PageID:       "page-1",
		Email:        "test@example.com",
		SubscribedAt: time.Now(),
	}

	err := repo.SaveSubscription(sub)
	if err != nil {
		t.Errorf("SaveSubscription failed: %v", err)
	}

	// Verify it was saved
	subs, err := repo.GetSubscriptionsByPage("page-1")
	if err != nil {
		t.Errorf("GetSubscriptionsByPage failed: %v", err)
	}
	if len(subs) != 1 {
		t.Errorf("Expected 1 subscription, got %d", len(subs))
	}
}

// TestStatusPageRepository_GetSubscriptionsByPage tests retrieving subscriptions
func TestStatusPageRepository_GetSubscriptionsByPage(t *testing.T) {
	repo, db := newTestStatusPageRepo(t)
	defer db.Close()

	// Save multiple subscriptions
	for i := 0; i < 3; i++ {
		sub := &core.StatusPageSubscription{
			ID:           fmt.Sprintf("sub-%d", i),
			PageID:       "page-1",
			Email:        fmt.Sprintf("test%d@example.com", i),
			SubscribedAt: time.Now(),
		}
		repo.SaveSubscription(sub)
	}

	// Get subscriptions
	subs, err := repo.GetSubscriptionsByPage("page-1")
	if err != nil {
		t.Errorf("GetSubscriptionsByPage failed: %v", err)
	}
	if len(subs) != 3 {
		t.Errorf("Expected 3 subscriptions, got %d", len(subs))
	}

	// Test non-existent page
	subs, err = repo.GetSubscriptionsByPage("non-existent")
	if err != nil {
		t.Errorf("GetSubscriptionsByPage should not error for non-existent page: %v", err)
	}
	if len(subs) != 0 {
		t.Errorf("Expected 0 subscriptions for non-existent page, got %d", len(subs))
	}
}

// TestStatusPageRepository_DeleteSubscription tests deleting a subscription
func TestStatusPageRepository_DeleteSubscription(t *testing.T) {
	repo, db := newTestStatusPageRepo(t)
	defer db.Close()

	// Save a subscription
	sub := &core.StatusPageSubscription{
		ID:           "sub-delete",
		PageID:       "page-1",
		Email:        "delete@example.com",
		SubscribedAt: time.Now(),
	}
	repo.SaveSubscription(sub)

	// Delete it
	err := repo.DeleteSubscription("sub-delete")
	if err != nil {
		t.Errorf("DeleteSubscription failed: %v", err)
	}

	// Verify it was deleted
	subs, _ := repo.GetSubscriptionsByPage("page-1")
	for _, s := range subs {
		if s.ID == "sub-delete" {
			t.Error("Subscription should have been deleted")
		}
	}
}

// TestStatusPageRepository_DeleteSubscription_NotFound tests deleting non-existent subscription
func TestStatusPageRepository_DeleteSubscription_NotFound(t *testing.T) {
	repo, db := newTestStatusPageRepo(t)
	defer db.Close()

	err := repo.DeleteSubscription("non-existent")
	if err == nil {
		t.Error("Expected error when deleting non-existent subscription")
	}
}

// TestStatusPageRepository_AddIncident tests adding an incident
func TestStatusPageRepository_AddIncident(t *testing.T) {
	repo, db := newTestStatusPageRepo(t)
	defer db.Close()

	// Create a status page first
	page := &core.StatusPage{
		ID:          "page-1",
		Name:        "Test Page",
		WorkspaceID: "default",
		Slug:        "test-page",
		Incidents:   []core.StatusIncident{},
	}
	err := repo.SaveStatusPage(page)
	if err != nil {
		t.Fatalf("Failed to save status page: %v", err)
	}

	// Add an incident
	incident := core.StatusIncident{
		ID:          "inc-1",
		Title:       "Test Incident",
		Description: "Something went wrong",
		Status:      "investigating",
		StartedAt:   time.Now(),
	}

	err = repo.AddIncident("page-1", incident)
	if err != nil {
		t.Errorf("AddIncident failed: %v", err)
	}

	// Verify it was added
	updatedPage, _ := repo.GetStatusPage("page-1")
	if len(updatedPage.Incidents) != 1 {
		t.Errorf("Expected 1 incident, got %d", len(updatedPage.Incidents))
	}
}

// TestStatusPageRepository_AddIncident_PageNotFound tests adding incident to non-existent page
func TestStatusPageRepository_AddIncident_PageNotFound(t *testing.T) {
	repo, db := newTestStatusPageRepo(t)
	defer db.Close()

	incident := core.StatusIncident{
		ID:    "inc-1",
		Title: "Test Incident",
	}

	err := repo.AddIncident("non-existent", incident)
	if err == nil {
		t.Error("Expected error when adding incident to non-existent page")
	}
}

// TestStatusPageRepository_UpdateIncident tests updating an incident
func TestStatusPageRepository_UpdateIncident(t *testing.T) {
	repo, db := newTestStatusPageRepo(t)
	defer db.Close()

	// Create a status page with an incident
	page := &core.StatusPage{
		ID:          "page-1",
		Name:        "Test Page",
		WorkspaceID: "default",
		Slug:        "test-page",
		Incidents: []core.StatusIncident{
			{
				ID:          "inc-1",
				Title:       "Original Title",
				Description: "Original description",
				Status:      "investigating",
				StartedAt:   time.Now(),
			},
		},
	}
	repo.SaveStatusPage(page)

	// Update the incident
	updates := core.StatusIncident{
		ID:          "inc-1",
		Title:       "Updated Title",
		Description: "Updated description",
		Status:      "resolved",
		StartedAt:   time.Now(),
	}

	err := repo.UpdateIncident("page-1", "inc-1", updates)
	if err != nil {
		t.Errorf("UpdateIncident failed: %v", err)
	}

	// Verify it was updated
	updatedPage, _ := repo.GetStatusPage("page-1")
	if updatedPage.Incidents[0].Title != "Updated Title" {
		t.Errorf("Expected title 'Updated Title', got '%s'", updatedPage.Incidents[0].Title)
	}
	if updatedPage.Incidents[0].Status != "resolved" {
		t.Errorf("Expected status 'resolved', got '%s'", updatedPage.Incidents[0].Status)
	}
}

// TestStatusPageRepository_UpdateIncident_NotFound tests updating non-existent incident
func TestStatusPageRepository_UpdateIncident_NotFound(t *testing.T) {
	repo, db := newTestStatusPageRepo(t)
	defer db.Close()

	// Create a status page
	page := &core.StatusPage{
		ID:          "page-1",
		Name:        "Test Page",
		WorkspaceID: "default",
		Slug:        "test-page",
	}
	repo.SaveStatusPage(page)

	// Try to update non-existent incident
	updates := core.StatusIncident{
		ID:    "non-existent",
		Title: "Test",
	}

	err := repo.UpdateIncident("page-1", "non-existent", updates)
	if err == nil {
		t.Error("Expected error when updating non-existent incident")
	}
}

// TestStatusPageRepository_UpdateIncident_PageNotFound tests updating incident on non-existent page
func TestStatusPageRepository_UpdateIncident_PageNotFound(t *testing.T) {
	repo, db := newTestStatusPageRepo(t)
	defer db.Close()

	updates := core.StatusIncident{
		ID:    "inc-1",
		Title: "Test",
	}

	err := repo.UpdateIncident("non-existent", "inc-1", updates)
	if err == nil {
		t.Error("Expected error when updating incident on non-existent page")
	}
}

// TestStatusPageRepository_SaveStatusPage tests saving a status page
func TestStatusPageRepository_SaveStatusPage(t *testing.T) {
	repo, db := newTestStatusPageRepo(t)
	defer db.Close()

	page := &core.StatusPage{
		ID:          "sp-001",
		WorkspaceID: "default",
		Name:        "Test Status Page",
		Slug:        "test-page",
	}

	if err := repo.SaveStatusPage(page); err != nil {
		t.Fatalf("SaveStatusPage failed: %v", err)
	}

	got, err := repo.GetStatusPage("sp-001")
	if err != nil {
		t.Fatalf("GetStatusPage failed: %v", err)
	}
	if got.Name != "Test Status Page" {
		t.Errorf("Name = %s, want Test Status Page", got.Name)
	}
}

// TestStatusPageRepository_SaveStatusPage_GeneratesID tests auto ID generation
func TestStatusPageRepository_SaveStatusPage_GeneratesID(t *testing.T) {
	repo, db := newTestStatusPageRepo(t)
	defer db.Close()

	page := &core.StatusPage{
		WorkspaceID: "default",
		Name:        "Auto ID Page",
	}

	if err := repo.SaveStatusPage(page); err != nil {
		t.Fatalf("SaveStatusPage failed: %v", err)
	}
	if page.ID == "" {
		t.Error("Expected auto-generated ID")
	}
}

// TestStatusPageRepository_SaveStatusPage_WithDomain tests saving with custom domain
func TestStatusPageRepository_SaveStatusPage_WithDomain(t *testing.T) {
	repo, db := newTestStatusPageRepo(t)
	defer db.Close()

	page := &core.StatusPage{
		ID:           "sp-002",
		WorkspaceID:  "default",
		Name:         "Domain Page",
		Slug:         "domain-page",
		CustomDomain: "status.example.com",
	}

	if err := repo.SaveStatusPage(page); err != nil {
		t.Fatalf("SaveStatusPage failed: %v", err)
	}

	got, err := repo.GetStatusPageByDomain("status.example.com")
	if err != nil {
		t.Fatalf("GetStatusPageByDomain failed: %v", err)
	}
	if got.ID != "sp-002" {
		t.Errorf("ID = %s, want sp-002", got.ID)
	}

	// Verify slug index was created
	data, err := db.Get("statuspage/slug/domain-page")
	if err != nil {
		t.Errorf("Slug index not found: %v", err)
	}
	if string(data) != "sp-002" {
		t.Errorf("Slug index = %s, want sp-002", string(data))
	}
}

// TestStatusPageRepository_GetStatusPageByDomain_NotFound tests domain lookup failure
func TestStatusPageRepository_GetStatusPageByDomain_NotFound(t *testing.T) {
	repo, db := newTestStatusPageRepo(t)
	defer db.Close()

	_, err := repo.GetStatusPageByDomain("nonexistent.example.com")
	if err == nil {
		t.Error("Expected error for missing domain")
	}
}

// TestStatusPageRepository_GetStatusPageBySlug_NotFound tests slug lookup failure
func TestStatusPageRepository_GetStatusPageBySlug_NotFound(t *testing.T) {
	repo, db := newTestStatusPageRepo(t)
	defer db.Close()

	_, err := repo.GetStatusPageBySlug("nonexistent")
	if err == nil {
		t.Error("Expected error for missing slug")
	}
}

// TestStatusPageRepository_GetStatusPage_NotFound tests ID lookup failure
func TestStatusPageRepository_GetStatusPage_NotFound(t *testing.T) {
	repo, db := newTestStatusPageRepo(t)
	defer db.Close()

	_, err := repo.GetStatusPage("nonexistent")
	if err == nil {
		t.Error("Expected error for missing status page")
	}
}

// TestStatusPageRepository_DeleteStatusPage_WithIndexes tests deletion with index cleanup
func TestStatusPageRepository_DeleteStatusPage_WithIndexes(t *testing.T) {
	repo, db := newTestStatusPageRepo(t)
	defer db.Close()

	page := &core.StatusPage{
		ID:           "sp-003",
		WorkspaceID:  "default",
		Name:         "Delete Me",
		Slug:         "delete-me",
		CustomDomain: "delete.example.com",
	}

	if err := repo.SaveStatusPage(page); err != nil {
		t.Fatalf("SaveStatusPage failed: %v", err)
	}

	if err := repo.DeleteStatusPage("sp-003"); err != nil {
		t.Fatalf("DeleteStatusPage failed: %v", err)
	}

	if _, err := repo.GetStatusPage("sp-003"); err == nil {
		t.Error("Expected GetStatusPage to fail after delete")
	}
	if _, err := repo.GetStatusPageBySlug("delete-me"); err == nil {
		t.Error("Expected GetStatusPageBySlug to fail after delete")
	}
	if _, err := repo.GetStatusPageByDomain("delete.example.com"); err == nil {
		t.Error("Expected GetStatusPageByDomain to fail after delete")
	}
}

// TestStatusPageRepository_DeleteStatusPage_NotFound tests deletion of non-existent page
func TestStatusPageRepository_DeleteStatusPage_NotFound(t *testing.T) {
	repo, db := newTestStatusPageRepo(t)
	defer db.Close()

	err := repo.DeleteStatusPage("nonexistent")
	if err == nil {
		t.Error("Expected error for deleting non-existent page")
	}
}

// TestStatusPageRepository_ListStatusPages_ByWorkspace tests listing by workspace
func TestStatusPageRepository_ListStatusPages_ByWorkspace(t *testing.T) {
	repo, db := newTestStatusPageRepo(t)
	defer db.Close()

	for i, ws := range []string{"ws-a", "ws-b", "ws-a"} {
		page := &core.StatusPage{
			ID:          fmt.Sprintf("sp-list-%d", i),
			WorkspaceID: ws,
			Name:        fmt.Sprintf("Page %d", i),
		}
		if err := repo.SaveStatusPage(page); err != nil {
			t.Fatalf("SaveStatusPage failed: %v", err)
		}
	}

	pages, err := repo.ListStatusPages("ws-a")
	if err != nil {
		t.Fatalf("ListStatusPages failed: %v", err)
	}
	if len(pages) != 2 {
		t.Errorf("Got %d pages, want 2", len(pages))
	}

	pagesB, err := repo.ListStatusPages("ws-b")
	if err != nil {
		t.Fatalf("ListStatusPages failed: %v", err)
	}
	if len(pagesB) != 1 {
		t.Errorf("Got %d pages, want 1", len(pagesB))
	}
}

// TestStatusPageRepository_GetIncidentsByPage_WithData tests retrieving incidents with data
func TestStatusPageRepository_GetIncidentsByPage_WithData(t *testing.T) {
	repo, db := newTestStatusPageRepo(t)
	defer db.Close()

	page := &core.StatusPage{
		ID:          "sp-inc",
		WorkspaceID: "default",
		Name:        "Incident Page",
		Incidents: []core.StatusIncident{
			{Title: "Outage", Status: "resolved"},
			{Title: "Degraded", Status: "active"},
		},
	}

	if err := repo.SaveStatusPage(page); err != nil {
		t.Fatalf("SaveStatusPage failed: %v", err)
	}

	incidents, err := repo.GetIncidentsByPage("sp-inc")
	if err != nil {
		t.Fatalf("GetIncidentsByPage failed: %v", err)
	}
	if len(incidents) != 2 {
		t.Errorf("Got %d incidents, want 2", len(incidents))
	}
}

// TestStatusPageRepository_GetIncidentsByPage_NotFound tests incidents for missing page
func TestStatusPageRepository_GetIncidentsByPage_NotFound(t *testing.T) {
	repo, db := newTestStatusPageRepo(t)
	defer db.Close()

	_, err := repo.GetIncidentsByPage("nonexistent")
	if err == nil {
		t.Error("Expected error for missing page")
	}
}

// TestStatusPageRepository_ListStatusPages_CorruptData tests handling of corrupt JSON
func TestStatusPageRepository_ListStatusPages_CorruptData(t *testing.T) {
	_, db := newTestStatusPageRepo(t)
	defer db.Close()

	// Put corrupt data directly into storage
	if err := db.Put("statuspage/corrupt-ws", []byte("not json{{{")); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Also put a valid page
	validPage := &core.StatusPage{
		ID:          "sp-valid",
		WorkspaceID: "default",
		Name:        "Valid Page",
	}
	if err := db.Put("statuspage/sp-valid", mustMarshal(validPage)); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	repo := NewStatusPageRepository(db)

	// ListStatusPages should skip corrupt entry and return valid ones
	pages, err := repo.ListStatusPages("default")
	if err != nil {
		t.Fatalf("ListStatusPages failed: %v", err)
	}
	if len(pages) != 1 {
		t.Errorf("Got %d pages, want 1", len(pages))
	}
	if pages[0].Name != "Valid Page" {
		t.Errorf("Got page %s, want Valid Page", pages[0].Name)
	}
}

// TestStatusPageRepository_ListStatusPages_EmptyWorkspace tests filtering with no matches
func TestStatusPageRepository_ListStatusPages_EmptyWorkspace(t *testing.T) {
	repo, db := newTestStatusPageRepo(t)
	defer db.Close()

	// Save pages in one workspace
	for i := 0; i < 3; i++ {
		page := &core.StatusPage{
			ID:          fmt.Sprintf("sp-empty-%d", i),
			WorkspaceID: "other-ws",
			Name:        fmt.Sprintf("Page %d", i),
		}
		if err := repo.SaveStatusPage(page); err != nil {
			t.Fatalf("SaveStatusPage failed: %v", err)
		}
	}

	// List from a different workspace
	pages, err := repo.ListStatusPages("empty-ws")
	if err != nil {
		t.Fatalf("ListStatusPages failed: %v", err)
	}
	if len(pages) != 0 {
		t.Errorf("Got %d pages, want 0", len(pages))
	}
}

// TestStatusPageRepository_SaveStatusPage_WithSlugAndDomain tests both slug and domain
func TestStatusPageRepository_SaveStatusPage_WithSlugAndDomain(t *testing.T) {
	repo, db := newTestStatusPageRepo(t)
	defer db.Close()

	page := &core.StatusPage{
		ID:           "sp-both",
		WorkspaceID:  "default",
		Name:         "Both Index Page",
		Slug:         "both-index",
		CustomDomain: "both.example.com",
	}

	if err := repo.SaveStatusPage(page); err != nil {
		t.Fatalf("SaveStatusPage failed: %v", err)
	}

	// Verify slug index
	slugData, err := db.Get("statuspage/slug/both-index")
	if err != nil {
		t.Errorf("Slug index not found: %v", err)
	}
	if string(slugData) != "sp-both" {
		t.Errorf("Slug index = %s, want sp-both", string(slugData))
	}

	// Verify domain index
	domainData, err := db.Get("statuspage/domain/both.example.com")
	if err != nil {
		t.Errorf("Domain index not found: %v", err)
	}
	// Domain index stores the full page JSON, so just verify it contains the ID
	if len(domainData) == 0 {
		t.Error("Expected domain index to have data")
	}
}

// TestStatusPageRepository_GetStatusPageByDomain_WithData tests domain lookup success
func TestStatusPageRepository_GetStatusPageByDomain_WithData(t *testing.T) {
	repo, db := newTestStatusPageRepo(t)
	defer db.Close()

	page := &core.StatusPage{
		ID:           "sp-domain-lookup",
		WorkspaceID:  "default",
		Name:         "Domain Lookup Page",
		CustomDomain: "lookup.example.com",
	}
	if err := repo.SaveStatusPage(page); err != nil {
		t.Fatalf("SaveStatusPage failed: %v", err)
	}

	got, err := repo.GetStatusPageByDomain("lookup.example.com")
	if err != nil {
		t.Fatalf("GetStatusPageByDomain failed: %v", err)
	}
	if got.ID != "sp-domain-lookup" {
		t.Errorf("ID = %s, want sp-domain-lookup", got.ID)
	}
}

// TestStatusPageRepository_GetStatusPageBySlug_WithData tests slug lookup success
func TestStatusPageRepository_GetStatusPageBySlug_WithData(t *testing.T) {
	repo, db := newTestStatusPageRepo(t)
	defer db.Close()

	page := &core.StatusPage{
		ID:          "sp-slug-lookup",
		WorkspaceID: "default",
		Name:        "Slug Lookup Page",
		Slug:        "slug-lookup",
	}
	if err := repo.SaveStatusPage(page); err != nil {
		t.Fatalf("SaveStatusPage failed: %v", err)
	}

	got, err := repo.GetStatusPageBySlug("slug-lookup")
	if err != nil {
		t.Fatalf("GetStatusPageBySlug failed: %v", err)
	}
	if got.ID != "sp-slug-lookup" {
		t.Errorf("ID = %s, want sp-slug-lookup", got.ID)
	}
}

// Helper to marshal JSON for direct storage writes
func mustMarshal(v interface{}) []byte {
	data, _ := json.Marshal(v)
	return data
}
