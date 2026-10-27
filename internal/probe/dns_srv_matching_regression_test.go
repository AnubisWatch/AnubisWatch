package probe

import "testing"

// TestParseDNSSRVRecordsFieldPresence pins how an SRV value is split into
// its fields. Each flag must reflect whether the field was actually present
// in the value, so an omitted field is never reported as a parsed zero.
func TestParseDNSSRVRecordsFieldPresence(t *testing.T) {
	for _, tc := range []struct {
		name        string
		value       string
		wantTarget  string
		wantPort    int
		wantHasPort bool
		wantPrio    int
		wantHasPrio bool
		wantWeight  int
		wantHasWt   bool
		wantHasMeta bool
	}{
		{
			name:        "resolve form carries all fields",
			value:       "sip.example.com.:5060 (priority=10, weight=60)",
			wantTarget:  "sip.example.com.",
			wantPort:    5060,
			wantHasPort: true,
			wantPrio:    10,
			wantHasPrio: true,
			wantWeight:  60,
			wantHasWt:   true,
			wantHasMeta: true,
		},
		{
			name:        "short host:port carries only the port",
			value:       "sip.example.com:5060",
			wantTarget:  "sip.example.com",
			wantPort:    5060,
			wantHasPort: true,
		},
		{
			name:        "dig form carries all fields",
			value:       "10 60 5060 sip.example.com.",
			wantTarget:  "sip.example.com.",
			wantPort:    5060,
			wantHasPort: true,
			wantPrio:    10,
			wantHasPrio: true,
			wantWeight:  60,
			wantHasWt:   true,
			wantHasMeta: true,
		},
		{
			name:       "bare target carries no fields",
			value:      "sip.example.com.",
			wantTarget: "sip.example.com.",
		},
		{
			name:        "unparsable port is not reported as present",
			value:       "sip.example.com:port",
			wantTarget:  "sip.example.com",
			wantHasPort: false,
		},
		{
			name:        "dig form with unparsable numbers is not reported as present",
			value:       "x y z sip.example.com.",
			wantTarget:  "sip.example.com.",
			wantHasPort: false,
			wantHasPrio: false,
			wantHasWt:   false,
			wantHasMeta: false,
		},
		{
			name:        "empty value yields no fields",
			value:       "",
			wantTarget:  "",
			wantHasPort: false,
			wantHasPrio: false,
			wantHasWt:   false,
			wantHasMeta: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := parseDNSSRV(tc.value)
			if got.target != tc.wantTarget {
				t.Errorf("target = %q, want %q", got.target, tc.wantTarget)
			}
			if got.port != tc.wantPort || got.hasPort != tc.wantHasPort {
				t.Errorf("port = %d (present=%v), want %d (present=%v)", got.port, got.hasPort, tc.wantPort, tc.wantHasPort)
			}
			if got.priority != tc.wantPrio || got.hasPriority != tc.wantHasPrio {
				t.Errorf("priority = %d (present=%v), want %d (present=%v)", got.priority, got.hasPriority, tc.wantPrio, tc.wantHasPrio)
			}
			if got.weight != tc.wantWeight || got.hasWeight != tc.wantHasWt {
				t.Errorf("weight = %d (present=%v), want %d (present=%v)", got.weight, got.hasWeight, tc.wantWeight, tc.wantHasWt)
			}
			if got.hasMeta != tc.wantHasMeta {
				t.Errorf("hasMeta = %v, want %v", got.hasMeta, tc.wantHasMeta)
			}
		})
	}
}

// TestParseDNSMXRecordsFieldPresence pins how an MX value is split into its
// fields: a bare host carries no preference.
func TestParseDNSMXRecordsFieldPresence(t *testing.T) {
	for _, tc := range []struct {
		name        string
		value       string
		wantHost    string
		wantPref    int
		wantHasPref bool
	}{
		{
			name:        "preference and host",
			value:       "10 mail.example.com.",
			wantHost:    "mail.example.com.",
			wantPref:    10,
			wantHasPref: true,
		},
		{
			name:     "bare host",
			value:    "mail.example.com.",
			wantHost: "mail.example.com.",
		},
		{
			name:     "bare host without trailing dot",
			value:    "mail.example.com",
			wantHost: "mail.example.com",
		},
		{
			name:        "unparsable preference is not reported as present",
			value:       "ten mail.example.com.",
			wantHost:    "mail.example.com.",
			wantHasPref: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := parseDNSMX(tc.value)
			if got.host != tc.wantHost {
				t.Errorf("host = %q, want %q", got.host, tc.wantHost)
			}
			if got.preference != tc.wantPref || got.hasPreference != tc.wantHasPref {
				t.Errorf("preference = %d (present=%v), want %d (present=%v)", got.preference, got.hasPreference, tc.wantPref, tc.wantHasPref)
			}
		})
	}
}

