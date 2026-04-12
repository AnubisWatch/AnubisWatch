package release

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Generator handles release artifact generation
type Generator struct {
	version   string
	commit    string
	outputDir string
	logger    *slog.Logger
}

// NewGenerator creates a new release generator
func NewGenerator(version, commit, outputDir string, logger *slog.Logger) *Generator {
	return &Generator{
		version:   version,
		commit:    commit,
		outputDir: outputDir,
		logger:    logger,
	}
}

// GenerateChecksums generates SHA256 checksums for all artifacts
func (g *Generator) GenerateChecksums(artifactsDir string) error {
	g.logger.Info("Generating checksums", "dir", artifactsDir)

	artifacts, err := g.findArtifacts(artifactsDir)
	if err != nil {
		return fmt.Errorf("failed to find artifacts: %w", err)
	}

	checksumFile := filepath.Join(g.outputDir, "checksums.txt")
	f, err := os.Create(checksumFile)
	if err != nil {
		return fmt.Errorf("failed to create checksum file: %w", err)
	}
	defer f.Close()

	for _, artifact := range artifacts {
		checksum, err := g.calculateChecksum(artifact)
		if err != nil {
			g.logger.Warn("Failed to calculate checksum", "file", artifact, "error", err)
			continue
		}

		filename := filepath.Base(artifact)
		fmt.Fprintf(f, "%s  %s\n", checksum, filename)
		g.logger.Debug("Calculated checksum", "file", filename, "checksum", checksum[:16])
	}

	g.logger.Info("Checksums written", "file", checksumFile, "count", len(artifacts))
	return nil
}

// GenerateReleaseNotes generates release notes from git history
func (g *Generator) GenerateReleaseNotes(since string) (string, error) {
	g.logger.Info("Generating release notes", "since", since)

	notes := &ReleaseNotes{
		Version:  g.version,
		Commit:   g.commit,
		Date:     time.Now().UTC(),
		Sections: make(map[string][]Change),
	}

	// Parse git log
	changes, err := g.parseGitLog(since)
	if err != nil {
		g.logger.Warn("Failed to parse git log, using empty changelog", "error", err)
	}

	// Categorize changes
	for _, change := range changes {
		category := g.categorizeChange(change)
		notes.Sections[category] = append(notes.Sections[category], change)
	}

	// Generate markdown
	var buf bytes.Buffer
	g.writeReleaseNotes(&buf, notes)

	return buf.String(), nil
}

// GenerateChangelog updates the CHANGELOG.md file
func (g *Generator) GenerateChangelog(changes []Change) error {
	changelogPath := filepath.Join(g.outputDir, "CHANGELOG.md")

	// Read existing changelog
	existingContent := ""
	if data, err := os.ReadFile(changelogPath); err == nil {
		existingContent = string(data)
	}

	// Generate new entry
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("## [%s] - %s\n\n", g.version, time.Now().UTC().Format("2006-01-02")))

	// Group by category
	byCategory := make(map[string][]Change)
	for _, c := range changes {
		cat := g.categorizeChange(c)
		byCategory[cat] = append(byCategory[cat], c)
	}

	// Write categories
	categories := []string{"Added", "Changed", "Deprecated", "Removed", "Fixed", "Security"}
	for _, cat := range categories {
		if changes, ok := byCategory[cat]; ok && len(changes) > 0 {
			buf.WriteString(fmt.Sprintf("### %s\n", cat))
			for _, c := range changes {
				buf.WriteString(fmt.Sprintf("- %s", c.Message))
				if c.Commit != "" {
					buf.WriteString(fmt.Sprintf(" (%s)", c.Commit[:7]))
				}
				buf.WriteString("\n")
			}
			buf.WriteString("\n")
		}
	}

	// Combine with existing content
	var output bytes.Buffer
	output.WriteString("# Changelog\n\n")
	output.WriteString("All notable changes to this project will be documented in this file.\n\n")
	output.WriteString("The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),\n")
	output.WriteString("and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).\n\n")
	output.Write(buf.Bytes())
	output.WriteString(existingContent)

	// Write to file
	if err := os.WriteFile(changelogPath, output.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write changelog: %w", err)
	}

	g.logger.Info("Changelog updated", "file", changelogPath)
	return nil
}

