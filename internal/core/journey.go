package core

// JourneyConfig defines a multi-step synthetic check (Duat Journey)
type JourneyConfig struct {
	Name              string            `json:"name" yaml:"name"`
	ID                string            `json:"id" yaml:"id"`
	WorkspaceID       string            `json:"workspace_id" yaml:"-"`
	Weight            Duration          `json:"weight" yaml:"weight"`   // check interval
	Timeout           Duration          `json:"timeout" yaml:"timeout"` // total journey timeout
	ContinueOnFailure bool              `json:"continue_on_failure" yaml:"continue_on_failure"`
	Variables         map[string]string `json:"variables" yaml:"variables"` // default variables
	Steps             []JourneyStep     `json:"steps" yaml:"steps"`
	Enabled           bool              `json:"enabled" yaml:"enabled"`
}

// JourneyStep represents a single step in a journey
type JourneyStep struct {
	Name    string                    `json:"name" yaml:"name"`
	Type    CheckType                 `json:"type" yaml:"type"`     // http, tcp, udp, dns, etc.
	Target  string                    `json:"target" yaml:"target"` // can use ${variable}
	Timeout Duration                  `json:"timeout" yaml:"timeout"`
	HTTP    *HTTPConfig               `json:"http,omitempty" yaml:"http,omitempty"`
	TCP     *TCPConfig                `json:"tcp,omitempty" yaml:"tcp,omitempty"`
	UDP     *UDPConfig                `json:"udp,omitempty" yaml:"udp,omitempty"`
	DNS     *DNSConfig                `json:"dns,omitempty" yaml:"dns,omitempty"`
	TLS     *TLSConfig                `json:"tls,omitempty" yaml:"tls,omitempty"`
	Extract map[string]ExtractionRule `json:"extract" yaml:"extract"`
}

// ExtractionRule defines how to extract a variable from a response
type ExtractionRule struct {
	From  string `json:"from" yaml:"from"`   // body, header, cookie
	Path  string `json:"path" yaml:"path"`   // JSON path for body, header name for header
	Regex string `json:"regex" yaml:"regex"` // regex to extract (optional)
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
