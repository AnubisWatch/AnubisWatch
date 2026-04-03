package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// MCPServer implements the Model Context Protocol for AI integration
// The scribes commune with the artificial spirits
type MCPServer struct {
	mu        sync.RWMutex
	tools     map[string]MCPTool
	resources map[string]MCPResource
	prompts   map[string]MCPPrompt
	logger    *slog.Logger

	// Dependencies
	store  Storage
	probe  ProbeEngine
	alert  AlertManager
}

// MCPTool represents an MCP tool (function)
type MCPTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Handler     func(args json.RawMessage) (interface{}, error)
}

// MCPResource represents an MCP resource
type MCPResource struct {
	URI         string            `json:"uri"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	MIMEType    string            `json:"mimeType"`
	Handler     func() (interface{}, error)
}

// MCPPrompt represents an MCP prompt
type MCPPrompt struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Arguments   []MCPPromptArg    `json:"arguments"`
	Handler     func(args map[string]string) (string, error)
}

// MCPPromptArg represents a prompt argument
type MCPPromptArg struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// MCPRequest is an incoming MCP request
type MCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// MCPResponse is an MCP response
type MCPResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
}

// MCPError represents an MCP error
type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewMCPServer creates a new MCP server
func NewMCPServer(store Storage, probe ProbeEngine, alert AlertManager, logger *slog.Logger) *MCPServer {
	s := &MCPServer{
		tools:     make(map[string]MCPTool),
		resources: make(map[string]MCPResource),
		prompts:   make(map[string]MCPPrompt),
		logger:    logger.With("component", "mcp_server"),
		store:     store,
		probe:     probe,
		alert:     alert,
	}

	s.registerBuiltinTools()
	s.registerBuiltinResources()
	s.registerBuiltinPrompts()

	return s
}

// registerBuiltinTools registers MCP tools
func (s *MCPServer) registerBuiltinTools() {
	// Tool: list_souls
	s.tools["list_souls"] = MCPTool{
		Name:        "list_souls",
		Description: "List all monitored souls (monitors)",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"workspace":{"type":"string","description":"Filter by workspace"},"status":{"type":"string","description":"Filter by status (alive, dead, degraded)"}},"required":[]}`),
		Handler:     s.handleListSouls,
	}

	// Tool: get_soul
	s.tools["get_soul"] = MCPTool{
		Name:        "get_soul",
		Description: "Get details of a specific soul",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"soul_id":{"type":"string","description":"The soul ID"}},"required":["soul_id"]}`),
		Handler:     s.handleGetSoul,
	}

	// Tool: force_check
	s.tools["force_check"] = MCPTool{
		Name:        "force_check",
		Description: "Force an immediate health check on a soul",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"soul_id":{"type":"string","description":"The soul ID to check"}},"required":["soul_id"]}`),
		Handler:     s.handleForceCheck,
	}

	// Tool: get_judgments
	s.tools["get_judgments"] = MCPTool{
		Name:        "get_judgments",
		Description: "Get recent judgments for a soul",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"soul_id":{"type":"string","description":"The soul ID"},"limit":{"type":"integer","description":"Max results (default 10)"}},"required":["soul_id"]}`),
		Handler:     s.handleGetJudgments,
	}

	// Tool: list_incidents
	s.tools["list_incidents"] = MCPTool{
		Name:        "list_incidents",
		Description: "List active alert incidents",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"status":{"type":"string","description":"Filter by status (firing, acknowledged, resolved)"}},"required":[]}`),
		Handler:     s.handleListIncidents,
	}

	// Tool: get_stats
	s.tools["get_stats"] = MCPTool{
		Name:        "get_stats",
		Description: "Get monitoring statistics",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"workspace":{"type":"string","description":"Filter by workspace"}},"required":[]}`),
		Handler:     s.handleGetStats,
	}

	// Tool: acknowledge_incident
	s.tools["acknowledge_incident"] = MCPTool{
		Name:        "acknowledge_incident",
		Description: "Acknowledge an alert incident",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"incident_id":{"type":"string","description":"The incident ID"}},"required":["incident_id"]}`),
		Handler:     s.handleAcknowledgeIncident,
	}

	// Tool: create_soul
	s.tools["create_soul"] = MCPTool{
		Name:        "create_soul",
		Description: "Create a new monitoring soul",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","description":"Soul name"},"type":{"type":"string","description":"Check type (http, tcp, etc.)"},"target":{"type":"string","description":"Target URL/address"},"interval":{"type":"string","description":"Check interval (e.g., 30s, 1m)"}},"required":["name","type","target"]}`),
		Handler:     s.handleCreateSoul,
	}
}

