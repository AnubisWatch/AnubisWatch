package auth

import (
	"crypto/rand"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

type alwaysErrorReader struct{}

func (alwaysErrorReader) Read([]byte) (int, error) { return 0, errors.New("injected entropy failure") }

type alwaysErrorBody struct{}

func (alwaysErrorBody) Read([]byte) (int, error) { return 0, errors.New("injected read failure") }

func TestCoverageOIDCEntropyFailure(t *testing.T) {
	oldReader := rand.Reader
	rand.Reader = alwaysErrorReader{}
	t.Cleanup(func() { rand.Reader = oldReader })

	if _, err := NewOIDCAuthenticator(core.OIDCAuth{}, "", "", ""); err == nil || !strings.Contains(err.Error(), "state HMAC") {
		t.Fatalf("NewOIDCAuthenticator error = %v", err)
	}
}

func TestCoverageBcryptFailures(t *testing.T) {
	oldCost := bcryptCost
	bcryptCost = 32
	t.Cleanup(func() { bcryptCost = oldCost })

	if _, err := HashPassword("password"); err == nil {
		t.Fatal("HashPassword should reject an invalid bcrypt cost")
	}
	if _, err := NewLocalAuthenticator("", "admin@example.com", "StrongPass123!"); err == nil || !strings.Contains(err.Error(), "hash admin") {
		t.Fatalf("NewLocalAuthenticator error = %v", err)
	}
}

func TestCoverageSessionPersistenceShapes(t *testing.T) {
	t.Run("nil lockout and expired records are ignored", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "sessions.json")
		now := timeNow()
		data := `{"tokens":{"expired":{"user_id":"u","expires_at":"` + now.Add(-1).Format(time.RFC3339Nano) + `"}},"users":{"u":{"id":"u"}},"reset_tokens":{"expired":{"email":"a@b.c","expires_at":"` + now.Add(-1).Format(time.RFC3339Nano) + `"}},"lockouts":{"nil":null,"stale":{"count":1,"last_try":"` + now.Add(-attemptResetWindow-time.Minute).Format(time.RFC3339Nano) + `"}}}`
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		a := newTestLocalAuth(t, path, "", "")
		if len(a.tokens) != 0 || len(a.resetTokens) != 0 || len(a.loginAttempts) != 0 {
			t.Fatalf("expired state loaded: tokens=%d resets=%d attempts=%d", len(a.tokens), len(a.resetTokens), len(a.loginAttempts))
		}
	})

	t.Run("nil lockout is omitted while saving", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "sessions.json")
		a := newTestLocalAuth(t, path, "", "")
		a.loginAttempts["nil"] = nil
		a.mu.Lock()
		a.saveSessionsLocked()
		a.mu.Unlock()
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("rename failure cleans temporary file", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "sessions")
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		a := newTestLocalAuth(t, "", "", "")
		a.sessionPath = path
		a.mu.Lock()
		a.saveSessionsLocked()
		a.mu.Unlock()
		if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
			t.Fatalf("temporary file not cleaned up: %v", err)
		}
	})
}

func TestCoverageOIDCURLAndBodyEdges(t *testing.T) {
	badEscape := "https://example.com/%zz"
	if _, _, err := normalizedIssuer(badEscape); err == nil {
		t.Fatal("normalizedIssuer accepted malformed escaping")
	}
	for _, raw := range []string{"http://localhost", "http://127.0.0.1", "http://[::1]"} {
		u, _, err := normalizedIssuer(raw)
		if err != nil || !issuerLoopback(u) {
			t.Fatalf("loopback %q: %v", raw, err)
		}
	}
	for _, tc := range []struct{ raw, want string }{
		{"https://example.com", "443"}, {"http://example.com", "80"}, {"ftp://example.com", ""}, {"https://example.com:8443", "8443"},
	} {
		u, _ := url.Parse(tc.raw)
		if got := effectivePort(u); got != tc.want {
			t.Errorf("effectivePort(%q)=%q want %q", tc.raw, got, tc.want)
		}
	}
	issuer, _ := url.Parse("https://issuer.example")
	if _, err := validateOIDCEndpoint(":bad", "token", issuer); err == nil {
		t.Fatal("accepted malformed endpoint")
	}
	if _, err := validateOIDCEndpoint("http://provider.example/token", "token", issuer); err == nil {
		t.Fatal("accepted insecure endpoint")
	}
	if err := decodeOIDCJSON(alwaysErrorBody{}, &map[string]any{}); err == nil || !strings.Contains(err.Error(), "read failure") {
		t.Fatalf("decode error=%v", err)
	}

	client := oidcHTTPClient(issuer)
	req, _ := http.NewRequest(http.MethodGet, "https://issuer.example/next", nil)
	via := make([]*http.Request, 10)
	if err := client.CheckRedirect(req, via); err == nil {
		t.Fatal("accepted excessive redirects")
	}
	req.URL, _ = url.Parse("http://issuer.example/next")
	if err := client.CheckRedirect(req, nil); err == nil {
		t.Fatal("accepted HTTPS downgrade")
	}
	req.URL, _ = url.Parse("https://other.example/next")
	if err := client.CheckRedirect(req, nil); err == nil {
		t.Fatal("accepted origin change")
	}
	req.URL, _ = url.Parse("https://issuer.example/next")
	if err := client.CheckRedirect(req, nil); err != nil {
		t.Fatalf("same-origin redirect: %v", err)
	}
}

func TestCoverageSessionJSONMarshalFailure(t *testing.T) {
	old := saveSessionsJSON
	saveSessionsJSON = func(interface{}) ([]byte, error) {
		return nil, errors.New("json injection")
	}
	t.Cleanup(func() { saveSessionsJSON = old })

	path := filepath.Join(t.TempDir(), "sessions.json")
	a := newTestLocalAuth(t, path, "", "")
	a.mu.Lock()
	a.saveSessionsLocked()
	a.mu.Unlock()
	// Should not panic; the error branch returns early.
}

// timeNow is a seam local to tests so timestamp construction remains readable.
func timeNow() time.Time { return time.Now() }

var _ io.Reader = alwaysErrorReader{}
