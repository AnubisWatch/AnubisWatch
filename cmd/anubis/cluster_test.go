package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSummonNode_APISuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/cluster/nodes" {
			t.Errorf("Expected /api/v1/cluster/nodes, got %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusCreated)
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
	os.Args = []string{"anubis", "summon", "10.0.0.2:7946", "--name", "jackal-02", "--region", "us-east"}
	defer func() { os.Args = oldArgs }()

	output := captureStdout(summonNode)
	if !strings.Contains(output, "Summoning Jackal") {
		t.Errorf("Expected summon message, got: %s", output)
	}
	if !strings.Contains(output, "added to cluster successfully via API") {
		t.Errorf("Expected API success message, got: %s", output)
	}
}

func TestSummonNode_StorageFallback(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	oldArgs := os.Args
	os.Args = []string{"anubis", "summon", "10.0.0.3:7946", "--name", "jackal-03"}
	defer func() { os.Args = oldArgs }()

	output := captureStdout(summonNode)
	if !strings.Contains(output, "Summoning Jackal") {
		t.Errorf("Expected summon message, got: %s", output)
	}
	if !strings.Contains(output, "added to cluster configuration") {
		t.Errorf("Expected storage fallback message, got: %s", output)
	}
}

func TestSummonNode_MissingArgsExits(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		os.Args = []string{"anubis", "summon"}
		summonNode()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestSummonNode_MissingArgsExits")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Usage: anubis summon") {
		t.Errorf("Expected usage message, got: %s", string(output))
	}
}

func TestBanishNode_APISuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/cluster/nodes/jackal-02" {
			t.Errorf("Expected /api/v1/cluster/nodes/jackal-02, got %s", r.URL.Path)
		}
		if r.Method != "DELETE" {
			t.Errorf("Expected DELETE, got %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
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
	os.Args = []string{"anubis", "banish", "jackal-02"}
	defer func() { os.Args = oldArgs }()

	output := captureStdout(banishNode)
	if !strings.Contains(output, "Banishing Jackal") {
		t.Errorf("Expected banish message, got: %s", output)
	}
	if !strings.Contains(output, "removed from cluster via API") {
		t.Errorf("Expected API success message, got: %s", output)
	}
}

func TestBanishNode_StorageFallback(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	// First summon a node so it exists in storage
	oldArgs := os.Args
	os.Args = []string{"anubis", "summon", "10.0.0.4:7946", "--name", "jackal-04"}
	captureStdout(summonNode)

	// Now banish it
	os.Args = []string{"anubis", "banish", "jackal-04"}
	defer func() { os.Args = oldArgs }()

	output := captureStdout(banishNode)
	if !strings.Contains(output, "Banishing Jackal") {
		t.Errorf("Expected banish message, got: %s", output)
	}
	if !strings.Contains(output, "removed from cluster configuration") {
		t.Errorf("Expected storage fallback message, got: %s", output)
	}
}

func TestBanishNode_MissingArgsExits(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		os.Args = []string{"anubis", "banish"}
		banishNode()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestBanishNode_MissingArgsExits")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Usage: anubis banish") {
		t.Errorf("Expected usage message, got: %s", string(output))
	}
}

