package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type pingRecord struct {
	soulID  string
	event   core.HeartbeatEvent
	message string
	source  string
}

type fakeHeartbeatRecorder struct {
	mu    sync.Mutex
	pings []pingRecord
	state map[string]*core.HeartbeatState
}

func (r *fakeHeartbeatRecorder) RecordHeartbeat(_ context.Context, soul *core.Soul, event core.HeartbeatEvent, message, source string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pings = append(r.pings, pingRecord{soul.ID, event, message, source})
	if r.state == nil {
		r.state = map[string]*core.HeartbeatState{}
	}
	st := r.state[soul.ID]
	if st == nil {
		st = &core.HeartbeatState{SoulID: soul.ID}
		r.state[soul.ID] = st
	}
	st.Apply(event, time.Now(), message, source, "n1")
	return nil
}

func (r *fakeHeartbeatRecorder) GetHeartbeatState(_ context.Context, _, soulID string) (*core.HeartbeatState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state[soulID], nil
}

func (r *fakeHeartbeatRecorder) recorded() []pingRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]pingRecord(nil), r.pings...)
}

func newHeartbeatTestServer(t *testing.T) (*RESTServer, *mockStorage, *fakeHeartbeatRecorder, *core.Soul) {
	t.Helper()
	store := newMockStorage()
	server := newTestServerWithStorage(store)
	soul := &core.Soul{ID: "hb-1", WorkspaceID: "default", Name: "nightly", Type: core.CheckHeartbeat,
		Weight: core.Duration{Duration: time.Hour}, Enabled: true, CreatedAt: time.Now()}
	soul.ApplyDefaults()
	store.souls[soul.ID] = soul
	rec := &fakeHeartbeatRecorder{}
	server.SetHeartbeat(rec, func(context.Context) ([]*core.Soul, error) {
		out := make([]*core.Soul, 0, len(store.souls))
		for _, s := range store.souls {
			out = append(out, s)
		}
		return out, nil
	})
	return server, store, rec, soul
}

func serve(server *RESTServer, method, path string, body []byte, header map[string]string) *httptest.ResponseRecorder {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.RemoteAddr = "198.51.100.7:4242"
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	server.router.ServeHTTP(rec, req)
	return rec
}

func TestHeartbeatPingEndpoints(t *testing.T) {
	server, _, recorder, soul := newHeartbeatTestServer(t)
	token := soul.Heartbeat.Token

	for _, tc := range []struct {
		method, suffix string
		body           []byte
		header         map[string]string
		event          core.HeartbeatEvent
		message        string
	}{
		{"GET", "", nil, nil, core.HeartbeatSuccess, ""},
		{"POST", "/start", nil, nil, core.HeartbeatStart, ""},
		{"POST", "", []byte("ignored"), map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, core.HeartbeatSuccess, ""},
		// curl --data-binary @log with a non-JSON content type must not be
		// refused by the JSON middleware; the message is capped at 1KB.
		{"POST", "/fail", []byte("backup failed:\n" + strings.Repeat("x", 3000)), map[string]string{"Content-Type": "text/plain"}, core.HeartbeatFail, ""},
		{"GET", "/fail?msg=exit%201", nil, nil, core.HeartbeatFail, "exit 1"},
	} {
		rec := serve(server, tc.method, "/api/v1/heartbeat/"+token+tc.suffix, tc.body, tc.header)
		if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "OK" {
			t.Fatalf("%s %s = %d %q", tc.method, tc.suffix, rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
			t.Fatalf("content type = %q", ct)
		}
		pings := recorder.recorded()
		last := pings[len(pings)-1]
		if last.event != tc.event || last.soulID != soul.ID || last.source != "198.51.100.7" {
			t.Fatalf("%s %s recorded %+v", tc.method, tc.suffix, last)
		}
		if tc.message != "" && last.message != tc.message {
			t.Fatalf("message = %q, want %q", last.message, tc.message)
		}
		if tc.event == core.HeartbeatFail && tc.message == "" {
			if len(last.message) != core.MaxHeartbeatMessage || !strings.HasPrefix(last.message, "backup failed: x") {
				t.Fatalf("fail body message len=%d prefix=%q", len(last.message), last.message[:20])
			}
		}
	}
}

func TestHeartbeatUnknownTokenIs404(t *testing.T) {
	server, _, recorder, soul := newHeartbeatTestServer(t)
	for _, token := range []string{
		strings.Repeat("A", 43),                // well-formed, unknown
		"short",                                // malformed
		soul.Heartbeat.Token[:42] + "Z",        // near miss
		soul.Heartbeat.Token + "/../../souls/", // traversal shape
	} {
		rec := serve(server, "GET", "/api/v1/heartbeat/"+token, nil, nil)
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusBadRequest {
			t.Fatalf("token %q = %d, want 404", token, rec.Code)
		}
	}
	if n := len(recorder.recorded()); n != 0 {
		t.Fatalf("unknown tokens recorded %d pings", n)
	}
	// A non-heartbeat soul's lookalike field never matches.
	server.heartbeat.invalidate("")
	if got := server.heartbeat.lookup(context.Background(), strings.Repeat("A", 43), nil); got != nil {
		t.Fatalf("lookup unknown = %+v", got)
	}
	// Without SetHeartbeat every token is unknown.
	plain := newTestServerWithStorage(newMockStorage())
	if rec := serve(plain, "GET", "/api/v1/heartbeat/"+soul.Heartbeat.Token, nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unconfigured = %d", rec.Code)
	}
}

func TestHeartbeatPerTokenRateLimit(t *testing.T) {
	server, _, _, soul := newHeartbeatTestServer(t)
	clock := time.Now()
	server.heartbeat.now = func() time.Time { return clock }
	path := "/api/v1/heartbeat/" + soul.Heartbeat.Token
	for i := 0; i < heartbeatTokenRateLimit; i++ {
		if rec := serve(server, "GET", path, nil, nil); rec.Code != http.StatusOK {
			t.Fatalf("ping %d = %d", i, rec.Code)
		}
	}
	rec := serve(server, "GET", path, nil, nil)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("over budget = %d (Retry-After %q)", rec.Code, rec.Header().Get("Retry-After"))
	}
	clock = clock.Add(heartbeatTokenRateWindow + time.Second)
	if rec := serve(server, "GET", path, nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("after window = %d", rec.Code)
	}
}

