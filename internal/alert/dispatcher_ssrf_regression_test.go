package alert

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AnubisWatch/anubiswatch/internal/core"
	"github.com/AnubisWatch/anubiswatch/internal/probe"
)

// The package runs under a policy that allows private targets (see TestMain)
// because most dispatcher tests talk to httptest servers on 127.0.0.1. The
// tests below publish an explicit policy for their duration and restore the
// package-wide one afterwards.

// publishSSRFPolicy publishes a validator built from allowPrivate and
// allowedCIDRs, and restores the previously published one when t ends.
func publishSSRFPolicy(t *testing.T, allowPrivate bool, allowedCIDRs ...string) {
	t.Helper()
	// ConfigureDefaultValidator ORs the environment switch into the policy.
	t.Setenv("ANUBIS_SSRF_ALLOW_PRIVATE", "")
	previous := probe.DefaultValidator.Load()
	t.Cleanup(func() { probe.DefaultValidator.Store(previous) })
	if err := probe.ConfigureDefaultValidator(allowPrivate, allowedCIDRs); err != nil {
		t.Fatalf("ConfigureDefaultValidator: %v", err)
	}
}

// acceptCounter counts connections accepted by a loopback listener.
func acceptCounter(t *testing.T) (port int, accepted *atomic.Int32) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	accepted = &atomic.Int32{}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			conn.Close()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, accepted
}

// An email channel used to dial smtp_host with a plain net.Dialer, so any
// user who could create a channel could make the server connect to loopback
// or internal addresses (an internal port scan through "test channel").
func TestEmailDispatcherHonoursSSRFPolicy(t *testing.T) {
	tests := []struct {
		name         string
		allowPrivate bool
		allowed      []string
		wantConnect  bool
	}{
		{name: "default policy refuses loopback", wantConnect: false},
		{name: "allow_private_targets permits loopback", allowPrivate: true, wantConnect: true},
		{name: "allowed_networks permits loopback", allowed: []string{"127.0.0.0/8"}, wantConnect: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			publishSSRFPolicy(t, tt.allowPrivate, tt.allowed...)
			port, accepted := acceptCounter(t)
			channel := &core.AlertChannel{Config: map[string]any{
				"smtp_host": "127.0.0.1",
				"smtp_port": float64(port),
				"from":      "alerts@example.com",
				"to":        []any{"ops@example.com"},
			}}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			err := (&EmailDispatcher{logger: newTestLogger()}).Send(ctx, &core.AlertEvent{SoulName: "x"}, channel)
			if err == nil {
				t.Fatal("Send succeeded against a listener that speaks no SMTP")
			}
			// Give the accept loop a moment to record a connection.
			deadline := time.Now().Add(500 * time.Millisecond)
			for tt.wantConnect && accepted.Load() == 0 && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			if got := accepted.Load() > 0; got != tt.wantConnect {
				t.Fatalf("connected = %v, want %v (send err: %v)", got, tt.wantConnect, err)
			}
			if !tt.wantConnect && !strings.Contains(err.Error(), "SSRF") {
				t.Fatalf("expected an SSRF refusal, got %v", err)
			}
		})
	}
}

func TestEmailDispatcherValidateRejectsBlockedHosts(t *testing.T) {
	tests := []struct {
		name         string
		host         any
		allowPrivate bool
		wantErr      bool
	}{
		{name: "missing host", host: nil, wantErr: true},
		{name: "empty host", host: "  ", wantErr: true},
		{name: "loopback", host: "127.0.0.1", wantErr: true},
		{name: "IPv6 loopback", host: "::1", wantErr: true},
		{name: "bracketed IPv6 loopback", host: "[::1]", wantErr: true},
		{name: "private", host: "10.1.2.3", wantErr: true},
		{name: "metadata", host: "169.254.169.254", wantErr: true},
		{name: "metadata with allow_private", host: "169.254.169.254", allowPrivate: true, wantErr: true},
		{name: "AWS IPv6 metadata with allow_private", host: "fd00:ec2::254", allowPrivate: true, wantErr: true},
		{name: "loopback with allow_private", host: "127.0.0.1", allowPrivate: true, wantErr: false},
		{name: "public IP", host: "8.8.8.8", wantErr: false},
		// Host names are enforced at dial time; saving must not need DNS.
		{name: "host name", host: "smtp.example.com", wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			publishSSRFPolicy(t, tt.allowPrivate)
			config := map[string]any{"to": []any{"ops@example.com"}}
			if tt.host != nil {
				config["smtp_host"] = tt.host
			}
			err := (&EmailDispatcher{}).Validate(config)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate(%v) error = %v, wantErr %v", tt.host, err, tt.wantErr)
			}
		})
	}
}

