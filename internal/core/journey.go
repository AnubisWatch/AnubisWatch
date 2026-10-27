package core

import (
	"time"

	"gopkg.in/yaml.v3"
)

// JourneyConfig defines a multi-step synthetic check (Duat Journey)
type JourneyConfig struct {
	Name              string            `json:"name" yaml:"name"`
	ID                string            `json:"id" yaml:"id"`
	Description       string            `json:"description,omitempty" yaml:"description,omitempty"`
	WorkspaceID       string            `json:"workspace_id" yaml:"-"`
	Weight            Duration          `json:"weight" yaml:"weight"`   // check interval
	Timeout           Duration          `json:"timeout" yaml:"timeout"` // total journey timeout
	ContinueOnFailure bool              `json:"continue_on_failure" yaml:"continue_on_failure"`
	Variables         map[string]string `json:"variables" yaml:"variables"` // default variables
	Steps             []JourneyStep     `json:"steps" yaml:"steps"`
	Enabled           bool              `json:"enabled" yaml:"enabled"`
	CreatedAt         time.Time         `json:"created_at" yaml:"-"`
	UpdatedAt         time.Time         `json:"updated_at" yaml:"-"`
}

// JourneyStep represents a single step in a journey
type JourneyStep struct {
	Name       string                    `json:"name" yaml:"name"`
	Type       CheckType                 `json:"type" yaml:"type"`     // http, tcp, udp, dns, etc.
	Target     string                    `json:"target" yaml:"target"` // can use ${variable}
	Timeout    Duration                  `json:"timeout" yaml:"timeout"`
	HTTP       *HTTPConfig               `json:"http,omitempty" yaml:"http,omitempty"`
	TCP        *TCPConfig                `json:"tcp,omitempty" yaml:"tcp,omitempty"`
	UDP        *UDPConfig                `json:"udp,omitempty" yaml:"udp,omitempty"`
	DNS        *DNSConfig                `json:"dns,omitempty" yaml:"dns,omitempty"`
	TLS        *TLSConfig                `json:"tls,omitempty" yaml:"tls,omitempty"`
	Extract    map[string]ExtractionRule `json:"extract" yaml:"extract"`
	Assertions []Assertion               `json:"assertions,omitempty" yaml:"assertions,omitempty"`
}

// Assertion defines an assertion to verify during a step
type Assertion struct {
	Type     string `json:"type" yaml:"type"`                           // status_code, body_contains, json_path, header, response_time, etc.
	Target   string `json:"target,omitempty" yaml:"target,omitempty"`   // JSON path, header name, etc.
	Operator string `json:"operator" yaml:"operator"`                   // equals, not_equals, contains, greater_than, less_than, regex
	Expected string `json:"expected" yaml:"expected"`                   // expected value
	Message  string `json:"message,omitempty" yaml:"message,omitempty"` // custom error message
}

// ExtractionRule defines how to extract a variable from a response
type ExtractionRule struct {
	From  string `json:"from" yaml:"from"`   // body, header, cookie
	Path  string `json:"path" yaml:"path"`   // JSON path for body, header name for header
	Regex string `json:"regex" yaml:"regex"` // regex to extract (optional)
}

// UnmarshalYAML accepts the key names documented for config files
// (docs/CONFIGURATION.md "Extraction Rule", configs/anubis.example.yaml) in
// addition to the canonical "path": json_path for a body, header_name for a
// header and cookie_name for a cookie. YAML drops unknown keys silently, so a
// rule written exactly as documented otherwise loaded with an empty Path and
// extracted nothing. An explicit "path" always wins. JSON, used by the REST
// API, is unchanged.
func (r *ExtractionRule) UnmarshalYAML(node *yaml.Node) error {
	var raw struct {
		From       string `yaml:"from"`
		Path       string `yaml:"path"`
		Regex      string `yaml:"regex"`
		JSONPath   string `yaml:"json_path"`
		HeaderName string `yaml:"header_name"`
		CookieName string `yaml:"cookie_name"`
	}
	if err := node.Decode(&raw); err != nil {
		return err
	}
	path := raw.Path
	if path == "" {
		switch raw.From {
		case "body":
			path = raw.JSONPath
		case "header":
			path = raw.HeaderName
		case "cookie":
			path = raw.CookieName
		}
	}
	*r = ExtractionRule{From: raw.From, Path: path, Regex: raw.Regex}
	return nil
}

// JourneyRun represents the result of executing a journey
type JourneyRun struct {
	ID          string              `json:"id"`
	JourneyID   string              `json:"journey_id"`
	WorkspaceID string              `json:"workspace_id"`
	JackalID    string              `json:"jackal_id"`
	Region      string              `json:"region"`
	StartedAt   int64               `json:"started_at"` // Unix timestamp (ms)
	CompletedAt int64               `json:"completed_at"`
	Duration    int64               `json:"duration"` // Total duration in ms
	Status      SoulStatus          `json:"status"`
	Steps       []JourneyStepResult `json:"steps"`
	Variables   map[string]string   `json:"variables"` // Captured variables
}

// JourneyStepResult represents the result of a single step
type JourneyStepResult struct {
	Name      string            `json:"name"`
	StepIndex int               `json:"step_index"`
	Status    SoulStatus        `json:"status"`
	Duration  int64             `json:"duration"` // ms
	Message   string            `json:"message"`
	Extracted map[string]string `json:"extracted,omitempty"` // Variables extracted from this step
}
