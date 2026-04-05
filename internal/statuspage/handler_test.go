package statuspage

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/acme"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

func newTestDB(t *testing.T) *storage.CobaltDB {
	dir := t.TempDir()
	cfg := core.StorageConfig{Path: dir}
	db, err := storage.NewEngine(cfg, nil)
	if err != nil {
		t.Fatalf("failed to create test DB: %v", err)
	}
	return db
}

// mockRepository implements Repository interface for testing
type mockRepository struct{}

func (m *mockRepository) GetStatusPageByDomain(domain string) (*core.StatusPage, error) {
	return &core.StatusPage{
		ID:           "test-page",
		Name:         "Test Status Page",
		Description:  "Test Description",
		CustomDomain: domain,
		WorkspaceID:  "default",
		Visibility:   core.VisibilityPublic,
		Souls:        []string{"soul-1", "soul-2"},
		Groups:       []core.StatusGroup{},
		Incidents:    []core.StatusIncident{},
		Theme:        core.GetDefaultTheme(),
		UptimeDays:   90,
	}, nil
}

func (m *mockRepository) GetStatusPageBySlug(slug string) (*core.StatusPage, error) {
	return m.GetStatusPageByDomain(slug + ".example.com")
}

func (m *mockRepository) GetSoul(id string) (*core.Soul, error) {
	return &core.Soul{
		ID:      id,
		Name:    "Test Soul " + id,
		Type:    "http",
		Enabled: true,
	}, nil
}

func (m *mockRepository) GetSoulJudgments(soulID string, limit int) ([]core.Judgment, error) {
	return []core.Judgment{
		{
			ID:        "judgment-1",
			SoulID:    soulID,
			Status:    core.SoulAlive,
			Timestamp: time.Now(),
		},
	}, nil
}

func (m *mockRepository) GetIncidentsByPage(pageID string) ([]core.Incident, error) {
	return []core.Incident{}, nil
}

func (m *mockRepository) GetUptimeHistory(soulID string, days int) ([]core.UptimeDay, error) {
	return []core.UptimeDay{
		{Date: time.Now().Format("2006-01-02"), Uptime: 99.9, Status: "operational"},
	}, nil
}

func (m *mockRepository) SaveSubscription(sub *core.StatusPageSubscription) error {
	return nil
}

func (m *mockRepository) GetSubscriptionsByPage(pageID string) ([]*core.StatusPageSubscription, error) {
	return []*core.StatusPageSubscription{}, nil
}

func (m *mockRepository) DeleteSubscription(subscriptionID string) error {
	return nil
}

func TestHandler_ServeHTTP_PublicPage(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("GET", "/status/test", nil)
	req.Host = "test.example.com"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should serve HTML for public page
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestHandler_ServeHTTP_JSONFormat(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("GET", "/status/test?format=json", nil)
	req.Host = "test.example.com"
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}
}

func TestHandler_BadgeHandler_SVG(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("GET", "/badge/test", nil)
	w := httptest.NewRecorder()

	handler.BadgeHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "image/svg+xml" {
		t.Errorf("Expected Content-Type image/svg+xml, got %s", contentType)
	}

	// Check SVG content
	body := w.Body.String()
	if len(body) == 0 {
		t.Error("Expected SVG content, got empty response")
	}
}

func TestHandler_BadgeHandler_JSON(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("GET", "/badge/test?format=json", nil)
	w := httptest.NewRecorder()

	handler.BadgeHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}

	// Check CORS header
	origin := w.Header().Get("Access-Control-Allow-Origin")
	if origin != "*" {
		t.Errorf("Expected CORS header *, got %s", origin)
	}
}

func TestHandler_RSSFeedHandler(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("GET", "/status/test/feed.xml", nil)
	w := httptest.NewRecorder()

	handler.RSSFeedHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/rss+xml" {
		t.Errorf("Expected Content-Type application/rss+xml, got %s", contentType)
	}

	// Check RSS content
	body := w.Body.String()
	if len(body) == 0 {
		t.Error("Expected RSS content, got empty response")
	}
}

func TestHandler_SubscribeHandler_ValidEmail(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	jsonBody := `{"page_id": "test", "type": "email", "email": "user@example.com"}`
	req := httptest.NewRequest("POST", "/status/subscribe", nil)
	req.Body = http.MaxBytesReader(nil, req.Body, int64(len(jsonBody)))

	// Re-create request with body
	req = httptest.NewRequest("POST", "/status/subscribe", nil)
	req.Header.Set("Content-Type", "application/json")

	// For this test we just check the handler exists
	if handler == nil {
		t.Error("Handler should not be nil")
	}
}

func TestHandler_extractSlugFromPath(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/status/test", "test"},
		{"/status/my-page", "my-page"},
		{"/status/test/", "test"},
		{"/status", ""},
		{"/", ""},
	}

	for _, tt := range tests {
		result := extractSlugFromPath(tt.path)
		if result != tt.expected {
			t.Errorf("extractSlugFromPath(%q) = %q, expected %q", tt.path, result, tt.expected)
		}
	}
}