func TestHeartbeatTokenRedactionAndReveal(t *testing.T) {
	server, store, _, soul := newHeartbeatTestServer(t)
	token := soul.Heartbeat.Token

	// List and get never contain the token.
	for _, path := range []string{"/api/v1/souls", "/api/v1/souls/" + soul.ID} {
		rec := serve(server, "GET", path, nil, map[string]string{"Authorization": "Bearer valid-token"})
		if rec.Code != http.StatusOK {
			t.Fatalf("%s = %d", path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), token) || !strings.Contains(rec.Body.String(), redactedSecretValue) {
			t.Fatalf("%s leaks or does not mark the token: %s", path, rec.Body.String())
		}
	}
	// The DTO flags the secret.
	dto := redactedSoulDTOFromCore(soul)
	if !dto.HasSecrets["heartbeat"] || dto.Heartbeat["token"] != redactedSecretValue {
		t.Fatalf("dto = %+v", dto.Heartbeat)
	}

	// Reveal: editors/admins get the URL, viewers only the state.
	rec := serve(server, "GET", "/api/v1/souls/"+soul.ID+"/heartbeat", nil, map[string]string{"Authorization": "Bearer valid-token"})
	var info heartbeatInfo
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &info) != nil {
		t.Fatalf("reveal = %d %s", rec.Code, rec.Body.String())
	}
	if info.Token != token || info.PingURL != "http://example.com/api/v1/heartbeat/"+token || info.FailURL != info.PingURL+"/fail" {
		t.Fatalf("reveal info = %+v", info)
	}
	rec = serve(server, "GET", "/api/v1/souls/"+soul.ID+"/heartbeat", nil, map[string]string{"Authorization": "Bearer viewer-token"})
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), token) {
		t.Fatalf("viewer reveal = %d %s", rec.Code, rec.Body.String())
	}

	// An update that echoes the redacted marker (or omits the block) keeps
	// the token; a client-supplied token is ignored.
	for _, body := range []string{
		`{"name":"nightly","type":"heartbeat","weight":"2h","heartbeat":{"grace":"10m","token":"[REDACTED]"}}`,
		`{"name":"nightly","type":"heartbeat","weight":"2h","heartbeat":{"token":"` + strings.Repeat("b", 43) + `"}}`,
		`{"name":"nightly","type":"heartbeat","weight":"2h"}`,
	} {
		rec = serve(server, "PUT", "/api/v1/souls/"+soul.ID, []byte(body), map[string]string{"Authorization": "Bearer valid-token", "Content-Type": "application/json"})
		if rec.Code != http.StatusOK {
			t.Fatalf("update = %d %s", rec.Code, rec.Body.String())
		}
		if got := store.souls[soul.ID].Heartbeat.Token; got != token {
			t.Fatalf("update changed token for body %s", body)
		}
	}

	// Rotate: new token works, old one is refused at once.
	rec = serve(server, "POST", "/api/v1/souls/"+soul.ID+"/heartbeat/rotate", nil, map[string]string{"Authorization": "Bearer valid-token"})
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &info) != nil || info.Token == token || !core.ValidHeartbeatToken(info.Token) {
		t.Fatalf("rotate = %d %s", rec.Code, rec.Body.String())
	}
	if rec := serve(server, "GET", "/api/v1/heartbeat/"+token, nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("old token after rotate = %d", rec.Code)
	}
	if rec := serve(server, "GET", "/api/v1/heartbeat/"+info.Token, nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("new token after rotate = %d", rec.Code)
	}
	// Viewers cannot rotate.
	rec = serve(server, "POST", "/api/v1/souls/"+soul.ID+"/heartbeat/rotate", nil, map[string]string{"Authorization": "Bearer viewer-token"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer rotate = %d", rec.Code)
	}
}

