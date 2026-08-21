package api

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

const redactedSecretValue = "[REDACTED]"

var secretFieldNames = map[string]struct{}{
	"password":        {},
	"pass":            {},
	"secret":          {},
	"token":           {},
	"auth_token":      {},
	"bot_token":       {},
	"api_key":         {},
	"integration_key": {},
	"webhook_url":     {},
}

var secretContainerNames = map[string]struct{}{
	"headers":  {},
	"metadata": {},
	"auth":     {},
	"config":   {},
}

type soulMonitorDTO struct {
	*redactedSoulDTO
	Status    string      `json:"status"`
	LastCheck interface{} `json:"last_check,omitempty"`
	Latency   int64       `json:"latency,omitempty"`
}

type redactedSoulDTO struct {
	ID          string           `json:"id"`
	WorkspaceID string           `json:"workspace_id"`
	Name        string           `json:"name"`
	Type        core.CheckType   `json:"type"`
	Target      string           `json:"target"`
	Weight      core.Duration    `json:"weight"`
	Timeout     core.Duration    `json:"timeout"`
	Enabled     bool             `json:"enabled"`
	Tags        []string         `json:"tags,omitempty"`
	Regions     []string         `json:"regions,omitempty"`
	Region      string           `json:"region,omitempty"`
	HTTP        map[string]any   `json:"http,omitempty"`
	TCP         map[string]any   `json:"tcp,omitempty"`
	UDP         map[string]any   `json:"udp,omitempty"`
	DNS         *core.DNSConfig  `json:"dns,omitempty"`
	SMTP        map[string]any   `json:"smtp,omitempty"`
	IMAP        map[string]any   `json:"imap,omitempty"`
	ICMP        *core.ICMPConfig `json:"icmp,omitempty"`
	GRPC        map[string]any   `json:"grpc,omitempty"`
	WebSocket   map[string]any   `json:"websocket,omitempty"`
	TLS         *core.TLSConfig  `json:"tls,omitempty"`
	CreatedAt   interface{}      `json:"created_at,omitempty"`
	UpdatedAt   interface{}      `json:"updated_at,omitempty"`
	HasSecrets  map[string]bool  `json:"has_secrets,omitempty"`
}

type redactedChannelDTO struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Type        core.AlertChannelType  `json:"type"`
	Enabled     bool                   `json:"enabled"`
	WorkspaceID string                 `json:"workspace_id,omitempty"`
	Config      map[string]any         `json:"config"`
	Filters     []core.AlertFilter     `json:"filters,omitempty"`
	RateLimit   core.RateLimitConfig   `json:"rate_limit"`
	RetryPolicy core.RetryPolicyConfig `json:"retry_policy"`
	CreatedAt   interface{}            `json:"created_at,omitempty"`
	UpdatedAt   interface{}            `json:"updated_at,omitempty"`
	HasSecrets  map[string]bool        `json:"has_secrets,omitempty"`
}

func redactSecretValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return value
	}
	return redactedSecretValue
}

// redactTarget strips URL userinfo while preserving the operational endpoint.
// Userinfo is sent as an Authorization header by net/http, so exposing Target
// verbatim would leak credentials even when protocol-specific fields are safe.
func redactTarget(target string) (string, bool) {
	u, err := url.Parse(target)
	if err != nil {
		// A malformed URL can still contain userinfo-like material. Never reflect
		// it verbatim just because parsing failed.
		if strings.Contains(target, "@") {
			return redactedSecretValue, true
		}
		return target, false
	}
	redacted := *u
	hasSecret := false
	if redacted.User != nil {
		redacted.User = nil
		hasSecret = true
	}
	if sanitized := sanitizeQueryString(redacted.RawQuery); sanitized != redacted.RawQuery {
		redacted.RawQuery = sanitized
		hasSecret = true
	}
	if !hasSecret {
		return target, false
	}
	return redacted.String(), true
}

func isSecretField(name string) bool {
	_, ok := secretFieldNames[strings.ToLower(name)]
	return ok
}

func isSecretContainer(name string) bool {
	_, ok := secretContainerNames[strings.ToLower(name)]
	return ok
}

func cloneMapStringAny(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	maps.Copy(dst, src)
	return dst
}

func cloneMapStringString(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	dst := make(map[string]string, len(src))
	maps.Copy(dst, src)
	return dst
}

func redactStringMap(src map[string]string) (map[string]any, bool) {
	if src == nil {
		return nil, false
	}
	out := make(map[string]any, len(src))
	hasSecret := false
	for k, v := range src {
		if strings.TrimSpace(v) == "" {
			out[k] = v
			continue
		}
		out[k] = redactedSecretValue
		hasSecret = true
	}
	return out, hasSecret
}

