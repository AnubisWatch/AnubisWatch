package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

func captureStdout(f func()) string {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func TestPrintJudgeHelp(t *testing.T) {
	output := captureStdout(printJudgeHelp)
	if !strings.Contains(output, "Judgment Management") {
		t.Errorf("Expected help output, got: %s", output)
	}
	if !strings.Contains(output, "--all") {
		t.Error("Expected --all flag in help")
	}
}

func TestJudgeCommand_Help(t *testing.T) {
	oldArgs := os.Args
	os.Args = []string{"anubis", "judge", "--help"}
	defer func() { os.Args = oldArgs }()

	output := captureStdout(judgeCommand)
	if !strings.Contains(output, "Judgment Management") {
		t.Errorf("Expected help output, got: %s", output)
	}
}

func TestJudgeCommand_ShowJudgments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/souls" {
			t.Errorf("Expected /api/v1/souls, got %s", r.URL.Path)
		}
		if r.Method != "GET" {
			t.Errorf("Expected GET, got %s", r.Method)
		}
		souls := []*core.Soul{
			{ID: "soul-1", Name: "test-soul", Type: "http", Target: "https://example.com", Enabled: true, Weight: core.Duration{Duration: time.Second}, Timeout: core.Duration{Duration: 5 * time.Second}, Region: "default"},
		}
		json.NewEncoder(w).Encode(souls)
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	os.Setenv("ANUBIS_HOST", u.Hostname())
	os.Setenv("ANUBIS_PORT", u.Port())
	os.Setenv("ANUBIS_API_TOKEN", "test-token")
	defer func() {
		os.Unsetenv("ANUBIS_HOST")
		os.Unsetenv("ANUBIS_PORT")
		os.Unsetenv("ANUBIS_API_TOKEN")
	}()

	oldArgs := os.Args
	os.Args = []string{"anubis", "judge"}
	defer func() { os.Args = oldArgs }()

	output := captureStdout(judgeCommand)
	if !strings.Contains(output, "The Judgment Never Sleeps") {
		t.Errorf("Expected judgment header, got: %s", output)
	}
	if !strings.Contains(output, "test-soul") {
		t.Errorf("Expected soul name in output, got: %s", output)
	}
}

