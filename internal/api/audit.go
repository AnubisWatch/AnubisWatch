package api

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// AuditEvent represents a security audit event
type AuditEvent struct {
	Timestamp   time.Time       `json:"timestamp"`
	EventType   string          `json:"event_type"`
	UserID      string          `json:"user_id,omitempty"`
	IPAddress   string          `json:"ip_address"`
	UserAgent   string          `json:"user_agent,omitempty"`
	Resource    string          `json:"resource"`
	Action      string          `json:"action"`
	Status      string          `json:"status"`
	Details     json.RawMessage `json:"details,omitempty"`
	RequestID   string          `json:"request_id"`
}

// AuditLogger handles security audit logging
type AuditLogger struct {
	logger   *slog.Logger
	backend  AuditBackend
	buffer   chan *AuditEvent
	shutdown chan struct{}
}

// AuditBackend defines the interface for audit log storage
type AuditBackend interface {
	Write(event *AuditEvent) error
	Query(filter AuditFilter) ([]*AuditEvent, error)
}

// AuditFilter for querying audit logs
type AuditFilter struct {
	StartTime  time.Time
	EndTime    time.Time
	EventTypes []string
	UserID     string
	Resource   string
	Action     string
	Status     string
	Limit      int
}

// NewAuditLogger creates a new audit logger
func NewAuditLogger(logger *slog.Logger, backend AuditBackend) *AuditLogger {
	al := &AuditLogger{
		logger:   logger,
		backend:  backend,
		buffer:   make(chan *AuditEvent, 1000),
		shutdown: make(chan struct{}),
	}

	// Start background writer
	go al.writeLoop()

	return al
}

// Log records an audit event
func (al *AuditLogger) Log(eventType, userID, resource, action, status string, details interface{}) {
	event := &AuditEvent{
		Timestamp: time.Now().UTC(),
		EventType: eventType,
		UserID:    userID,
		Resource:  resource,
		Action:    action,
		Status:    status,
		RequestID: generateRequestID(),
	}

	if details != nil {
		data, err := json.Marshal(details)
		if err == nil {
			event.Details = data
		}
	}

	select {
	case al.buffer <- event:
		// Buffered successfully
	default:
		// Buffer full, log directly (may block)
		al.logger.Warn("Audit buffer full, logging synchronously",
			"event_type", eventType,
			"resource", resource,
			"action", action)
		if al.backend != nil {
			al.backend.Write(event)
		}
	}
}

// LogRequest logs an HTTP request
func (al *AuditLogger) LogRequest(r *http.Request, userID string, status int, duration time.Duration) {
	eventType := "http_request"
	action := r.Method
	resource := r.URL.Path
	statusStr := "success"
	if status >= 400 {
		statusStr = "failure"
	}

	details := map[string]interface{}{
		"method":        r.Method,
		"path":          r.URL.Path,
		"query":         r.URL.RawQuery,
		"status_code":   status,
		"duration_ms":   duration.Milliseconds(),
		"content_type":  r.Header.Get("Content-Type"),
		"referer":       r.Header.Get("Referer"),
	}

	al.Log(eventType, userID, resource, action, statusStr, details)
}

// LogAuth logs authentication events
func (al *AuditLogger) LogAuth(userID, ipAddress, action, status string, details map[string]interface{}) {
	event := &AuditEvent{
		Timestamp: time.Now().UTC(),
		EventType: "authentication",
		UserID:    userID,
		IPAddress: ipAddress,
		Resource:  "auth",
		Action:    action,
		Status:    status,
		RequestID: generateRequestID(),
	}

	if details != nil {
		data, err := json.Marshal(details)
		if err == nil {
			event.Details = data
		}
	}

	select {
	case al.buffer <- event:
	default:
		if al.backend != nil {
			al.backend.Write(event)
		}
	}
}

// LogSecurity logs security-related events
func (al *AuditLogger) LogSecurity(eventType, userID, resource, action string, severity string, details map[string]interface{}) {
	status := "detected"
	if severity == "critical" {
		status = "blocked"
	}

	al.Log(eventType, userID, resource, action, status, details)

	// Also log to main logger for immediate visibility
	al.logger.Warn("Security event detected",
		"event_type", eventType,
		"user_id", userID,
		"resource", resource,
		"action", action,
		"severity", severity,
	)
}

