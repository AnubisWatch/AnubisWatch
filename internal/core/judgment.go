package core

import "time"

// Judgment is the result of weighing a soul — a single check execution.
type Judgment struct {
	ID         string        `json:"id"`
	SoulID     string        `json:"soul_id"`
	JackalID   string        `json:"jackal_id"`      // which probe node
	Region     string        `json:"region"`
	Timestamp  time.Time     `json:"timestamp"`
	Duration   time.Duration `json:"duration"`        // check latency
	Status     SoulStatus    `json:"status"`
	StatusCode int           `json:"status_code"`     // protocol-specific
	Message    string        `json:"message"`
	Details    *JudgmentDetails `json:"details,omitempty"`
	TLSInfo    *TLSInfo      `json:"tls_info,omitempty"`
}

// JudgmentDetails holds protocol-specific result data
type JudgmentDetails struct {
	// HTTP
	ResponseHeaders map[string]string `json:"response_headers,omitempty"`
	ResponseBody    string            `json:"response_body,omitempty"`
	RedirectChain   []string          `json:"redirect_chain,omitempty"`

	// DNS
	ResolvedAddresses []string        `json:"resolved_addresses,omitempty"`
	DNSSECValid       *bool           `json:"dnssec_valid,omitempty"`
	PropagationResult map[string]bool `json:"propagation_result,omitempty"`

	// ICMP
	PacketsSent     int     `json:"packets_sent,omitempty"`
	PacketsReceived int     `json:"packets_received,omitempty"`
	PacketLoss      float64 `json:"packet_loss,omitempty"`
	MinLatency      float64 `json:"min_latency_ms,omitempty"`
	AvgLatency      float64 `json:"avg_latency_ms,omitempty"`
	MaxLatency      float64 `json:"max_latency_ms,omitempty"`
	Jitter          float64 `json:"jitter_ms,omitempty"`

	// TCP
	Banner string `json:"banner,omitempty"`

	// SMTP/IMAP
	Capabilities []string `json:"capabilities,omitempty"`

	// gRPC
	ServiceStatus string `json:"service_status,omitempty"`

	// WebSocket
	CloseCode int `json:"close_code,omitempty"`

	// Assertions
	Assertions []AssertionResult `json:"assertions,omitempty"`
}

// AssertionResult records pass/fail of a specific assertion
type AssertionResult struct {
	Type     string `json:"type"`      // status_code, body_contains, json_path, etc.
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Passed   bool   `json:"passed"`
}

// TLSInfo holds TLS/certificate details
type TLSInfo struct {
	Protocol       string    `json:"protocol"`         // TLS 1.2, TLS 1.3
	CipherSuite    string    `json:"cipher_suite"`
	Issuer         string    `json:"issuer"`
	Subject        string    `json:"subject"`
	SANs           []string  `json:"sans"`
	NotBefore      time.Time `json:"not_before"`
	NotAfter       time.Time `json:"not_after"`
	DaysUntilExpiry int      `json:"days_until_expiry"`
	KeyType        string    `json:"key_type"`         // RSA, ECDSA
	KeyBits        int       `json:"key_bits"`
	OCSPStapled    bool      `json:"ocsp_stapled"`
	ChainValid     bool      `json:"chain_valid"`
	ChainLength    int       `json:"chain_length"`
}
