package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// TestMCPCallToolRBAC verifies that mutating MCP tools are gated by the
// caller's workspace role at dispatch time, so the MCP endpoint cannot be used
// to bypass the RBAC enforced on the equivalent REST routes.
func TestMCPCallToolRBAC(t *testing.T) {
	server := NewMCPServer(newMockStorage(), &mockProbeEngine{}, &mockAlertManager{}, newTestLogger())

	call := func(role, tool, args string) *MCPResponse {
		ctx := core.ContextWithUserRole(context.Background(), role)
		params, _ := json.Marshal(map[string]json.RawMessage{
			"name":      json.RawMessage(`"` + tool + `"`),
			"arguments": json.RawMessage(args),
		})
		return server.handleCallTool(ctx, &MCPRequest{ID: 1, Params: params})
	}

	isDenied := func(resp *MCPResponse) bool {
		return resp.Error != nil && strings.Contains(resp.Error.Message, "permission denied")
	}

	cases := []struct {
		name       string
		role       string
		tool       string
		args       string
		wantDenied bool
	}{
		{"viewer denied create_soul", string(core.RoleViewer), "create_soul", `{"name":"x","type":"http","target":"https://a.example"}`, true},
		{"viewer denied acknowledge", string(core.RoleViewer), "acknowledge_incident", `{"incident_id":"i1"}`, true},
		{"no role denied create_soul", "", "create_soul", `{"name":"x","type":"http","target":"https://a.example"}`, true},
		{"editor allowed create_soul", string(core.RoleEditor), "create_soul", `{"name":"x","type":"http","target":"https://a.example"}`, false},
		{"admin allowed acknowledge", string(core.RoleAdmin), "acknowledge_incident", `{"incident_id":"i1"}`, false},
		{"editor denied acknowledge", string(core.RoleEditor), "acknowledge_incident", `{"incident_id":"i1"}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := call(tc.role, tc.tool, tc.args)
			if got := isDenied(resp); got != tc.wantDenied {
				t.Fatalf("role=%q tool=%q: denied=%v want=%v (err=%+v)", tc.role, tc.tool, got, tc.wantDenied, resp.Error)
			}
		})
	}

	// Read-only tools must remain callable by a viewer.
	resp := call(string(core.RoleViewer), "list_souls", `{}`)
	if isDenied(resp) {
		t.Fatalf("viewer should be allowed to list_souls, got %+v", resp.Error)
	}
}
