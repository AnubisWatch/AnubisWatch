package alert

import (
	"net/smtp"
	"strings"
	"testing"

	"github.com/AnubisWatch/anubiswatch/internal/core"
)

// net/smtp.PlainAuth documents that it "will only send the credentials if the
// connection is using TLS or is connected to localhost", and it refuses a server
// presenting a name other than the configured host. The hand-rolled plainAuth in
// dispatchers.go reimplemented PLAIN without either guard, so EmailDispatcher.Send
// passed the operator's SMTP username and password to smtp.SendMail, which hands
// them over in the clear whenever the server advertises AUTH but not STARTTLS.
//
// Credentials are generated per run so no credential-shaped literal lives here.

func plainAuthRegression() (*plainAuth, string) {
	password := "pw-" + core.GenerateID() + "-" + core.GenerateID()
	return &plainAuth{
		username: "alerts-" + core.GenerateID() + "@example.com",
		password: password,
		host:     "smtp.example.com",
	}, password
}

func TestPlainAuthRefusesCredentialsOverAnUnencryptedConnection(t *testing.T) {
	auth, password := plainAuthRegression()

	_, resp, err := auth.Start(&smtp.ServerInfo{Name: "smtp.example.com", TLS: false})
	if err == nil {
		t.Fatalf("Start() accepted an unencrypted connection and returned %q", string(resp))
	}
	if strings.Contains(string(resp), password) {
		t.Errorf("Start() returned the password despite refusing: %q", string(resp))
	}
}

func TestPlainAuthRefusesCredentialsForAnotherHost(t *testing.T) {
	auth, password := plainAuthRegression()

	_, resp, err := auth.Start(&smtp.ServerInfo{Name: "attacker.example", TLS: true})
	if err == nil {
		t.Fatalf("Start() accepted a server that is not the configured host and returned %q", string(resp))
	}
	if strings.Contains(string(resp), password) {
		t.Errorf("Start() returned the password despite refusing: %q", string(resp))
	}
}

// TestPlainAuthStillAuthenticatesWhereTheStandardLibraryDoes is the neighbouring
// control: the fix must not refuse the connections net/smtp allows, or SMTP alerts
// would stop working entirely.
func TestPlainAuthStillAuthenticatesWhereTheStandardLibraryDoes(t *testing.T) {
	auth, password := plainAuthRegression()

	mechanism, resp, err := auth.Start(&smtp.ServerInfo{Name: "smtp.example.com", TLS: true})
	if err != nil {
		t.Fatalf("Start() refused a TLS connection: %v", err)
	}
	if mechanism != "PLAIN" || !strings.Contains(string(resp), password) {
		t.Errorf("Start() did not offer the credentials over TLS (mechanism %q)", mechanism)
	}

	for _, name := range []string{"localhost", "127.0.0.1", "::1"} {
		local := &plainAuth{username: auth.username, password: password, host: name}
		if _, resp, err := local.Start(&smtp.ServerInfo{Name: name, TLS: false}); err != nil {
			t.Errorf("Start() refused the local relay %q, which net/smtp allows: %v", name, err)
		} else if !strings.Contains(string(resp), password) {
			t.Errorf("Start() did not offer the credentials to the local relay %q", name)
		}
	}
}
