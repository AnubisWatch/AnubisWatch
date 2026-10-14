package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// TestWorkspaceOwnershipSurvivesRestart is the HTTP-level check for the fix that
// keeps an account's ID stable across a restart (internal/auth: the constructor
// restores persisted accounts before seeding the admin, and loadSessions restores
// every persisted account rather than only those with a live session).
//
// Why it matters: workspace access is decided by ws.OwnerID == ctx.User.ID
// (internal/api canAccessWorkspace) and workspace creation stores ctx.User.ID as
// the owner. An identity that changed on restart therefore silently revoked the
// operator's access to workspaces they created: the switch returns 403 and the
// workspace disappears from GET /api/v1/workspaces. This drives the real server,
// the real local authenticator and the real store over HTTP, which is what the
// earlier verification of that fix did not do.
func TestWorkspaceOwnershipSurvivesRestart(t *testing.T) {
	const (
		adminEmail    = "admin@example.com"
		adminPassword = "TestPass1234!"
		workspaceID   = "restart-owned-workspace"
	)

	dataDir := filepath.Join(t.TempDir(), "data")
	port := freeLoopbackPort(t)
	configPath := writeServerConfig(t, dataDir, port, adminEmail, adminPassword)

	first := bootAuthServer(t, configPath, port)
	client, adminID := loginAsAdmin(t, first.baseURL, adminEmail, adminPassword)

	// A workspace this operator owns, as one created through the API would be.
	if err := first.deps.Store.SaveWorkspaceNoCtx(&core.Workspace{
		ID: workspaceID, Name: "Restart Owned", Slug: "restart-owned",
		OwnerID: adminID, Status: core.WorkspaceActive,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seeding the workspace failed: %v", err)
	}

	// CONTROL: before any restart the owner can reach it, so the failure below is
	// about the restart and not about workspace switching in general.
	if got := switchWorkspace(t, client, first.baseURL, workspaceID); got != http.StatusOK {
		t.Fatalf("CONTROL FAILED: the owner could not switch to their own workspace before any restart (got %d)", got)
	}

	// Log out so the restart has no live session: with one, the previous identity
	// would also be restored from the session and the outcome would depend on map
	// iteration order rather than on the fix.
	logout(t, client, first.baseURL)
	first.stop(t)

	// Restart over the same data directory, the way an operator restarts the binary.
	second := bootAuthServer(t, configPath, port)
	defer second.stop(t)

	client2, adminID2 := loginAsAdmin(t, second.baseURL, adminEmail, adminPassword)

	if got := switchWorkspace(t, client2, second.baseURL, workspaceID); got != http.StatusOK {
		t.Fatalf("FAIL: after a restart the account that owns %s can no longer switch to it (got %d). "+
			"The account id changed across the restart (%s -> %s), so ws.OwnerID == ctx.User.ID no longer holds: "+
			"every workspace the operator created becomes inaccessible over HTTP (403 here) and disappears from "+
			"GET /api/v1/workspaces", workspaceID, got, adminID, adminID2)
	}

	// The same fact seen through the listing that the dashboard uses.
	listed := listWorkspaces(t, client2, second.baseURL)
	found := false
	for _, id := range listed {
		if id == workspaceID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("the owned workspace is missing from GET /api/v1/workspaces after the restart (got %v)", listed)
	}
}

type authTestServer struct {
	deps    *ServerDependencies
	server  *Server
	baseURL string
}

func (s *authTestServer) stop(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := s.server.Stop(ctx); err != nil {
		t.Fatalf("stopping the server failed: %v", err)
	}
	// Release the data directory so the restarted instance can open it.
	_ = s.deps.Store.Close()
}

func bootAuthServer(t *testing.T, configPath string, port int) *authTestServer {
	t.Helper()

	deps, err := BuildServerDependencies(ServerOptions{
		ConfigPath: configPath,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("BuildServerDependencies failed: %v", err)
	}

	server := NewServer(deps)
	if err := server.Start(context.Background()); err != nil {
		t.Fatalf("Server.Start failed: %v", err)
	}

	ts := &authTestServer{deps: deps, server: server, baseURL: fmt.Sprintf("http://127.0.0.1:%d", port)}
	waitForHTTP(t, ts.baseURL)
	return ts
}

func waitForHTTP(t *testing.T, baseURL string) {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/api/v1/workspaces")
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the server never became reachable at %s", baseURL)
}

func freeLoopbackPort(t *testing.T) int {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not reserve a port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func writeServerConfig(t *testing.T, dataDir string, port int, email, password string) string {
	t.Helper()

	config := fmt.Sprintf(`{
  "storage": {"path": %q},
  "server": {"host": "127.0.0.1", "port": %d, "grpc_port": 0},
  "necropolis": {"node_name": "restart-test-node"},
  "auth": {"type": "local", "local": {"admin_email": %q, "admin_password": %q}}
}`, filepath.ToSlash(dataDir), port, email, password)

	path := filepath.Join(filepath.Dir(dataDir), "anubis.json")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatalf("writing the config failed: %v", err)
	}
	return path
}

func newCookieClient(t *testing.T) *http.Client {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	return &http.Client{Jar: jar, Timeout: 10 * time.Second}
}

func loginAsAdmin(t *testing.T, baseURL, email, password string) (*http.Client, string) {
	t.Helper()

	client := newCookieClient(t)
	body := fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)

	resp, err := client.Post(baseURL+"/api/v1/auth/login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login failed with %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var out struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("could not parse the login response %q: %v", string(raw), err)
	}
	if out.User.ID == "" {
		t.Fatalf("login returned no user id: %s", string(raw))
	}
	return client, out.User.ID
}

func logout(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()

	resp, err := client.Post(baseURL+"/api/v1/auth/logout", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("logout request failed: %v", err)
	}
	resp.Body.Close()
}

func switchWorkspace(t *testing.T, client *http.Client, baseURL, workspaceID string) int {
	t.Helper()

	body := fmt.Sprintf(`{"workspace":%q}`, workspaceID)
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/auth/workspace", strings.NewReader(body))
	if err != nil {
		t.Fatalf("could not build the switch request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("switch request failed: %v", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func listWorkspaces(t *testing.T, client *http.Client, baseURL string) []string {
	t.Helper()

	resp, err := client.Get(baseURL + "/api/v1/workspaces")
	if err != nil {
		t.Fatalf("list request failed: %v", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("listing workspaces failed with %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var workspaces []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &workspaces); err != nil {
		t.Fatalf("could not parse the workspace list %q: %v", string(raw), err)
	}

	ids := make([]string, 0, len(workspaces))
	for _, ws := range workspaces {
		ids = append(ids, ws.ID)
	}
	return ids
}