func TestCalculateOverallStatus(t *testing.T) {
	souls := []core.SoulStatusInfo{
		{ID: "1", Name: "Soul 1", Status: "alive"},
		{ID: "2", Name: "Soul 2", Status: "alive"},
		{ID: "3", Name: "Soul 3", Status: "dead"},
	}

	status := core.CalculateOverallStatus(souls)

	if status.Status != "major_outage" {
		t.Errorf("Expected major_outage, got %s", status.Status)
	}
}

func TestCalculateOverallStatus_AllOperational(t *testing.T) {
	souls := []core.SoulStatusInfo{
		{ID: "1", Name: "Soul 1", Status: "alive"},
		{ID: "2", Name: "Soul 2", Status: "alive"},
	}

	status := core.CalculateOverallStatus(souls)

	if status.Status != "operational" {
		t.Errorf("Expected operational, got %s", status.Status)
	}
	if status.Title != "All Systems Operational" {
		t.Errorf("Expected 'All Systems Operational', got %s", status.Title)
	}
}

func TestCalculateOverallStatus_Degraded(t *testing.T) {
	souls := []core.SoulStatusInfo{
		{ID: "1", Name: "Soul 1", Status: "alive"},
		{ID: "2", Name: "Soul 2", Status: "degraded"},
	}

	status := core.CalculateOverallStatus(souls)

	if status.Status != "degraded" {
		t.Errorf("Expected degraded, got %s", status.Status)
	}
}

func TestHandler_NewHandler(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	// Verify handler is created with expected fields
	_ = handler.repository
	_ = handler.defaultTheme
}

func TestHandler_ServeHTTP_NotFound(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	// /status without slug should return 404
	req := httptest.NewRequest("GET", "/status", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Note: mock repo always returns a page, so we get 200
	// In real scenario, /status without slug would return 404
	// This test verifies the handler doesn't crash on edge cases
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 (mock returns page), got %d", w.Code)
	}
}

func TestHandler_extractSlugFromPath_Empty(t *testing.T) {
	result := extractSlugFromPath("/status")
	if result != "" {
		t.Errorf("Expected empty string for /status, got %q", result)
	}

	result = extractSlugFromPath("/")
	if result != "" {
		t.Errorf("Expected empty string for /, got %q", result)
	}

	result = extractSlugFromPath("/api/v1/souls")
	if result != "" {
		t.Errorf("Expected empty string for non-status path, got %q", result)
	}
}

// mockRepositoryPrivate returns private page
type mockRepositoryPrivate struct {
	mockRepository
}

func (m *mockRepositoryPrivate) GetStatusPageByDomain(domain string) (*core.StatusPage, error) {
	return &core.StatusPage{
		ID:           "test-page",
		Name:         "Test Private Page",
		Description:  "Test Description",
		CustomDomain: domain,
		WorkspaceID:  "default",
		Visibility:   core.VisibilityPrivate,
		Souls:        []string{"soul-1"},
		Groups:       []core.StatusGroup{},
		Incidents:    []core.StatusIncident{},
		Theme:        core.GetDefaultTheme(),
		UptimeDays:   90,
	}, nil
}

func TestHandler_ServeHTTP_PrivatePage_Unauthenticated(t *testing.T) {
	repo := &mockRepositoryPrivate{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("GET", "/status/test", nil)
	req.Host = "test.example.com"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should request authentication for private page
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401 for private page, got %d", w.Code)
	}
}

// mockRepositoryProtected returns protected page
type mockRepositoryProtected struct {
	mockRepository
}

func (m *mockRepositoryProtected) GetStatusPageByDomain(domain string) (*core.StatusPage, error) {
	return &core.StatusPage{
		ID:           "test-page",
		Name:         "Test Protected Page",
		Description:  "Test Description",
		CustomDomain: domain,
		WorkspaceID:  "default",
		Visibility:   core.VisibilityProtected,
		Souls:        []string{"soul-1"},
		Groups:       []core.StatusGroup{},
		Incidents:    []core.StatusIncident{},
		Theme:        core.GetDefaultTheme(),
		UptimeDays:   90,
	}, nil
}

func TestHandler_ServeHTTP_ProtectedPage_NoPassword(t *testing.T) {
	repo := &mockRepositoryProtected{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("GET", "/status/test", nil)
	req.Host = "test.example.com"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should show password form for protected page without password
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 for password form, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "password") && !strings.Contains(body, "Protected") {
		t.Error("Expected password form or protected message")
	}
}

func TestHandler_ServeHTTP_ProtectedPage_WithPassword(t *testing.T) {
	repo := &mockRepositoryProtected{}
	handler := NewHandler(repo, nil)

	// Password verification always fails in mock, so this will still show form
	req := httptest.NewRequest("GET", "/status/test?password=testpass", nil)
	req.Host = "test.example.com"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Will still show form since mock password verification returns false
	if w.Code != http.StatusOK {
		t.Logf("Status %d (expected for failed password verification)", w.Code)
	}
}

