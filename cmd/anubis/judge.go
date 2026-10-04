package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/storage"
)

type judgeSoul struct {
	core.Soul
	Status    string     `json:"status"`
	LastCheck *time.Time `json:"last_check"`
	Latency   int64      `json:"latency"`
}

func fetchJudgeSouls(apiURL, token string) ([]judgeSoul, error) {
	souls := make([]judgeSoul, 0)
	for offset := 0; ; {
		resp, err := httpGet(fmt.Sprintf("%s/api/v1/souls?offset=%d&limit=100", apiURL, offset), token)
		if err != nil {
			return nil, fmt.Errorf("Error connecting to API: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("Error: API returned status %d", resp.StatusCode)
		}
		var raw json.RawMessage
		err = json.NewDecoder(resp.Body).Decode(&raw)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("Error parsing response: %w", err)
		}
		raw = bytes.TrimSpace(raw)
		// Keep compatibility with older servers that returned a bare array.
		if len(raw) > 0 && (raw[0] == '[' || bytes.Equal(raw, []byte("null"))) {
			var legacy []judgeSoul
			if err := json.Unmarshal(raw, &legacy); err != nil {
				return nil, fmt.Errorf("Error parsing response: %w", err)
			}
			if legacy == nil && offset == 0 {
				return nil, nil
			}
			return append(souls, legacy...), nil
		}
		var page struct {
			Data       *[]judgeSoul `json:"data"`
			Pagination struct {
				HasMore    bool `json:"has_more"`
				NextOffset *int `json:"next_offset"`
			} `json:"pagination"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, fmt.Errorf("Error parsing response: %w", err)
		}
		if page.Data == nil {
			return nil, fmt.Errorf("Error parsing response: missing souls data")
		}
		souls = append(souls, (*page.Data)...)
		if !page.Pagination.HasMore {
			return souls, nil
		}
		if page.Pagination.NextOffset == nil || *page.Pagination.NextOffset <= offset {
			return nil, fmt.Errorf("Error parsing response: invalid next offset")
		}
		offset = *page.Pagination.NextOffset
	}
}

func showJudgments() {
	fmt.Println("⚖️  AnubisWatch — The Judgment Never Sleeps")
	fmt.Println("────────────────────────────────────────────")
	fmt.Println()

	// Try API first
	apiURL := getAPIURL()
	token := getAPIToken()

	var souls []judgeSoul

	if token != "" {
		souls, _ = fetchJudgeSouls(apiURL, token)
	}

	// Fall back to direct storage access
	var store *storage.CobaltDB
	if souls == nil {
		var err error
		store, err = openLocalStorage()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: cannot connect to API or open storage: %v\n", err)
			fmt.Println("\nMake sure AnubisWatch is running, or run from the data directory.")
			os.Exit(1)
		}
		defer store.Close()

		ctx := context.Background()
		storedSouls, err := store.ListSouls(ctx, "default", 0, 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing souls: %v\n", err)
			os.Exit(1)
		}
		for _, soul := range storedSouls {
			souls = append(souls, judgeSoul{Soul: *soul})
		}
	}

	if len(souls) == 0 {
		fmt.Println("No souls configured yet.")
		fmt.Println("Run 'anubis watch <target>' to add your first soul.")
		return
	}

	// Print header
	fmt.Printf("%-24s %-10s %-10s %-12s %-20s\n", "Soul", "Status", "Latency", "Region", "Last Judged")
	fmt.Println("──────────────────────── ────────── ────────── ──────────── ────────────────────")

	// Get latest judgments for each soul
	ctx := context.Background()

	for _, soul := range souls {
		status := "unknown"
		latency := "-"
		region := soul.Region
		if region == "" {
			region = "default"
		}
		lastJudged := "never"
		switch soul.Status {
		case "healthy", "alive":
			status = "alive"
		case "unhealthy", "dead":
			status = "dead"
		case "degraded", "embalmed":
			status = soul.Status
		}
		if soul.LastCheck != nil {
			latency = (time.Duration(soul.Latency) * time.Millisecond).String()
			lastJudged = time.Since(*soul.LastCheck).Round(time.Second).String() + " ago"
		}

		// Try to get latest judgment
		if store != nil {
			j, err := storageGetLatestJudgment(store, ctx, soul.WorkspaceID, soul.ID)
			if err == nil {
				status = string(j.Status)
				latency = j.Duration.String()
				if j.Region != "" {
					region = j.Region
				}
				lastJudged = time.Since(j.Timestamp).Round(time.Second).String() + " ago"
			}
		}

		// Color status (using simple text indicators)
		statusIcon := "○"
		switch status {
		case "alive":
			statusIcon = "✓"
		case "dead":
			statusIcon = "✗"
		case "degraded":
			statusIcon = "~"
		}

		name := truncate(soul.Name, 22)
		fmt.Printf("%-24s %s %-8s %-10s %-12s %-20s\n", name, statusIcon, status, latency, region, lastJudged)
	}

	fmt.Println()
	fmt.Printf("Total souls: %d\n", len(souls))
}

// verdictCommand handles verdict subcommands
func verdictCommand() {
	if len(os.Args) < 3 {
		fmt.Println("⚖️  Verdict Management")
		fmt.Println("────────────────────────────────────────────")
		fmt.Println()
		fmt.Println("Usage: anubis verdict <subcommand> [options]")
		fmt.Println()
		fmt.Println("Subcommands:")
		fmt.Println("  test <channel>    Send test notification to channel")
		fmt.Println("  history           Show recent alert history")
		fmt.Println("  ack <id>          Acknowledge an incident")
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  anubis verdict test ops-slack")
		fmt.Println("  anubis verdict history")
		fmt.Println("  anubis verdict ack alert_123456")
		return
	}

	subcmd := os.Args[2]
	switch subcmd {
	case "test":
		verdictTest()
	case "history":
		verdictHistory()
	case "ack":
		verdictAck()
	default:
		fmt.Fprintf(os.Stderr, "Unknown verdict subcommand: %s\n", subcmd)
		fmt.Println("Run 'anubis verdict' for usage information")
		os.Exit(1)
	}
}

// verdictTest sends a test notification to a channel
func verdictTest() {
	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "Usage: anubis verdict test <channel>\n")
		os.Exit(1)
	}

	channelName := os.Args[3]
	fmt.Printf("⚖️  Sending test notification to channel: %s\n", channelName)

	// Try to connect to local API
	apiURL := getAPIURL()
	token := getAPIToken()

	if token == "" {
		fmt.Println("Note: No API token found. Run 'anubis serve' and login first.")
		fmt.Println()
		fmt.Println("Test notification would be sent to:", channelName)
		fmt.Println("✓ Test prepared (requires running server to send)")
		return
	}

	// Make API request to send test notification
	reqBody := fmt.Sprintf(`{"message": "Test notification from AnubisWatch CLI", "channel": "%s"}`, channelName)

	resp, err := httpPost(apiURL+"/api/v1/channels/"+channelName+"/test", "application/json", []byte(reqBody), token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		fmt.Println("✓ Test notification sent successfully")
	} else {
		fmt.Fprintf(os.Stderr, "Error: Server returned status %d\n", resp.StatusCode)
		os.Exit(1)
	}
}

// verdictHistory shows recent alert history
func verdictHistory() {
	fmt.Println("⚖️  Alert History")
	fmt.Println("────────────────────────────────────────────")
	fmt.Println()

	// Try to connect to local API
	apiURL := getAPIURL()
	token := getAPIToken()

	if token == "" {
		fmt.Println("No API token found. Run 'anubis serve' and login first.")
		fmt.Println()
		fmt.Println("Recent alerts would be displayed here.")
		return
	}

	resp, err := httpGet(apiURL+"/api/v1/incidents", token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching incidents: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Error: Server returned status %d\n", resp.StatusCode)
		os.Exit(1)
	}

	var incidents []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&incidents); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
		os.Exit(1)
	}

	if len(incidents) == 0 {
		fmt.Println("No active incidents.")
		return
	}

	fmt.Printf("%-20s %-12s %-10s %-20s\n", "ID", "Soul", "Severity", "Status")
	fmt.Println("────────────────────────────────────────────────────────────────")
	for _, inc := range incidents {
		id, _ := inc["id"].(string)
		soul, _ := inc["soul_name"].(string)
		severity, _ := inc["severity"].(string)
		status, _ := inc["status"].(string)
		fmt.Printf("%-20s %-12s %-10s %-20s\n", truncate(id, 18), truncate(soul, 12), severity, status)
	}
}

// verdictAck acknowledges an incident
func verdictAck() {
	if len(os.Args) < 4 {
		fmt.Fprintf(os.Stderr, "Usage: anubis verdict ack <incident-id>\n")
		os.Exit(1)
	}

	incidentID := os.Args[3]
	fmt.Printf("⚖️  Acknowledging incident: %s\n", incidentID)

	// Try to connect to local API
	apiURL := getAPIURL()
	token := getAPIToken()

	if token == "" {
		fmt.Println("Note: No API token found. Run 'anubis serve' and login first.")
		fmt.Println()
		fmt.Println("Incident would be acknowledged:", incidentID)
		fmt.Println("✓ Acknowledgment prepared (requires running server)")
		return
	}

	resp, err := httpPost(apiURL+"/api/v1/incidents/"+incidentID+"/acknowledge", "application/json", []byte("{}"), token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		fmt.Printf("✓ Incident %s acknowledged successfully\n", incidentID)
	} else {
		fmt.Fprintf(os.Stderr, "Error: Server returned status %d\n", resp.StatusCode)
		os.Exit(1)
	}
}

// judgeCommand handles the judge command with subcommands and flags
func judgeCommand() {
	// Check for --help flag
	for i := 2; i < len(os.Args); i++ {
		if os.Args[i] == "--help" || os.Args[i] == "-h" {
			printJudgeHelp()
			return
		}
	}

	// Check for --all flag
	for i := 2; i < len(os.Args); i++ {
		if os.Args[i] == "--all" || os.Args[i] == "-a" {
			judgeAll()
			return
		}
	}

	// Check for a soul name argument (anubis judge <name>)
	if len(os.Args) >= 3 && !strings.HasPrefix(os.Args[2], "-") {
		judgeSingle(os.Args[2])
		return
	}

	// No flags or args — show judgments table
	showJudgments()
}

func printJudgeHelp() {
	fmt.Println("⚖️  Judgment Management")
	fmt.Println("────────────────────────────────────────────")
	fmt.Println()
	fmt.Println("Usage: anubis judge [options]")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  <name>      Force-check a specific soul by name or ID")
	fmt.Println("  --all, -a   Force-check all souls now")
	fmt.Println("  (no args)   Show current verdicts table")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  anubis judge                # Show status table")
	fmt.Println("  anubis judge my-api         # Force-check 'my-api' soul")
	fmt.Println("  anubis judge --all          # Check all souls immediately")
}

// judgeSingle force-checks a specific soul by name or ID
func judgeSingle(nameOrID string) {
	fmt.Printf("⚖️  Force-checking soul: %s\n", nameOrID)

	apiURL := getAPIURL()
	token := getAPIToken()

	if token == "" {
		fmt.Println("Error: No API token found. Set ANUBIS_API_TOKEN environment variable.")
		fmt.Println("       The server must be running to trigger force checks.")
		os.Exit(1)
	}

	// First, find the soul by name or ID
	souls, err := fetchJudgeSouls(apiURL, token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	// Find matching soul
	var targetSoul *judgeSoul
	for _, s := range souls {
		if s.ID == nameOrID || s.Name == nameOrID {
			targetSoul = &s
			break
		}
	}

	if targetSoul == nil {
		fmt.Fprintf(os.Stderr, "Error: soul '%s' not found\n", nameOrID)
		os.Exit(1)
	}

	// Trigger force check
	resp, err := httpPost(apiURL+"/api/v1/souls/"+targetSoul.ID+"/check", "application/json", []byte("{}"), token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error triggering check: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Error: API returned status %d\n", resp.StatusCode)
		os.Exit(1)
	}

	var judgment core.Judgment
	if err := json.NewDecoder(resp.Body).Decode(&judgment); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
		os.Exit(1)
	}

	statusIcon := "○"
	switch judgment.Status {
	case core.SoulAlive:
		statusIcon = "✓"
	case core.SoulDead:
		statusIcon = "✗"
	case core.SoulDegraded:
		statusIcon = "~"
	}

	fmt.Printf("✓ Check complete: %s %s (%s)\n", statusIcon, judgment.Status, judgment.Duration)
	fmt.Printf("  Duration: %s\n", judgment.Duration)
	fmt.Printf("  Timestamp: %s\n", judgment.Timestamp.Format("2006-01-02 15:04:05"))
	if judgment.Message != "" {
		fmt.Printf("  Message: %s\n", judgment.Message)
	}
}

// judgeAll force-checks all souls immediately
func judgeAll() {
	fmt.Println("⚖️  Force-checking all souls...")

	apiURL := getAPIURL()
	token := getAPIToken()

	if token == "" {
		fmt.Println("Error: No API token found. Set ANUBIS_API_TOKEN environment variable.")
		fmt.Println("       The server must be running to trigger force checks.")
		os.Exit(1)
	}

	// Get all souls
	souls, err := fetchJudgeSouls(apiURL, token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	if len(souls) == 0 {
		fmt.Println("No souls configured.")
		return
	}

	fmt.Printf("Found %d souls, triggering checks...\n\n", len(souls))

	successCount := 0
	failCount := 0

	for _, soul := range souls {
		resp, err := httpPost(apiURL+"/api/v1/souls/"+soul.ID+"/check", "application/json", []byte("{}"), token)
		if err != nil || resp.StatusCode != http.StatusOK {
			failCount++
			if resp != nil {
				resp.Body.Close()
			}
			fmt.Printf("  ✗ %s: failed to trigger\n", soul.Name)
			continue
		}
		resp.Body.Close()
		successCount++
		fmt.Printf("  ✓ %s: check triggered\n", soul.Name)
	}

	fmt.Println()
	fmt.Printf("Results: %d triggered, %d failed\n", successCount, failCount)
	fmt.Println("Checks run asynchronously. Use 'anubis judge' to view results.")
}
