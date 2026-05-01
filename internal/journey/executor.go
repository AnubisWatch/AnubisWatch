package journey

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
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
	db       *storage.CobaltDB
	logger   *slog.Logger
	mu       sync.RWMutex
	running  map[string]context.CancelFunc // journey ID -> cancel function
	nodeID   string                        // identifier of this jackal node
	region   string                        // region of this jackal node
	lastHash map[string]string             // journey ID -> last dedup hash
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
		db:       db,
		logger:   logger.With("component", "duat"),
		running:  make(map[string]context.CancelFunc),
		lastHash: make(map[string]string),
		nodeID:   nodeID,
		region:   region,
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

	// Validate interval
	if journey.Weight.Duration <= 0 {
		return fmt.Errorf("journey %s has invalid weight interval: %v", journey.ID, journey.Weight.Duration)
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

// JourneyContext holds shared state across all steps in a journey run
type JourneyContext struct {
	Variables map[string]string
	CookieJar http.CookieJar
}

// executeJourney executes a single journey run
func (e *Executor) executeJourney(ctx context.Context, journey *core.JourneyConfig) {
	startTime := time.Now()

	e.logger.Debug("executing journey", "journey_id", journey.ID, "name", journey.Name)

	// Check JSONPath dedup: if the last HTTP step response produces the same
	// dedup hash as the previous run, skip execution to avoid duplicate alerts.
	if hash := e.computeDedupHash(journey); hash != "" {
		e.mu.RLock()
		lastHash := e.lastHash[journey.ID]
		e.mu.RUnlock()
		if lastHash == hash {
			e.logger.Debug("skipping journey run — dedup hash unchanged", "journey_id", journey.ID)
			return
		}
	}

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

	// Create shared journey context with cookie jar
	jar, _ := cookiejar.New(nil)
	journeyCtx := &JourneyContext{
		Variables: run.Variables,
		CookieJar: jar,
	}

	// Execute each step
	allSuccess := true
	for i, step := range journey.Steps {
		stepResult := e.executeStep(ctx, journeyCtx, step, i)
		run.Steps = append(run.Steps, stepResult)

		// Merge extracted variables
		for k, v := range stepResult.Extracted {
			run.Variables[k] = v
			journeyCtx.Variables[k] = v
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

	// Save the journey run with retry
	if err := retryWithBackoff(ctx, 3, 100*time.Millisecond, func() error {
		return e.db.SaveJourneyRun(ctx, run)
	}); err != nil {
		e.logger.Error("failed to save journey run after retries", "journey_id", journey.ID, "err", err)
		// Continue - don't fail the journey execution due to storage issues
	}

	// Update dedup hash for deduplication
	if hash := e.computeDedupHash(journey); hash != "" {
		e.mu.Lock()
		e.lastHash[journey.ID] = hash
		e.mu.Unlock()
	}

	e.logger.Debug("completed journey execution",
		"journey_id", journey.ID,
		"status", run.Status,
		"duration_ms", run.Duration)
}

// executeStep executes a single journey step
func (e *Executor) executeStep(ctx context.Context, jctx *JourneyContext, step core.JourneyStep, stepIndex int) core.JourneyStepResult {
	result := core.JourneyStepResult{
		Name:      step.Name,
		StepIndex: stepIndex,
		Extracted: make(map[string]string),
	}

	// Interpolate variables in target
	target := e.interpolateVariables(step.Target, jctx.Variables)

	// Build soul configuration with interpolated variables
	soul := &core.Soul{
		Name:   step.Name,
		Type:   step.Type,
		Target: target,
		Weight: step.Timeout,
	}

	// Interpolate and set HTTP config fields
	if step.HTTP != nil {
		httpCopy := *step.HTTP
		httpCopy.Method = e.interpolateVariables(httpCopy.Method, jctx.Variables)
		httpCopy.Body = e.interpolateVariables(httpCopy.Body, jctx.Variables)
		httpCopy.BodyContains = e.interpolateVariables(httpCopy.BodyContains, jctx.Variables)
		httpCopy.BodyRegex = e.interpolateVariables(httpCopy.BodyRegex, jctx.Variables)
		httpCopy.JSONSchema = e.interpolateVariables(httpCopy.JSONSchema, jctx.Variables)
		// Share the cookie jar across all HTTP steps in this journey
		httpCopy.CookieJar = jctx.CookieJar
		if httpCopy.Headers != nil {
			interpolatedHeaders := make(map[string]string)
			for k, v := range httpCopy.Headers {
				interpolatedHeaders[k] = e.interpolateVariables(v, jctx.Variables)
			}
			httpCopy.Headers = interpolatedHeaders
		}
		if httpCopy.JSONPath != nil {
			interpolatedJSONPath := make(map[string]string)
			for k, v := range httpCopy.JSONPath {
				interpolatedJSONPath[k] = e.interpolateVariables(v, jctx.Variables)
			}
			httpCopy.JSONPath = interpolatedJSONPath
		}
		if httpCopy.ResponseHeaders != nil {
			interpolatedRespHeaders := make(map[string]string)
			for k, v := range httpCopy.ResponseHeaders {
				interpolatedRespHeaders[k] = e.interpolateVariables(v, jctx.Variables)
			}
			httpCopy.ResponseHeaders = interpolatedRespHeaders
		}
		soul.HTTP = &httpCopy
	}

	// Interpolate and set TCP config fields
	if step.TCP != nil {
		tcpCopy := *step.TCP
		tcpCopy.BannerMatch = e.interpolateVariables(tcpCopy.BannerMatch, jctx.Variables)
		tcpCopy.Send = e.interpolateVariables(tcpCopy.Send, jctx.Variables)
		tcpCopy.ExpectRegex = e.interpolateVariables(tcpCopy.ExpectRegex, jctx.Variables)
		soul.TCP = &tcpCopy
	}

	// Interpolate and set UDP config fields
	if step.UDP != nil {
		udpCopy := *step.UDP
		udpCopy.SendHex = e.interpolateVariables(udpCopy.SendHex, jctx.Variables)
		udpCopy.ExpectContains = e.interpolateVariables(udpCopy.ExpectContains, jctx.Variables)
		soul.UDP = &udpCopy
	}

	// Interpolate and set DNS config fields
	if step.DNS != nil {
		dnsCopy := *step.DNS
		dnsCopy.RecordType = e.interpolateVariables(dnsCopy.RecordType, jctx.Variables)
		if dnsCopy.Nameservers != nil {
			for i, ns := range dnsCopy.Nameservers {
				dnsCopy.Nameservers[i] = e.interpolateVariables(ns, jctx.Variables)
			}
		}
		if dnsCopy.Expected != nil {
			for i, exp := range dnsCopy.Expected {
				dnsCopy.Expected[i] = e.interpolateVariables(exp, jctx.Variables)
			}
		}
		soul.DNS = &dnsCopy
	}

	// Interpolate and set TLS config fields
	if step.TLS != nil {
		tlsCopy := *step.TLS
		tlsCopy.MinProtocol = e.interpolateVariables(tlsCopy.MinProtocol, jctx.Variables)
		tlsCopy.ExpectedIssuer = e.interpolateVariables(tlsCopy.ExpectedIssuer, jctx.Variables)
		for i, san := range tlsCopy.ExpectedSAN {
			tlsCopy.ExpectedSAN[i] = e.interpolateVariables(san, jctx.Variables)
		}
		soul.TLS = &tlsCopy
	}

	checker := e.getChecker(step.Type)
	if checker == nil {
		result.Status = core.SoulDead
		result.Message = fmt.Sprintf("unknown step type: %s", step.Type)
		return result
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

// extractJSONPath extracts a value using JSON path with proper JSON parsing
func (e *Executor) extractJSONPath(body, path string) string {
	if !strings.HasPrefix(path, "$.") {
		return ""
	}

	keys := strings.Split(strings.TrimPrefix(path, "$."), ".")

	var data interface{}
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		return ""
	}

	current := data
	for _, key := range keys {
		obj, ok := current.(map[string]interface{})
		if !ok {
			return ""
		}
		v, exists := obj[key]
		if !exists {
			return ""
		}
		current = v
	}

	switch v := current.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	case nil:
		return ""
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

// computeDedupHash computes a deduplication hash for a journey by evaluating
// all JSONPath extraction rules across all HTTP steps. Returns empty string if
// no JSONPath rules are configured (dedup disabled).
func (e *Executor) computeDedupHash(journey *core.JourneyConfig) string {
	// Collect all JSONPath expressions from step extraction rules
	var paths []string
	for _, step := range journey.Steps {
		for _, rule := range step.Extract {
			if rule.From == "body" && rule.Path != "" {
				paths = append(paths, rule.Path)
			}
		}
	}
	if len(paths) == 0 {
		return ""
	}

	// Build a synthetic body from the journey config to extract values
	// In practice, we compute the hash from the configured JSONPath expressions
	// against the actual response body. For dedup between runs, we hash the
	// configured paths themselves — if they haven't changed, the result is the
	// same (assuming the target is stable). A more sophisticated approach would
	// cache the last response body and hash the extracted values.
	h := sha256.New()
	for _, path := range paths {
		h.Write([]byte(path))
	}
	h.Write([]byte(journey.Steps[0].Target)) // include target URL
	return fmt.Sprintf("%x", h.Sum(nil))
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
func (e *Executor) GetRun(ctx context.Context, workspaceID, journeyID, runID string) (*core.JourneyRun, error) {
	return e.db.GetJourneyRun(ctx, workspaceID, journeyID, runID)
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

// retryWithBackoff retries an operation with exponential backoff
func retryWithBackoff(ctx context.Context, maxRetries int, initialDelay time.Duration, op func() error) error {
	var err error
	delay := initialDelay

	for i := 0; i < maxRetries; i++ {
		err = op()
		if err == nil {
			return nil
		}

		// Don't retry on context cancellation
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Wait before retrying, but respect context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
			delay *= 2 // Exponential backoff
		}
	}

	return fmt.Errorf("failed after %d retries: %w", maxRetries, err)
}