func redactAnyValue(value any, forceSecret bool) (any, bool) {
	switch v := value.(type) {
	case nil:
		return nil, false
	case string:
		if forceSecret {
			return redactSecretValue(v), strings.TrimSpace(v) != ""
		}
		return v, false
	case map[string]string:
		if forceSecret {
			redacted, hasSecret := redactStringMap(v)
			return redacted, hasSecret
		}
		out := make(map[string]any, len(v))
		hasSecret := false
		for key, child := range v {
			secret := forceSecret || isSecretField(key) || isSecretContainer(key)
			redactedChild, childHasSecret := redactAnyValue(child, secret)
			out[key] = redactedChild
			hasSecret = hasSecret || childHasSecret
		}
		return out, hasSecret
	case map[string]any:
		out := make(map[string]any, len(v))
		hasSecret := false
		for key, child := range v {
			secret := forceSecret || isSecretField(key) || isSecretContainer(key)
			redactedChild, childHasSecret := redactAnyValue(child, secret)
			out[key] = redactedChild
			hasSecret = hasSecret || childHasSecret
		}
		return out, hasSecret
	case []string:
		if !forceSecret {
			return slices.Clone(v), false
		}
		out := make([]any, len(v))
		hasSecret := false
		for i, item := range v {
			out[i] = redactSecretValue(item)
			hasSecret = hasSecret || strings.TrimSpace(item) != ""
		}
		return out, hasSecret
	case []any:
		out := make([]any, len(v))
		hasSecret := false
		for i, item := range v {
			redactedChild, childHasSecret := redactAnyValue(item, forceSecret)
			out[i] = redactedChild
			hasSecret = hasSecret || childHasSecret
		}
		return out, hasSecret
	default:
		if forceSecret {
			text := fmt.Sprintf("%v", v)
			if strings.TrimSpace(text) == "" {
				return v, false
			}
			return redactedSecretValue, true
		}
		return v, false
	}
}

func redactTCPConfig(cfg *core.TCPConfig) (map[string]any, bool) {
	if cfg == nil {
		return nil, false
	}
	out := map[string]any{
		"banner_match": cfg.BannerMatch,
		"expect_regex": cfg.ExpectRegex,
	}
	if send, secret := redactAnyValue(cfg.Send, true); send != nil && send != "" {
		out["send"] = send
		return out, secret
	}
	return out, false
}

func redactUDPConfig(cfg *core.UDPConfig) (map[string]any, bool) {
	if cfg == nil {
		return nil, false
	}
	out := map[string]any{"expect_contains": cfg.ExpectContains}
	if send, secret := redactAnyValue(cfg.SendHex, true); send != nil && send != "" {
		out["send_hex"] = send
		return out, secret
	}
	return out, false
}

func redactHTTPConfig(cfg *core.HTTPConfig) (map[string]any, bool) {
	if cfg == nil {
		return nil, false
	}
	out := map[string]any{
		"method":               cfg.Method,
		"valid_status":         slices.Clone(cfg.ValidStatus),
		"body_contains":        cfg.BodyContains,
		"body_regex":           cfg.BodyRegex,
		"json_path":            cloneMapStringString(cfg.JSONPath),
		"json_schema":          cfg.JSONSchema,
		"json_schema_strict":   cfg.JSONSchemaStrict,
		"feather":              cfg.Feather,
		"follow_redirects":     cfg.FollowRedirects,
		"max_redirects":        cfg.MaxRedirects,
		"insecure_skip_verify": cfg.InsecureSkipVerify,
	}
	hasSecret := false
	if responseHeaders, secret := redactAnyValue(cfg.ResponseHeaders, true); responseHeaders != nil {
		out["response_headers"] = responseHeaders
		hasSecret = hasSecret || secret
	}
	if headers, secret := redactAnyValue(cfg.Headers, true); headers != nil {
		out["headers"] = headers
		hasSecret = hasSecret || secret
	}
	if body, secret := redactAnyValue(cfg.Body, true); body != nil && body != "" {
		out["body"] = body
		hasSecret = hasSecret || secret
	}
	return out, hasSecret
}

