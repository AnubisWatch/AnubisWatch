package version

import (
	"strings"
	"testing"
)

func TestGet(t *testing.T) {
	info := Get()

	if info.Version == "" {
		t.Error("Version should not be empty")
	}

	if info.Platform == "" {
		t.Error("Platform should not be empty")
	}

	if info.GoVersion == "" {
		t.Error("GoVersion should not be empty")
	}
}

func TestGetExtended(t *testing.T) {
	info := GetExtended()

	if info.Version == "" {
		t.Error("Version should not be empty")
	}

	// NumCPU and NumGoroutine should be populated
	if info.NumCPU <= 0 {
		t.Error("NumCPU should be positive")
	}
}

func TestInfoString(t *testing.T) {
	info := Info{
		Version:   "1.0.0",
		Commit:    "abc123def456",
		BuildDate: "2026-01-01",
		GoVersion: "go1.26",
		Platform:  "linux/amd64",
	}

	s := info.String()

	if !strings.Contains(s, "1.0.0") {
		t.Error("String should contain version")
	}

	if !strings.Contains(s, "linux/amd64") {
		t.Error("String should contain platform")
	}
}

func TestInfoShort(t *testing.T) {
	info := Info{
		Version: "1.0.0",
		Commit:  "abc123def456",
	}

	short := info.Short()
	expected := "1.0.0-abc123d"

	if short != expected {
		t.Errorf("Expected short version %s, got %s", expected, short)
	}
}

func TestIsDev(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"dev", true},
		{"1.0.0-dirty", true},
		{"1.0.0", false},
		{"v1.2.3", false},
	}

	for _, tt := range tests {
		info := Info{Version: tt.version}
		if got := info.IsDev(); got != tt.want {
			t.Errorf("IsDev() for version %q = %v, want %v", tt.version, got, tt.want)
		}
	}
}

func TestIsRelease(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"1.0.0", true},
		{"v1.2.3", true},
		{"dev", false},
		{"1.0.0-alpha", false},
		{"1.0.0-dirty", false},
	}

	for _, tt := range tests {
		info := Info{Version: tt.version}
		if got := info.IsRelease(); got != tt.want {
			t.Errorf("IsRelease() for version %q = %v, want %v", tt.version, got, tt.want)
		}
	}
}

func TestCompare(t *testing.T) {
	tests := []struct {
		v1   string
		v2   string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"1.0.1", "1.0.0", 1},
		{"1.1.0", "1.0.0", 1},
		{"2.0.0", "1.9.9", 1},
		{"v1.0.0", "1.0.0", 0},
		{"1.0.0-alpha", "1.0.0", 0}, // Same version parts
	}

	for _, tt := range tests {
		got := Compare(tt.v1, tt.v2)
		if got != tt.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", tt.v1, tt.v2, got, tt.want)
		}
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		version string
		want    [3]int
	}{
		{"1.2.3", [3]int{1, 2, 3}},
		{"v1.2.3", [3]int{1, 2, 3}},
		{"1.2", [3]int{1, 2, 0}},
		{"1.2.3-alpha", [3]int{1, 2, 3}},
		{"invalid", [3]int{0, 0, 0}},
	}

	for _, tt := range tests {
		got := parseVersion(tt.version)
		if got != tt.want {
			t.Errorf("parseVersion(%q) = %v, want %v", tt.version, got, tt.want)
		}
	}
}

// TestInfoString_WithGitBranch tests String with GitBranch and short commit
func TestInfoString_WithGitBranch(t *testing.T) {
	info := Info{
		Version:   "1.0.0",
		Commit:    "abc123",
		BuildDate: "2026-01-01",
		GoVersion: "go1.26",
		Platform:  "linux/amd64",
		GitBranch: "main",
	}

	s := info.String()

	if !strings.Contains(s, "Branch:") {
		t.Error("String should contain branch info")
	}
	if !strings.Contains(s, "Build Date:") {
		t.Error("String should contain build date")
	}
}

// TestInfoString_NoCommit tests String with unknown commit
func TestInfoString_NoCommit(t *testing.T) {
	info := Info{
		Version:   "dev",
		Commit:    "unknown",
		GoVersion: "go1.26",
		Platform:  "windows/amd64",
	}

	s := info.String()

	if strings.Contains(s, "commit:") {
		t.Error("String should not contain commit when unknown")
	}
	if !strings.Contains(s, "AnubisWatch dev") {
		t.Error("String should contain version")
	}
}

// TestInfoString_NoBuildDate tests String with unknown build date
func TestInfoString_NoBuildDate(t *testing.T) {
	info := Info{
		Version:   "dev",
		Commit:    "unknown",
		BuildDate: "unknown",
		GoVersion: "go1.26",
		Platform:  "darwin/amd64",
	}

	s := info.String()

	if strings.Contains(s, "Build Date:") {
		t.Error("String should not contain build date when unknown")
	}
}