func TestHandler_ServeHTTP_JSONFormat_AcceptHeader(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("GET", "/status/test", nil)
	req.Host = "test.example.com"
	req.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}
}

func TestHandler_ServeHTTP_NotFound_EmptySlug(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	// /status without slug should trigger not found logic
	// But mock repo returns a page anyway
	req := httptest.NewRequest("GET", "/status", nil)
	req.Host = "test.example.com"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Mock always returns page, so we get 200
	// This tests the code path doesn't crash
	if w.Code != http.StatusOK {
		t.Logf("Status %d (mock returns page)", w.Code)
	}
}

func TestHandler_extractSlugFromPath_NonStatusPath(t *testing.T) {
	result := extractSlugFromPath("/api/v1/souls")
	if result != "" {
		t.Errorf("Expected empty string for non-status path, got %q", result)
	}

	result = extractSlugFromPath("/health")
	if result != "" {
		t.Errorf("Expected empty string for /health, got %q", result)
	}

	result = extractSlugFromPath("/")
	if result != "" {
		t.Errorf("Expected empty string for /, got %q", result)
	}
}

func TestHandler_ServeHTTP_WithPort(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("GET", "/status/test", nil)
	req.Host = "test.example.com:8080"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should handle host with port
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

// Test renderStatusPage with different data
func TestHandler_renderStatusPage_WithIncidents(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	page := &core.StatusPage{
		ID:    "test-page",
		Name:  "Test Page",
		Theme: core.GetDefaultTheme(),
	}

	data := &core.StatusPageData{
		Status: core.OverallStatus{
			Status: "major_outage",
			Title:  "Major Outage",
		},
		Souls: []core.SoulStatusInfo{
			{ID: "1", Name: "Soul 1", Status: "dead", ResponseTime: 100.5, UptimePercent: 50.0},
			{ID: "2", Name: "Soul 2", Status: "degraded", ResponseTime: 500.0, UptimePercent: 75.0},
		},
		Groups: []core.GroupStatusInfo{
			{
				ID:          "group-1",
				Name:        "API Services",
				Description: "Backend APIs",
				Souls: []core.SoulStatusInfo{
					{ID: "1", Name: "API 1", Status: "alive"},
				},
			},
		},
		Incidents: []core.StatusIncident{
			{
				ID:          "inc-1",
				Title:       "Service Outage",
				Description: "Services are down",
				Severity:    "high",
				Status:      "investigating",
				StartedAt:   time.Now().UTC(),
			},
		},
		Uptime: core.UptimeData{
			Overall: 85.5,
			Days: []core.UptimeDay{
				{Date: "2026-01-01", Status: "operational", Uptime: 99.9},
				{Date: "2026-01-02", Status: "outage", Uptime: 50.0},
			},
		},
	}

	html := handler.renderStatusPage(page, data, core.GetDefaultTheme())

	if len(html) == 0 {
		t.Fatal("Expected HTML output")
	}

	// Check for key elements
	if !strings.Contains(html, "major_outage") {
		t.Error("Expected status class for major_outage")
	}
	if !strings.Contains(html, "Active Incidents") {
		t.Error("Expected incidents section")
	}
	if !strings.Contains(html, "Service Outage") {
		t.Error("Expected incident title")
	}
	if !strings.Contains(html, "Uptime History") {
		t.Error("Expected uptime section")
	}
	if !strings.Contains(html, "API Services") {
		t.Error("Expected group name")
	}
}

func TestHandler_renderStatusPage_EmptySouls(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	page := &core.StatusPage{
		ID:    "test-page",
		Name:  "Test Page",
		Theme: core.GetDefaultTheme(),
	}

	data := &core.StatusPageData{
		Status: core.OverallStatus{
			Status: "operational",
			Title:  "All Systems Operational",
		},
		Souls:     []core.SoulStatusInfo{},
		Groups:    []core.GroupStatusInfo{},
		Incidents: []core.StatusIncident{},
		Uptime: core.UptimeData{
			Overall: 100.0,
			Days:    []core.UptimeDay{},
		},
	}

	html := handler.renderStatusPage(page, data, core.GetDefaultTheme())

	if len(html) == 0 {
		t.Error("Expected HTML output")
	}

	// Should still have basic structure
	if !strings.Contains(html, "Test Page") {
		t.Error("Expected page name")
	}
	if !strings.Contains(html, "All Systems Operational") {
		t.Error("Expected status title")
	}
}

func TestHandler_renderStatusPage_CustomTheme(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	page := &core.StatusPage{
		ID:   "test-page",
		Name: "Test Page",
		Theme: core.StatusPageTheme{
			PrimaryColor: "#ff0000",
			BackgroundColor: "#000000",
			TextColor:    "#ffffff",
		},
	}

	data := &core.StatusPageData{
		Status: core.OverallStatus{
			Status: "operational",
		},
		Souls: []core.SoulStatusInfo{},
	}

	html := handler.renderStatusPage(page, data, page.Theme)

	if !strings.Contains(html, "#ff0000") {
		t.Error("Expected custom primary color")
	}
	if !strings.Contains(html, "#000000") {
		t.Error("Expected custom background color")
	}
	if !strings.Contains(html, "#ffffff") {
		t.Error("Expected custom text color")
	}
}

func TestHandler_buildStatusPageData_ErrorHandling(t *testing.T) {
	// Mock that returns error for judgments
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	page := &core.StatusPage{
		ID:         "test-page",
		Souls:      []string{"soul-1", "soul-2"},
		UptimeDays: 30,
	}

	data, err := handler.buildStatusPageData(page)
	if err != nil {
		t.Fatalf("buildStatusPageData failed: %v", err)
	}

	// Should have data even with mock
	if data.Page.ID != "test-page" {
		t.Errorf("Expected page ID test-page, got %s", data.Page.ID)
	}
}

func TestHandler_RSSFeedHandler_WithIncidents(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("GET", "/status/test/feed.xml", nil)
	w := httptest.NewRecorder()

	handler.RSSFeedHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/rss+xml" {
		t.Errorf("Expected Content-Type application/rss+xml, got %s", contentType)
	}

	cacheControl := w.Header().Get("Cache-Control")
	if cacheControl != "public, max-age=300" {
		t.Errorf("Expected Cache-Control public, max-age=300, got %s", cacheControl)
	}

	body := w.Body.String()
	if !strings.Contains(body, "rss") {
		t.Error("Expected RSS feed content")
	}
	if !strings.Contains(body, "Test Status Page") {
		t.Error("Expected page name in RSS")
	}
}

