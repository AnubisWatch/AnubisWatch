package core

import "context"

// contextKey is a type-safe key for context values
type contextKey string

const (
	// WorkspaceIDKey is the context key for workspace ID
	WorkspaceIDKey contextKey = "workspace_id"
	// UserRoleKey is the context key for the authenticated user's workspace role
	UserRoleKey contextKey = "user_role"
)

// ContextWithWorkspaceID returns a context with the workspace ID attached
func ContextWithWorkspaceID(ctx context.Context, workspaceID string) context.Context {
	return context.WithValue(ctx, WorkspaceIDKey, workspaceID)
}

// WorkspaceIDFromContext extracts the workspace ID from a context
func WorkspaceIDFromContext(ctx context.Context) string {
	if v := ctx.Value(WorkspaceIDKey); v != nil {
		if id, ok := v.(string); ok {
			return id
		}
	}
	return "default"
}

// ContextWithUserRole returns a context with the caller's workspace role attached.
func ContextWithUserRole(ctx context.Context, role string) context.Context {
	return context.WithValue(ctx, UserRoleKey, role)
}

// UserRoleFromContext extracts the caller's workspace role from a context.
// Returns an empty string when no role is present.
func UserRoleFromContext(ctx context.Context) string {
	if v := ctx.Value(UserRoleKey); v != nil {
		if role, ok := v.(string); ok {
			return role
		}
	}
	return ""
}
