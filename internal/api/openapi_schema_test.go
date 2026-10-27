package api

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/sla"
)

// specSchemaTypes ties component schemas to the Go type the server decodes
// (requests) or encodes (responses) for them. Every property a schema
// documents must exist as a JSON field of that type: a documented field the
// server never reads (http_config instead of http, condition instead of
// conditions) is silently dropped, and the request fails or misbehaves while
// following the docs exactly.
var specSchemaTypes = map[string]reflect.Type{
	"Soul":                       reflect.TypeOf(soulMonitorDTO{}),
	"SoulCreate":                 reflect.TypeOf(core.Soul{}),
	"HTTPConfig":                 reflect.TypeOf(core.HTTPConfig{}),
	"AlertChannel":               reflect.TypeOf(redactedChannelDTO{}),
	"AlertChannelCreate":         reflect.TypeOf(core.AlertChannel{}),
	"AlertFilter":                reflect.TypeOf(core.AlertFilter{}),
	"AlertRateLimit":             reflect.TypeOf(core.RateLimitConfig{}),
	"AlertRetryPolicy":           reflect.TypeOf(core.RetryPolicyConfig{}),
	"AlertRule":                  reflect.TypeOf(core.AlertRule{}),
	"AlertRuleCreate":            reflect.TypeOf(core.AlertRule{}),
	"AlertRuleScope":             reflect.TypeOf(core.RuleScope{}),
	"AlertCondition":             reflect.TypeOf(core.AlertCondition{}),
	"ClusterNodeRequest":         reflect.TypeOf(clusterNodeRequest{}),
	"Journey":                    reflect.TypeOf(core.JourneyConfig{}),
	"JourneyCreate":              reflect.TypeOf(core.JourneyConfig{}),
	"JourneyStep":                reflect.TypeOf(core.JourneyStep{}),
	"JourneyAssertion":           reflect.TypeOf(core.Assertion{}),
	"StatusPage":                 reflect.TypeOf(statusPageResponse{}),
	"StatusPageCreate":           reflect.TypeOf(statusPageRequest{}),
	"StatusIncident":             reflect.TypeOf(core.StatusIncident{}),
	"StatusIncidentCreate":       reflect.TypeOf(statusIncidentCreateRequest{}),
	"StatusIncidentPatch":        reflect.TypeOf(statusIncidentPatchRequest{}),
	"StatusIncidentUpdateCreate": reflect.TypeOf(statusIncidentUpdateRequest{}),
	"StatusAnnouncement":         reflect.TypeOf(core.StatusAnnouncement{}),
	"StatusAnnouncementInput":    reflect.TypeOf(statusAnnouncementRequest{}),
	"MaintenanceWindowCreate":    reflect.TypeOf(core.MaintenanceWindow{}),
	"MaintenanceWindowUpdate":    reflect.TypeOf(core.MaintenanceWindow{}),
	"CustomDashboardInput":       reflect.TypeOf(core.CustomDashboard{}),
	"WidgetQuery":                reflect.TypeOf(core.WidgetQuery{}),
	"WorkspaceInput":             reflect.TypeOf(core.Workspace{}),
	"MCPRequest":                 reflect.TypeOf(MCPRequest{}),
}