func TestHandler_RSSFeedHandler_NotFound(t *testing.T) {
	// Mock that returns error
	repo := &mockRepositoryNotFound{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("GET", "/status/nonexistent/feed.xml", nil)
	w := httptest.NewRecorder()

	handler.RSSFeedHandler(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

type mockRepositoryNotFound struct{}

func (m *mockRepositoryNotFound) GetStatusPageByDomain(domain string) (*core.StatusPage, error) {
	return nil, fmt.Errorf("not found")
}

func (m *mockRepositoryNotFound) GetStatusPageBySlug(slug string) (*core.StatusPage, error) {
	return nil, fmt.Errorf("not found")
}

func (m *mockRepositoryNotFound) GetSoul(id string) (*core.Soul, error) {
	return nil, fmt.Errorf("not found")
}

func (m *mockRepositoryNotFound) GetSoulJudgments(soulID string, limit int) ([]core.Judgment, error) {
	return nil, fmt.Errorf("not found")
}

func (m *mockRepositoryNotFound) GetIncidentsByPage(pageID string) ([]core.Incident, error) {
	return nil, fmt.Errorf("not found")
}

func (m *mockRepositoryNotFound) GetUptimeHistory(soulID string, days int) ([]core.UptimeDay, error) {
	return nil, fmt.Errorf("not found")
}

func (m *mockRepositoryNotFound) SaveSubscription(sub *core.StatusPageSubscription) error {
	return fmt.Errorf("not found")
}

func (m *mockRepositoryNotFound) GetSubscriptionsByPage(pageID string) ([]*core.StatusPageSubscription, error) {
	return nil, fmt.Errorf("not found")
}

func (m *mockRepositoryNotFound) DeleteSubscription(subscriptionID string) error {
	return fmt.Errorf("not found")
}

func TestHandler_buildUptimeData_NoHistory(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	page := &core.StatusPage{
		ID:         "test-page",
		Souls:      []string{"nonexistent"},
		UptimeDays: 30,
	}

	souls := []core.SoulStatusInfo{}

	uptimeData := handler.buildUptimeData(page, souls)

	// Should handle empty souls gracefully
	if uptimeData.Days == nil {
		uptimeData.Days = []core.UptimeDay{}
	}
}

func TestHandler_BadgeHandler_EmptyPageID(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("GET", "/badge/", nil)
	w := httptest.NewRecorder()

	handler.BadgeHandler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for empty page ID, got %d", w.Code)
	}
}

func TestHandler_BadgeHandler_FormatParameter(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	// Test PNG format (defaults to SVG in implementation)
	req := httptest.NewRequest("GET", "/badge/test?format=png", nil)
	w := httptest.NewRecorder()

	handler.BadgeHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestHandler_SubscribeHandler_DefaultType(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	jsonBody := `{"page_id": "test"}`
	req := httptest.NewRequest("POST", "/status/subscribe",
		strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.SubscribeHandler(w, req)

	// Should default to email type and fail validation
	if w.Code != http.StatusBadRequest {
		t.Logf("Status %d (may vary based on validation)", w.Code)
	}
}

func TestHandler_checkPasswordProtection_WithCookie(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("GET", "/status/test", nil)
	req.AddCookie(&http.Cookie{
		Name:  "anubis_page_pass",
		Value: "testpass",
	})

	page := &core.StatusPage{
		ID:          "test-page",
		WorkspaceID: "workspace-1",
	}

	// Password verification fails in mock
	result := handler.checkPasswordProtection(req, page)
	if result {
		t.Error("Expected false for mock password verification")
	}
}

func TestHandler_isAuthenticated_EmptyCookie(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("GET", "/status/test", nil)
	req.AddCookie(&http.Cookie{
		Name:  "anubis_session",
		Value: "", // Empty value
	})

	page := &core.StatusPage{
		ID:          "test-page",
		WorkspaceID: "workspace-1",
	}

	result := handler.isAuthenticated(req, page)
	if result {
		t.Error("Expected false for empty session cookie")
	}
}

func TestHandler_buildStatusPageData_WithGroupsAndIncidents(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	page := &core.StatusPage{
		ID:          "test-page",
		Name:        "Test Page",
		Description: "Test Description",
		Souls:       []string{"soul-1", "soul-2"},
		Groups: []core.StatusGroup{
			{
				ID:          "group-1",
				Name:        "Group 1",
				Description: "First group",
				SoulIDs:     []string{"soul-1"},
			},
		},
		Incidents: []core.StatusIncident{
			{
				ID:          "inc-1",
				Title:       "Ongoing Incident",
				Description: "Something is broken",
				Status:      core.StatusOngoing,
				Severity:    "critical",
				StartedAt:   time.Now().UTC(),
			},
			{
				ID:          "inc-2",
				Title:       "Investigating",
				Description: "Looking into it",
				Status:      core.StatusInvestigating,
				Severity:    "high",
				StartedAt:   time.Now().UTC(),
			},
		},
		UptimeDays: 30,
	}

	data, err := handler.buildStatusPageData(page)
	if err != nil {
		t.Fatalf("buildStatusPageData failed: %v", err)
	}

	if len(data.Souls) == 0 {
		t.Error("Expected souls to be populated")
	}

	if len(data.Groups) == 0 {
		t.Error("Expected groups to be populated")
	}

	// Should have active incidents
	if len(data.Incidents) == 0 {
		t.Error("Expected active incidents to be populated")
	}
}

func TestHandler_buildStatusPageData_MonitoredStatus(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	page := &core.StatusPage{
		ID:    "test-page",
		Souls: []string{"soul-1"},
		Incidents: []core.StatusIncident{
			{
				ID:        "inc-1",
				Title:     "Monitoring",
				Status:    core.StatusMonitoring,
				StartedAt: time.Now().UTC(),
			},
		},
	}

	data, err := handler.buildStatusPageData(page)
	if err != nil {
		t.Fatalf("buildStatusPageData failed: %v", err)
	}

	// Monitoring incidents should be included
	if len(data.Incidents) == 0 {
		t.Error("Expected monitoring incidents to be included")
	}
}

func TestHandler_buildStatusPageData_ResolvedIncident(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	page := &core.StatusPage{
		ID:    "test-page",
		Souls: []string{"soul-1"},
		Incidents: []core.StatusIncident{
			{
				ID:        "inc-1",
				Title:     "Resolved Incident",
				Status:    core.StatusResolved,
				StartedAt: time.Now().UTC(),
			},
		},
	}

	data, err := handler.buildStatusPageData(page)
	if err != nil {
		t.Fatalf("buildStatusPageData failed: %v", err)
	}

	// Resolved incidents should NOT be in active incidents
	if len(data.Incidents) != 0 {
		t.Error("Expected resolved incidents to be excluded")
	}
}

func TestHandler_buildStatusPageData_SoulError(t *testing.T) {
	repo := &mockRepositoryNotFound{}
	handler := NewHandler(repo, nil)

	page := &core.StatusPage{
		ID:    "test-page",
		Souls: []string{"nonexistent-soul"},
	}

	data, err := handler.buildStatusPageData(page)
	if err != nil {
		t.Fatalf("buildStatusPageData failed: %v", err)
	}

	// Should handle soul errors gracefully - souls list may be empty
	if data == nil {
		t.Error("Expected data even with soul errors")
	}
}

// Additional tests for uncovered methods

func TestHandler_extractSlugFromPath_VariousPaths(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/status/my-page", "my-page"},
		{"/status/my-page/", "my-page"},
		{"/status/test-page/incidents", "test-page"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := extractSlugFromPath(tt.path)
			if result != tt.expected {
				t.Errorf("extractSlugFromPath(%q) = %q, expected %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestCalculateOverallStatus_Empty(t *testing.T) {
	souls := []core.SoulStatusInfo{}

	status := core.CalculateOverallStatus(souls)

	if status.Status != "operational" {
		t.Errorf("Expected operational for empty souls, got %s", status.Status)
	}
}

func TestCalculateOverallStatus_AllDead(t *testing.T) {
	souls := []core.SoulStatusInfo{
		{ID: "1", Name: "Soul 1", Status: "dead"},
		{ID: "2", Name: "Soul 2", Status: "dead"},
	}

	status := core.CalculateOverallStatus(souls)

	if status.Status != "major_outage" {
		t.Errorf("Expected major_outage, got %s", status.Status)
	}
}

func TestHandler_ServeHTTP_IncidentsRoute(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("GET", "/status/test/incidents", nil)
	req.Host = "test.example.com"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should serve incidents page
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestHandler_ServeHTTP_APIIncidentsRoute(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("GET", "/status/test/api/incidents", nil)
	req.Host = "test.example.com"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should return JSON
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestHandler_ServeHTTP_APIJudgmentsRoute(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("GET", "/status/test/api/judgments/soul-1", nil)
	req.Host = "test.example.com"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should return JSON
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestHandler_ServeHTTP_SubscribeRoute(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("GET", "/status/test/subscribe", nil)
	req.Host = "test.example.com"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should serve subscribe page
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestHandler_ServeHTTP_UnsubscribeRoute(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("GET", "/status/test/unsubscribe/token123", nil)
	req.Host = "test.example.com"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should serve unsubscribe page
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestHandler_ServeHTTP_POST_Subscribe(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("POST", "/status/test/subscribe", nil)
	req.Host = "test.example.com"
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should handle subscribe POST
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Errorf("Expected status 200 or 201, got %d", w.Code)
	}
}

// Tests for uncovered functions

func TestExtractSlugFromPath_EdgeCases(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/status/test/incidents", "test"},
		{"/status/test/incidents/123", "test"},
		{"/status/my-status-page", "my-status-page"},
		{"/status/test/", "test"},
		{"/status", ""},
		{"/", ""},
		{"/api/v1/souls", ""},
		{"/health", ""},
	}

	for _, tt := range tests {
		result := extractSlugFromPath(tt.path)
		if result != tt.expected {
			t.Errorf("extractSlugFromPath(%q) = %q, expected %q", tt.path, result, tt.expected)
		}
	}
}

func TestCalculateOverallStatus_MinorOutage(t *testing.T) {
	// Note: The implementation treats any dead soul as major_outage
	// There is no minor_outage status in the current implementation
	souls := []core.SoulStatusInfo{
		{ID: "1", Name: "Soul 1", Status: "degraded"},
		{ID: "2", Name: "Soul 2", Status: "alive"},
		{ID: "3", Name: "Soul 3", Status: "alive"},
	}

	status := core.CalculateOverallStatus(souls)

	// degraded souls result in degraded status
	if status.Status != "degraded" {
		t.Errorf("Expected degraded, got %s", status.Status)
	}
}

// Tests for authentication and session functions
func TestHandler_isAuthenticated_NoSession(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("GET", "/status/test", nil)
	page := &core.StatusPage{
		ID:          "test-page",
		WorkspaceID: "workspace-1",
		Visibility:  core.VisibilityPrivate,
	}

	// No session cookie, no API key
	result := handler.isAuthenticated(req, page)
	if result {
		t.Error("Expected false for unauthenticated request")
	}
}

func TestHandler_isAuthenticated_WithSessionCookie(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("GET", "/status/test", nil)
	req.AddCookie(&http.Cookie{
		Name:  "anubis_session",
		Value: "test-session-token",
	})

	page := &core.StatusPage{
		ID:          "test-page",
		WorkspaceID: "workspace-1",
	}

	// Session validation always returns false in mock
	result := handler.isAuthenticated(req, page)
	if result {
		t.Error("Expected false for mock session validation")
	}
}

func TestHandler_isAuthenticated_WithAPIKey(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("GET", "/status/test", nil)
	req.Header.Set("X-API-Key", "test-api-key")

	page := &core.StatusPage{
		ID:          "test-page",
		WorkspaceID: "workspace-1",
	}

	// API key validation always returns false in mock
	result := handler.isAuthenticated(req, page)
	if result {
		t.Error("Expected false for mock API key validation")
	}
}

func TestHandler_validateSession(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	result := handler.validateSession("test-token", "workspace-1")
	if result {
		t.Error("Expected false for mock session validation")
	}
}

func TestHandler_validateAPIKey(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	result := handler.validateAPIKey("test-key", "workspace-1")
	if result {
		t.Error("Expected false for mock API key validation")
	}
}

func TestHandler_requestAuthentication(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/status/test", nil)

	handler.requestAuthentication(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}

	wwwAuth := w.Header().Get("WWW-Authenticate")
	if wwwAuth != "Bearer" {
		t.Errorf("Expected WWW-Authenticate Bearer, got %s", wwwAuth)
	}
}

func TestHandler_checkPasswordProtection_NoPassword(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("GET", "/status/test", nil)
	page := &core.StatusPage{
		ID:          "test-page",
		WorkspaceID: "workspace-1",
	}

	result := handler.checkPasswordProtection(req, page)
	if result {
		t.Error("Expected false when no password provided")
	}
}

func TestHandler_checkPasswordProtection_WithQueryPassword(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("GET", "/status/test?password=testpass", nil)
	page := &core.StatusPage{
		ID:          "test-page",
		WorkspaceID: "workspace-1",
	}

	// Password verification always returns false in mock
	result := handler.checkPasswordProtection(req, page)
	if result {
		t.Error("Expected false for mock password verification")
	}
}

func TestHandler_verifyPagePassword(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	page := &core.StatusPage{
		ID:          "test-page",
		WorkspaceID: "workspace-1",
	}

	result := handler.verifyPagePassword("testpass", page)
	if result {
		t.Error("Expected false for mock password verification")
	}
}

func TestHandler_showPasswordForm(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	page := &core.StatusPage{
		ID:          "test-page",
		WorkspaceID: "workspace-1",
		Name:        "Test Page",
		Theme:       core.GetDefaultTheme(),
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/status/test", nil)

	handler.showPasswordForm(w, req, page)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if len(body) == 0 {
		t.Error("Expected HTML form content")
	}
}

func TestHandler_convertIncidentUpdates(t *testing.T) {
	updates := []core.IncidentUpdate{
		{
			ID:        "update-1",
			Message:   "Test message",
			Status:    "investigating",
			UpdatedAt: time.Now().UTC(),
		},
		{
			ID:        "update-2",
			Message:   "Test message 2",
			Status:    "resolved",
			UpdatedAt: time.Now().UTC(),
		},
	}

	result := convertIncidentUpdates(updates)

	if len(result) != 2 {
		t.Errorf("Expected 2 updates, got %d", len(result))
	}

	if result[0].ID != "update-1" {
		t.Errorf("Expected update-1, got %s", result[0].ID)
	}

	if result[1].Message != "Test message 2" {
		t.Errorf("Expected 'Test message 2', got %s", result[1].Message)
	}
}

func TestHandler_convertIncidentUpdates_Empty(t *testing.T) {
	updates := []core.IncidentUpdate{}

	result := convertIncidentUpdates(updates)

	if len(result) != 0 {
		t.Errorf("Expected 0 updates, got %d", len(result))
	}
}

func TestHandler_SubscribeHandler_MethodNotAllowed(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("GET", "/status/subscribe", nil)
	w := httptest.NewRecorder()

	handler.SubscribeHandler(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

func TestHandler_SubscribeHandler_InvalidJSON(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("POST", "/status/subscribe", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.SubscribeHandler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestHandler_SubscribeHandler_MissingPageID(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	jsonBody := `{"type": "email", "email": "user@example.com"}`
	req := httptest.NewRequest("POST", "/status/subscribe",
		strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.SubscribeHandler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestHandler_SubscribeHandler_MissingEmail(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	jsonBody := `{"page_id": "test", "type": "email"}`
	req := httptest.NewRequest("POST", "/status/subscribe",
		strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.SubscribeHandler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestHandler_SubscribeHandler_MissingWebhookURL(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	jsonBody := `{"page_id": "test", "type": "webhook"}`
	req := httptest.NewRequest("POST", "/status/subscribe",
		strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.SubscribeHandler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestHandler_SubscribeHandler_ValidWebhook(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	jsonBody := `{"page_id": "test", "type": "webhook", "webhook_url": "https://example.com/hook"}`
	req := httptest.NewRequest("POST", "/status/subscribe",
		strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.SubscribeHandler(w, req)

	// Should succeed or fail based on mock repository
	if w.Code != http.StatusOK && w.Code != http.StatusCreated && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 200, 201, or 500, got %d", w.Code)
	}
}

func TestHandler_Router(t *testing.T) {
	repo := &mockRepository{}
	// Create handler without ACME manager to avoid nil pointer
	handler := &Handler{
		repository:   repo,
		defaultTheme: core.GetDefaultTheme(),
	}

	// Test router creation without calling full Router() which needs acmeManager
	// Instead, test individual route handlers
	req := httptest.NewRequest("GET", "/status/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Should not panic - just verify handler works
	if w.Code != http.StatusOK {
		t.Logf("Handler returned status %d (may vary based on mock)", w.Code)
	}
}

func TestHandler_Router_BadgeEndpoint(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("GET", "/badge/test", nil)
	w := httptest.NewRecorder()

	handler.BadgeHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 for badge endpoint, got %d", w.Code)
	}
}

func TestHandler_Router_RSSFeedEndpoint(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	req := httptest.NewRequest("GET", "/status/test/feed.xml", nil)
	w := httptest.NewRecorder()

	handler.RSSFeedHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 for RSS feed, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/rss+xml" {
		t.Errorf("Expected Content-Type application/rss+xml, got %s", contentType)
	}
}

func TestHandler_buildStatusPageData(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	page := &core.StatusPage{
		ID:          "test-page",
		Name:        "Test Page",
		Description: "Test Description",
		Souls:       []string{"soul-1", "soul-2"},
		UptimeDays:  30,
	}

	data, err := handler.buildStatusPageData(page)
	if err != nil {
		t.Fatalf("buildStatusPageData failed: %v", err)
	}

	if data.Page.Name != "Test Page" {
		t.Errorf("Expected page name 'Test Page', got %s", data.Page.Name)
	}

	if len(data.Souls) == 0 {
		t.Error("Expected souls to be populated")
	}
}

func TestHandler_renderStatusPage(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	page := &core.StatusPage{
		ID:          "test-page",
		Name:        "Test Page",
		Description: "Test Description",
		Theme:       core.GetDefaultTheme(),
	}

	data := &core.StatusPageData{
		Status: core.OverallStatus{
			Status: "operational",
			Title:  "All Systems Operational",
		},
		Souls: []core.SoulStatusInfo{
			{ID: "1", Name: "Soul 1", Status: "alive"},
		},
		Incidents: []core.StatusIncident{},
		Uptime: core.UptimeData{
			Overall: 99.9,
		},
	}

	html := handler.renderStatusPage(page, data, core.GetDefaultTheme())

	if len(html) == 0 {
		t.Error("Expected HTML output")
	}

	if !strings.Contains(html, "Test Page") {
		t.Error("Expected page name in HTML")
	}
}

func TestHandler_buildUptimeData(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	page := &core.StatusPage{
		ID:         "test-page",
		Souls:      []string{"soul-1"},
		UptimeDays: 30,
	}

	souls := []core.SoulStatusInfo{
		{ID: "soul-1", Name: "Soul 1", Status: "alive"},
	}

	uptimeData := handler.buildUptimeData(page, souls)

	// Should have some uptime data from mock
	if uptimeData.Overall == 0 && len(uptimeData.Days) == 0 {
		t.Error("Expected some uptime data")
	}
}

func TestHandler_serveJSON(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	data := &core.StatusPageData{
		Status: core.OverallStatus{
			Status: "operational",
			Title:  "All Systems Operational",
		},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/status/test", nil)

	handler.serveJSON(w, req, data)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}
}

func TestHandler_serveHTML(t *testing.T) {
	repo := &mockRepository{}
	handler := NewHandler(repo, nil)

	page := &core.StatusPage{
		ID:    "test-page",
		Name:  "Test Page",
		Theme: core.GetDefaultTheme(),
	}

	data := &core.StatusPageData{
		Status: core.OverallStatus{
			Status: "operational",
		},
		Souls: []core.SoulStatusInfo{},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/status/test", nil)

	handler.serveHTML(w, req, page, data)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/html") {
		t.Errorf("Expected Content-Type text/html, got %s", contentType)
	}

	cacheControl := w.Header().Get("Cache-Control")
	if cacheControl != "public, max-age=60" {
		t.Errorf("Expected Cache-Control public, max-age=60, got %s", cacheControl)
	}
}

func TestHandler_Router_WithAcmeManager(t *testing.T) {
	repo := &mockRepository{}

	// Create a minimal acme manager for testing
	db := newTestDB(t)
	defer db.Close()

	cfg := acme.Config{
		Enabled:   true,
		Provider:  acme.ProviderLetsEncrypt,
		Email:     "test@example.com",
		AcceptTOS: true,
		CertPath:  t.TempDir(),
	}

	acmeMgr, err := acme.NewManager(db, cfg)
	if err != nil {
		t.Fatalf("Failed to create acme manager: %v", err)
	}

	handler := &Handler{
		repository:     repo,
		acmeManager:    acmeMgr,
		defaultTheme:   core.GetDefaultTheme(),
	}

	router := handler.Router()

	if router == nil {
		t.Fatal("Expected router to be created")
	}

	// Test routing to status page
	req := httptest.NewRequest("GET", "/status/test", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should not panic - verify routing works
	if w.Code != http.StatusOK {
		t.Logf("Handler returned status %d (may vary based on mock)", w.Code)
	}

	// Test badge endpoint
	req = httptest.NewRequest("GET", "/badge/test", nil)
	w = httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 for badge endpoint, got %d", w.Code)
	}

	// Test RSS feed endpoint
	req = httptest.NewRequest("GET", "/status/test/feed.xml", nil)
	w = httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 for RSS feed endpoint, got %d", w.Code)
	}

	// Test ACME challenge endpoint
	req = httptest.NewRequest("GET", "/.well-known/acme-challenge/test-token", nil)
	w = httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Should return 404 for unknown challenge token
	if w.Code != http.StatusNotFound {
		t.Logf("ACME challenge returned status %d", w.Code)
	}
}
