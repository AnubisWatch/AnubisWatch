package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

// Version information - set at build time via ldflags
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
	GoVersion = runtime.Version()
)

// Info holds complete version information
type Info struct {
	Version      string `json:"version"`
	Commit       string `json:"commit"`
	BuildDate    string `json:"build_date"`
	GoVersion    string `json:"go_version"`
	Platform     string `json:"platform"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	NumCPU       int    `json:"num_cpu"`
	NumGoroutine int    `json:"num_goroutine"`
	GitBranch    string `json:"git_branch,omitempty"`
	GitTag       string `json:"git_tag,omitempty"`
}

// Get returns version information
func Get() Info {
	return Info{
		Version:      Version,
		Commit:       Commit,
		BuildDate:    BuildDate,
		GoVersion:    GoVersion,
		Platform:     fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		NumCPU:       runtime.NumCPU(),
		NumGoroutine: runtime.NumGoroutine(),
	}
}

// GetExtended returns version information with git details from debug info
func GetExtended() Info {
	info := Get()

	// Try to get additional info from debug build info
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range bi.Settings {
			switch setting.Key {
			case "vcs.revision":
				if info.Commit == "unknown" {
					info.Commit = setting.Value
				}
			case "vcs.time":
				if info.BuildDate == "unknown" {
					info.BuildDate = setting.Value
				}
			case "vcs.branch":
				info.GitBranch = setting.Value
			case "vcs.tag":
				info.GitTag = setting.Value
			}
		}
	}

	return info
}

// String returns a formatted version string
func (i Info) String() string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("AnubisWatch %s", i.Version))
	if i.Commit != "unknown" {
		b.WriteString(fmt.Sprintf(" (commit: %s)", i.Commit[:min(7, len(i.Commit))]))
	}
	b.WriteString(fmt.Sprintf("\n  Platform:    %s", i.Platform))
	b.WriteString(fmt.Sprintf("\n  Go Version:  %s", i.GoVersion))
	if i.BuildDate != "unknown" {
		b.WriteString(fmt.Sprintf("\n  Build Date:  %s", i.BuildDate))
	}
	if i.GitBranch != "" {
		b.WriteString(fmt.Sprintf("\n  Branch:      %s", i.GitBranch))
	}

	return b.String()
}

// Short returns a short version string
func (i Info) Short() string {
	return fmt.Sprintf("%s-%s", i.Version, i.Commit[:min(7, len(i.Commit))])
}

// IsDev returns true if this is a development build
func (i Info) IsDev() bool {
	return i.Version == "dev" || strings.Contains(i.Version, "dirty")
}

// IsRelease returns true if this is a release build
func (i Info) IsRelease() bool {
	return !i.IsDev() && !strings.Contains(i.Version, "-")
}

// Compare compares two version strings
// Returns -1 if v1 < v2, 0 if v1 == v2, 1 if v1 > v2
func Compare(v1, v2 string) int {
	// Simple semver comparison
	v1Parts := parseVersion(v1)
	v2Parts := parseVersion(v2)

	for i := 0; i < 3; i++ {
		if v1Parts[i] < v2Parts[i] {
			return -1
		}
		if v1Parts[i] > v2Parts[i] {
			return 1
		}
	}

	return 0
}

// parseVersion parses a version string into [major, minor, patch]
func parseVersion(v string) [3]int {
	// Remove 'v' prefix if present
	v = strings.TrimPrefix(v, "v")

	// Split by '.'
	parts := strings.Split(v, ".")

	var result [3]int
	for i := 0; i < 3 && i < len(parts); i++ {
		// Extract numeric part only
		var num int
		for _, c := range parts[i] {
			if c >= '0' && c <= '9' {
				num = num*10 + int(c-'0')
			} else {
				break
			}
		}
		result[i] = num
	}

	return result
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