func redactSMTPConfig(cfg *core.SMTPConfig) (map[string]any, bool) {
	if cfg == nil {
		return nil, false
	}
	out := map[string]any{
		"ehlo_domain":          cfg.EHLODomain,
		"starttls":             cfg.StartTLS,
		"insecure_skip_verify": cfg.InsecureSkipVerify,
		"banner_contains":      cfg.BannerContains,
	}
	hasSecret := false
	if cfg.Auth != nil {
		auth := map[string]any{"username": cfg.Auth.Username}
		if password, secret := redactAnyValue(cfg.Auth.Password, true); password != nil {
			auth["password"] = password
			hasSecret = hasSecret || secret
		}
		out["auth"] = auth
	}
	return out, hasSecret
}

func redactIMAPConfig(cfg *core.IMAPConfig) (map[string]any, bool) {
	if cfg == nil {
		return nil, false
	}
	out := map[string]any{
		"tls":                  cfg.TLS,
		"check_mailbox":        cfg.CheckMailbox,
		"insecure_skip_verify": cfg.InsecureSkipVerify,
	}
	hasSecret := false
	if cfg.Auth != nil {
		auth := map[string]any{"username": cfg.Auth.Username}
		if password, secret := redactAnyValue(cfg.Auth.Password, true); password != nil {
			auth["password"] = password
			hasSecret = hasSecret || secret
		}
		out["auth"] = auth
	}
	return out, hasSecret
}

func redactGRPCConfig(cfg *core.GRPCConfig) (map[string]any, bool) {
	if cfg == nil {
		return nil, false
	}
	out := map[string]any{
		"service":              cfg.Service,
		"tls":                  cfg.TLS,
		"tls_ca":               cfg.TLSCA,
		"insecure_skip_verify": cfg.InsecureSkipVerify,
		"feather":              cfg.Feather,
	}
	metadata, hasSecret := redactAnyValue(cfg.Metadata, true)
	if metadata != nil {
		out["metadata"] = metadata
	}
	return out, hasSecret
}

func redactWebSocketConfig(cfg *core.WebSocketConfig) (map[string]any, bool) {
	if cfg == nil {
		return nil, false
	}
	out := map[string]any{
		"subprotocols":         slices.Clone(cfg.Subprotocols),
		"expect_contains":      cfg.ExpectContains,
		"ping_check":           cfg.PingCheck,
		"feather":              cfg.Feather,
		"insecure_skip_verify": cfg.InsecureSkipVerify,
	}
	headers, hasSecret := redactAnyValue(cfg.Headers, true)
	if headers != nil {
		out["headers"] = headers
	}
	if send, secret := redactAnyValue(cfg.Send, true); send != nil && send != "" {
		out["send"] = send
		hasSecret = hasSecret || secret
	}
	return out, hasSecret
}

func redactedSoulDTOFromCore(soul *core.Soul) *redactedSoulDTO {
	if soul == nil {
		return nil
	}
	hasSecrets := map[string]bool{}
	setSecretFlag := func(key string, hasSecret bool) {
		if hasSecret {
			hasSecrets[key] = true
		}
	}
	target, targetSecret := redactTarget(soul.Target)
	httpCfg, httpSecret := redactHTTPConfig(soul.HTTP)
	tcpCfg, tcpSecret := redactTCPConfig(soul.TCP)
	udpCfg, udpSecret := redactUDPConfig(soul.UDP)
	smtpCfg, smtpSecret := redactSMTPConfig(soul.SMTP)
	imapCfg, imapSecret := redactIMAPConfig(soul.IMAP)
	grpcCfg, grpcSecret := redactGRPCConfig(soul.GRPC)
	wsCfg, wsSecret := redactWebSocketConfig(soul.WebSocket)
	setSecretFlag("target", targetSecret)
	setSecretFlag("http", httpSecret)
	setSecretFlag("tcp", tcpSecret)
	setSecretFlag("udp", udpSecret)
	setSecretFlag("smtp", smtpSecret)
	setSecretFlag("imap", imapSecret)
	setSecretFlag("grpc", grpcSecret)
	setSecretFlag("websocket", wsSecret)
	var secretFlags map[string]bool
	if len(hasSecrets) > 0 {
		secretFlags = hasSecrets
	}
	return &redactedSoulDTO{
		ID:          soul.ID,
		WorkspaceID: soul.WorkspaceID,
		Name:        soul.Name,
		Type:        soul.Type,
		Target:      target,
		Weight:      soul.Weight,
		Timeout:     soul.Timeout,
		Enabled:     soul.Enabled,
		Tags:        slices.Clone(soul.Tags),
		Regions:     slices.Clone(soul.Regions),
		Region:      soul.Region,
		HTTP:        httpCfg,
		TCP:         tcpCfg,
		UDP:         udpCfg,
		DNS:         soul.DNS,
		SMTP:        smtpCfg,
		IMAP:        imapCfg,
		ICMP:        soul.ICMP,
		GRPC:        grpcCfg,
		WebSocket:   wsCfg,
		TLS:         soul.TLS,
		CreatedAt:   soul.CreatedAt,
		UpdatedAt:   soul.UpdatedAt,
		HasSecrets:  secretFlags,
	}
}

