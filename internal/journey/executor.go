package journey

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/probe"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

// Executor handles multi-step synthetic monitoring journeys
// Named after Duat, the Egyptian realm of the afterlife through which souls journey
type Executor struct {
	db      *storage.CobaltDB
	logger  *slog.Logger
	mu      sync.RWMutex
	running map[string]context.CancelFunc // journey ID -> cancel function
	nodeID  string                        // identifier of this jackal node
	region  string                        // region of this jackal node
}

// NewExecutor creates a new Journey executor
func NewExecutor(db *storage.CobaltDB, logger *slog.Logger) *Executor {
	return NewExecutorWithNodeID(db, logger, "local", "default")
}

// NewExecutorWithNodeID creates a new Journey executor with node identification
func NewExecutorWithNodeID(db *storage.CobaltDB, logger *slog.Logger, nodeID, region string) *Executor {
	if nodeID == "" {
		nodeID = "local"
	}
	if region == "" {
		region = "default"
	}
	return &Executor{
		db:      db,
		logger:  logger.With("component", "duat"),
		running: make(map[string]context.CancelFunc),
		nodeID:  nodeID,
		region:  region,
	}
}

// Start begins executing a journey
func (e *Executor) Start(ctx context.Context, journey *core.JourneyConfig) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check if already running
	if _, exists := e.running[journey.ID]; exists {
		return fmt.Errorf("journey %s is already running", journey.ID)
	}

	// Create context for this journey
	journeyCtx, cancel := context.WithCancel(ctx)
	e.running[journey.ID] = cancel

	// Start journey execution goroutine
	go e.runJourneyLoop(journeyCtx, journey)

	e.logger.Info("started journey executor", "journey_id", journey.ID, "name", journey.Name)
	return nil
}

// Stop stops executing a journey
func (e *Executor) Stop(journeyID string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if cancel, exists := e.running[journeyID]; exists {
		cancel()
		delete(e.running, journeyID)
		e.logger.Info("stopped journey executor", "journey_id", journeyID)
	}
}

// StopAll stops all running journeys
func (e *Executor) StopAll() {
	e.mu.Lock()
	defer e.mu.Unlock()

	for journeyID, cancel := range e.running {
		cancel()
		delete(e.running, journeyID)
	}
	e.logger.Info("stopped all journey executors")
}

// runJourneyLoop executes a journey repeatedly based on its weight (interval)
func (e *Executor) runJourneyLoop(ctx context.Context, journey *core.JourneyConfig) {
	ticker := time.NewTicker(journey.Weight.Duration)
	defer ticker.Stop()

	// Run immediately on start
	e.executeJourney(ctx, journey)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.executeJourney(ctx, journey)
		}
	}
}

// executeJourney executes a single journey run
func (e *Executor) executeJourney(ctx context.Context, journey *core.JourneyConfig) {
	startTime := time.Now()

	e.logger.Debug("executing journey", "journey_id", journey.ID, "name", journey.Name)

	run := &core.JourneyRun{
		ID:          core.GenerateID(),
		JourneyID:   journey.ID,
		WorkspaceID: journey.WorkspaceID,
		JackalID:    e.nodeID,
		Region:      e.region,
		StartedAt:   startTime.UnixMilli(),
		Variables:   make(map[string]string),
		Steps:       make([]core.JourneyStepResult, 0, len(journey.Steps)),
		Status:      core.SoulAlive,
	}

	// Initialize variables with defaults
	for k, v := range journey.Variables {
		run.Variables[k] = v
	}

	// Execute each step
	allSuccess := true
	for i, step := range journey.Steps {
		stepResult := e.executeStep(ctx, step, run.Variables, i)
		run.Steps = append(run.Steps, stepResult)

		// Merge extracted variables
		for k, v := range stepResult.Extracted {
			run.Variables[k] = v
		}

		// Check if we should continue
		if stepResult.Status != core.SoulAlive {
			allSuccess = false
			if !journey.ContinueOnFailure {
				e.logger.Debug("stopping journey due to step failure",
					"journey_id", journey.ID, "step", step.Name)
				break
			}
		}
	}

	// Calculate final status and duration
	completedTime := time.Now()
	run.CompletedAt = completedTime.UnixMilli()
	run.Duration = completedTime.Sub(startTime).Milliseconds()

	if allSuccess {
		run.Status = core.SoulAlive
	} else {
		run.Status = core.SoulDead
	}

	// Save the journey run
	if err := e.db.SaveJourneyRun(ctx, run); err != nil {
		e.logger.Error("failed to save journey run", "journey_id", journey.ID, "err", err)
	}

	e.logger.Debug("completed journey execution",
		"journey_id", journey.ID,
		"status", run.Status,
		"duration_ms", run.Duration)
}

