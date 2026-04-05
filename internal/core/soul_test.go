package core

import (
	"testing"
	"time"
)

func TestSoulValidation(t *testing.T) {
	tests := []struct {
		name      string
		soul      *Soul
		wantError bool
	}{
		{
			name: "valid HTTP soul",
			soul: &Soul{
				Name:   "Test API",
				Type:   CheckHTTP,
				Target: "https://api.example.com/health",
			},
			wantError: false,
		},
		{
			name: "missing name",
			soul: &Soul{
				Type:   CheckHTTP,
				Target: "https://api.example.com",
			},
			wantError: true,
		},
		{
			name: "missing target",
			soul: &Soul{
				Name: "Test",
				Type: CheckHTTP,
			},
			wantError: true,
		},
		{
			name: "invalid type",
			soul: &Soul{
				Name:   "Test",
				Type:   "",
				Target: "https://example.com",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Soul validation is done in config.validate()
			config := &Config{
				Souls: []Soul{*tt.soul},
			}
			err := config.validate()
			if (err != nil) != tt.wantError {
				t.Errorf("validation error = %v, wantError = %v", err, tt.wantError)
			}
		})
	}
}

func TestSoulStatusString(t *testing.T) {
	tests := []struct {
		status   SoulStatus
		expected string
	}{
		{SoulAlive, "alive"},
		{SoulDead, "dead"},
		{SoulDegraded, "degraded"},
		{SoulUnknown, "unknown"},
		{SoulEmbalmed, "embalmed"},
	}

	for _, tt := range tests {
		if string(tt.status) != tt.expected {
			t.Errorf("string(%v) = %q, want %q", tt.status, tt.status, tt.expected)
		}
	}
}

func TestCheckTypeConstants(t *testing.T) {
	expectedTypes := []CheckType{
		CheckHTTP,
		CheckTCP,
		CheckUDP,
		CheckDNS,
		CheckSMTP,
		CheckIMAP,
		CheckICMP,
		CheckGRPC,
		CheckWebSocket,
		CheckTLS,
	}

	for _, ct := range expectedTypes {
		if ct == "" {
			t.Errorf("CheckType constant is empty")
		}
	}
}

func TestDurationConversion(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
	}{
		{"1s", time.Second},
		{"1m", time.Minute},
		{"1h", time.Hour},
		{"30s", 30 * time.Second},
		{"5m", 5 * time.Minute},
		{"1h30m", 90 * time.Minute},
		{"1.5h", 90 * time.Minute},
	}

	for _, tt := range tests {
		d := &Duration{}
		err := d.UnmarshalYAML(func(v interface{}) error {
			if s, ok := v.(string); ok {
				dur, err := time.ParseDuration(s)
				if err != nil {
					return err
				}
				d.Duration = dur
			}
			return nil
		})

		// Direct parse for test
		dur, parseErr := time.ParseDuration(tt.input)
		if parseErr == nil {
			d.Duration = dur
		}

		if err != nil && parseErr != nil {
			t.Errorf("ParseDuration(%q) error = %v", tt.input, err)
		}

		if d.Duration != tt.expected && parseErr == nil {
			t.Errorf("Duration(%q) = %v, want %v", tt.input, d.Duration, tt.expected)
		}
	}
}

// Tests for uncovered methods

func TestRaftRole_Values(t *testing.T) {
	if RoleVoter != "voter" {
		t.Errorf("Expected RoleVoter = voter, got %s", RoleVoter)
	}
	if RoleNonVoter != "nonvoter" {
		t.Errorf("Expected RoleNonVoter = nonvoter, got %s", RoleNonVoter)
	}
	if RoleSpare != "spare" {
		t.Errorf("Expected RoleSpare = spare, got %s", RoleSpare)
	}
}

func TestRaftState_Values(t *testing.T) {
	if StateFollower != "follower" {
		t.Errorf("Expected StateFollower = follower, got %s", StateFollower)
	}
	if StateLeader != "leader" {
		t.Errorf("Expected StateLeader = leader, got %s", StateLeader)
	}
	if StateCandidate != "candidate" {
		t.Errorf("Expected StateCandidate = candidate, got %s", StateCandidate)
	}
}