func redactedSoulMonitorResponse(soul *core.Soul, status string, lastCheck interface{}, latency int64) soulMonitorDTO {
	return soulMonitorDTO{
		redactedSoulDTOFromCore(soul),
		status,
		lastCheck,
		latency,
	}
}

// redactedJudgmentFromCore clones a judgment for REST serialization and drops
// raw HTTP response material. Assertions and status metadata are sufficient for
// monitoring, while bodies and headers commonly contain tokens, cookies, PII,
// or provider-specific credentials.
func redactedJudgmentFromCore(judgment *core.Judgment) *core.Judgment {
	if judgment == nil {
		return nil
	}
	out := *judgment
	if judgment.Details != nil {
		details := *judgment.Details
		details.ResponseHeaders = nil
		details.ResponseBody = ""
		details.RedirectChain = slices.Clone(judgment.Details.RedirectChain)
		details.ResolvedAddresses = slices.Clone(judgment.Details.ResolvedAddresses)
		details.Capabilities = slices.Clone(judgment.Details.Capabilities)
		details.Assertions = slices.Clone(judgment.Details.Assertions)
		if judgment.Details.PropagationResult != nil {
			details.PropagationResult = maps.Clone(judgment.Details.PropagationResult)
		}
		out.Details = &details
	}
	return &out
}

func redactedJudgmentsFromCore(judgments []*core.Judgment) []*core.Judgment {
	out := make([]*core.Judgment, 0, len(judgments))
	for _, judgment := range judgments {
		if judgment != nil {
			out = append(out, redactedJudgmentFromCore(judgment))
		}
	}
	return out
}

func redactChannelConfig(config map[string]interface{}) (map[string]any, bool) {
	if config == nil {
		return map[string]any{}, false
	}
	redacted, hasSecret := redactAnyValue(config, true)
	redactedMap, ok := redacted.(map[string]any)
	if !ok {
		return map[string]any{}, hasSecret
	}
	return redactedMap, hasSecret
}

func redactedChannelDTOFromCore(ch *core.AlertChannel) *redactedChannelDTO {
	if ch == nil {
		return nil
	}
	config, hasSecret := redactChannelConfig(ch.Config)
	var flags map[string]bool
	if hasSecret {
		flags = map[string]bool{"config": true}
	}
	return &redactedChannelDTO{
		ID:          ch.ID,
		Name:        ch.Name,
		Type:        ch.Type,
		Enabled:     ch.Enabled,
		WorkspaceID: ch.WorkspaceID,
		Config:      config,
		Filters:     slices.Clone(ch.Filters),
		RateLimit:   ch.RateLimit,
		RetryPolicy: ch.RetryPolicy,
		CreatedAt:   ch.CreatedAt,
		UpdatedAt:   ch.UpdatedAt,
		HasSecrets:  flags,
	}
}

func isRedactedSecretString(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed == "" || trimmed == redactedSecretValue || trimmed == "***"
}

func preserveSecretString(existing, incoming string) string {
	if isRedactedSecretString(incoming) {
		return existing
	}
	return incoming
}

func mergeSecretStringMap(existing map[string]string, incoming map[string]string) map[string]string {
	if incoming == nil {
		return cloneMapStringString(existing)
	}
	merged := cloneMapStringString(existing)
	if merged == nil {
		merged = make(map[string]string, len(incoming))
	}
	for key, value := range incoming {
		current := existing[key]
		merged[key] = preserveSecretString(current, value)
	}
	return merged
}

func mergeSecretAnyMap(existing map[string]interface{}, incoming map[string]interface{}) map[string]interface{} {
	if incoming == nil {
		return cloneMapStringAny(existing)
	}
	merged := cloneMapStringAny(existing)
	if merged == nil {
		merged = make(map[string]interface{}, len(incoming))
	}
	for key, value := range incoming {
		current, ok := existing[key]
		switch typed := value.(type) {
		case string:
			if ok {
				if currentString, ok := current.(string); ok {
					merged[key] = preserveSecretString(currentString, typed)
					continue
				}
			}
			merged[key] = typed
		case map[string]interface{}:
			currentMap, _ := current.(map[string]interface{})
			merged[key] = mergeSecretAnyMap(currentMap, typed)
		case map[string]string:
			currentMap, _ := current.(map[string]string)
			merged[key] = mergeSecretStringMap(currentMap, typed)
		default:
			merged[key] = value
		}
	}
	return merged
}