// executeStep executes a single journey step
func (e *Executor) executeStep(ctx context.Context, step core.JourneyStep, variables map[string]string, stepIndex int) core.JourneyStepResult {
	result := core.JourneyStepResult{
		Name:      step.Name,
		StepIndex: stepIndex,
		Extracted: make(map[string]string),
	}

	// Interpolate variables in target
	target := e.interpolateVariables(step.Target, variables)

	// Create a checker for this step
	checker := e.getChecker(step.Type)
	if checker == nil {
		result.Status = core.SoulDead
		result.Message = fmt.Sprintf("unknown step type: %s", step.Type)
		return result
	}

	// Build soul configuration for this step
	soul := &core.Soul{
		Name:   step.Name,
		Type:   step.Type,
		Target: target,
		Weight: step.Timeout,
		HTTP:   step.HTTP,
		TCP:    step.TCP,
		UDP:    step.UDP,
		DNS:    step.DNS,
		TLS:    step.TLS,
	}

	if err := checker.Validate(soul); err != nil {
		result.Status = core.SoulDead
		result.Message = fmt.Sprintf("validation failed: %v", err)
		return result
	}

	// Execute the check
	stepStart := time.Now()
	judgment, err := checker.Judge(ctx, soul)
	stepDuration := time.Since(stepStart)

	result.Duration = stepDuration.Milliseconds()

	if err != nil {
		result.Status = core.SoulDead
		result.Message = fmt.Sprintf("check failed: %v", err)
		return result
	}

	result.Status = judgment.Status
	result.Message = judgment.Message

	// Extract variables if configured
	if len(step.Extract) > 0 {
		result.Extracted = e.extractVariables(judgment, step.Extract)
	}

	// Run assertions if configured
	if len(step.Assertions) > 0 {
		assertionResults := e.runAssertions(judgment, step.Assertions)
		failedAssertions := 0
		for _, ar := range assertionResults {
			if !ar.Passed {
				failedAssertions++
				if result.Message == "" {
					result.Message = ar.Message
				}
			}
		}
		if failedAssertions > 0 {
			result.Status = core.SoulDead
			result.Message = fmt.Sprintf("%d of %d assertions failed", failedAssertions, len(step.Assertions))
		}
	}

	return result
}

// interpolateVariables replaces ${variable} placeholders with values
func (e *Executor) interpolateVariables(s string, variables map[string]string) string {
	for k, v := range variables {
		s = strings.ReplaceAll(s, "${"+k+"}", v)
	}
	return s
}

// extractVariables extracts variables from a judgment result
func (e *Executor) extractVariables(judgment *core.Judgment, rules map[string]core.ExtractionRule) map[string]string {
	extracted := make(map[string]string)

	for name, rule := range rules {
		var value string

		switch rule.From {
		case "body":
			value = e.extractFromBody(judgment.Details.ResponseBody, rule)
		case "header":
			value = e.extractFromHeader(judgment.Details.ResponseHeaders, rule)
		case "cookie":
			value = e.extractFromCookie(judgment.Details.ResponseHeaders, rule)
		}

		if value != "" {
			extracted[name] = value
			e.logger.Debug("extracted variable", "name", name, "value", value)
		}
	}

	return extracted
}

// extractFromBody extracts a value from the response body
func (e *Executor) extractFromBody(body string, rule core.ExtractionRule) string {
	if rule.Path != "" {
		// Try JSON path extraction
		value := e.extractJSONPath(body, rule.Path)
		if value != "" {
			return value
		}
	}

	// Try regex extraction
	if rule.Regex != "" {
		return e.extractRegex(body, rule.Regex)
	}

	return ""
}

// extractFromHeader extracts a value from response headers
func (e *Executor) extractFromHeader(headers map[string]string, rule core.ExtractionRule) string {
	if value, ok := headers[rule.Path]; ok {
		if rule.Regex != "" {
			return e.extractRegex(value, rule.Regex)
		}
		return value
	}
	return ""
}

// extractFromCookie extracts a value from cookies
func (e *Executor) extractFromCookie(headers map[string]string, rule core.ExtractionRule) string {
	setCookie := headers["Set-Cookie"]
	if setCookie == "" {
		return ""
	}

	// Parse cookie
	cookies := strings.Split(setCookie, ";")
	for _, cookie := range cookies {
		parts := strings.SplitN(strings.TrimSpace(cookie), "=", 2)
		if len(parts) == 2 && parts[0] == rule.Path {
			return parts[1]
		}
	}

	return ""
}

// extractJSONPath extracts a value using simple JSON path
func (e *Executor) extractJSONPath(body, path string) string {
	// Simple JSON path implementation (supports $.key and $.key.subkey)
	// For full JSON path support, consider using a library like github.com/antchfx/jsonquery
	if !strings.HasPrefix(path, "$.") {
		return ""
	}

	keys := strings.Split(strings.TrimPrefix(path, "$."), ".")

	// Simple implementation - would need full JSON parser for production
	// This is a placeholder that demonstrates the concept
	for _, key := range keys {
		searchFor := fmt.Sprintf(`"%s":`, key)
		idx := strings.Index(body, searchFor)
		if idx == -1 {
			return ""
		}
		// Extract value after colon
		start := idx + len(searchFor)
		for start < len(body) && (body[start] == ' ' || body[start] == '"') {
			start++
		}
		end := start
		for end < len(body) && body[end] != '"' && body[end] != ',' && body[end] != '}' {
			end++
		}
		if end > start {
			return strings.Trim(body[start:end], `"`)
		}
	}

	return ""
}

