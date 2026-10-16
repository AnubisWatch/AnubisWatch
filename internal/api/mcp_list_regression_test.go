package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// Discovery methods used to return a 200 with an empty body because the
// Handler func fields made json encoding fail and the error was discarded.
func TestMCPServer_DiscoveryMethodsReturnEntries(t *testing.T) {
	server := NewMCPServer(newMockStorage(), &mockProbeEngine{}, &mockAlertManager{}, newTestLogger())

	tests := []struct {
		method string
		key    string
	}{
		{"tools/list", "tools"},
		{"resources/list", "resources"},
		{"prompts/list", "prompts"},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			body := `{"jsonrpc":"2.0","id":1,"method":"` + tt.method + `","params":{}}`
			w := httptest.NewRecorder()
			server.ServeHTTP(w, httptest.NewRequest("POST", "/mcp", strings.NewReader(body)))

			if w.Body.Len() == 0 {
				t.Fatalf("%s returned an empty body (status %d)", tt.method, w.Code)
			}
			var resp struct {
				Result map[string][]map[string]any `json:"result"`
				Error  any                         `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode %s: %v (body %q)", tt.method, err, w.Body.String())
			}
			if resp.Error != nil {
				t.Fatalf("%s returned error: %v", tt.method, resp.Error)
			}
			entries := resp.Result[tt.key]
			if len(entries) == 0 {
				t.Fatalf("%s returned no %s", tt.method, tt.key)
			}
			for _, e := range entries {
				if e["name"] == nil || e["name"] == "" {
					t.Errorf("%s entry without name: %v", tt.method, e)
				}
				if _, ok := e["Handler"]; ok {
					t.Errorf("%s leaked Handler field: %v", tt.method, e)
				}
			}
		})
	}
}