// writeLoop processes audit events asynchronously
func (al *AuditLogger) writeLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	batch := make([]*AuditEvent, 0, 100)

	for {
		select {
		case event := <-al.buffer:
			batch = append(batch, event)
			if len(batch) >= 100 {
				al.flush(batch)
				batch = batch[:0]
			}

		case <-ticker.C:
			if len(batch) > 0 {
				al.flush(batch)
				batch = batch[:0]
			}

		case <-al.shutdown:
			// Flush remaining events
			for len(al.buffer) > 0 && len(batch) < cap(batch) {
				select {
				case event := <-al.buffer:
					batch = append(batch, event)
				default:
					break
				}
			}
			if len(batch) > 0 {
				al.flush(batch)
			}
			return
		}
	}
}

// flush writes a batch of events to the backend
func (al *AuditLogger) flush(events []*AuditEvent) {
	if al.backend == nil {
		return
	}

	for _, event := range events {
		if err := al.backend.Write(event); err != nil {
			al.logger.Error("Failed to write audit event",
				"error", err,
				"event_type", event.EventType)
		}
	}
}

// Stop gracefully shuts down the audit logger
func (al *AuditLogger) Stop() {
	close(al.shutdown)
}

// Query retrieves audit events based on filter
func (al *AuditLogger) Query(filter AuditFilter) ([]*AuditEvent, error) {
	if al.backend == nil {
		return nil, fmt.Errorf("no audit backend configured")
	}
	return al.backend.Query(filter)
}

// generateRequestID creates a unique request identifier
func generateRequestID() string {
	return fmt.Sprintf("%d-%x", time.Now().UnixNano(),
		[]byte(fmt.Sprintf("%d", time.Now().UnixNano())))[:20]
}

// AuditMiddleware creates an HTTP middleware for audit logging
func AuditMiddleware(auditLogger *AuditLogger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Wrap response writer to capture status code
			wrapped := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}

			// Extract user ID from context if available
			userID := ""
			if user, ok := r.Context().Value("user").(*User); ok {
				userID = user.ID
			}

			next.ServeHTTP(wrapped, r)

			duration := time.Since(start)
			auditLogger.LogRequest(r, userID, wrapped.statusCode, duration)
		})
	}
}

// responseRecorder wraps http.ResponseWriter to capture status code
type responseRecorder struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (rr *responseRecorder) WriteHeader(code int) {
	if !rr.written {
		rr.statusCode = code
		rr.written = true
		rr.ResponseWriter.WriteHeader(code)
	}
}

func (rr *responseRecorder) Write(p []byte) (int, error) {
	if !rr.written {
		rr.WriteHeader(http.StatusOK)
	}
	return rr.ResponseWriter.Write(p)
}

// SecurityEvent types
const (
	EventTypeAuthSuccess     = "auth_success"
	EventTypeAuthFailure     = "auth_failure"
	EventTypeAuthLogout      = "auth_logout"
	EventTypeAccessDenied    = "access_denied"
	EventTypeRateLimited     = "rate_limited"
	EventTypeSuspicious      = "suspicious_activity"
	EventTypeInjection       = "injection_attempt"
	EventTypePrivEscalation  = "privilege_escalation"
	EventTypeDataExport      = "data_export"
	EventTypeConfigChange    = "config_change"
	EventTypeSoulCreate      = "soul_create"
	EventTypeSoulUpdate      = "soul_update"
	EventTypeSoulDelete      = "soul_delete"
)

// AuditEventLogger interface for dependency injection
type AuditEventLogger interface {
	Log(eventType, userID, resource, action, status string, details interface{})
	LogRequest(r *http.Request, userID string, status int, duration time.Duration)
	LogAuth(userID, ipAddress, action, status string, details map[string]interface{})
	LogSecurity(eventType, userID, resource, action string, severity string, details map[string]interface{})
}

// Constant-time comparison to prevent timing attacks
func ConstantTimeCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// SanitizeInput removes potentially dangerous characters
func SanitizeInput(input string) string {
	// Remove null bytes
	sanitized := strings.ReplaceAll(input, "\x00", "")

	// Remove control characters except newline and tab
	result := make([]rune, 0, len(sanitized))
	for _, r := range sanitized {
		if r == '\n' || r == '\t' || (r >= 32 && r < 127) || r > 127 {
			result = append(result, r)
		}
	}

	return string(result)
}

// ValidateSecureHeaders checks for required security headers in response
func ValidateSecureHeaders(headers http.Header) []string {
	issues := []string{}

	required := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"X-XSS-Protection":       "1; mode=block",
	}

	for header, expected := range required {
		value := headers.Get(header)
		if value == "" {
			issues = append(issues, fmt.Sprintf("Missing %s header", header))
		} else if value != expected {
			issues = append(issues, fmt.Sprintf("Invalid %s header: got %q, want %q", header, value, expected))
		}
	}

	return issues
}
