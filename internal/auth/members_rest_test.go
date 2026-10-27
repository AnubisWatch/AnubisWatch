package auth

import (
	"bufio"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/api"
	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/mail"
	"github.com/AnubisWatch/anubiswatch/internal/mail/mailtest"
	"github.com/AnubisWatch/anubiswatch/internal/probe"
)

// The members API against a real LocalAuthenticator.

type membersHarness struct {
	t    *testing.T
	auth *LocalAuthenticator
	srv  *api.RESTServer
	http *httptest.Server
}

func newMembersHarness(t *testing.T) *membersHarness {
	t.Helper()
	a := newTestLocalAuth(t, "", dirAdminEmail, dirAdminPass)
	srv := api.NewRESTServer(core.ServerConfig{Host: "127.0.0.1", Port: 8080}, core.AuthConfig{Enabled: core.BoolPtr(true)},
		nil, nil, nil, a, nil, nil, nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	hs := httptest.NewServer(srv.Handler())
	t.Cleanup(func() {
		hs.CloseClientConnections()
		hs.Close()
		_ = srv.Stop(t.Context())
	})
	return &membersHarness{t: t, auth: a, srv: srv, http: hs}
}

func (h *membersHarness) call(method, path, token, body string) (int, map[string]any) {
	h.t.Helper()
	req, _ := http.NewRequest(method, h.http.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := h.http.Client().Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = map[string]any{"raw": string(raw)}
	}
	return resp.StatusCode, out
}

func (h *membersHarness) login(email, password string) string {
	h.t.Helper()
	_, tok, err := h.auth.Login(email, password)
	if err != nil {
		h.t.Fatalf("login %s: %v", email, err)
	}
	return tok
}

func tokenFromLink(t *testing.T, link string) string {
	t.Helper()
	u, err := url.Parse(link)
	if err != nil || u.Path != "/accept-invite" || u.Query().Get("token") == "" {
		t.Fatalf("invite link %q", link)
	}
	return u.Query().Get("token")
}

func TestMembersAPIInviteWithoutMail(t *testing.T) {
	h := newMembersHarness(t)
	owner := h.login(dirAdminEmail, dirAdminPass)

	code, body := h.call("POST", "/api/v1/invitations", owner, `{"email":"inv@example.com","role":"editor"}`)
	if code != http.StatusCreated || body["email_sent"] != false {
		t.Fatalf("invite = %d %v", code, body)
	}
	link, _ := body["invite_url"].(string)
	token := tokenFromLink(t, link)
	if !strings.HasPrefix(link, "/accept-invite?") {
		t.Fatalf("without a public URL the link is relative, got %q", link)
	}
	// The listing never shows the token.
	req, _ := http.NewRequest("GET", h.http.URL+"/api/v1/invitations", nil)
	req.Header.Set("Authorization", "Bearer "+owner)
	resp, err := h.http.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || strings.Contains(string(raw), token) || !strings.Contains(string(raw), "inv@example.com") {
		t.Fatalf("list invitations = %d %s", resp.StatusCode, raw)
	}

	accept := `{"token":"` + token + `","name":"Invited","password":"` + dirUserPass + `"}`
	if code, body := h.call("POST", "/api/v1/auth/accept-invite", "", accept); code != http.StatusCreated || body["role"] != "editor" {
		t.Fatalf("accept = %d %v", code, body)
	}
	if code, _ := h.call("POST", "/api/v1/auth/accept-invite", "", accept); code != http.StatusBadRequest {
		t.Fatalf("second accept = %d, want 400", code)
	}
	editor := h.login("inv@example.com", dirUserPass)
	if code, _ := h.call("GET", "/api/v1/members", editor, ""); code != http.StatusForbidden {
		t.Fatalf("editor listed members: %d", code)
	}
	if code, _ := h.call("POST", "/api/v1/invitations", editor, `{"email":"x@example.com","role":"viewer"}`); code != http.StatusForbidden {
		t.Fatalf("editor invited: %d", code)
	}
}

func TestMembersAPIInviteOverFakeSMTP(t *testing.T) {
	previous := probe.DefaultValidator.Load()
	t.Cleanup(func() { probe.DefaultValidator.Store(previous) })
	if err := probe.ConfigureDefaultValidator(true, nil); err != nil {
		t.Fatal(err)
	}
	relay := mailtest.NewServer(t)
	mailer, err := mail.New(core.MailConfig{Host: relay.Host(), Port: relay.Port(), From: "anubis@example.com", TLS: core.MailTLSNone})
	if err != nil {
		t.Fatal(err)
	}
	queue := mail.NewQueue(mailer, slog.New(slog.NewTextHandler(io.Discard, nil)), mail.QueueOptions{})
	t.Cleanup(queue.Close)

	h := newMembersHarness(t)
	h.srv.SetPasswordResetMailer(queue, "https://anubis.example.com/")
	owner := h.login(dirAdminEmail, dirAdminPass)

	code, body := h.call("POST", "/api/v1/invitations", owner, `{"email":"mailed@example.com","role":"viewer"}`)
	if code != http.StatusCreated || body["email_sent"] != true {
		t.Fatalf("invite = %d %v", code, body)
	}
	if _, ok := body["invite_url"]; ok {
		t.Fatal("an emailed invitation also returned its link")
	}
	msgs := relay.WaitFor(1, 5*time.Second)
	if len(msgs) != 1 || msgs[0].To[0] != "mailed@example.com" {
		t.Fatalf("relay got %d messages", len(msgs))
	}
	link := regexp.MustCompile(`https://anubis\.example\.com/accept-invite\?token=[0-9a-f]+`).FindString(msgs[0].Body())
	if link == "" {
		t.Fatalf("no accept link in email:\n%s", msgs[0].Body())
	}
	token := tokenFromLink(t, strings.TrimPrefix(link, "https://anubis.example.com"))
	accept := `{"token":"` + token + `","name":"Mailed","password":"` + dirUserPass + `"}`
	if code, body := h.call("POST", "/api/v1/auth/accept-invite", "", accept); code != http.StatusCreated {
		t.Fatalf("accept = %d %v", code, body)
	}
	h.login("mailed@example.com", dirUserPass)
}

// stubKeys authenticates every "anb_" token as an owner-role API key.
type stubKeys struct{}

func (stubKeys) AuthenticateKey(string) (*api.User, error) {
	return &api.User{ID: "apikey:k1", APIKeyID: "k1", Role: "owner", Workspace: "default"}, nil
}
func (stubKeys) CreateKey(*api.User, string, core.MemberRole, *time.Time) (*core.APIKey, string, error) {
	return nil, "", nil
}
func (stubKeys) ListKeys(string) ([]*core.APIKey, error)                { return nil, nil }
func (stubKeys) RevokeKey(string, string, string) (*core.APIKey, error) { return nil, nil }

func TestMembersAPIRefusesAPIKeys(t *testing.T) {
	h := newMembersHarness(t)
	h.srv.SetAPIKeys(stubKeys{})
	key := "anb_k1_secret"
	cases := []struct{ method, path, body string }{
		{"GET", "/api/v1/members", ""},
		{"POST", "/api/v1/members", `{"email":"k@example.com","password":"` + dirUserPass + `"}`},
		{"PATCH", "/api/v1/members/x", `{"role":"viewer"}`},
		{"DELETE", "/api/v1/members/x", ""},
		{"DELETE", "/api/v1/users/x", ""},
		{"GET", "/api/v1/invitations", ""},
		{"POST", "/api/v1/invitations", `{"email":"k@example.com"}`},
		{"DELETE", "/api/v1/invitations/x", ""},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			if code, body := h.call(tc.method, tc.path, key, tc.body); code != http.StatusForbidden || !strings.Contains(body["error"].(string), "API keys") {
				t.Fatalf("API key got %d %v", code, body)
			}
		})
	}
}