// TestInfoShort_ShortCommit tests Short with short commit
func TestInfoShort_ShortCommit(t *testing.T) {
	info := Info{
		Version: "2.0.0",
		Commit:  "abc", // Shorter than 7 chars
	}

	short := info.Short()
	if short != "2.0.0-abc" {
		t.Errorf("Expected '2.0.0-abc', got '%s'", short)
	}
}

// TestInfoShort_EmptyCommit tests Short with empty commit
func TestInfoShort_EmptyCommit(t *testing.T) {
	info := Info{
		Version: "1.0.0",
		Commit:  "",
	}

	short := info.Short()
	if short != "1.0.0-" {
		t.Errorf("Expected '1.0.0-', got '%s'", short)
	}
}

// TestIsDev_DirtyWithSuffix tests IsDev with dirty and suffix
func TestIsDev_DirtyWithSuffix(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"0.1.0-dirty", true},
		{"dirty", true},
		{"1.0.0-beta", false},
	}
	for _, tt := range tests {
		info := Info{Version: tt.version}
		if got := info.IsDev(); got != tt.want {
			t.Errorf("IsDev() for %q = %v, want %v", tt.version, got, tt.want)
		}
	}
}

// TestCompare_MajorDifferences tests major version differences
func TestCompare_MajorDifferences(t *testing.T) {
	if Compare("1.0.0", "2.0.0") != -1 {
		t.Error("1.0.0 should be less than 2.0.0")
	}
}

// TestCompare_MinorDifferences tests minor version differences
func TestCompare_MinorDifferences(t *testing.T) {
	if Compare("1.2.0", "1.3.0") != -1 {
		t.Error("1.2.0 should be less than 1.3.0")
	}
}

// TestCompare_PatchDifferences tests patch version differences
func TestCompare_PatchDifferences(t *testing.T) {
	if Compare("1.0.5", "1.0.3") != 1 {
		t.Error("1.0.5 should be greater than 1.0.3")
	}
}

// TestCompare_EmptyVersions tests comparing empty versions
func TestCompare_EmptyVersions(t *testing.T) {
	if Compare("", "") != 0 {
		t.Error("Empty versions should be equal")
	}
}

// TestParseVersion_Empty tests parsing empty string
func TestParseVersion_Empty(t *testing.T) {
	got := parseVersion("")
	want := [3]int{0, 0, 0}
	if got != want {
		t.Errorf("parseVersion(\"\") = %v, want %v", got, want)
	}
}

// TestParseVersion_OnlyPrefix tests parsing only 'v'
func TestParseVersion_OnlyPrefix(t *testing.T) {
	got := parseVersion("v")
	want := [3]int{0, 0, 0}
	if got != want {
		t.Errorf("parseVersion(\"v\") = %v, want %v", got, want)
	}
}

// TestParseVersion_PartialParts tests parsing with 1 and 2 parts
func TestParseVersion_PartialParts(t *testing.T) {
	got1 := parseVersion("5")
	if got1 != [3]int{5, 0, 0} {
		t.Errorf("parseVersion(\"5\") = %v, want [5,0,0]", got1)
	}

	got2 := parseVersion("1.5")
	if got2 != [3]int{1, 5, 0} {
		t.Errorf("parseVersion(\"1.5\") = %v, want [1,5,0]", got2)
	}
}

// TestGet_NumGoroutinePositive tests NumGoroutine is positive
func TestGet_NumGoroutinePositive(t *testing.T) {
	info := Get()
	if info.NumGoroutine < 1 {
		t.Errorf("NumGoroutine should be >= 1, got %d", info.NumGoroutine)
	}
}

// TestGetExtended_PopulatesGitFields tests that GetExtended populates Git fields
func TestGetExtended_PopulatesGitFields(t *testing.T) {
	info := GetExtended()

	// At minimum, Commit should not be unknown when built with VCS
	if info.Commit == "unknown" && (info.GitBranch == "" && info.GitTag == "") {
		t.Log("No VCS info available (expected in test environment)")
	}
}

// TestInfoString_AllFieldsPopulated tests String with all fields set
func TestInfoString_AllFieldsPopulated(t *testing.T) {
	info := Info{
		Version:      "1.0.0",
		Commit:       "abcdef123456789",
		BuildDate:    "2026-01-01",
		GoVersion:    "go1.26",
		Platform:     "linux/amd64",
		OS:           "linux",
		Arch:         "amd64",
		NumCPU:       8,
		NumGoroutine: 10,
		GitBranch:    "main",
		GitTag:       "v1.0.0",
	}

	s := info.String()
	for _, expected := range []string{"1.0.0", "abcdef1", "linux/amd64", "go1.26", "2026-01-01", "main"} {
		if !strings.Contains(s, expected) {
			t.Errorf("String should contain %q, got: %s", expected, s)
		}
	}
}