// GenerateDockerManifest generates Docker image manifest
func (g *Generator) GenerateDockerManifest(images []string) error {
	g.logger.Info("Generating Docker manifest", "images", len(images))

	// For now, just log the images that would be included
	for _, image := range images {
		g.logger.Debug("Including image in manifest", "image", image)
	}

	return nil
}

// GenerateInstallScript generates installation script
func (g *Generator) GenerateInstallScript() error {
	script := `#!/bin/bash
# AnubisWatch Installation Script
# ═══════════════════════════════════════════════════════════

set -e

BINARY_NAME="anubis"
INSTALL_DIR="/usr/local/bin"
VERSION="{{VERSION}}"

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case $ARCH in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    armv7l) ARCH="armv7" ;;
    *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

case $OS in
    linux|darwin|freebsd) ;;
    mingw*|cygwin*|msys*) OS="windows"; BINARY_NAME="anubis.exe" ;;
    *) echo "Unsupported OS: $OS"; exit 1 ;;
esac

echo "⚖️  Installing AnubisWatch ${VERSION} for ${OS}/${ARCH}..."

# Download URL
DOWNLOAD_URL="https://github.com/AnubisWatch/anubiswatch/releases/download/${VERSION}/${BINARY_NAME}-${OS}-${ARCH}"

if [ "$OS" = "windows" ]; then
    DOWNLOAD_URL="${DOWNLOAD_URL}.exe"
fi

# Create temp directory
TMP_DIR=$(mktemp -d)
trap "rm -rf $TMP_DIR" EXIT

# Download binary
echo "📥 Downloading from ${DOWNLOAD_URL}..."
if command -v curl &> /dev/null; then
    curl -fsSL -o "${TMP_DIR}/${BINARY_NAME}" "$DOWNLOAD_URL"
elif command -v wget &> /dev/null; then
    wget -q -O "${TMP_DIR}/${BINARY_NAME}" "$DOWNLOAD_URL"
else
    echo "Error: curl or wget is required"
    exit 1
fi

# Make executable
chmod +x "${TMP_DIR}/${BINARY_NAME}"

# Install
if [ "$OS" = "windows" ]; then
    echo "Please manually move ${BINARY_NAME} to your PATH"
    echo "Location: ${TMP_DIR}/${BINARY_NAME}"
else
    echo "📦 Installing to ${INSTALL_DIR}..."
    sudo mv "${TMP_DIR}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
    echo "✓ Installed successfully!"
    echo ""
    echo "Run 'anubis version' to verify the installation"
    echo "Run 'anubis init' to create a default configuration"
    echo "Run 'anubis serve' to start the server"
fi
`

	script = strings.ReplaceAll(script, "{{VERSION}}", g.version)

	scriptPath := filepath.Join(g.outputDir, "install.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return fmt.Errorf("failed to write install script: %w", err)
	}

	g.logger.Info("Install script generated", "file", scriptPath)
	return nil
}

// GeneratePackageScripts generates package installation scripts
func (g *Generator) GeneratePackageScripts() error {
	// Systemd service file
	service := `[Unit]
Description=AnubisWatch - The Judgment Never Sleeps
Documentation=https://github.com/AnubisWatch/anubiswatch
After=network.target

[Service]
Type=simple
User=anubis
Group=anubis
ExecStart=/usr/bin/anubis serve --config /etc/anubis/anubis.yaml
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=anubis

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/anubis

[Install]
WantedBy=multi-user.target
`

	servicePath := filepath.Join(g.outputDir, "anubis.service")
	if err := os.WriteFile(servicePath, []byte(service), 0644); err != nil {
		return fmt.Errorf("failed to write service file: %w", err)
	}

	// Post-install script
	postinst := `#!/bin/bash
set -e

# Create user if doesn't exist
if ! id -u anubis &>/dev/null; then
    useradd -r -s /bin/false -d /var/lib/anubis -c "AnubisWatch" anubis
fi

# Create directories
mkdir -p /var/lib/anubis
mkdir -p /etc/anubis
chown anubis:anubis /var/lib/anubis

# Enable and start service
if command -v systemctl &> /dev/null; then
    systemctl daemon-reload
    systemctl enable anubis
fi

echo "AnubisWatch has been installed!"
echo "Run 'sudo systemctl start anubis' to start the service"
`

	postinstPath := filepath.Join(g.outputDir, "postinstall.sh")
	if err := os.WriteFile(postinstPath, []byte(postinst), 0755); err != nil {
		return fmt.Errorf("failed to write postinstall script: %w", err)
	}

	g.logger.Info("Package scripts generated")
	return nil
}

