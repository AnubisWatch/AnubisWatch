package grpcapi

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	v1 "github.com/AnubisWatch/anubiswatch/internal/grpcapi/v1"
)

const redactedSecretValue = "[REDACTED]"

var grpcSecretFieldNames = map[string]struct{}{
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

var grpcChannelDestinationFieldNames = map[string]struct{}{
	"webhook_url": {},
	"url":         {},
	"endpoint":    {},
	"address":     {},
	"host":        {},
}

func isGRPCSecretField(name string) bool {
	_, ok := grpcSecretFieldNames[strings.ToLower(name)]
	return ok
}

func redactSecretString(value string) string {
	if strings.TrimSpace(value) == "" {
		return value
	}
	return redactedSecretValue
}

func cloneStringMap(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	dst := make(map[string]string, len(src))
	maps.Copy(dst, src)
	return dst
}

func isGRPCRedactedSecretString(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed == "***" || strings.Contains(trimmed, redactedSecretValue)
}

func isGRPCChannelDestinationField(name string) bool {
	_, ok := grpcChannelDestinationFieldNames[strings.ToLower(name)]
	return ok
}

func existingStringValue(config map[string]interface{}, key string) (string, bool) {
	if config == nil {
		return "", false
	}
	value, ok := config[key]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	return text, ok
}

func grpcChannelDestinationChanged(existing map[string]interface{}, updates map[string]string) bool {
	for key, incoming := range updates {
		if !isGRPCChannelDestinationField(key) || isGRPCRedactedSecretString(incoming) {
			continue
		}
		current, ok := existingStringValue(existing, key)
		if !ok || current != incoming {
			return true
		}
	}
	return false
}

func grpcChannelUpdateHasPreservedSecrets(existing map[string]interface{}, updates map[string]string) bool {
	for key, currentValue := range existing {
		if isGRPCChannelDestinationField(key) || currentValue == nil {
			continue
		}
		if current, ok := currentValue.(string); ok && strings.TrimSpace(current) == "" {
			continue
		}
		incoming, supplied := updates[key]
		if !supplied || isGRPCRedactedSecretString(incoming) {
			return true
		}
	}
	return false
}

func grpcChannelUpdateHasUnresolvedPlaceholder(existing map[string]interface{}, updates map[string]string) bool {
	for key, incoming := range updates {
		if !isGRPCRedactedSecretString(incoming) {
			continue
		}
		current, ok := existing[key]
		if !ok || current == nil {
			return true
		}
		if currentString, ok := current.(string); ok && strings.TrimSpace(currentString) == "" {
			return true
		}
	}
	return false
}

func grpcSoulHasSecrets(soul *core.Soul) bool {
	if soul == nil {
		return false
	}
	if soul.HTTP != nil && (len(soul.HTTP.Headers) > 0 || strings.TrimSpace(soul.HTTP.Body) != "") {
		return true
	}
	if soul.TCP != nil && strings.TrimSpace(soul.TCP.Send) != "" {
		return true
	}
	if soul.SMTP != nil && soul.SMTP.Auth != nil && strings.TrimSpace(soul.SMTP.Auth.Password) != "" {
		return true
	}
	if soul.IMAP != nil && soul.IMAP.Auth != nil && strings.TrimSpace(soul.IMAP.Auth.Password) != "" {
		return true
	}
	if soul.GRPC != nil && len(soul.GRPC.Metadata) > 0 {
		return true
	}
	return soul.WebSocket != nil && len(soul.WebSocket.Headers) > 0
}

func mergeGRPCChannelConfig(existing map[string]interface{}, updates map[string]string) map[string]interface{} {
	merged := make(map[string]interface{}, len(existing)+len(updates))
	maps.Copy(merged, existing)
	for key, incoming := range updates {
		if current, ok := existing[key]; ok && isGRPCRedactedSecretString(incoming) {
			merged[key] = current
			continue
		}
		merged[key] = incoming
	}
	return merged
}

func redactStringMap(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		if strings.TrimSpace(v) == "" {
			out[k] = v
			continue
		}
		out[k] = redactedSecretValue
	}
	return out
}