func TestShowCluster_ViaAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/cluster/status" {
			t.Errorf("Expected /api/v1/cluster/status, got %s", r.URL.Path)
		}
		if r.Method != "GET" {
			t.Errorf("Expected GET, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"is_clustered":true,"node_id":"node-1","state":"leader","leader":"node-1","term":5,"peer_count":2}`))
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

	output := captureStdout(showCluster)
	if !strings.Contains(output, "Necropolis") {
		t.Errorf("Expected header, got: %s", output)
	}
	if !strings.Contains(output, "leader") {
		t.Errorf("Expected state in output, got: %s", output)
	}
	if !strings.Contains(output, "node-1") {
		t.Errorf("Expected node ID in output, got: %s", output)
	}
	if !strings.Contains(output, "5") {
		t.Errorf("Expected term in output, got: %s", output)
	}
}

func TestShowCluster_StorageFallback(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	// Summon a node first
	oldArgs := os.Args
	os.Args = []string{"anubis", "summon", "10.0.0.5:7946", "--name", "jackal-05", "--region", "eu-west"}
	captureStdout(summonNode)
	os.Args = oldArgs

	output := captureStdout(showCluster)
	if !strings.Contains(output, "Necropolis") {
		t.Errorf("Expected header, got: %s", output)
	}
	if !strings.Contains(output, "Standalone") {
		t.Errorf("Expected standalone state, got: %s", output)
	}
	if !strings.Contains(output, "jackal-05") {
		t.Errorf("Expected jackal in output, got: %s", output)
	}
	if !strings.Contains(output, "eu-west") {
		t.Errorf("Expected region in output, got: %s", output)
	}
}

func TestShowCluster_NoStorage(t *testing.T) {
	// Point to a non-existent data dir
	t.Setenv("ANUBIS_DATA_DIR", filepath.Join(t.TempDir(), "does-not-exist"))

	output := captureStdout(showCluster)
	if !strings.Contains(output, "Standalone") {
		t.Errorf("Expected standalone state, got: %s", output)
	}
	if !strings.Contains(output, "Pharaoh") {
		t.Errorf("Expected Pharaoh role, got: %s", output)
	}
}

func TestSummonNode_StorageFallbackOnAPIError(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		t.Setenv("ANUBIS_DATA_DIR", t.TempDir())
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		u, _ := url.Parse(server.URL)
		os.Setenv("ANUBIS_HOST", u.Hostname())
		os.Setenv("ANUBIS_PORT", u.Port())
		os.Setenv("ANUBIS_API_TOKEN", "test-token")

		os.Args = []string{"anubis", "summon", "10.0.0.2:7946", "--name", "jackal-02"}
		summonNode()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestSummonNode_StorageFallbackOnAPIError")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "added to cluster configuration") {
		t.Errorf("Expected storage fallback message, got: %s", string(output))
	}
}

func TestShowCluster_StorageFallbackOnAPIError(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		t.Setenv("ANUBIS_DATA_DIR", t.TempDir())
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		u, _ := url.Parse(server.URL)
		os.Setenv("ANUBIS_HOST", u.Hostname())
		os.Setenv("ANUBIS_PORT", u.Port())
		os.Setenv("ANUBIS_API_TOKEN", "test-token")

		showCluster()
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestShowCluster_StorageFallbackOnAPIError")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	output, _ := cmd.CombinedOutput()

	if !strings.Contains(string(output), "Standalone") {
		t.Errorf("Expected standalone fallback, got: %s", string(output))
	}
}

func TestSummonNode_StorageOnlyWithFlags(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	store.Close()

	oldArgs := os.Args
	os.Args = []string{"anubis", "summon", "10.0.0.5:7946", "--name", "jackal-test", "--region", "east"}
	defer func() { os.Args = oldArgs }()

	output := captureStdout(summonNode)
	if !strings.Contains(output, "jackal-test") {
		t.Errorf("Expected node ID in output, got: %s", output)
	}
	if !strings.Contains(output, "added") {
		t.Errorf("Expected added message, got: %s", output)
	}
}

func TestBanishNode_FromStorage(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	ctx := context.Background()
	store.SaveJackal(ctx, "jackal-remove", "10.0.0.6:7946", "west")
	store.Close()

	oldArgs := os.Args
	os.Args = []string{"anubis", "banish", "jackal-remove"}
	defer func() { os.Args = oldArgs }()

	output := captureStdout(banishNode)
	if !strings.Contains(output, "removed") {
		t.Errorf("Expected removed message, got: %s", output)
	}
}

func TestShowCluster_WithStoredJackals(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("ANUBIS_DATA_DIR", tmpDir)

	store, err := openLocalStorage()
	if err != nil {
		t.Fatalf("Failed to open storage: %v", err)
	}
	ctx := context.Background()
	store.SaveJackal(ctx, "jackal-01", "10.0.0.2:7946", "default")
	store.Close()

	output := captureStdout(showCluster)
	if !strings.Contains(output, "jackal-01") {
		t.Errorf("Expected jackal in output, got: %s", output)
	}
	if !strings.Contains(output, "Pharaoh") {
		t.Errorf("Expected Pharaoh role, got: %s", output)
	}
}