// specResponseTypes ties response schemas to the Go type the server encodes.
// Besides the property check above, every name a response schema lists as
// required must be a JSON field encoding/json always writes (no omitempty or
// omitzero, unless it is a struct, which omitempty never drops): the
// dashboard's generated TypeScript types (web/src/api/schema.generated.ts)
// treat required properties as always present.
var specResponseTypes = map[string]reflect.Type{
	"Soul":               reflect.TypeOf(soulMonitorDTO{}),
	"TCPConfig":          reflect.TypeOf(core.TCPConfig{}),
	"UDPConfig":          reflect.TypeOf(core.UDPConfig{}),
	"DNSConfig":          reflect.TypeOf(core.DNSConfig{}),
	"SMTPConfig":         reflect.TypeOf(core.SMTPConfig{}),
	"IMAPConfig":         reflect.TypeOf(core.IMAPConfig{}),
	"ICMPConfig":         reflect.TypeOf(core.ICMPConfig{}),
	"GRPCConfig":         reflect.TypeOf(core.GRPCConfig{}),
	"WebSocketConfig":    reflect.TypeOf(core.WebSocketConfig{}),
	"TLSConfig":          reflect.TypeOf(core.TLSConfig{}),
	"AuthCreds":          reflect.TypeOf(core.AuthCreds{}),
	"FlapDetection":      reflect.TypeOf(core.FlapDetectionConfig{}),
	"Judgment":           reflect.TypeOf(judgmentDTO{}),
	"HeartbeatInfo":      reflect.TypeOf(heartbeatInfo{}),
	"HeartbeatState":     reflect.TypeOf(core.HeartbeatState{}),
	"SoulUptime":         reflect.TypeOf(SoulUptimeSummary{}),
	"UptimeWindow":       reflect.TypeOf(UptimeWindow{}),
	"UptimePoint":        reflect.TypeOf(UptimePoint{}),
	"AlertChannel":       reflect.TypeOf(redactedChannelDTO{}),
	"AlertRule":          reflect.TypeOf(core.AlertRule{}),
	"AlertManagerStats":  reflect.TypeOf(core.AlertManagerStats{}),
	"Incident":           reflect.TypeOf(core.Incident{}),
	"ClusterStatus":      reflect.TypeOf(ClusterStatus{}),
	"User":               reflect.TypeOf(User{}),
	"QuotaCounter":       reflect.TypeOf(core.QuotaCounter{}),
	"QuotaUsage":         reflect.TypeOf(core.WorkspaceQuotaUsage{}),
	"Member":             reflect.TypeOf(Member{}),
	"Invitation":         reflect.TypeOf(Invitation{}),
	"APIKey":             reflect.TypeOf(apiKeyView{}),
	"Pagination":         reflect.TypeOf(Pagination{}),
	"StatusPage":         reflect.TypeOf(statusPageResponse{}),
	"StatusPageTheme":    reflect.TypeOf(core.StatusPageTheme{}),
	"StatusGroup":        reflect.TypeOf(core.StatusGroup{}),
	"Journey":            reflect.TypeOf(core.JourneyConfig{}),
	"JourneyListItem":    reflect.TypeOf(journeyListItem{}),
	"JourneyRun":         reflect.TypeOf(core.JourneyRun{}),
	"JourneyStepResult":  reflect.TypeOf(core.JourneyStepResult{}),
	"CustomDashboard":    reflect.TypeOf(core.CustomDashboard{}),
	"WidgetConfig":       reflect.TypeOf(core.WidgetConfig{}),
	"Workspace":          reflect.TypeOf(core.Workspace{}),
	"Peer":               reflect.TypeOf(ClusterPeer{}),
	"SLAReport":          reflect.TypeOf(sla.Report{}),
	"SLAResponse":        reflect.TypeOf(SLAResponse{}),
	"StatusIncident":     reflect.TypeOf(core.StatusIncident{}),
	"StatusAnnouncement": reflect.TypeOf(core.StatusAnnouncement{}),
	"MaintenanceWindow":  reflect.TypeOf(core.MaintenanceWindow{}),
}

func TestOpenAPIResponseSchemasMatchGoTypes(t *testing.T) {
	spec, err := openAPISpec()
	if err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	schemas := specComponentSchemas(t, spec)
	names := make([]string, 0, len(specResponseTypes))
	for name := range specResponseTypes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		schema, ok := schemas[name]
		if !ok {
			t.Errorf("components.schemas.%s is mapped to a Go type but not in the spec", name)
			continue
		}
		typ := specResponseTypes[name]
		checkSchemaFields(t, schemas, name, schema, typ, 0)
		always := alwaysEncodedFields(typ)
		fields := jsonFields(typ)
		props := schemaProperties(schemas, schema)
		for _, prop := range schemaRequired(schemas, schema) {
			if !always[prop] {
				t.Errorf("%s.%s is required in the spec but %s can omit it (missing field or omitempty)", name, prop, typ)
				continue
			}
			// A nil slice, map or pointer encodes as null.
			switch fields[prop].Kind() {
			case reflect.Slice, reflect.Map, reflect.Pointer, reflect.Interface:
				if !schemaNullable(schemas, props[prop]) && !requiredNeverNull[name+"."+prop] {
					t.Errorf("%s.%s is required and not nullable, but %s encodes a nil %s as null", name, prop, typ, fields[prop].Kind())
				}
			}
		}
	}
}

// requiredNeverNull lists required slice/map/pointer properties the server
// never encodes as null, with the reason.
var requiredNeverNull = map[string]bool{
	"AlertRule.conditions":    true, // core.AlertRule.Validate rejects an empty list
	"AlertRule.channels":      true, // core.AlertRule.Validate rejects an empty list
	"AlertChannel.config":     true, // redactChannelConfig returns {} for nil
	"Journey.steps":           true, // validateJourneyForAPI rejects an empty list
	"JourneyListItem.steps":   true, // as Journey.steps
	"SLAResponse.souls":       true, // built with make
	"SoulUptime.windows":      true, // built as a literal map
	"CustomDashboard.widgets": true, // dashboard handlers replace nil with [] (dashboardWithWidgets)
}

func schemaNullable(schemas map[string]any, schema any) bool {
	node, _ := schema.(map[string]any)
	if ref, ok := node["$ref"].(string); ok {
		return schemaNullable(schemas, schemas[strings.TrimPrefix(ref, "#/components/schemas/")])
	}
	nullable, _ := node["nullable"].(bool)
	return nullable
}

