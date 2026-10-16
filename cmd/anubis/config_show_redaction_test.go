package main

import (
	"strings"
	"testing"
)

// `anubis config show` used to print the admin password and other secrets.
func TestRedactConfigSecrets(t *testing.T) {
	in := `{
  "auth": {"local": {"admin_email": "admin@x", "admin_password": "s3cr\"et!"}},
  "necropolis": {"cluster_secret": "abc", "empty_secret": ""},
  "env": {"password": "${ANUBIS_ADMIN_PASSWORD}"},
  "storage": {"encryption": {"key": "deadbeef"}},
  "zoo": {"monkey": "banana"}
}
auth:
  local:
    admin_password: hunter22
channels:
  - config: {bot_token: '123:abc', routing_key: xyz}
`
	out := redactConfigSecrets(in)
	for _, secret := range []string{`s3cr\"et!`, `"abc"`, "hunter22", "123:abc", "xyz", "deadbeef"} {
		if strings.Contains(out, secret) {
			t.Errorf("secret %q still visible:\n%s", secret, out)
		}
	}
	for _, keep := range []string{"admin@x", "${ANUBIS_ADMIN_PASSWORD}", `"empty_secret": ""`, `"monkey": "banana"`} {
		if !strings.Contains(out, keep) {
			t.Errorf("non-secret %q was masked:\n%s", keep, out)
		}
	}
}