// registerBuiltinResources registers MCP resources
func (s *MCPServer) registerBuiltinResources() {
	s.resources["anubis://docs/getting-started"] = MCPResource{
		URI:         "anubis://docs/getting-started",
		Name:        "Getting Started",
		Description: "Guide to getting started with AnubisWatch",
		MIMEType:    "text/markdown",
		Handler:     s.resourceGettingStarted,
	}

	s.resources["anubis://docs/api-reference"] = MCPResource{
		URI:         "anubis://docs/api-reference",
		Name:        "API Reference",
		Description: "Complete API documentation",
		MIMEType:    "text/markdown",
		Handler:     s.resourceAPIReference,
	}

	s.resources["anubis://status/current"] = MCPResource{
		URI:         "anubis://status/current",
		Name:        "Current Status",
		Description: "Current system and soul status",
		MIMEType:    "application/json",
		Handler:     s.resourceCurrentStatus,
	}
}

// registerBuiltinPrompts registers MCP prompts
func (s *MCPServer) registerBuiltinPrompts() {
	s.prompts["analyze-soul"] = MCPPrompt{
		Name:        "analyze-soul",
		Description: "Analyze a soul's health and provide recommendations",
		Arguments: []MCPPromptArg{
			{Name: "soul_id", Description: "The soul ID to analyze", Required: true},
		},
		Handler: s.promptAnalyzeSoul,
	}

	s.prompts["incident-summary"] = MCPPrompt{
		Name:        "incident-summary",
		Description: "Generate a summary of current incidents",
		Arguments:   []MCPPromptArg{},
		Handler:     s.promptIncidentSummary,
	}

	s.prompts["create-monitor-guide"] = MCPPrompt{
		Name:        "create-monitor-guide",
		Description: "Guide for creating a new monitor",
		Arguments: []MCPPromptArg{
			{Name: "target_type", Description: "Type of target (website, api, server)", Required: true},
		},
		Handler: s.promptCreateMonitorGuide,
	}
}

// ServeHTTP handles MCP requests
func (s *MCPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req MCPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, nil, -32700, "Parse error")
		return
	}

	resp := s.handleRequest(&req)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleRequest processes an MCP request
func (s *MCPServer) handleRequest(req *MCPRequest) *MCPResponse {
	s.logger.Debug("MCP request", "method", req.Method, "id", req.ID)

	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleListTools(req)
	case "tools/call":
		return s.handleCallTool(req)
	case "resources/list":
		return s.handleListResources(req)
	case "resources/read":
		return s.handleReadResource(req)
	case "prompts/list":
		return s.handleListPrompts(req)
	case "prompts/get":
		return s.handleGetPrompt(req)
	default:
		return s.errorResponse(req.ID, -32601, "Method not found")
	}
}

// handleInitialize handles MCP initialization
func (s *MCPServer) handleInitialize(req *MCPRequest) *MCPResponse {
	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools":     map[string]interface{}{"listChanged": false},
				"resources": map[string]interface{}{"subscribe": false, "listChanged": false},
				"prompts":   map[string]interface{}{"listChanged": false},
			},
			"serverInfo": map[string]string{
				"name":    "anubiswatch",
				"version": "0.1.0",
			},
		},
	}
}