func redactAnyMapForGRPC(config map[string]interface{}) map[string]string {
	if config == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(config))
	for key, value := range config {
		if value == nil {
			out[key] = ""
			continue
		}
		switch typed := value.(type) {
		case string:
			out[key] = redactSecretString(typed)
		case map[string]string:
			data, _ := json.Marshal(redactStringMap(typed))
			out[key] = string(data)
		case map[string]interface{}:
			data, _ := json.Marshal(redactNestedMapForGRPC(typed, true))
			out[key] = string(data)
		default:
			if isGRPCSecretField(key) {
				out[key] = redactedSecretValue
			} else {
				out[key] = fmt.Sprintf("%v", value)
			}
		}
	}
	return out
}

func redactNestedMapForGRPC(input map[string]interface{}, forceSecret bool) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		secret := forceSecret || isGRPCSecretField(key)
		switch typed := value.(type) {
		case string:
			if secret {
				out[key] = redactSecretString(typed)
			} else {
				out[key] = typed
			}
		case map[string]interface{}:
			out[key] = redactNestedMapForGRPC(typed, secret)
		case map[string]string:
			if secret {
				redacted := make(map[string]any, len(typed))
				for nestedKey, nestedValue := range typed {
					redacted[nestedKey] = redactSecretString(nestedValue)
				}
				out[key] = redacted
			} else {
				plain := make(map[string]any, len(typed))
				for nestedKey, nestedValue := range typed {
					plain[nestedKey] = nestedValue
				}
				out[key] = plain
			}
		default:
			if secret {
				out[key] = redactedSecretValue
			} else {
				out[key] = value
			}
		}
	}
	return out
}

func grpcHTTPCheck(cfg *core.HTTPConfig) *v1.HTTPCheck {
	if cfg == nil {
		return nil
	}
	bodyContains := []string{}
	if cfg.BodyContains != "" {
		bodyContains = append(bodyContains, cfg.BodyContains)
	}
	responseHeaders := []string{}
	for key, value := range cfg.ResponseHeaders {
		responseHeaders = append(responseHeaders, key+":"+value)
	}
	return &v1.HTTPCheck{
		Method:          cfg.Method,
		Headers:         redactStringMap(cfg.Headers),
		Body:            redactSecretString(cfg.Body),
		FollowRedirects: cfg.FollowRedirects,
		BodyContains:    bodyContains,
		BodyRegex:       cfg.BodyRegex,
		JsonSchema:      cfg.JSONSchema,
		ResponseHeaders: responseHeaders,
		Feather:         optionalDurationString(cfg.Feather),
	}
}

func grpcTCPCheck(cfg *core.TCPConfig) *v1.TCPCheck {
	if cfg == nil {
		return nil
	}
	return &v1.TCPCheck{
		BannerMatch: cfg.BannerMatch,
		Send:        redactSecretString(cfg.Send),
		ExpectRegex: cfg.ExpectRegex,
	}
}

func grpcDNSCheck(cfg *core.DNSConfig) *v1.DNSCheck {
	if cfg == nil {
		return nil
	}
	expected := ""
	if len(cfg.Expected) > 0 {
		expected = strings.Join(cfg.Expected, ",")
	}
	return &v1.DNSCheck{
		RecordType:       cfg.RecordType,
		Nameservers:      slices.Clone(cfg.Nameservers),
		Expected:         expected,
		PropagationCheck: cfg.PropagationCheck,
	}
}

func grpcTLSCheck(cfg *core.TLSConfig) *v1.TLSCheck {
	if cfg == nil {
		return nil
	}
	expectedSAN := ""
	if len(cfg.ExpectedSAN) > 0 {
		expectedSAN = strings.Join(cfg.ExpectedSAN, ",")
	}
	return &v1.TLSCheck{
		MinProtocol:         cfg.MinProtocol,
		ExpectedIssuer:      cfg.ExpectedIssuer,
		ExpectedSan:         expectedSAN,
		MinDaysBeforeExpiry: int32(cfg.ExpiryWarnDays),
		CheckChain:          cfg.CheckOCSP,
	}
}

func grpcGRPCCheck(cfg *core.GRPCConfig) *v1.GRPCCheck {
	if cfg == nil {
		return nil
	}
	return &v1.GRPCCheck{
		Service:  cfg.Service,
		Tls:      cfg.TLS,
		TlsCa:    cfg.TLSCA,
		Metadata: redactStringMap(cfg.Metadata),
	}
}

func optionalDurationString(d core.Duration) *string {
	if d.Duration == 0 {
		return nil
	}
	value := d.String()
	return &value
}