// Implicit TLS must still present the configured host name (SNI and
// certificate verification) although the dial is pinned to a literal IP.
func TestEmailImplicitTLSKeepsServerName(t *testing.T) {
	addrs, err := net.DefaultResolver.LookupHost(context.Background(), "localhost")
	if err != nil || len(addrs) == 0 || addrs[0] != "127.0.0.1" {
		t.Skipf("localhost does not resolve to 127.0.0.1 first (%v, %v)", addrs, err)
	}
	publishSSRFPolicy(t, true)

	certSrv := httptest.NewTLSServer(http.NotFoundHandler())
	certSrv.Close()
	serverName := make(chan string, 1)
	cfg := certSrv.TLS.Clone()
	cfg.GetConfigForClient = func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		serverName <- hello.ServerName
		return nil, nil
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.(*tls.Conn).Handshake()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	port := ln.Addr().(*net.TCPAddr).Port
	err = sendSMTP(ctx, "localhost", port, "implicit", "", "", "a@example.com", []string{"b@example.com"}, []byte("x"))
	if err == nil {
		t.Fatal("handshake with an untrusted test certificate succeeded")
	}
	select {
	case got := <-serverName:
		if got != "localhost" {
			t.Fatalf("SNI = %q, want %q", got, "localhost")
		}
	case <-time.After(time.Second):
		t.Fatalf("server saw no ClientHello (send err: %v)", err)
	}
	if strings.Contains(err.Error(), "SSRF") {
		t.Fatalf("permitted host was refused: %v", err)
	}
}

// HTTP dispatchers validated the URL with one DNS lookup and then let the
// default transport resolve the host again, so a rebinding name could pass
// validation and connect to an internal address. The shared client must
// enforce the policy on the address it actually dials.
func TestDispatcherHTTPClientEnforcesPolicyAtDialTime(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	port := u.Port()

	tests := []struct {
		name         string
		host         string
		allowPrivate bool
		wantOK       bool
	}{
		// "localhost" stands in for a name that rebinds to loopback after
		// validation: the request goes straight to the client, so only the
		// dialer stands between it and the internal service.
		{name: "host name resolving to loopback is refused", host: "localhost", wantOK: false},
		{name: "loopback literal is refused", host: "127.0.0.1", wantOK: false},
		{name: "decimal loopback literal is refused", host: "2130706433", wantOK: false},
		{name: "NAT64 loopback is refused", host: "[64:ff9b::7f00:1]", allowPrivate: false, wantOK: false},
		{name: "loopback allowed by policy", host: "127.0.0.1", allowPrivate: true, wantOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			publishSSRFPolicy(t, tt.allowPrivate)
			before := hits.Load()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+net.JoinHostPort(strings.Trim(tt.host, "[]"), port)+"/hook", nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := getHTTPClient().Do(req)
			if resp != nil {
				closeResponseBody(resp)
			}
			if tt.wantOK {
				if err != nil {
					t.Fatalf("request failed: %v", err)
				}
				if hits.Load() == before {
					t.Fatal("server was not reached")
				}
				return
			}
			if err == nil {
				t.Fatal("request to a blocked address succeeded")
			}
			if !strings.Contains(err.Error(), "SSRF") {
				t.Fatalf("expected an SSRF refusal, got %v", err)
			}
			if hits.Load() != before {
				t.Fatal("blocked request reached the server")
			}
		})
	}
}

// An environment proxy would receive the request and connect to the alert
// target itself, so the dial-time policy would never see the real address.
func TestDispatcherHTTPClientIgnoresEnvironmentProxy(t *testing.T) {
	transport, ok := getHTTPClient().Transport.(*http.Transport)
	if !ok {
		t.Fatalf("dispatcher client transport is %T, want *http.Transport", getHTTPClient().Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("dispatcher transport honours an environment proxy, bypassing the SSRF dialer")
	}
	if transport.DialContext == nil {
		t.Fatal("dispatcher transport has no SSRF-checking dialer")
	}
}