func TestJudgeCommand_Single(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/souls":
			if r.Method != "GET" {
				t.Errorf("Expected GET, got %s", r.Method)
			}
			souls := []*core.Soul{{ID: "soul-1", Name: "test-soul", Type: "http", Target: "https://example.com", Enabled: true, Weight: core.Duration{Duration: time.Second}, Timeout: core.Duration{Duration: 5 * time.Second}}}
			json.NewEncoder(w).Encode(souls)
		case "/api/v1/souls/soul-1/check":
			if r.Method != "POST" {
				t.Errorf("Expected POST, got %s", r.Method)
			}
			j := core.Judgment{ID: "j1", SoulID: "soul-1", Status: core.SoulAlive, Duration: 100}
			json.NewEncoder(w).Encode(j)
		default:
			t.Errorf("Unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	os.Setenv("ANUBIS_HOST", u.Hostname())
	os.Setenv("ANUBIS_PORT", u.Port())
	os.Setenv("ANUBIS_API_TOKEN", "test-token")
	defer func() {
		os.Unsetenv("ANUBIS_HOST")
		os.Unsetenv("ANUBIS_PORT")
		os.Unsetenv("ANUBIS_API_TOKEN")
	}()

	oldArgs := os.Args
	os.Args = []string{"anubis", "judge", "test-soul"}
	defer func() { os.Args = oldArgs }()

	output := captureStdout(judgeCommand)
	if !strings.Contains(output, "Force-checking soul") {
		t.Errorf("Expected force-check output, got: %s", output)
	}
}

func TestJudgeCommand_All(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/souls":
			if r.Method != "GET" {
				t.Errorf("Expected GET, got %s", r.Method)
			}
			souls := []*core.Soul{
				{ID: "soul-1", Name: "soul1", Type: "http", Target: "https://a.com", Enabled: true, Weight: core.Duration{Duration: time.Second}, Timeout: core.Duration{Duration: 5 * time.Second}},
				{ID: "soul-2", Name: "soul2", Type: "http", Target: "https://b.com", Enabled: true, Weight: core.Duration{Duration: time.Second}, Timeout: core.Duration{Duration: 5 * time.Second}},
			}
			json.NewEncoder(w).Encode(souls)
		case "/api/v1/souls/soul-1/check", "/api/v1/souls/soul-2/check":
			callCount++
			if r.Method != "POST" {
				t.Errorf("Expected POST, got %s", r.Method)
			}
			j := core.Judgment{ID: "j1", SoulID: "soul-1", Status: core.SoulAlive, Duration: 100}
			json.NewEncoder(w).Encode(j)
		default:
			t.Errorf("Unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	os.Setenv("ANUBIS_HOST", u.Hostname())
	os.Setenv("ANUBIS_PORT", u.Port())
	os.Setenv("ANUBIS_API_TOKEN", "test-token")
	defer func() {
		os.Unsetenv("ANUBIS_HOST")
		os.Unsetenv("ANUBIS_PORT")
		os.Unsetenv("ANUBIS_API_TOKEN")
	}()

	oldArgs := os.Args
	os.Args = []string{"anubis", "judge", "--all"}
	defer func() { os.Args = oldArgs }()

	output := captureStdout(judgeCommand)
	if !strings.Contains(output, "Force-checking all souls") {
		t.Errorf("Expected all-souls output, got: %s", output)
	}
	if callCount != 2 {
		t.Errorf("Expected 2 force-check calls, got %d", callCount)
	}
}

func TestJudgeSingle_NoToken(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		os.Unsetenv("ANUBIS_API_TOKEN")
		os.Args = []string{"anubis", "judge", "test-soul"}
		judgeCommand()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestJudgeSingle_NoToken")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "No API token found") {
		t.Errorf("Expected 'No API token found' error, got: %s", string(output))
	}
}

func TestShowJudgments_StorageFallback(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	ctx := context.Background()
	ws := &core.Workspace{ID: "default", Name: "Default"}
	store.SaveWorkspace(ctx, ws)
	soul := &core.Soul{ID: "sj1", Name: "storage-judge", Type: "http", Target: "https://example.com", WorkspaceID: "default"}
	store.SaveSoul(ctx, soul)
	store.Close()

	oldArgs := os.Args
	os.Args = []string{"anubis", "judge"}
	defer func() { os.Args = oldArgs }()

	output := captureStdout(showJudgments)
	if !strings.Contains(output, "storage-judge") {
		t.Errorf("Expected soul name in output, got: %s", output)
	}
	if !strings.Contains(output, "Total souls: 1") {
		t.Errorf("Expected total souls count, got: %s", output)
	}
}

func TestVerdictCommand_UnknownSubcommandExits(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		os.Args = []string{"anubis", "verdict", "unknown"}
		verdictCommand()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestVerdictCommand_UnknownSubcommandExits")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Unknown verdict subcommand") {
		t.Errorf("Expected unknown subcommand error, got: %s", string(output))
	}
}

func TestVerdictTest_APISuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/channels/slack/test" {
			t.Errorf("Expected /api/v1/channels/slack/test, got %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	os.Setenv("ANUBIS_HOST", u.Hostname())
	os.Setenv("ANUBIS_PORT", u.Port())
	os.Setenv("ANUBIS_API_TOKEN", "test-token")
	defer func() {
		os.Unsetenv("ANUBIS_HOST")
		os.Unsetenv("ANUBIS_PORT")
		os.Unsetenv("ANUBIS_API_TOKEN")
	}()

	oldArgs := os.Args
	os.Args = []string{"anubis", "verdict", "test", "slack"}
	defer func() { os.Args = oldArgs }()

	output := captureStdout(verdictTest)
	if !strings.Contains(output, "Test notification sent successfully") {
		t.Errorf("Expected success message, got: %s", output)
	}
}