// extractRegex extracts a value using regex
func (e *Executor) extractRegex(s, pattern string) string {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return ""
	}

	matches := re.FindStringSubmatch(s)
	if len(matches) < 2 {
		return ""
	}

	// Return first capturing group, or full match if no groups
	if len(matches) > 1 {
		return matches[1]
	}
	return matches[0]
}

// getChecker returns a checker for the given type
func (e *Executor) getChecker(checkType core.CheckType) probe.Checker {
	// Use the probe package's global checker registry
	return probe.GetChecker(checkType)
}

// ListRuns returns journey runs for a journey
func (e *Executor) ListRuns(ctx context.Context, workspaceID, journeyID string, limit int) ([]*core.JourneyRun, error) {
	return e.db.QueryJourneyRuns(ctx, workspaceID, journeyID, limit)
}

// GetRun returns a specific journey run
func (e *Executor) GetRun(ctx context.Context, workspaceID, runID string) (*core.JourneyRun, error) {
	// This would need a GetJourneyRun method in storage
	// For now, return not implemented
	return nil, fmt.Errorf("not implemented")
}

// AssertionResult represents the result of a single assertion
type AssertionResult struct {
	Passed  bool   `json:"passed"`
	Type    string `json:"type"`
	Message string `json:"message"`
}

// runAssertions runs all assertions against a judgment
func (e *Executor) runAssertions(judgment *core.Judgment, assertions []core.Assertion) []AssertionResult {
	results := make([]AssertionResult, 0, len(assertions))

	for _, assertion := range assertions {
		result := e.runAssertion(judgment, assertion)
		results = append(results, result)
	}

	return results
}

// runAssertion runs a single assertion
func (e *Executor) runAssertion(judgment *core.Judgment, assertion core.Assertion) AssertionResult {
	ar := AssertionResult{
		Passed: false,
		Type:   assertion.Type,
	}

	var actual string
	var passed bool

	switch assertion.Type {
	case "status_code":
		actual = fmt.Sprintf("%d", judgment.StatusCode)
		passed = e.compareValues(actual, assertion.Operator, assertion.Expected)

	case "response_time":
		actual = fmt.Sprintf("%d", judgment.Duration.Milliseconds())
		passed = e.compareValues(actual, assertion.Operator, assertion.Expected)

	case "body_contains":
		if judgment.Details != nil {
			actual = judgment.Details.ResponseBody
			passed = strings.Contains(actual, assertion.Expected)
		}

	case "header":
		if judgment.Details != nil && assertion.Target != "" {
			actual = judgment.Details.ResponseHeaders[assertion.Target]
			passed = e.compareValues(actual, assertion.Operator, assertion.Expected)
		}

	case "json_path":
		if judgment.Details != nil && assertion.Target != "" {
			actual = e.extractJSONPath(judgment.Details.ResponseBody, assertion.Target)
			passed = e.compareValues(actual, assertion.Operator, assertion.Expected)
		}

	case "regex":
		if judgment.Details != nil && assertion.Target != "" {
			matched := e.extractRegex(judgment.Details.ResponseBody, assertion.Expected)
			passed = matched != ""
		}

	default:
		ar.Message = fmt.Sprintf("unknown assertion type: %s", assertion.Type)
		return ar
	}

	ar.Passed = passed
	if !passed {
		if assertion.Message != "" {
			ar.Message = assertion.Message
		} else {
			ar.Message = fmt.Sprintf("assertion failed: expected %s %s %s, got %s",
				assertion.Type, assertion.Operator, assertion.Expected, actual)
		}
	}

	return ar
}

// compareValues compares two values using the given operator
func (e *Executor) compareValues(actual, operator, expected string) bool {
	switch operator {
	case "equals", "eq", "==":
		return actual == expected
	case "not_equals", "ne", "!=":
		return actual != expected
	case "contains":
		return strings.Contains(actual, expected)
	case "greater_than", "gt", ">":
		actualNum, _ := strconv.ParseFloat(actual, 64)
		expectedNum, _ := strconv.ParseFloat(expected, 64)
		return actualNum > expectedNum
	case "less_than", "lt", "<":
		actualNum, _ := strconv.ParseFloat(actual, 64)
		expectedNum, _ := strconv.ParseFloat(expected, 64)
		return actualNum < expectedNum
	case "greater_equals", "ge", ">=":
		actualNum, _ := strconv.ParseFloat(actual, 64)
		expectedNum, _ := strconv.ParseFloat(expected, 64)
		return actualNum >= expectedNum
	case "less_equals", "le", "<=":
		actualNum, _ := strconv.ParseFloat(actual, 64)
		expectedNum, _ := strconv.ParseFloat(expected, 64)
		return actualNum <= expectedNum
	default:
		return actual == expected
	}
}
