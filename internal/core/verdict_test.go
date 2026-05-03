package core

import (
	"testing"
	"time"
)

func TestAlertChannel_ShouldNotify(t *testing.T) {
	channel := &AlertChannel{
		ID:      "ch1",
		Enabled: true,
		Type:    ChannelWebHook,
	}

	event := &AlertEvent{
		ID:        "event1",
		SoulID:    "soul1",
		Status:    SoulDead,
		Severity:  SeverityCritical,
		Timestamp: time.Now().UTC(),
	}

	// No filters - should notify
	if !channel.ShouldNotify(event) {
		t.Error("Expected ShouldNotify to return true with no filters")
	}

	// Disabled channel - should not notify
	channel.Enabled = false
	if channel.ShouldNotify(event) {
		t.Error("Expected ShouldNotify to return false for disabled channel")
	}
}

func TestAlertChannel_ShouldNotify_WithFilters(t *testing.T) {
	channel := &AlertChannel{
		ID:      "ch1",
		Enabled: true,
		Type:    ChannelWebHook,
		Filters: []AlertFilter{
			{Field: "status", Operator: "eq", Value: "dead"},
		},
	}

	// Matching filter
	event := &AlertEvent{
		ID:        "event1",
		SoulID:    "soul1",
		Status:    SoulDead,
		Severity:  SeverityCritical,
		Timestamp: time.Now().UTC(),
	}

	if !channel.ShouldNotify(event) {
		t.Error("Expected ShouldNotify to return true with matching filter")
	}

	// Non-matching filter
	event.Status = SoulAlive
	if channel.ShouldNotify(event) {
		t.Error("Expected ShouldNotify to return false with non-matching filter")
	}
}

func TestAlertFilter_Matches(t *testing.T) {
	filter := AlertFilter{
		Field:    "status",
		Operator: "eq",
		Value:    "dead",
	}

	event := &AlertEvent{
		ID:       "event1",
		SoulID:   "soul1",
		Status:   SoulDead,
		Severity: SeverityCritical,
	}

	if !filter.Matches(event) {
		t.Error("Expected filter to match")
	}

	// Non-matching
	event.Status = SoulAlive
	if filter.Matches(event) {
		t.Error("Expected filter to not match")
	}
}

func TestAlertFilter_Matches_Operators(t *testing.T) {
	event := &AlertEvent{
		ID:          "event1",
		SoulID:      "soul1",
		Status:      SoulDead,
		Severity:    SeverityCritical,
		ChannelType: ChannelSlack,
	}

	// eq operator
	filter := AlertFilter{Field: "status", Operator: "eq", Value: "dead"}
	if !filter.Matches(event) {
		t.Error("Expected eq filter to match")
	}

	// ne operator
	filter = AlertFilter{Field: "status", Operator: "ne", Value: "alive"}
	if !filter.Matches(event) {
		t.Error("Expected ne filter to match")
	}

	// in operator
	filter = AlertFilter{Field: "status", Operator: "in", Values: []string{"dead", "degraded"}}
	if !filter.Matches(event) {
		t.Error("Expected in filter to match")
	}

	// not_in operator
	filter = AlertFilter{Field: "status", Operator: "not_in", Values: []string{"alive"}}
	if !filter.Matches(event) {
		t.Error("Expected not_in filter to match")
	}

	// type field
	filter = AlertFilter{Field: "type", Operator: "eq", Value: "slack"}
	if !filter.Matches(event) {
		t.Error("Expected type filter to match")
	}

	// severity field
	filter = AlertFilter{Field: "severity", Operator: "eq", Value: "critical"}
	if !filter.Matches(event) {
		t.Error("Expected severity filter to match")
	}

	// soul_id field
	filter = AlertFilter{Field: "soul_id", Operator: "eq", Value: "soul1"}
	if !filter.Matches(event) {
		t.Error("Expected soul_id filter to match")
	}

	// contains operator
	filter = AlertFilter{Field: "soul_id", Operator: "contains", Value: "oul"}
	if !filter.Matches(event) {
		t.Error("Expected contains filter to match")
	}

	// default operator (returns true)
	filter = AlertFilter{Field: "status", Operator: "unknown", Value: "dead"}
	if !filter.Matches(event) {
		t.Error("Expected default operator to return true")
	}
}

func TestAlertFilter_Matches_Details(t *testing.T) {
	event := &AlertEvent{
		ID:       "event1",
		SoulID:   "soul1",
		Status:   SoulDead,
		Severity: SeverityCritical,
		Details: map[string]string{
			"custom_field":  "custom_value",
			"response_time": "500ms",
		},
	}

	// Custom field from details
	filter := AlertFilter{Field: "custom_field", Operator: "eq", Value: "custom_value"}
	if !filter.Matches(event) {
		t.Error("Expected custom field filter to match")
	}

	// Non-existent custom field
	filter = AlertFilter{Field: "non_existent", Operator: "eq", Value: "value"}
	if filter.Matches(event) {
		t.Error("Expected non-existent field to not match")
	}

	// Response time contains
	filter = AlertFilter{Field: "response_time", Operator: "contains", Value: "500"}
	if !filter.Matches(event) {
		t.Error("Expected response_time contains filter to match")
	}
}