func TestVerdictHistory_APISuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/incidents" {
			t.Errorf("Expected /api/v1/incidents, got %s", r.URL.Path)
		}
		if r.Method != "GET" {
			t.Errorf("Expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	os.Setenv("ANUBIS_HOST", u.Hostname())
	os.Setenv("ANUBIS_PORT", u.Port())
	os.Setenv("ANUBIS_API_TOKEN", "test-token")
	defer func() {
		os.Unsetenv("ANUBIS_HOST")
		os.Unsetenv("ANUBIS_PORT")
		os.Unsetenv("ANUBIS_API_TOKEN")
	}()

	oldArgs := os.Args
	os.Args = []string{"anubis", "verdict", "history"}
	defer func() { os.Args = oldArgs }()

	output := captureStdout(verdictHistory)
	if !strings.Contains(output, "No active incidents") {
		t.Errorf("Expected no incidents message, got: %s", output)
	}
}

func TestVerdictAck_APISuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/incidents/inc-123/acknowledge" {
			t.Errorf("Expected /api/v1/incidents/inc-123/acknowledge, got %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	os.Setenv("ANUBIS_HOST", u.Hostname())
	os.Setenv("ANUBIS_PORT", u.Port())
	os.Setenv("ANUBIS_API_TOKEN", "test-token")
	defer func() {
		os.Unsetenv("ANUBIS_HOST")
		os.Unsetenv("ANUBIS_PORT")
		os.Unsetenv("ANUBIS_API_TOKEN")
	}()

	oldArgs := os.Args
	os.Args = []string{"anubis", "verdict", "ack", "inc-123"}
	defer func() { os.Args = oldArgs }()

	output := captureStdout(verdictAck)
	if !strings.Contains(output, "acknowledged successfully") {
		t.Errorf("Expected success message, got: %s", output)
	}
}

func TestJudgeSingle_APISuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/souls":
			if r.Method != "GET" {
				t.Errorf("Expected GET, got %s", r.Method)
			}
			souls := []*core.Soul{{ID: "js1", Name: "judge-single", Type: "http", Target: "https://example.com", Enabled: true, Weight: core.Duration{Duration: time.Second}, Timeout: core.Duration{Duration: 5 * time.Second}}}
			json.NewEncoder(w).Encode(souls)
		case "/api/v1/souls/js1/check":
			if r.Method != "POST" {
				t.Errorf("Expected POST, got %s", r.Method)
			}
			j := core.Judgment{ID: "j1", SoulID: "js1", Status: core.SoulAlive, Duration: 100}
			json.NewEncoder(w).Encode(j)
		default:
			t.Errorf("Unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	os.Setenv("ANUBIS_HOST", u.Hostname())
	os.Setenv("ANUBIS_PORT", u.Port())
	os.Setenv("ANUBIS_API_TOKEN", "test-token")
	defer func() {
		os.Unsetenv("ANUBIS_HOST")
		os.Unsetenv("ANUBIS_PORT")
		os.Unsetenv("ANUBIS_API_TOKEN")
	}()

	oldArgs := os.Args
	os.Args = []string{"anubis", "judge", "judge-single"}
	defer func() { os.Args = oldArgs }()

	output := captureStdout(judgeCommand)
	if !strings.Contains(output, "Force-checking soul") {
		t.Errorf("Expected force-check output, got: %s", output)
	}
}

func TestMain_Help(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		os.Args = []string{"anubis", "help"}
		main()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestMain_Help")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Usage: anubis") {
		t.Errorf("Expected usage output, got: %s", string(output))
	}
}

func TestMain_NoArgs(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		os.Args = []string{"anubis"}
		main()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestMain_NoArgs")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "The Judgment Never Sleeps") {
		t.Errorf("Expected usage output, got: %s", string(output))
	}
}