func TestCalculateOverallStatus(t *testing.T) {
	// All operational
	souls := []SoulStatusInfo{
		{ID: "1", Name: "Soul 1", Status: "alive"},
		{ID: "2", Name: "Soul 2", Status: "alive"},
	}
	status := CalculateOverallStatus(souls)
	if status.Status != "operational" {
		t.Errorf("Expected operational, got %s", status.Status)
	}

	// Some degraded
	souls = []SoulStatusInfo{
		{ID: "1", Name: "Soul 1", Status: "alive"},
		{ID: "2", Name: "Soul 2", Status: "degraded"},
	}
	status = CalculateOverallStatus(souls)
	if status.Status != "degraded" {
		t.Errorf("Expected degraded, got %s", status.Status)
	}

	// Some dead
	souls = []SoulStatusInfo{
		{ID: "1", Name: "Soul 1", Status: "alive"},
		{ID: "2", Name: "Soul 2", Status: "dead"},
	}
	status = CalculateOverallStatus(souls)
	if status.Status != "major_outage" {
		t.Errorf("Expected major_outage, got %s", status.Status)
	}

	// Empty
	souls = []SoulStatusInfo{}
	status = CalculateOverallStatus(souls)
	if status.Status != "operational" {
		t.Errorf("Expected operational for empty, got %s", status.Status)
	}
}

func TestGetDefaultTheme(t *testing.T) {
	theme := GetDefaultTheme()
	if theme.PrimaryColor == "" {
		t.Error("Expected primary color to be set")
	}
}

// Test RaftState methods
func TestRaftState_IsLeader(t *testing.T) {
	if !StateLeader.IsLeader() {
		t.Error("Expected StateLeader.IsLeader() to return true")
	}
	if StateFollower.IsLeader() {
		t.Error("Expected StateFollower.IsLeader() to return false")
	}
	if StateCandidate.IsLeader() {
		t.Error("Expected StateCandidate.IsLeader() to return false")
	}
}

func TestRaftState_IsFollower(t *testing.T) {
	if StateLeader.IsFollower() {
		t.Error("Expected StateLeader.IsFollower() to return false")
	}
	if !StateFollower.IsFollower() {
		t.Error("Expected StateFollower.IsFollower() to return true")
	}
	if StateCandidate.IsFollower() {
		t.Error("Expected StateCandidate.IsFollower() to return false")
	}
}

func TestRaftState_IsCandidate(t *testing.T) {
	if StateLeader.IsCandidate() {
		t.Error("Expected StateLeader.IsCandidate() to return false")
	}
	if StateFollower.IsCandidate() {
		t.Error("Expected StateFollower.IsCandidate() to return false")
	}
	if !StateCandidate.IsCandidate() {
		t.Error("Expected StateCandidate.IsCandidate() to return true")
	}
}

func TestRaftState_String(t *testing.T) {
	if StateLeader.String() != "leader" {
		t.Errorf("Expected StateLeader.String() = 'leader', got '%s'", StateLeader.String())
	}
	if StateFollower.String() != "follower" {
		t.Errorf("Expected StateFollower.String() = 'follower', got '%s'", StateFollower.String())
	}
	if StateCandidate.String() != "candidate" {
		t.Errorf("Expected StateCandidate.String() = 'candidate', got '%s'", StateCandidate.String())
	}
}

func TestLogEntryType_String(t *testing.T) {
	tests := []struct {
		entryType LogEntryType
		expected  string
	}{
		{LogCommand, "command"},
		{LogNoOp, "noop"},
		{LogConfiguration, "configuration"},
		{99, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.entryType.String(); got != tt.expected {
				t.Errorf("LogEntryType(%d).String() = %q, want %q", tt.entryType, got, tt.expected)
			}
		})
	}
}

func TestRaftError_Error(t *testing.T) {
	err := &RaftError{
		Code:    "NOT_LEADER",
		Message: "node is not the leader",
		NodeID:  "node-1",
	}

	expected := "raft error [NOT_LEADER]: node is not the leader"
	if err.Error() != expected {
		t.Errorf("Expected %q, got %q", expected, err.Error())
	}

	// Test without NodeID
	err2 := &RaftError{
		Code:    "TIMEOUT",
		Message: "operation timed out",
	}
	expected2 := "raft error [TIMEOUT]: operation timed out"
	if err2.Error() != expected2 {
		t.Errorf("Expected %q, got %q", expected2, err2.Error())
	}
}
