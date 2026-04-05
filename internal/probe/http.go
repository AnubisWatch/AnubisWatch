package probe

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// HTTPChecker implements HTTP/HTTPS health checks
type HTTPChecker struct {
	client *http.Client
}

// NewHTTPChecker creates a new HTTP checker
func NewHTTPChecker() *HTTPChecker {
	return &HTTPChecker{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Type returns the protocol identifier
func (c *HTTPChecker) Type() core.CheckType {
	return core.CheckHTTP
}

// Validate checks if the soul configuration is valid
func (c *HTTPChecker) Validate(soul *core.Soul) error {
	if soul.Target == "" {
		return configError("target", "target URL is required")
	}
	if !strings.HasPrefix(soul.Target, "http://") && !strings.HasPrefix(soul.Target, "https://") {
		return configError("target", "target must start with http:// or https://")
	}
	return nil
}

// Judge performs the HTTP health check
func (c *HTTPChecker) Judge(ctx context.Context, soul *core.Soul) (*core.Judgment, error) {
	cfg := soul.HTTP
	if cfg == nil {
		cfg = &core.HTTPConfig{
			Method:      "GET",
			ValidStatus: []int{200},
		}
	}

	method := strings.ToUpper(cfg.Method)
	if method == "" {
		method = "GET"
	}

	// Build request
	var body io.Reader
	if cfg.Body != "" {
		body = strings.NewReader(cfg.Body)
	}

	req, err := http.NewRequestWithContext(ctx, method, soul.Target, body)
	if err != nil {
		return failJudgment(soul, fmt.Errorf("failed to create request: %w", err)), nil
	}

	// Set headers
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "AnubisWatch/1.0 (The Judgment Never Sleeps)")
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "*/*")
	}

	// Configure transport
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.InsecureSkipVerify,
		},
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 0,
		}).DialContext,
		DisableKeepAlives: true,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   soul.Timeout.Duration,
	}

	// Handle redirects
	if !cfg.FollowRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	} else if cfg.MaxRedirects > 0 {
		maxRedir := cfg.MaxRedirects
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedir {
				return fmt.Errorf("stopped after %d redirects", maxRedir)
			}
			return nil
		}
	}

	// Execute request
	start := time.Now()
	resp, err := client.Do(req)
	duration := time.Since(start)

	if err != nil {
		return failJudgment(soul, fmt.Errorf("request failed: %w", err)), nil
	}
	defer resp.Body.Close()

	// Read body (limited to maxReadSize)
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxReadSize))
	if err != nil {
		return failJudgment(soul, fmt.Errorf("failed to read body: %w", err)), nil
	}

	// Build judgment
	judgment := &core.Judgment{
		ID:         core.GenerateID(),
		SoulID:     soul.ID,
		Timestamp:  time.Now().UTC(),
		Duration:   duration,
		StatusCode: resp.StatusCode,
		Details: &core.JudgmentDetails{
			ResponseHeaders: make(map[string]string),
			ResponseBody:    string(bodyBytes),
		},
	}

	// Copy response headers
	for k, v := range resp.Header {
		if len(v) > 0 {
			judgment.Details.ResponseHeaders[k] = v[0]
		}
	}

	// Extract TLS info
	if resp.TLS != nil {
		judgment.TLSInfo = extractTLSInfo(resp.TLS)
	}

	// Run assertions
	assertions := make([]core.AssertionResult, 0)
	allPassed := true

	// 1. Status code assertion
	if len(cfg.ValidStatus) > 0 {
		statusOK := false
		for _, s := range cfg.ValidStatus {
			if resp.StatusCode == s {
				statusOK = true
				break
			}
		}
		assertions = append(assertions, core.AssertionResult{
			Type:     "status_code",
			Expected: fmt.Sprintf("%v", cfg.ValidStatus),
			Actual:   fmt.Sprintf("%d", resp.StatusCode),
			Passed:   statusOK,
		})
		if !statusOK {
			allPassed = false
		}
	}

	// 2. Body contains assertion
	if cfg.BodyContains != "" {
		contains := strings.Contains(string(bodyBytes), cfg.BodyContains)
		assertions = append(assertions, core.AssertionResult{
			Type:     "body_contains",
			Expected: cfg.BodyContains,
			Actual:   truncateString(string(bodyBytes), 200),
			Passed:   contains,
		})
		if !contains {
			allPassed = false
		}
	}

	// 3. Body regex assertion
	if cfg.BodyRegex != "" {
		re, err := regexp.Compile(cfg.BodyRegex)
		matched := err == nil && re.Match(bodyBytes)
		assertions = append(assertions, core.AssertionResult{
			Type:     "body_regex",
			Expected: cfg.BodyRegex,
			Actual:   truncateString(string(bodyBytes), 200),
			Passed:   matched,
		})
		if !matched {
			allPassed = false
		}
	}

	// 4. JSON path assertions
	if cfg.JSONPath != nil {
		for path, expected := range cfg.JSONPath {
			actual := extractJSONPath(bodyBytes, path)
			passed := actual == expected
			assertions = append(assertions, core.AssertionResult{
				Type:     "json_path",
				Expected: path + "=" + expected,
				Actual:   actual,
				Passed:   passed,
			})
			if !passed {
				allPassed = false
			}
		}
	}

	// 5. JSON schema assertion
	if cfg.JSONSchema != "" {
		passed := validateJSONSchema(bodyBytes, cfg.JSONSchema, cfg.JSONSchemaStrict)
		assertions = append(assertions, core.AssertionResult{
			Type:     "json_schema",
			Expected: "valid",
			Actual:   boolToString(passed, "valid", "invalid"),
			Passed:   passed,
		})
		if !passed {
			allPassed = false
		}
	}

	// 6. Response header assertions
	if cfg.ResponseHeaders != nil {
		for headerName, expectedValue := range cfg.ResponseHeaders {
			actualValue := resp.Header.Get(headerName)
			passed := actualValue == expectedValue
			assertions = append(assertions, core.AssertionResult{
				Type:     "response_header",
				Expected: headerName + ": " + expectedValue,
				Actual:   headerName + ": " + actualValue,
				Passed:   passed,
			})
			if !passed {
				allPassed = false
			}
		}
	}

	// 7. Performance budget (Feather of Ma'at)
	if cfg.Feather.Duration > 0 {
		withinBudget := duration <= cfg.Feather.Duration
		assertions = append(assertions, core.AssertionResult{
			Type:     "feather",
			Expected: cfg.Feather.String(),
			Actual:   duration.String(),
			Passed:   withinBudget,
		})
		if !withinBudget && allPassed {
			// Mark as degraded, not dead
			judgment.Status = core.SoulDegraded
			judgment.Message = fmt.Sprintf("HTTP %d in %s (exceeds feather %s)",
				resp.StatusCode, duration.Round(time.Millisecond), cfg.Feather.Duration)
		}
	}

	judgment.Details.Assertions = assertions

	// Determine final status
	if judgment.Status == "" {
		if allPassed {
			judgment.Status = core.SoulAlive
			judgment.Message = fmt.Sprintf("HTTP %d in %s", resp.StatusCode, duration.Round(time.Millisecond))
		} else {
			judgment.Status = core.SoulDead
			// Build failure message
			var failures []string
			for _, a := range assertions {
				if !a.Passed {
					failures = append(failures, a.Type+": expected "+a.Expected)
				}
			}
			judgment.Message = strings.Join(failures, "; ")
		}
	}

	return judgment, nil
}