func TestJudgeAll_APISuccess(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/souls":
			if r.Method != "GET" {
				t.Errorf("Expected GET, got %s", r.Method)
			}
			souls := []*core.Soul{
				{ID: "soul-1", Name: "pass-soul", Type: "http", Target: "https://a.com", Enabled: true, Weight: core.Duration{Duration: time.Second}, Timeout: core.Duration{Duration: 5 * time.Second}},
				{ID: "soul-2", Name: "fail-soul", Type: "http", Target: "https://b.com", Enabled: true, Weight: core.Duration{Duration: time.Second}, Timeout: core.Duration{Duration: 5 * time.Second}},
			}
			json.NewEncoder(w).Encode(souls)
		case "/api/v1/souls/soul-1/check":
			callCount++
			w.WriteHeader(http.StatusOK)
		case "/api/v1/souls/soul-2/check":
			callCount++
			w.WriteHeader(http.StatusInternalServerError)
		default:
			t.Errorf("Unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	os.Setenv("ANUBIS_HOST", u.Hostname())
	os.Setenv("ANUBIS_PORT", u.Port())
	os.Setenv("ANUBIS_API_TOKEN", "test-token")
	defer func() {
		os.Unsetenv("ANUBIS_HOST")
		os.Unsetenv("ANUBIS_PORT")
		os.Unsetenv("ANUBIS_API_TOKEN")
	}()

	oldArgs := os.Args
	os.Args = []string{"anubis", "judge", "--all"}
	defer func() { os.Args = oldArgs }()

	output := captureStdout(judgeCommand)
	if !strings.Contains(output, "Force-checking all souls") {
		t.Errorf("Expected header, got: %s", output)
	}
	if !strings.Contains(output, "pass-soul: check triggered") {
		t.Errorf("Expected success message, got: %s", output)
	}
	if !strings.Contains(output, "fail-soul: failed to trigger") {
		t.Errorf("Expected failure message, got: %s", output)
	}
	if callCount != 2 {
		t.Errorf("Expected 2 check calls, got %d", callCount)
	}
}

func TestJudgeAll_EmptySouls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/souls" {
			t.Errorf("Expected /api/v1/souls, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode([]*core.Soul{})
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	os.Setenv("ANUBIS_HOST", u.Hostname())
	os.Setenv("ANUBIS_PORT", u.Port())
	os.Setenv("ANUBIS_API_TOKEN", "test-token")
	defer func() {
		os.Unsetenv("ANUBIS_HOST")
		os.Unsetenv("ANUBIS_PORT")
		os.Unsetenv("ANUBIS_API_TOKEN")
	}()

	oldArgs := os.Args
	os.Args = []string{"anubis", "judge", "--all"}
	defer func() { os.Args = oldArgs }()

	output := captureStdout(judgeCommand)
	if !strings.Contains(output, "No souls configured") {
		t.Errorf("Expected empty souls message, got: %s", output)
	}
}

func TestJudgeAll_NoToken(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		os.Unsetenv("ANUBIS_API_TOKEN")
		os.Args = []string{"anubis", "judge", "--all"}
		judgeAll()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestJudgeAll_NoToken")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "No API token found") {
		t.Errorf("Expected no token error, got: %s", string(output))
	}
}

func TestJudgeAll_APIError(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		u, _ := url.Parse(server.URL)
		os.Setenv("ANUBIS_HOST", u.Hostname())
		os.Setenv("ANUBIS_PORT", u.Port())
		os.Setenv("ANUBIS_API_TOKEN", "test-token")

		os.Args = []string{"anubis", "judge", "--all"}
		judgeAll()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestJudgeAll_APIError")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "API returned status") {
		t.Errorf("Expected API error, got: %s", string(output))
	}
}