// TestDNSRecordMatchesMXAndSRVNameVariants covers the trailing-dot and case
// variants of the expectation, for both MX and SRV.
func TestDNSRecordMatchesMXAndSRVNameVariants(t *testing.T) {
	for _, tc := range []struct {
		name       string
		recordType string
		actual     string
		expected   string
		want       bool
	}{
		{name: "MX trailing dot", recordType: "MX", actual: "10 mail.example.com.", expected: "mail.example.com", want: true},
		{name: "MX case", recordType: "MX", actual: "10 MAIL.Example.COM.", expected: "mail.example.com", want: true},
		{name: "MX full form with trailing dot", recordType: "MX", actual: "10 mail.example.com.", expected: "10 mail.example.com.", want: true},
		{name: "MX preference differs", recordType: "MX", actual: "10 mail.example.com.", expected: "20 mail.example.com", want: false},
		{name: "MX host differs", recordType: "MX", actual: "10 mail.example.com.", expected: "mail.other.example", want: false},
		{name: "SRV trailing dot", recordType: "SRV", actual: "sip.example.com.:5060 (priority=10, weight=60)", expected: "sip.example.com", want: true},
		{name: "SRV case", recordType: "SRV", actual: "SIP.Example.COM.:5060 (priority=10, weight=60)", expected: "sip.example.com:5060", want: true},
		{name: "SRV dig form with trailing dot", recordType: "SRV", actual: "10 60 5060 sip.example.com.", expected: "10 60 5060 sip.example.com", want: true},
		{name: "SRV target differs", recordType: "SRV", actual: "10 60 5060 sip.example.com.", expected: "sip.other.example:5060", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dnsRecordMatches(tc.recordType, tc.actual, tc.expected); got != tc.want {
				t.Fatalf("dnsRecordMatches(%s, %q, %q) = %v, want %v", tc.recordType, tc.actual, tc.expected, got, tc.want)
			}
		})
	}
}

// TestDNSRecordMatchesSRVComparesOnlyFieldsPresentOnBothSides pins the SRV
// expectation matching rules. A "host:port" expectation carries no priority
// or weight, so it must not be compared against the priority/weight of the
// resolved record — only the fields present on both sides constrain the match.
//
// Before the fix parseDNSSRV flagged every "<target>:<port>" value as
// carrying metadata, so a short expectation such as "sip.example.com:5060"
// was compared against the resolved record's priority=10/weight=60 and
// false-failed every short host:port SRV expectation.
func TestDNSRecordMatchesSRVComparesOnlyFieldsPresentOnBothSides(t *testing.T) {
	for _, tc := range []struct {
		name     string
		actual   string
		expected string
		want     bool
	}{
		{
			name:     "short host:port expectation against resolve form",
			actual:   "sip.example.com.:5060 (priority=10, weight=60)",
			expected: "sip.example.com:5060",
			want:     true,
		},
		{
			name:     "short host:port expectation with different case",
			actual:   "sip.example.com.:5060 (priority=10, weight=60)",
			expected: "SIP.EXAMPLE.COM:5060",
			want:     true,
		},
		{
			name:     "short host:port expectation against dig form",
			actual:   "10 60 5060 sip.example.com.",
			expected: "sip.example.com:5060",
			want:     true,
		},
		{
			name:     "resolve form against short host:port expectation",
			actual:   "sip.example.com:5060",
			expected: "sip.example.com.:5060 (priority=10, weight=60)",
			want:     true,
		},
		{
			name:     "dig form against resolve form",
			actual:   "10 60 5060 sip.example.com.",
			expected: "sip.example.com.:5060 (priority=10, weight=60)",
			want:     true,
		},
		{
			name:     "bare target expectation ignores resolve form metadata",
			actual:   "sip.example.com.:5060 (priority=10, weight=60)",
			expected: "sip.example.com",
			want:     true,
		},
		{
			name:     "both sides fully specified",
			actual:   "sip.example.com.:5060 (priority=10, weight=60)",
			expected: "sip.example.com.:5060 (priority=10, weight=60)",
			want:     true,
		},
		{
			name:     "port differs on both sides",
			actual:   "sip.example.com.:5060 (priority=10, weight=60)",
			expected: "sip.example.com:5061",
			want:     false,
		},
		{
			name:     "port differs against dig form",
			actual:   "10 60 5060 sip.example.com.",
			expected: "sip.example.com:5061",
			want:     false,
		},
		{
			name:     "priority differs on both sides",
			actual:   "sip.example.com.:5060 (priority=10, weight=60)",
			expected: "sip.example.com.:5060 (priority=20, weight=60)",
			want:     false,
		},
		{
			name:     "weight differs on both sides",
			actual:   "10 60 5060 sip.example.com.",
			expected: "10 61 5060 sip.example.com.",
			want:     false,
		},
		{
			name:     "target differs",
			actual:   "sip.example.com.:5060 (priority=10, weight=60)",
			expected: "sip.other.example:5060",
			want:     false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dnsRecordMatches("SRV", tc.actual, tc.expected); got != tc.want {
				t.Fatalf("dnsRecordMatches(SRV, %q, %q) = %v, want %v", tc.actual, tc.expected, got, tc.want)
			}
		})
	}
}