// handleListTools returns available tools
func (s *MCPServer) handleListTools(req *MCPRequest) *MCPResponse {
	s.mu.RLock()
	tools := make([]MCPTool, 0, len(s.tools))
	for _, tool := range s.tools {
		tools = append(tools, MCPTool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		})
	}
	s.mu.RUnlock()

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]interface{}{"tools": tools},
	}
}

// handleCallTool executes a tool
func (s *MCPServer) handleCallTool(req *MCPRequest) *MCPResponse {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return s.errorResponse(req.ID, -32602, "Invalid params")
	}

	s.mu.RLock()
	tool, ok := s.tools[params.Name]
	s.mu.RUnlock()

	if !ok {
		return s.errorResponse(req.ID, -32601, "Tool not found")
	}

	result, err := tool.Handler(params.Arguments)
	if err != nil {
		return s.errorResponse(req.ID, -32603, err.Error())
	}

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]interface{}{"content": []interface{}{map[string]string{"type": "text", "text": fmt.Sprintf("%v", result)}}},
	}
}

// handleListResources returns available resources
func (s *MCPServer) handleListResources(req *MCPRequest) *MCPResponse {
	s.mu.RLock()
	resources := make([]MCPResource, 0, len(s.resources))
	for _, res := range s.resources {
		resources = append(resources, res)
	}
	s.mu.RUnlock()

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]interface{}{"resources": resources},
	}
}

// handleReadResource returns resource content
func (s *MCPServer) handleReadResource(req *MCPRequest) *MCPResponse {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return s.errorResponse(req.ID, -32602, "Invalid params")
	}

	s.mu.RLock()
	resource, ok := s.resources[params.URI]
	s.mu.RUnlock()

	if !ok {
		return s.errorResponse(req.ID, -32602, "Resource not found")
	}

	content, err := resource.Handler()
	if err != nil {
		return s.errorResponse(req.ID, -32603, err.Error())
	}

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]interface{}{"contents": []interface{}{map[string]interface{}{"uri": params.URI, "mimeType": resource.MIMEType, "text": content}}},
	}
}

// handleListPrompts returns available prompts
func (s *MCPServer) handleListPrompts(req *MCPRequest) *MCPResponse {
	s.mu.RLock()
	prompts := make([]MCPPrompt, 0, len(s.prompts))
	for _, prompt := range s.prompts {
		prompts = append(prompts, MCPPrompt{
			Name:        prompt.Name,
			Description: prompt.Description,
			Arguments:   prompt.Arguments,
		})
	}
	s.mu.RUnlock()

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]interface{}{"prompts": prompts},
	}
}

