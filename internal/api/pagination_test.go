package api

import (
	"net/http/httptest"
	"testing"
)

// Test parsePagination with default values
func TestParsePagination_Defaults(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	offset, limit := parsePagination(req, 20, 100)
	if offset != 0 {
		t.Errorf("Expected offset 0, got %d", offset)
	}
	if limit != 20 {
		t.Errorf("Expected limit 20, got %d", limit)
	}
}

// Test parsePagination with custom values
func TestParsePagination_CustomValues(t *testing.T) {
	req := httptest.NewRequest("GET", "/test?offset=10&limit=50", nil)
	offset, limit := parsePagination(req, 20, 100)
	if offset != 10 {
		t.Errorf("Expected offset 10, got %d", offset)
	}
	if limit != 50 {
		t.Errorf("Expected limit 50, got %d", limit)
	}
}

// Test parsePagination with limit exceeding max
func TestParsePagination_LimitExceedsMax(t *testing.T) {
	req := httptest.NewRequest("GET", "/test?limit=200", nil)
	offset, limit := parsePagination(req, 20, 100)
	if offset != 0 {
		t.Errorf("Expected offset 0, got %d", offset)
	}
	// When limit exceeds max, it falls back to default
	if limit != 20 {
		t.Errorf("Expected limit 20 (default), got %d", limit)
	}
}

// Test parsePagination with invalid values
func TestParsePagination_InvalidValues(t *testing.T) {
	req := httptest.NewRequest("GET", "/test?offset=invalid&limit=invalid", nil)
	offset, limit := parsePagination(req, 20, 100)
	if offset != 0 {
		t.Errorf("Expected offset 0 for invalid value, got %d", offset)
	}
	if limit != 20 {
		t.Errorf("Expected limit 20 for invalid value, got %d", limit)
	}
}

// Test parsePagination with negative offset
func TestParsePagination_NegativeOffset(t *testing.T) {
	req := httptest.NewRequest("GET", "/test?offset=-5", nil)
	offset, limit := parsePagination(req, 20, 100)
	if offset != 0 {
		t.Errorf("Expected offset 0 for negative value, got %d", offset)
	}
	if limit != 20 {
		t.Errorf("Expected limit 20, got %d", limit)
	}
}

// Test parsePagination with limit at max boundary
func TestParsePagination_LimitAtBoundary(t *testing.T) {
	req := httptest.NewRequest("GET", "/test?limit=100", nil)
	_, limit := parsePagination(req, 20, 100)
	if limit != 100 {
		t.Errorf("Expected limit 100 at boundary, got %d", limit)
	}
}

// Test parsePagination with custom defaults
func TestParsePagination_CustomDefaults(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	_, limit := parsePagination(req, 10, 50)
	if limit != 10 {
		t.Errorf("Expected limit 10 (custom default), got %d", limit)
	}
}

// Test parsePagination with custom max
func TestParsePagination_CustomMax(t *testing.T) {
	req := httptest.NewRequest("GET", "/test?limit=500", nil)
	_, limit := parsePagination(req, 20, 200)
	// When limit exceeds max, it falls back to default
	if limit != 20 {
		t.Errorf("Expected limit 20 (default when exceeding max), got %d", limit)
	}
}
