package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"
)

// settingsSystemConfigKey is the system-config key presentation settings are
// stored under. The backup manager already includes it in backups.
const settingsSystemConfigKey = "settings"

// SettingsStore persists presentation settings. storage.CobaltDB satisfies it.
type SettingsStore interface {
	GetSystemConfig(ctx context.Context, key string) ([]byte, error)
	SaveSystemConfig(ctx context.Context, key string, value []byte) error
}

// editableSettingDefaults lists the only keys PUT /api/v1/config accepts:
// presentation preferences that change nothing about how the server runs.
// Everything else GET reports comes from the config file and is read-only.
var editableSettingDefaults = map[string]string{
	"instance_name": "AnubisWatch",
	// Empty means each viewer's own local time; otherwise an IANA zone name
	// the dashboard formats every date in.
	"timezone": "",
	"language": "en",
	"theme":    "dark",
}

// SetRuntimeConfig records the loaded config values GET /api/v1/config
// reports, so the Settings page shows what the server actually runs with.
func (s *RESTServer) SetRuntimeConfig(storagePath string, rawRetention time.Duration) {
	s.uiConfigMu.Lock()
	defer s.uiConfigMu.Unlock()
	s.storagePath = storagePath
	s.retention = rawRetention
}

// SetSettingsStore installs persistence for presentation settings and loads
// any saved ones. A missing or unreadable record leaves the defaults; it is
// not a startup failure.
func (s *RESTServer) SetSettingsStore(store SettingsStore) {
	s.uiConfigMu.Lock()
	defer s.uiConfigMu.Unlock()
	s.settingsStore = store
	s.reloadSettingsLocked()
}

// reloadSettingsLocked replaces the editable settings with the stored ones.
// In cluster mode another node may have written them, so GET re-reads storage
// instead of trusting the copy loaded at startup. Caller holds uiConfigMu.
func (s *RESTServer) reloadSettingsLocked() {
	if s.settingsStore == nil {
		return
	}
	data, err := s.settingsStore.GetSystemConfig(context.Background(), settingsSystemConfigKey)
	if err != nil || len(data) == 0 {
		if err != nil && !isNotFoundError(err) {
			s.logger.Warn("failed to load saved settings", "error", err)
		}
		return
	}
	var saved map[string]string
	if err := json.Unmarshal(data, &saved); err != nil {
		s.logger.Warn("ignoring unreadable saved settings", "error", err)
		return
	}
	next := make(map[string]interface{}, len(saved))
	for key, value := range saved {
		if _, ok := editableSettingDefaults[key]; !ok {
			continue
		}
		if validateSetting(key, value) == nil {
			next[key] = value
		}
	}
	s.uiConfig = next
}

func (s *RESTServer) handleGetConfig(ctx *Context) error {
	s.uiConfigMu.Lock()
	s.reloadSettingsLocked()
	s.uiConfigMu.Unlock()
	return ctx.JSON(http.StatusOK, s.currentUIConfig())
}

// readOnlyUIConfig is the config-file-derived part of the payload. The caller
// must hold uiConfigMu.
func (s *RESTServer) readOnlyUIConfig() map[string]interface{} {
	return map[string]interface{}{
		"retention_days":    int(s.retention / (24 * time.Hour)),
		"storage_path":      s.storagePath,
		"auth_enabled":      s.authConfig.IsEnabled(),
		"mcp_enabled":       s.mcp != nil,
		"websocket_enabled": true,
		"host":              s.config.Host,
		"port":              s.config.Port,
		"grpc_port":         s.config.GRPCPort,
		"tls_enabled":       s.config.TLS.Enabled,
		"auth_type":         s.authConfig.Type,
	}
}