func TestShowJudgments_StorageWithJudgments(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	ctx := context.Background()
	ws := &core.Workspace{ID: "default", Name: "Default"}
	store.SaveWorkspace(ctx, ws)

	aliveSoul := &core.Soul{ID: "sj-alive", Name: "alive-soul", Type: "http", Target: "https://a.com", WorkspaceID: "default", Region: "us-east"}
	deadSoul := &core.Soul{ID: "sj-dead", Name: "dead-soul", Type: "http", Target: "https://b.com", WorkspaceID: "default"}
	degradedSoul := &core.Soul{ID: "sj-deg", Name: "degraded-soul", Type: "http", Target: "https://c.com", WorkspaceID: "default"}
	store.SaveSoul(ctx, aliveSoul)
	store.SaveSoul(ctx, deadSoul)
	store.SaveSoul(ctx, degradedSoul)

	now := time.Now()
	store.SaveJudgment(ctx, &core.Judgment{ID: "j1", SoulID: "sj-alive", Status: core.SoulAlive, Timestamp: now, Duration: 100, Region: "us-east"})
	store.SaveJudgment(ctx, &core.Judgment{ID: "j2", SoulID: "sj-dead", Status: core.SoulDead, Timestamp: now, Duration: 200})
	store.SaveJudgment(ctx, &core.Judgment{ID: "j3", SoulID: "sj-deg", Status: core.SoulDegraded, Timestamp: now, Duration: 300})

	store.Close()

	oldArgs := os.Args
	os.Args = []string{"anubis", "judge"}
	defer func() { os.Args = oldArgs }()

	output := captureStdout(showJudgments)
	if !strings.Contains(output, "alive-soul") {
		t.Errorf("Expected alive soul in output, got: %s", output)
	}
	if !strings.Contains(output, "dead-soul") {
		t.Errorf("Expected dead soul in output, got: %s", output)
	}
	if !strings.Contains(output, "degraded-soul") {
		t.Errorf("Expected degraded soul in output, got: %s", output)
	}
	if !strings.Contains(output, "us-east") {
		t.Errorf("Expected region in output, got: %s", output)
	}
}

func TestVerdictTest_MissingArgs(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		os.Setenv("ANUBIS_API_TOKEN", "test-token")
		os.Args = []string{"anubis", "verdict", "test"}
		verdictTest()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestVerdictTest_MissingArgs")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Usage: anubis verdict test") {
		t.Errorf("Expected usage message, got: %s", string(output))
	}
}

func TestVerdictTest_APIError(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		u, _ := url.Parse(server.URL)
		os.Setenv("ANUBIS_HOST", u.Hostname())
		os.Setenv("ANUBIS_PORT", u.Port())
		os.Setenv("ANUBIS_API_TOKEN", "test-token")

		os.Args = []string{"anubis", "verdict", "test", "slack"}
		verdictTest()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestVerdictTest_APIError")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Server returned status") {
		t.Errorf("Expected server error, got: %s", string(output))
	}
}

func TestVerdictHistory_APIError(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		u, _ := url.Parse(server.URL)
		os.Setenv("ANUBIS_HOST", u.Hostname())
		os.Setenv("ANUBIS_PORT", u.Port())
		os.Setenv("ANUBIS_API_TOKEN", "test-token")

		os.Args = []string{"anubis", "verdict", "history"}
		verdictHistory()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestVerdictHistory_APIError")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Server returned status") {
		t.Errorf("Expected server error, got: %s", string(output))
	}
}

func TestVerdictAck_MissingArgs(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		os.Setenv("ANUBIS_API_TOKEN", "test-token")
		os.Args = []string{"anubis", "verdict", "ack"}
		verdictAck()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestVerdictAck_MissingArgs")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Usage: anubis verdict ack") {
		t.Errorf("Expected usage message, got: %s", string(output))
	}
}

func TestVerdictAck_APIError(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		u, _ := url.Parse(server.URL)
		os.Setenv("ANUBIS_HOST", u.Hostname())
		os.Setenv("ANUBIS_PORT", u.Port())
		os.Setenv("ANUBIS_API_TOKEN", "test-token")

		os.Args = []string{"anubis", "verdict", "ack", "inc-123"}
		verdictAck()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestVerdictAck_APIError")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Server returned status") {
		t.Errorf("Expected server error, got: %s", string(output))
	}
}

func TestJudgeSingle_MissingArgs(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode([]*core.Soul{})
		}))
		defer server.Close()

		u, _ := url.Parse(server.URL)
		os.Setenv("ANUBIS_HOST", u.Hostname())
		os.Setenv("ANUBIS_PORT", u.Port())
		os.Setenv("ANUBIS_API_TOKEN", "test-token")

		os.Args = []string{"anubis", "judge", "nonexistent-soul"}
		judgeSingle("nonexistent-soul")
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestJudgeSingle_MissingArgs")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "not found") {
		t.Errorf("Expected not found error, got: %s", string(output))
	}
}