// schemaRequired returns a schema's required names, following $ref and allOf.
func schemaRequired(schemas map[string]any, schema any) []string {
	node, _ := schema.(map[string]any)
	if ref, ok := node["$ref"].(string); ok {
		return schemaRequired(schemas, schemas[strings.TrimPrefix(ref, "#/components/schemas/")])
	}
	var out []string
	if all, ok := node["allOf"].([]any); ok {
		for _, part := range all {
			out = append(out, schemaRequired(schemas, part)...)
		}
	}
	if req, ok := node["required"].([]any); ok {
		for _, name := range req {
			if s, ok := name.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// alwaysEncodedFields reports the JSON fields of typ that encoding/json always
// writes, including those promoted from embedded structs.
func alwaysEncodedFields(typ reflect.Type) map[string]bool {
	fields := map[string]bool{}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return fields
	}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		parts := strings.Split(f.Tag.Get("json"), ",")
		name := parts[0]
		if name == "-" {
			continue
		}
		if f.Anonymous && name == "" {
			for k, v := range alwaysEncodedFields(f.Type) {
				if _, taken := fields[k]; !taken {
					fields[k] = v
				}
			}
			continue
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		omittable := false
		for _, opt := range parts[1:] {
			if opt == "omitzero" || (opt == "omitempty" && f.Type.Kind() != reflect.Struct) {
				omittable = true
			}
		}
		fields[name] = !omittable
	}
	return fields
}

func TestOpenAPISchemasMatchGoTypes(t *testing.T) {
	spec, err := openAPISpec()
	if err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	schemas := specComponentSchemas(t, spec)
	names := make([]string, 0, len(specSchemaTypes))
	for name := range specSchemaTypes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		schema, ok := schemas[name]
		if !ok {
			t.Errorf("components.schemas.%s is mapped to a Go type but not in the spec", name)
			continue
		}
		checkSchemaFields(t, schemas, name, schema, specSchemaTypes[name], 0)
	}
}

// TestOpenAPISchemaRefsResolve: every $ref names a schema that exists.
func TestOpenAPISchemaRefsResolve(t *testing.T) {
	spec, err := openAPISpec()
	if err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	schemas := specComponentSchemas(t, spec)
	var walk func(node any, where string)
	walk = func(node any, where string) {
		switch v := node.(type) {
		case map[string]any:
			if ref, ok := v["$ref"].(string); ok && strings.HasPrefix(ref, "#/components/schemas/") {
				if _, ok := schemas[strings.TrimPrefix(ref, "#/components/schemas/")]; !ok {
					t.Errorf("%s: $ref %s does not resolve", where, ref)
				}
			}
			for k, child := range v {
				walk(child, where+"."+k)
			}
		case []any:
			for _, child := range v {
				walk(child, where)
			}
		}
	}
	walk(spec, "spec")
}

func specComponentSchemas(t *testing.T, spec map[string]any) map[string]any {
	t.Helper()
	components, _ := spec["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	if len(schemas) == 0 {
		t.Fatal("spec has no components.schemas")
	}
	return schemas
}

// schemaProperties returns a schema's properties, following $ref and allOf.
func schemaProperties(schemas map[string]any, schema any) map[string]any {
	node, _ := schema.(map[string]any)
	props := map[string]any{}
	if ref, ok := node["$ref"].(string); ok {
		return schemaProperties(schemas, schemas[strings.TrimPrefix(ref, "#/components/schemas/")])
	}
	if all, ok := node["allOf"].([]any); ok {
		for _, part := range all {
			for k, v := range schemaProperties(schemas, part) {
				props[k] = v
			}
		}
	}
	if own, ok := node["properties"].(map[string]any); ok {
		for k, v := range own {
			props[k] = v
		}
	}
	return props
}

// jsonFields maps JSON names to fields the way encoding/json sees a struct,
// including fields promoted from embedded structs.
func jsonFields(typ reflect.Type) map[string]reflect.Type {
	fields := map[string]reflect.Type{}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return fields
	}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		tag := f.Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if name == "-" {
			continue
		}
		if f.Anonymous && name == "" {
			for k, v := range jsonFields(f.Type) {
				if _, taken := fields[k]; !taken {
					fields[k] = v
				}
			}
			continue
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		fields[name] = f.Type
	}
	return fields
}

// elemStruct unwraps pointers, slices and maps down to a struct type, if any.
func elemStruct(typ reflect.Type) (reflect.Type, bool) {
	for {
		switch typ.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
			typ = typ.Elem()
		case reflect.Struct:
			return typ, typ != reflect.TypeOf(core.Duration{})
		default:
			return nil, false
		}
	}
}

func checkSchemaFields(t *testing.T, schemas map[string]any, where string, schema any, typ reflect.Type, depth int) {
	t.Helper()
	if depth > 6 {
		return
	}
	fields := jsonFields(typ)
	for prop, propSchema := range schemaProperties(schemas, schema) {
		fieldType, ok := fields[prop]
		if !ok {
			t.Errorf("%s.%s is documented but %s has no such JSON field", where, prop, typ)
			continue
		}
		node, _ := propSchema.(map[string]any)
		if _, isRef := node["$ref"]; isRef {
			continue // a mapped schema is checked on its own
		}
		if items, ok := node["items"].(map[string]any); ok {
			if _, isRef := items["$ref"]; !isRef {
				node = items
			} else {
				continue
			}
		}
		if _, hasProps := node["properties"]; !hasProps {
			continue
		}
		if inner, ok := elemStruct(fieldType); ok {
			checkSchemaFields(t, schemas, where+"."+prop, node, inner, depth+1)
		}
	}
}
