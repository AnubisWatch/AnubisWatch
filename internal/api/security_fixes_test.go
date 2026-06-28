package api

import (
	"context"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// TestMCPCanAccessWorkspace_FailClosed verifies that empty workspace context
// defaults to "default" rather than granting access to all workspaces.
func TestMCPCanAccessWorkspace_FailClosed(t *testing.T) {
	// Empty context → requestWorkspace defaults to "default"
	ctx := context.Background()

	// Resource in "default" workspace → should be accessible
	if !mcpCanAccessWorkspace(ctx, "default") {
		t.Error("Expected access to default workspace with empty context")
	}

	// Resource in empty workspace → should be accessible (both default)
	if !mcpCanAccessWorkspace(ctx, "") {
		t.Error("Expected access when both request and resource are default/empty")
	}

	// Resource in "other" workspace → should be DENIED
	if mcpCanAccessWorkspace(ctx, "other-workspace") {
		t.Error("Expected denial to other-workspace with empty context")
	}

	// With workspace context set to "ws-a"
	ctxWithWs := core.ContextWithWorkspaceID(context.Background(), "ws-a")

	// Resource in same workspace → accessible
	if !mcpCanAccessWorkspace(ctxWithWs, "ws-a") {
		t.Error("Expected access to ws-a with ws-a context")
	}

	// Resource in different workspace → denied
	if mcpCanAccessWorkspace(ctxWithWs, "ws-b") {
		t.Error("Expected denial to ws-b with ws-a context")
	}
}