func TestJudgeSingle_APIError(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/souls":
				souls := []*core.Soul{{ID: "js1", Name: "judge-single", Type: "http", Target: "https://example.com", Enabled: true, Weight: core.Duration{Duration: time.Second}, Timeout: core.Duration{Duration: 5 * time.Second}}}
				json.NewEncoder(w).Encode(souls)
			case "/api/v1/souls/js1/check":
				w.WriteHeader(http.StatusInternalServerError)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		u, _ := url.Parse(server.URL)
		os.Setenv("ANUBIS_HOST", u.Hostname())
		os.Setenv("ANUBIS_PORT", u.Port())
		os.Setenv("ANUBIS_API_TOKEN", "test-token")

		os.Args = []string{"anubis", "judge", "judge-single"}
		judgeSingle("judge-single")
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestJudgeSingle_APIError")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "API returned status") {
		t.Errorf("Expected API error, got: %s", string(output))
	}
}

func TestShowJudgments_APIParseError(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("not json"))
		}))
		defer server.Close()

		u, _ := url.Parse(server.URL)
		os.Setenv("ANUBIS_HOST", u.Hostname())
		os.Setenv("ANUBIS_PORT", u.Port())
		os.Setenv("ANUBIS_API_TOKEN", "test-token")

		os.Args = []string{"anubis", "judge"}
		showJudgments()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestShowJudgments_APIParseError")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "The Judgment Never Sleeps") {
		t.Errorf("Expected parse error, got: %s", string(output))
	}
}

func TestShowJudgments_EmptySouls(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode([]*core.Soul{})
		}))
		defer server.Close()

		u, _ := url.Parse(server.URL)
		os.Setenv("ANUBIS_HOST", u.Hostname())
		os.Setenv("ANUBIS_PORT", u.Port())
		os.Setenv("ANUBIS_API_TOKEN", "test-token")

		os.Args = []string{"anubis", "judge"}
		showJudgments()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestShowJudgments_EmptySouls")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "No souls configured yet") {
		t.Errorf("Expected empty message, got: %s", string(output))
	}
}

func TestVerdictTest_ConnectionError(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		os.Setenv("ANUBIS_HOST", "127.0.0.1")
		os.Setenv("ANUBIS_PORT", "65535")
		os.Setenv("ANUBIS_API_TOKEN", "test-token")
		os.Args = []string{"anubis", "verdict", "test", "slack"}
		verdictTest()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestVerdictTest_ConnectionError")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Error:") {
		t.Errorf("Expected error message, got: %s", string(output))
	}
}

func TestVerdictHistory_ConnectionError(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		os.Setenv("ANUBIS_HOST", "127.0.0.1")
		os.Setenv("ANUBIS_PORT", "65535")
		os.Setenv("ANUBIS_API_TOKEN", "test-token")
		os.Args = []string{"anubis", "verdict", "history"}
		verdictHistory()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestVerdictHistory_ConnectionError")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Error fetching incidents") {
		t.Errorf("Expected error message, got: %s", string(output))
	}
}

func TestVerdictHistory_ParseError(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("not json"))
		}))
		defer server.Close()

		u, _ := url.Parse(server.URL)
		os.Setenv("ANUBIS_HOST", u.Hostname())
		os.Setenv("ANUBIS_PORT", u.Port())
		os.Setenv("ANUBIS_API_TOKEN", "test-token")

		os.Args = []string{"anubis", "verdict", "history"}
		verdictHistory()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestVerdictHistory_ParseError")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Error parsing response") {
		t.Errorf("Expected parse error, got: %s", string(output))
	}
}

func TestVerdictAck_ConnectionError(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		os.Setenv("ANUBIS_HOST", "127.0.0.1")
		os.Setenv("ANUBIS_PORT", "65535")
		os.Setenv("ANUBIS_API_TOKEN", "test-token")
		os.Args = []string{"anubis", "verdict", "ack", "inc-123"}
		verdictAck()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestVerdictAck_ConnectionError")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Error:") {
		t.Errorf("Expected error message, got: %s", string(output))
	}
}

