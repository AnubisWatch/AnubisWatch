package api

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateClientID_Format(t *testing.T) {
	id := generateClientID()

	if !strings.HasPrefix(id, "ws_") {
		t.Errorf("Expected client ID to start with 'ws_', got %q", id)
	}

	// Should have more than just the prefix
	if len(id) <= 3 {
		t.Errorf("Expected client ID to have more than just prefix, got %q", id)
	}
}

func TestGenerateClientID_Uniqueness(t *testing.T) {
	id1 := generateClientID()
	time.Sleep(1 * time.Millisecond) // Ensure time advances
	id2 := generateClientID()

	if id1 == id2 {
		t.Errorf("Expected different client IDs, got same: %s", id1)
	}
}