// findArtifacts finds all release artifacts
func (g *Generator) findArtifacts(dir string) ([]string, error) {
	var artifacts []string

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		// Skip checksum file itself
		if name == "checksums.txt" {
			continue
		}

		// Include binaries, archives, and packages
		if g.isArtifact(name) {
			artifacts = append(artifacts, filepath.Join(dir, name))
		}
	}

	sort.Strings(artifacts)
	return artifacts, nil
}

// isArtifact checks if a file is a release artifact
func (g *Generator) isArtifact(name string) bool {
	patterns := []string{
		`^anubis-`,
		`\.tar\.gz$`,
		`\.zip$`,
		`\.deb$`,
		`\.rpm$`,
		`\.pkg$`,
	}

	for _, pattern := range patterns {
		if matched, _ := regexp.MatchString(pattern, name); matched {
			return true
		}
	}

	return false
}

// calculateChecksum calculates SHA256 checksum of a file
func (g *Generator) calculateChecksum(filepath string) (string, error) {
	f, err := os.Open(filepath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// parseGitLog parses git log for changes
func (g *Generator) parseGitLog(since string) ([]Change, error) {
	// This would normally call git log
	// For now, return empty list
	return []Change{}, nil
}

// categorizeChange categorizes a change by type
func (g *Generator) categorizeChange(change Change) string {
	msg := strings.ToLower(change.Message)

	if strings.HasPrefix(msg, "feat:") || strings.HasPrefix(msg, "add") {
		return "Added"
	}
	if strings.HasPrefix(msg, "fix:") || strings.HasPrefix(msg, "bugfix") {
		return "Fixed"
	}
	if strings.HasPrefix(msg, "change:") || strings.HasPrefix(msg, "update") {
		return "Changed"
	}
	if strings.HasPrefix(msg, "deprecate:") {
		return "Deprecated"
	}
	if strings.HasPrefix(msg, "remove:") || strings.HasPrefix(msg, "delete") {
		return "Removed"
	}
	if strings.HasPrefix(msg, "security:") || strings.Contains(msg, "cve") {
		return "Security"
	}

	return "Changed"
}

// writeReleaseNotes writes release notes to buffer
func (g *Generator) writeReleaseNotes(w io.Writer, notes *ReleaseNotes) {
	fmt.Fprintf(w, "# AnubisWatch %s\n\n", notes.Version)
	fmt.Fprintf(w, "⚖️ **The Judgment Never Sleeps**\n\n")
	fmt.Fprintf(w, "**Release Date:** %s\n", notes.Date.Format("2006-01-02"))
	if notes.Commit != "" {
		fmt.Fprintf(w, "**Commit:** %s\n\n", notes.Commit[:min(7, len(notes.Commit))])
	} else {
		fmt.Fprintln(w)
	}

	// Write sections
	order := []string{"Added", "Changed", "Fixed", "Security", "Deprecated", "Removed"}
	for _, category := range order {
		if changes, ok := notes.Sections[category]; ok && len(changes) > 0 {
			fmt.Fprintf(w, "## %s\n", category)
			for _, change := range changes {
				fmt.Fprintf(w, "- %s", change.Message)
				if change.Commit != "" {
					commitLen := min(7, len(change.Commit))
					fmt.Fprintf(w, " (%s)", change.Commit[:commitLen])
				}
				fmt.Fprintln(w)
			}
			fmt.Fprintln(w)
		}
	}

	// Installation section
	fmt.Fprintln(w, "## Installation")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "### Binary")
	fmt.Fprintln(w, "Download the appropriate binary for your platform and extract it.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "### Docker")
	fmt.Fprintln(w, "```bash")
	fmt.Fprintf(w, "docker run -d -p 8443:8443 anubiswatch/anubis:%s\n", notes.Version)
	fmt.Fprintln(w, "```")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "### Quick Install")
	fmt.Fprintln(w, "```bash")
	fmt.Fprintln(w, "curl -fsSL https://get.anubis.watch | bash")
	fmt.Fprintln(w, "```")
}

// Change represents a single change
type Change struct {
	Type     string
	Scope    string
	Message  string
	Commit   string
	Author   string
	Date     time.Time
	Breaking bool
}

// ReleaseNotes contains all release note data
type ReleaseNotes struct {
	Version  string
	Commit   string
	Date     time.Time
	Sections map[string][]Change
}