// extractTLSInfo extracts TLS information from connection state
func extractTLSInfo(state *tls.ConnectionState) *core.TLSInfo {
	if state == nil {
		return nil
	}
	info := &core.TLSInfo{
		Protocol:    tlsVersionString(state.Version),
		CipherSuite: tls.CipherSuiteName(state.CipherSuite),
	}

	if len(state.PeerCertificates) > 0 {
		cert := state.PeerCertificates[0]
		info.Issuer = cert.Issuer.CommonName
		info.Subject = cert.Subject.CommonName
		info.SANs = cert.DNSNames
		info.NotBefore = cert.NotBefore
		info.NotAfter = cert.NotAfter
		info.DaysUntilExpiry = int(time.Until(cert.NotAfter).Hours() / 24)
		info.KeyType = cert.PublicKeyAlgorithm.String()
		info.ChainLength = len(state.PeerCertificates)
		info.ChainValid = len(state.VerifiedChains) > 0
		info.OCSPStapled = len(state.OCSPResponse) > 0
	}

	return info
}

func tlsVersionString(v uint16) string {
	switch v {
	case tls.VersionTLS10:
		return "TLS1.0"
	case tls.VersionTLS11:
		return "TLS1.1"
	case tls.VersionTLS12:
		return "TLS1.2"
	case tls.VersionTLS13:
		return "TLS1.3"
	default:
		return fmt.Sprintf("0x%04x", v)
	}
}