func mergeSoulSecrets(existing, incoming *core.Soul) {
	if existing == nil || incoming == nil {
		return
	}
	// A redaction marker means "reuse the secret already bound to this
	// endpoint", not "copy it to an arbitrary destination". Refuse all
	// implicit reuse when either the protocol or target changes.
	if existing.Type != incoming.Type || existing.Target != incoming.Target {
		return
	}
	if incoming.HTTP != nil && existing.HTTP != nil && strings.EqualFold(existing.HTTP.Method, incoming.HTTP.Method) {
		incoming.HTTP.Headers = mergeSecretStringMap(existing.HTTP.Headers, incoming.HTTP.Headers)
		incoming.HTTP.Body = preserveSecretString(existing.HTTP.Body, incoming.HTTP.Body)
	}
	if incoming.TCP != nil && existing.TCP != nil {
		incoming.TCP.Send = preserveSecretString(existing.TCP.Send, incoming.TCP.Send)
	}
	if incoming.UDP != nil && existing.UDP != nil {
		incoming.UDP.SendHex = preserveSecretString(existing.UDP.SendHex, incoming.UDP.SendHex)
	}
	if incoming.SMTP != nil && existing.SMTP != nil && incoming.SMTP.Auth != nil && existing.SMTP.Auth != nil && incoming.SMTP.Auth.Username == existing.SMTP.Auth.Username {
		incoming.SMTP.Auth.Password = preserveSecretString(existing.SMTP.Auth.Password, incoming.SMTP.Auth.Password)
	}
	if incoming.IMAP != nil && existing.IMAP != nil && incoming.IMAP.Auth != nil && existing.IMAP.Auth != nil && incoming.IMAP.Auth.Username == existing.IMAP.Auth.Username {
		incoming.IMAP.Auth.Password = preserveSecretString(existing.IMAP.Auth.Password, incoming.IMAP.Auth.Password)
	}
	if incoming.GRPC != nil && existing.GRPC != nil {
		incoming.GRPC.Metadata = mergeSecretStringMap(existing.GRPC.Metadata, incoming.GRPC.Metadata)
	}
	if incoming.WebSocket != nil && existing.WebSocket != nil {
		incoming.WebSocket.Headers = mergeSecretStringMap(existing.WebSocket.Headers, incoming.WebSocket.Headers)
		incoming.WebSocket.Send = preserveSecretString(existing.WebSocket.Send, incoming.WebSocket.Send)
	}
}

func mergeChannelSecrets(existing, incoming *core.AlertChannel) error {
	if existing == nil || incoming == nil || existing.Type != incoming.Type {
		return nil
	}
	// When the destination endpoint changes, the caller must re-enter secrets
	// explicitly. Preserving a [REDACTED] credential on a different destination
	// would leak the credential to an attacker-controlled endpoint.
	if channelDestinationChanged(existing.Config, incoming.Config) && hasRedactedConfigValues(incoming.Config) {
		return fmt.Errorf("channel destination changed: secrets must be re-entered for the new destination")
	}
	incoming.Config = mergeSecretAnyMap(existing.Config, incoming.Config)
	return nil
}

// channelDestinationChanged returns true when the channel-type-specific
// destination endpoint has changed between existing and incoming config.
// A redacted placeholder in the incoming config means "preserve the existing
// value", so it is never considered a destination change.
func channelDestinationChanged(existing, incoming map[string]any) bool {
	for _, key := range []string{"url", "webhook_url"} {
		old, oldOk := existing[key].(string)
		newVal, newOk := incoming[key].(string)
		if oldOk && newOk && old != newVal && !isRedactedSecretString(newVal) {
			return true
		}
	}
	return false
}

// hasRedactedConfigValues returns true when any string value in the config map
// is a redaction placeholder (empty, [REDACTED], or ***), indicating the caller
// expects the existing value to be preserved.
func hasRedactedConfigValues(config map[string]any) bool {
	for _, v := range config {
		if s, ok := v.(string); ok && isRedactedSecretString(s) {
			return true
		}
		if m, ok := v.(map[string]any); ok {
			for _, mv := range m {
				if s, ok := mv.(string); ok && isRedactedSecretString(s) {
					return true
				}
			}
		}
	}
	return false
}

func mcpTextResult(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(data)
}