func TestMembersAPIDisableEndsSessionsAndStreams(t *testing.T) {
	h := newMembersHarness(t)
	owner := h.login(dirAdminEmail, dirAdminPass)

	code, body := h.call("POST", "/api/v1/members", owner, `{"email":"ed@example.com","name":"Ed","password":"`+dirUserPass+`","role":"editor"}`)
	if code != http.StatusCreated || body["must_change_password"] != true {
		t.Fatalf("create = %d %v", code, body)
	}
	id := body["id"].(string)

	// The initial password must be changed before anything else.
	first := h.login("ed@example.com", dirUserPass)
	if code, body := h.call("GET", "/api/v1/quota", first, ""); code != http.StatusForbidden || body["code"] != "password_change_required" {
		t.Fatalf("before the change = %d %v", code, body)
	}
	if code, _ := h.call("GET", "/api/v1/auth/me", first, ""); code != http.StatusOK {
		t.Fatalf("/auth/me during forced change = %d", code)
	}
	if code, _ := h.call("PUT", "/api/v1/auth/change-password", first, `{"current_password":"`+dirUserPass+`","new_password":"NewPass2468!x"}`); code != http.StatusOK {
		t.Fatalf("change password = %d", code)
	}
	ed := h.login("ed@example.com", "NewPass2468!x")
	if code, _ := h.call("GET", "/api/v1/quota", ed, ""); code != http.StatusOK {
		t.Fatalf("after the change = %d", code)
	}

	// A live stream of that user.
	req, _ := http.NewRequest("GET", h.http.URL+"/api/v1/events", nil)
	req.Header.Set("Authorization", "Bearer "+ed)
	resp, err := h.http.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	stream := bufio.NewReader(resp.Body)
	if line, err := stream.ReadString('\n'); err != nil || !strings.Contains(line, "connected") {
		t.Fatalf("SSE: %q %v", line, err)
	}
	ended := make(chan struct{})
	go func() {
		for {
			if _, err := stream.ReadString('\n'); err != nil {
				close(ended)
				return
			}
		}
	}()

	// Last-owner and self-protection hold through the API too.
	if code, _ := h.call("PATCH", "/api/v1/members/"+h.auth.findOwnerID(t), owner, `{"role":"admin"}`); code != http.StatusConflict {
		t.Fatalf("last owner demoted itself: %d", code)
	}

	if code, body := h.call("PATCH", "/api/v1/members/"+id, owner, `{"disabled":true}`); code != http.StatusOK || body["disabled"] != true {
		t.Fatalf("disable = %d %v", code, body)
	}
	select {
	case <-ended:
	case <-time.After(3 * time.Second):
		t.Fatal("the disabled user's live stream stayed open")
	}
	if code, _ := h.call("GET", "/api/v1/auth/me", ed, ""); code != http.StatusUnauthorized {
		t.Fatalf("disabled user's session = %d", code)
	}
	// Remove, then the member is gone.
	if code, _ := h.call("DELETE", "/api/v1/members/"+id, owner, ""); code != http.StatusNoContent {
		t.Fatalf("remove = %d", code)
	}
	if code, _ := h.call("PATCH", "/api/v1/members/"+id, owner, `{"role":"viewer"}`); code != http.StatusNotFound {
		t.Fatalf("removed member still found: %d", code)
	}
}

