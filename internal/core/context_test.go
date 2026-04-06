package core

import (
	"context"
	"testing"
)

func TestContextWithWorkspaceID(t *testing.T) {
	tests := []struct {
		name        string
		workspaceID string
	}{
		{
			name:        "default workspace",
			workspaceID: "default",
		},
		{
			name:        "custom workspace",
			workspaceID: "workspace-123",
		},
		{
			name:        "empty workspace",
			workspaceID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			newCtx := ContextWithWorkspaceID(ctx, tt.workspaceID)

			// Verify context is not nil
			if newCtx == nil {
				t.Error("Context should not be nil")
			}

			// Verify value is set
			retrievedID := WorkspaceIDFromContext(newCtx)
			if retrievedID != tt.workspaceID {
				t.Errorf("Expected workspace ID %q, got %q", tt.workspaceID, retrievedID)
			}
		})
	}
}

func TestWorkspaceIDFromContext_NoValue(t *testing.T) {
	ctx := context.Background()

	// Should return "default" when no workspace ID is set
	id := WorkspaceIDFromContext(ctx)
	if id != "default" {
		t.Errorf("Expected 'default', got %q", id)
	}
}

func TestWorkspaceIDFromContext_InvalidType(t *testing.T) {
	// Test with wrong type in context
	ctx := context.WithValue(context.Background(), WorkspaceIDKey, 123)

	// Should return "default" when type is not string
	id := WorkspaceIDFromContext(ctx)
	if id != "default" {
		t.Errorf("Expected 'default' for invalid type, got %q", id)
	}
}

func TestWorkspaceIDFromContext_NilContext(t *testing.T) {
	// Even with nil value in context, should handle gracefully
	ctx := context.WithValue(context.Background(), WorkspaceIDKey, nil)

	id := WorkspaceIDFromContext(ctx)
	if id != "default" {
		t.Errorf("Expected 'default' for nil value, got %q", id)
	}
}

func TestContextKey_Constants(t *testing.T) {
	// Verify WorkspaceIDKey constant
	if WorkspaceIDKey != "workspace_id" {
		t.Errorf("Expected WorkspaceIDKey to be 'workspace_id', got %q", WorkspaceIDKey)
	}
}

func TestWorkspaceIDFromContext_DifferentContextKeys(t *testing.T) {
	// Test that other context keys don't interfere
	ctx := context.Background()
	ctx = context.WithValue(ctx, contextKey("other_key"), "other_value")
	ctx = ContextWithWorkspaceID(ctx, "my-workspace")

	id := WorkspaceIDFromContext(ctx)
	if id != "my-workspace" {
		t.Errorf("Expected 'my-workspace', got %q", id)
	}
}

func TestContextWithWorkspaceID_Chaining(t *testing.T) {
	// Test chaining multiple context values
	ctx := context.Background()
	ctx = ContextWithWorkspaceID(ctx, "workspace-1")
	ctx = ContextWithWorkspaceID(ctx, "workspace-2")

	id := WorkspaceIDFromContext(ctx)
	if id != "workspace-2" {
		t.Errorf("Expected 'workspace-2' (latest value), got %q", id)
	}
}

func TestWorkspaceIDFromContext_LongID(t *testing.T) {
	// Test with a very long workspace ID
	longID := "workspace-"
	for i := 0; i < 100; i++ {
		longID += "a"
		_ = i
	}

	ctx := ContextWithWorkspaceID(context.Background(), longID)
	retrieved := WorkspaceIDFromContext(ctx)

	if retrieved != longID {
		t.Error("Long workspace ID should be preserved")
	}
}

func TestWorkspaceIDFromContext_SpecialCharacters(t *testing.T) {
	specialID := "workspace-123_test.xyz@domain"

	ctx := ContextWithWorkspaceID(context.Background(), specialID)
	retrieved := WorkspaceIDFromContext(ctx)

	if retrieved != specialID {
		t.Errorf("Expected %q, got %q", specialID, retrieved)
	}
}