// A token rotated on another node: this node's index still maps the old
// hash, but the stored soul no longer carries it, so the ping is refused.
func TestHeartbeatStaleIndexRechecksStoredToken(t *testing.T) {
	server, store, _, soul := newHeartbeatTestServer(t)
	old := soul.Heartbeat.Token
	if rec := serve(server, "GET", "/api/v1/heartbeat/"+old, nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("first ping = %d", rec.Code)
	}
	rotated := *soul
	rotated.Heartbeat = &core.HeartbeatConfig{Token: strings.Repeat("c", 43)}
	store.souls[soul.ID] = &rotated
	if rec := serve(server, "GET", "/api/v1/heartbeat/"+old, nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("stale token = %d", rec.Code)
	}
	// Deleted soul: refused too.
	delete(store.souls, soul.ID)
	if rec := serve(server, "GET", "/api/v1/heartbeat/"+rotated.Heartbeat.Token, nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("deleted soul token = %d", rec.Code)
	}
}

func TestHeartbeatCreateGeneratesTokenAndForceCheckConflicts(t *testing.T) {
	server, store, _, _ := newHeartbeatTestServer(t)
	body := `{"name":"cron","type":"heartbeat","weight":"5m","heartbeat":{"grace":"1m","token":"` + strings.Repeat("d", 43) + `"}}`
	rec := serve(server, "POST", "/api/v1/souls", []byte(body), map[string]string{"Authorization": "Bearer valid-token", "Content-Type": "application/json"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	s := store.souls[created.ID]
	if s == nil || s.Heartbeat == nil || !core.ValidHeartbeatToken(s.Heartbeat.Token) || s.Heartbeat.Token == strings.Repeat("d", 43) {
		t.Fatalf("created soul token not server-generated: %+v", s)
	}
	if strings.Contains(rec.Body.String(), s.Heartbeat.Token) {
		t.Fatal("create response leaks the token")
	}
	rec = serve(server, "POST", "/api/v1/souls/"+created.ID+"/check", nil, map[string]string{"Authorization": "Bearer valid-token"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("force check on heartbeat = %d", rec.Code)
	}
	// Reveal on a non-heartbeat soul is a 400.
	store.souls["http-1"] = &core.Soul{ID: "http-1", WorkspaceID: "default", Type: core.CheckHTTP}
	rec = serve(server, "GET", "/api/v1/souls/http-1/heartbeat", nil, map[string]string{"Authorization": "Bearer valid-token"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("reveal on http soul = %d", rec.Code)
	}
}

func TestRedactHeartbeatRequest(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/v1/heartbeat/secret-token-value/fail", nil)
	got := redactHeartbeatRequest(r)
	if got.URL.Path != "/api/v1/heartbeat/[REDACTED]/fail" || r.URL.Path != "/api/v1/heartbeat/secret-token-value/fail" {
		t.Fatalf("redacted path = %q (original %q)", got.URL.Path, r.URL.Path)
	}
	other := httptest.NewRequest("GET", "/api/v1/souls", nil)
	if redactHeartbeatRequest(other) != other {
		t.Fatal("non-heartbeat request was copied")
	}
	if sanitizeHeartbeatMessage("a\x00b\nc\x1b") != "ab c" {
		t.Fatalf("sanitize = %q", sanitizeHeartbeatMessage("a\x00b\nc\x1b"))
	}
}