// handleGetPrompt returns a prompt
func (s *MCPServer) handleGetPrompt(req *MCPRequest) *MCPResponse {
	var params struct {
		Name      string            `json:"name"`
		Arguments map[string]string `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return s.errorResponse(req.ID, -32602, "Invalid params")
	}

	s.mu.RLock()
	prompt, ok := s.prompts[params.Name]
	s.mu.RUnlock()

	if !ok {
		return s.errorResponse(req.ID, -32602, "Prompt not found")
	}

	text, err := prompt.Handler(params.Arguments)
	if err != nil {
		return s.errorResponse(req.ID, -32603, err.Error())
	}

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"description": prompt.Description,
			"messages": []interface{}{
				map[string]interface{}{
					"role": "user",
					"content": map[string]string{
						"type": "text",
						"text": text,
					},
				},
			},
		},
	}
}

// errorResponse creates an error response
func (s *MCPServer) errorResponse(id interface{}, code int, message string) *MCPResponse {
	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &MCPError{
			Code:    code,
			Message: message,
		},
	}
}

// writeError writes an error response
func (s *MCPServer) writeError(w http.ResponseWriter, id interface{}, code int, message string) {
	resp := s.errorResponse(id, code, message)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Tool handlers

func (s *MCPServer) handleListSouls(args json.RawMessage) (interface{}, error) {
	var params struct {
		Workspace string `json:"workspace"`
		Status    string `json:"status"`
	}
	json.Unmarshal(args, &params)

	souls, err := s.store.ListSouls(params.Workspace, 0, 100)
	if err != nil {
		return nil, err
	}

	if params.Status != "" {
		filtered := make([]*core.Soul, 0)
		for _, soul := range souls {
			// Note: Would need to get current status from probe
			_ = soul
			filtered = append(filtered, soul)
		}
		souls = filtered
	}

	return souls, nil
}

func (s *MCPServer) handleGetSoul(args json.RawMessage) (interface{}, error) {
	var params struct {
		SoulID string `json:"soul_id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	return s.store.GetSoul(params.SoulID)
}

func (s *MCPServer) handleForceCheck(args json.RawMessage) (interface{}, error) {
	var params struct {
		SoulID string `json:"soul_id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	return s.probe.ForceCheck(params.SoulID)
}

func (s *MCPServer) handleGetJudgments(args json.RawMessage) (interface{}, error) {
	var params struct {
		SoulID string `json:"soul_id"`
		Limit  int    `json:"limit"`
	}
	json.Unmarshal(args, &params)
	if params.Limit == 0 {
		params.Limit = 10
	}

	end := time.Now()
	start := end.Add(-24 * time.Hour)
	return s.store.ListJudgments(params.SoulID, start, end, params.Limit)
}

func (s *MCPServer) handleListIncidents(args json.RawMessage) (interface{}, error) {
	// Return active incidents
	return []interface{}{}, nil
}

func (s *MCPServer) handleGetStats(args json.RawMessage) (interface{}, error) {
	var params struct {
		Workspace string `json:"workspace"`
	}
	json.Unmarshal(args, &params)

	end := time.Now()
	start := end.Add(-24 * time.Hour)
	return s.store.GetStats(params.Workspace, start, end)
}

func (s *MCPServer) handleAcknowledgeIncident(args json.RawMessage) (interface{}, error) {
	var params struct {
		IncidentID string `json:"incident_id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	return nil, s.alert.AcknowledgeIncident(params.IncidentID, "mcp-user")
}

func (s *MCPServer) handleCreateSoul(args json.RawMessage) (interface{}, error) {
	var params struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Target   string `json:"target"`
		Interval string `json:"interval"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, err
	}

	soul := &core.Soul{
		ID:      core.GenerateID(),
		Name:    params.Name,
		Type:    core.CheckType(params.Type),
		Target:  params.Target,
		Enabled: true,
	}

	if params.Interval != "" {
		d, _ := time.ParseDuration(params.Interval)
		soul.Weight = core.Duration{d}
	}

	return soul, s.store.SaveSoul(soul)
}

// Resource handlers

func (s *MCPServer) resourceGettingStarted() (interface{}, error) {
	return `# Getting Started with AnubisWatch

AnubisWatch is a distributed uptime monitoring platform with Egyptian mythology theming.

## Core Concepts

- **Souls**: Monitored targets (websites, APIs, servers)
- **Judgments**: Health check results
- **Jackals**: Probe nodes in the cluster
- **Pharaoh**: The Raft leader
- **Necropolis**: The distributed cluster

## Quick Start

1. Create a soul: Use the create_soul tool
2. View status: Use the list_souls tool
3. Check history: Use the get_judgments tool
4. Set up alerts: Use the create_channel tool

## API Endpoints

- REST API: http://localhost:8080/api/v1
- WebSocket: ws://localhost:8080/ws
- gRPC: localhost:9090
- MCP: http://localhost:8080/mcp
`, nil
}

func (s *MCPServer) resourceAPIReference() (interface{}, error) {
	return `# AnubisWatch API Reference

## REST API Endpoints

### Souls
- GET /api/v1/souls - List souls
- POST /api/v1/souls - Create soul
- GET /api/v1/souls/:id - Get soul
- PUT /api/v1/souls/:id - Update soul
- DELETE /api/v1/souls/:id - Delete soul
- POST /api/v1/souls/:id/check - Force check

### Judgments
- GET /api/v1/judgments - List judgments
- GET /api/v1/judgments/:id - Get judgment

### Channels
- GET /api/v1/channels - List channels
- POST /api/v1/channels - Create channel

### Rules
- GET /api/v1/rules - List rules
- POST /api/v1/rules - Create rule

### Cluster
- GET /api/v1/cluster/status - Cluster status
- GET /api/v1/cluster/peers - List peers
`, nil
}

func (s *MCPServer) resourceCurrentStatus() (interface{}, error) {
	return map[string]interface{}{
		"status":    "operational",
		"timestamp": time.Now().UTC(),
	}, nil
}

// Prompt handlers

func (s *MCPServer) promptAnalyzeSoul(args map[string]string) (string, error) {
	soulID := args["soul_id"]
	return fmt.Sprintf(`Please analyze the health and status of soul %s.

1. Retrieve the soul's current configuration
2. Check recent judgments (last 24 hours)
3. Identify any patterns in failures
4. Provide recommendations for improvement
5. Suggest alert rules that might be helpful

Focus on actionable insights that would help improve reliability.`, soulID), nil
}

func (s *MCPServer) promptIncidentSummary(args map[string]string) (string, error) {
	return `Please provide a summary of the current incident status:

1. List all active (firing) incidents
2. Show recently resolved incidents (last 24 hours)
3. Identify which souls are having the most issues
4. Highlight any patterns or common causes
5. Recommend next steps for resolution

Be concise but thorough.`, nil
}

func (s *MCPServer) promptCreateMonitorGuide(args map[string]string) (string, error) {
	targetType := args["target_type"]

	guides := map[string]string{
		"website": `Guide: Creating a Website Monitor

1. Choose HTTP check type
2. Set target to the full URL (https://example.com)
3. Configure expected status codes (usually 200)
4. Set check interval (30s for critical, 1m for standard)
5. Add assertions:
   - Status code check
   - Response time threshold (e.g., < 2s)
   - Body contains check (optional)
6. Set up alerts for downtime
7. Consider adding SSL/TLS expiry monitoring`,
		"api": `Guide: Creating an API Monitor

1. Choose HTTP check type
2. Set target to API endpoint
3. Configure method (GET, POST, etc.)
4. Add headers if authentication required
5. Set JSON path assertions for response validation
6. Configure performance budget (Feather)
7. Set up alerts for errors or latency`,
		"server": `Guide: Creating a Server Monitor

1. Choose TCP or ICMP check type
2. Set target to IP:port or hostname
3. Configure timeout appropriately
4. For TCP: consider banner matching
5. Set up alerts for connection failures
6. Consider ICMP for basic connectivity`,
	}

	if guide, ok := guides[targetType]; ok {
		return guide, nil
	}
	return "Generic monitor creation guide: Choose appropriate check type, set target, configure interval, and set up alerts.", nil
}

// RegisterTool allows registration of custom tools
func (s *MCPServer) RegisterTool(tool MCPTool) {
	s.mu.Lock()
	s.tools[tool.Name] = tool
	s.mu.Unlock()
}

// RegisterResource allows registration of custom resources
func (s *MCPServer) RegisterResource(resource MCPResource) {
	s.mu.Lock()
	s.resources[resource.URI] = resource
	s.mu.Unlock()
}

// RegisterPrompt allows registration of custom prompts
func (s *MCPServer) RegisterPrompt(prompt MCPPrompt) {
	s.mu.Lock()
	s.prompts[prompt.Name] = prompt
	s.mu.Unlock()
}