func TestAlertFilter_Matches_InNotIn(t *testing.T) {
	event := &AlertEvent{
		ID:       "event1",
		SoulID:   "soul1",
		Status:   SoulDead,
		Severity: SeverityCritical,
	}

	// in operator - no match
	filter := AlertFilter{Field: "status", Operator: "in", Values: []string{"alive", "degraded"}}
	if filter.Matches(event) {
		t.Error("Expected in filter to not match when value not in list")
	}

	// not_in operator - match
	filter = AlertFilter{Field: "status", Operator: "not_in", Values: []string{"alive", "degraded"}}
	if !filter.Matches(event) {
		t.Error("Expected not_in filter to match when value not in list")
	}

	// not_in operator - no match (value in list)
	filter = AlertFilter{Field: "status", Operator: "not_in", Values: []string{"dead", "alive"}}
	if filter.Matches(event) {
		t.Error("Expected not_in filter to not match when value in list")
	}
}

func TestAlertFilter_Matches_NilDetails(t *testing.T) {
	event := &AlertEvent{
		ID:       "event1",
		SoulID:   "soul1",
		Status:   SoulDead,
		Severity: SeverityCritical,
		Details:  nil,
	}

	// Custom field with nil details
	filter := AlertFilter{Field: "custom_field", Operator: "eq", Value: "value"}
	if filter.Matches(event) {
		t.Error("Expected filter to not match with nil details")
	}
}

func TestMemberRole_Can(t *testing.T) {
	// Owner can do everything
	if !RoleOwner.Can("souls:*") {
		t.Error("Expected Owner to have souls:* permission")
	}

	// Viewer can read souls
	if !RoleViewer.Can("souls:read") {
		t.Error("Expected Viewer to have souls:read permission")
	}

	// Viewer cannot write souls
	if RoleViewer.Can("souls:write") {
		t.Error("Expected Viewer to not have souls:write permission")
	}

	// Editor has "souls:*" permission
	if !RoleEditor.Can("souls:*") {
		t.Error("Expected Editor to have souls:* permission")
	}
	if !RoleEditor.Can("souls:read") {
		t.Error("Expected Editor wildcard to allow souls:read permission")
	}
	if !RoleEditor.Can("souls:write") {
		t.Error("Expected Editor wildcard to allow souls:write permission")
	}
	if !RoleAdmin.Can("souls:read") {
		t.Error("Expected Admin wildcard to allow souls:read permission")
	}

	// Editor has "channels:read" permission
	if !RoleEditor.Can("channels:read") {
		t.Error("Expected Editor to have channels:read permission")
	}

	// Editor does not have "channels:write" permission
	if RoleEditor.Can("channels:write") {
		t.Error("Expected Editor to NOT have channels:write permission")
	}

	// Owner has "*" wildcard - should have all permissions
	if !RoleOwner.Can("souls:read") {
		t.Error("Expected Owner to have souls:read permission")
	}
	if !RoleOwner.Can("channels:write") {
		t.Error("Expected Owner to have channels:write permission")
	}
	if !RoleOwner.Can("random:permission") {
		t.Error("Expected Owner to have any permission via wildcard")
	}

	// Unknown role should not have any permissions
	unknownRole := MemberRole("unknown")
	if unknownRole.Can("souls:read") {
		t.Error("Unknown role should not have any permissions")
	}

	// Empty permission string
	if RoleViewer.Can("") {
		t.Error("Empty permission should not be valid")
	}
}

func TestWorkspace_NamespaceKey(t *testing.T) {
	workspace := &Workspace{
		ID: "workspace-1",
	}

	key := workspace.NamespaceKey("souls/soul-1")
	expected := "workspace-1/souls/soul-1"
	if key != expected {
		t.Errorf("NamespaceKey() = %s, want %s", key, expected)
	}

	// Nil workspace
	var nilWorkspace *Workspace
	key = nilWorkspace.NamespaceKey("test")
	if key != "test" {
		t.Errorf("Nil workspace NamespaceKey() = %s, want test", key)
	}
}

func TestValidateSlug(t *testing.T) {
	// Valid slugs
	validSlugs := []string{"test", "test-slug", "slug123", "abc"}
	for _, slug := range validSlugs {
		if err := ValidateSlug(slug); err != nil {
			t.Errorf("ValidateSlug(%q) unexpected error: %v", slug, err)
		}
	}

	// Invalid slugs
	invalidSlugs := []string{
		"",          // empty
		"test_slug", // underscore
		"TEST",      // uppercase
		"test slug", // space
		"a",         // too short
		"a very long slug that exceeds the maximum length allowed", // too long
	}
	for _, slug := range invalidSlugs {
		if err := ValidateSlug(slug); err == nil {
			t.Errorf("ValidateSlug(%q) expected error", slug)
		}
	}
}

func TestIsReservedSlug(t *testing.T) {
	reserved := []string{"api", "admin", "dashboard", "www"}
	for _, slug := range reserved {
		if !IsReservedSlug(slug) {
			t.Errorf("IsReservedSlug(%q) expected true", slug)
		}
	}

	if IsReservedSlug("my-page") {
		t.Error("IsReservedSlug(my-page) expected false")
	}
}

// Test AlertChannel.Validate
func TestAlertChannel_Validate(t *testing.T) {
	tests := []struct {
		name      string
		channel   *AlertChannel
		wantError bool
	}{
		{
			name: "valid channel",
			channel: &AlertChannel{
				ID:   "ch1",
				Name: "Test Channel",
				Type: ChannelWebHook,
			},
			wantError: false,
		},
		{
			name: "missing ID",
			channel: &AlertChannel{
				Name: "Test Channel",
				Type: ChannelWebHook,
			},
			wantError: true,
		},
		{
			name: "missing name",
			channel: &AlertChannel{
				ID:   "ch1",
				Type: ChannelWebHook,
			},
			wantError: true,
		},
		{
			name: "missing type",
			channel: &AlertChannel{
				ID:   "ch1",
				Name: "Test Channel",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.channel.Validate()
			if (err != nil) != tt.wantError {
				t.Errorf("AlertChannel.Validate() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}
