package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunAPIKeyCommand(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		switch {
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`[{"id":"abc","name":"ci","role":"viewer","status":"active"}]`))
		case r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"abc","role":"viewer","key":"anb_abc_secret"}`))
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/missing"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"API key not found"}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()

	tests := []struct {
		name               string
		args               []string
		token              string
		wantErr            string
		wantOut            string
		wantMethod, wantAt string
		wantBody           string
	}{
		{name: "help", args: nil, wantOut: "Usage: anubis apikey"},
		{name: "needs login", args: []string{"list"}, wantErr: "anubis login"},
		{name: "list", args: []string{"list"}, token: "sess", wantOut: "ci", wantMethod: "GET", wantAt: "/api/v1/api-keys"},
		{name: "create", args: []string{"create", "--name", "ci", "--role", "viewer", "--expires-in-days", "30"}, token: "sess",
			wantOut: "anb_abc_secret", wantMethod: "POST", wantAt: "/api/v1/api-keys", wantBody: `"expires_in_days":30`},
		{name: "create needs name", args: []string{"create"}, token: "sess", wantErr: "--name"},
		{name: "revoke", args: []string{"revoke", "abc"}, token: "sess", wantOut: "Revoked API key abc", wantMethod: "DELETE", wantAt: "/api/v1/api-keys/abc"},
		{name: "revoke error", args: []string{"revoke", "missing"}, token: "sess", wantErr: "API key not found"},
		{name: "unknown", args: []string{"rotate"}, token: "sess", wantErr: "unknown apikey command"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMethod, gotPath, gotAuth, gotBody = "", "", "", ""
			var out bytes.Buffer
			err := runAPIKeyCommand(tt.args, srv.URL, tt.token, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(out.String(), tt.wantOut) {
				t.Fatalf("output %q lacks %q", out.String(), tt.wantOut)
			}
			if tt.wantMethod != "" && (gotMethod != tt.wantMethod || gotPath != tt.wantAt || gotAuth != "Bearer "+tt.token) {
				t.Fatalf("request = %s %s (%s)", gotMethod, gotPath, gotAuth)
			}
			if tt.wantBody != "" && !strings.Contains(gotBody, tt.wantBody) {
				t.Fatalf("body %q lacks %q", gotBody, tt.wantBody)
			}
		})
	}
}