// extractJSONPath extracts a value from JSON using simple path syntax ($.key.subkey)
func extractJSONPath(data []byte, path string) string {
	// Strip leading "$"
	path = strings.TrimPrefix(path, "$")
	path = strings.TrimPrefix(path, ".")

	if path == "" {
		return ""
	}

	parts := strings.Split(path, ".")

	var current interface{}
	if err := json.Unmarshal(data, &current); err != nil {
		return ""
	}

	for _, part := range parts {
		if part == "" {
			continue
		}

		switch v := current.(type) {
		case map[string]interface{}:
			val, ok := v[part]
			if !ok {
				return ""
			}
			current = val
		default:
			return ""
		}
	}

	// Convert to string
	switch v := current.(type) {
	case string:
		return v
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprintf("%g", v)
	case bool:
		return fmt.Sprintf("%t", v)
	case nil:
		return "null"
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

// validateJSONSchema validates JSON against a schema (simplified implementation)
func validateJSONSchema(data []byte, schema string, strict bool) bool {
	var schemaObj map[string]interface{}
	if err := json.Unmarshal([]byte(schema), &schemaObj); err != nil {
		return false
	}

	var dataObj interface{}
	if err := json.Unmarshal(data, &dataObj); err != nil {
		return false
	}

	return validateNode(dataObj, schemaObj, strict)
}

func validateNode(data interface{}, schema map[string]interface{}, strict bool) bool {
	// Type validation
	if expectedType, ok := schema["type"].(string); ok {
		if !matchesType(data, expectedType) {
			return false
		}
	}

	// Required fields
	if required, ok := schema["required"].([]interface{}); ok {
		obj, isObj := data.(map[string]interface{})
		if !isObj {
			return false
		}
		for _, r := range required {
			if key, isStr := r.(string); isStr {
				if _, exists := obj[key]; !exists {
					return false
				}
			}
		}
	}

	// Properties validation
	if props, ok := schema["properties"].(map[string]interface{}); ok {
		obj, isObj := data.(map[string]interface{})
		if !isObj {
			return false
		}
		for key, propSchema := range props {
			if val, exists := obj[key]; exists {
				if ps, isMap := propSchema.(map[string]interface{}); isMap {
					if !validateNode(val, ps, strict) {
						return false
					}
				}
			}
		}
	}

	// Enum validation
	if enum, ok := schema["enum"].([]interface{}); ok {
		for _, allowed := range enum {
			if data == allowed {
				return true
			}
		}
		return false
	}

	// Strict mode: check for additional properties
	if strict {
		if props, ok := schema["properties"].(map[string]interface{}); ok {
			obj, isObj := data.(map[string]interface{})
			if isObj {
				for key := range obj {
					if _, allowed := props[key]; !allowed {
						return false
					}
				}
			}
		}
	}

	return true
}

func matchesType(data interface{}, expectedType string) bool {
	switch expectedType {
	case "object":
		_, ok := data.(map[string]interface{})
		return ok
	case "array":
		_, ok := data.([]interface{})
		return ok
	case "string":
		_, ok := data.(string)
		return ok
	case "number":
		_, ok := data.(float64)
		return ok
	case "integer":
		if f, ok := data.(float64); ok {
			return f == float64(int64(f))
		}
		return false
	case "boolean":
		_, ok := data.(bool)
		return ok
	case "null":
		return data == nil
	}
	return true
}