func (s *RESTServer) currentUIConfig() map[string]interface{} {
	s.uiConfigMu.RLock()
	defer s.uiConfigMu.RUnlock()

	config := s.readOnlyUIConfig()
	readOnly := make([]string, 0, len(config))
	for key := range config {
		readOnly = append(readOnly, key)
	}
	sort.Strings(readOnly)
	config["read_only_keys"] = readOnly

	for key, value := range editableSettingDefaults {
		config[key] = value
	}
	for key, value := range s.uiConfig {
		config[key] = value
	}
	return config
}

// validateSetting checks a presentation value before it is stored.
func validateSetting(key, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" && key == "timezone" && value == "" {
		return nil // the viewer's local time
	}
	if trimmed == "" {
		return fmt.Errorf("%s must not be empty", key)
	}
	if strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return fmt.Errorf("%s must not contain control characters", key)
	}
	switch key {
	case "theme":
		switch value {
		case "dark", "light", "system":
			return nil
		}
		return fmt.Errorf("theme must be one of dark, light, system")
	case "instance_name":
		if len(value) > 100 {
			return fmt.Errorf("instance_name must be at most 100 characters")
		}
	case "timezone":
		if value == "" {
			return nil
		}
		if len(value) > 64 {
			return fmt.Errorf("timezone must be at most 64 characters")
		}
		// "Local" would mean the server's zone, not the viewer's.
		if _, err := time.LoadLocation(value); err != nil || value == "Local" {
			return fmt.Errorf("timezone must be an IANA time zone such as Europe/Istanbul, or empty for the viewer's local time")
		}
	case "language":
		if len(value) > 16 {
			return fmt.Errorf("language must be at most 16 characters")
		}
		for _, r := range value {
			if r != '-' && r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
				return fmt.Errorf("language must be a language tag such as en or pt-BR")
			}
		}
	}
	return nil
}

// handleUpdateConfig accepts only presentation keys (editableSettingDefaults).
// A string sets the value, null restores the default. Any other key is
// rejected rather than silently dropped: a value written to memory and
// applied nowhere looked saved but changed nothing.
func (s *RESTServer) handleUpdateConfig(ctx *Context) error {
	var input map[string]interface{}
	// Limit request body size to prevent memory exhaustion
	ctx.Request.Body = http.MaxBytesReader(ctx.Response, ctx.Request.Body, maxRequestBodySize)
	if err := json.NewDecoder(ctx.Request.Body).Decode(&input); err != nil {
		return ctx.Error(http.StatusBadRequest, "invalid JSON")
	}

	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	s.uiConfigMu.Lock()
	readOnly := s.readOnlyUIConfig()
	readOnly["read_only_keys"] = nil
	next := make(map[string]interface{}, len(s.uiConfig))
	for key, value := range s.uiConfig {
		next[key] = value
	}
	for _, key := range keys {
		if _, ok := editableSettingDefaults[key]; !ok {
			s.uiConfigMu.Unlock()
			if _, known := readOnly[key]; known {
				return ctx.Error(http.StatusBadRequest, key+" is set in the config file and cannot be changed at runtime")
			}
			return ctx.Error(http.StatusBadRequest, key+" is not a known setting")
		}
		switch value := input[key].(type) {
		case nil:
			delete(next, key)
		case string:
			if err := validateSetting(key, value); err != nil {
				s.uiConfigMu.Unlock()
				return ctx.Error(http.StatusBadRequest, err.Error())
			}
			next[key] = value
		default:
			s.uiConfigMu.Unlock()
			return ctx.Error(http.StatusBadRequest, key+" must be a string or null")
		}
	}

	if s.settingsStore != nil {
		data, err := json.Marshal(next)
		if err == nil {
			err = s.settingsStore.SaveSystemConfig(ctx.Request.Context(), settingsSystemConfigKey, data)
		}
		if err != nil {
			s.uiConfigMu.Unlock()
			return s.internalError(ctx, err, "failed to save settings")
		}
	}
	s.uiConfig = next
	s.uiConfigMu.Unlock()

	return ctx.JSON(http.StatusOK, s.currentUIConfig())
}