func TestJudgeSingle_ConnectionError(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		os.Setenv("ANUBIS_HOST", "127.0.0.1")
		os.Setenv("ANUBIS_PORT", "65535")
		os.Setenv("ANUBIS_API_TOKEN", "test-token")
		judgeSingle("some-soul")
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestJudgeSingle_ConnectionError")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Error connecting to API") {
		t.Errorf("Expected connection error, got: %s", string(output))
	}
}


func TestJudgeSingle_DeadStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/souls":
			souls := []*core.Soul{{ID: "js-dead", Name: "dead-soul", Type: "http", Target: "https://example.com", Enabled: true, Weight: core.Duration{Duration: time.Second}, Timeout: core.Duration{Duration: 5 * time.Second}}}
			json.NewEncoder(w).Encode(souls)
		case "/api/v1/souls/js-dead/check":
			j := core.Judgment{ID: "j-dead", SoulID: "js-dead", Status: core.SoulDead, Duration: 200, Message: "connection refused"}
			json.NewEncoder(w).Encode(j)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	os.Setenv("ANUBIS_HOST", u.Hostname())
	os.Setenv("ANUBIS_PORT", u.Port())
	os.Setenv("ANUBIS_API_TOKEN", "test-token")
	defer func() {
		os.Unsetenv("ANUBIS_HOST")
		os.Unsetenv("ANUBIS_PORT")
		os.Unsetenv("ANUBIS_API_TOKEN")
	}()

	oldArgs := os.Args
	os.Args = []string{"anubis", "judge", "dead-soul"}
	defer func() { os.Args = oldArgs }()

	output := captureStdout(judgeCommand)
	if !strings.Contains(output, "dead") {
		t.Errorf("Expected dead status, got: %s", output)
	}
	if !strings.Contains(output, "connection refused") {
		t.Errorf("Expected message, got: %s", output)
	}
}

func TestJudgeSingle_DegradedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/souls":
			souls := []*core.Soul{{ID: "js-deg", Name: "degraded-soul", Type: "http", Target: "https://example.com", Enabled: true, Weight: core.Duration{Duration: time.Second}, Timeout: core.Duration{Duration: 5 * time.Second}}}
			json.NewEncoder(w).Encode(souls)
		case "/api/v1/souls/js-deg/check":
			j := core.Judgment{ID: "j-deg", SoulID: "js-deg", Status: core.SoulDegraded, Duration: 500}
			json.NewEncoder(w).Encode(j)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	os.Setenv("ANUBIS_HOST", u.Hostname())
	os.Setenv("ANUBIS_PORT", u.Port())
	os.Setenv("ANUBIS_API_TOKEN", "test-token")
	defer func() {
		os.Unsetenv("ANUBIS_HOST")
		os.Unsetenv("ANUBIS_PORT")
		os.Unsetenv("ANUBIS_API_TOKEN")
	}()

	oldArgs := os.Args
	os.Args = []string{"anubis", "judge", "degraded-soul"}
	defer func() { os.Args = oldArgs }()

	output := captureStdout(judgeCommand)
	if !strings.Contains(output, "degraded") {
		t.Errorf("Expected degraded status, got: %s", output)
	}
}


func TestSubprocess_showVersion_temp(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		showVersion()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestSubprocess_showVersion_temp")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()
	if !strings.Contains(string(output), "AnubisWatch") {
		t.Errorf("Expected version output, got: %s", string(output))
	}
}

func TestSubprocess_configCommand_temp(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		tmpDir := t.TempDir()
		configPath := tmpDir + "/anubis.json"
		os.WriteFile(configPath, []byte("{}"), 0644)
		os.Setenv("ANUBIS_CONFIG", configPath)
		os.Args = []string{"anubis", "config", "show"}
		configCommand()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestSubprocess_configCommand_temp")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()
	if !strings.Contains(string(output), "Current Configuration") {
		t.Errorf("Expected config output, got: %s", string(output))
	}
}