func TestMembersAPIQuota(t *testing.T) {
	h := newMembersHarness(t)
	h.auth.SetMemberQuota(func(ws string, current int) error {
		return core.CheckQuotaCount(ws, core.QuotaMaxTeamMembers, 2, current)
	})
	owner := h.login(dirAdminEmail, dirAdminPass)
	if code, _ := h.call("POST", "/api/v1/invitations", owner, `{"email":"one@example.com"}`); code != http.StatusCreated {
		t.Fatalf("first invite = %d", code)
	}
	code, body := h.call("POST", "/api/v1/invitations", owner, `{"email":"two@example.com"}`)
	if code != http.StatusConflict || body["code"] != "quota_exceeded" || body["quota"] != "max_team_members" {
		t.Fatalf("invite past quota = %d %v", code, body)
	}
	code, body = h.call("GET", "/api/v1/quota", owner, "")
	members, _ := body["team_members"].(map[string]any)
	if code != http.StatusOK || members["used"] != float64(2) {
		t.Fatalf("quota = %d %v", code, body)
	}
}

func (a *LocalAuthenticator) findOwnerID(t *testing.T) string {
	t.Helper()
	a.mu.RLock()
	defer a.mu.RUnlock()
	if u := a.findByEmailLocked(dirAdminEmail); u != nil {
		return u.ID
	}
	t.Fatal("no admin")
	return ""
}
