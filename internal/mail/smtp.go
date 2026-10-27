// Package mail delivers email over SMTP for AnubisWatch: the server-level
// relay (status page subscriptions, password resets) and the alert email
// channel share the transport here, so every SMTP connection dials through
// the SSRF policy in probe.SafeDialContext.
package mail

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"

	"github.com/AnubisWatch/anubiswatch/internal/probe"
)

// Server describes an SMTP relay.
type Server struct {
	Host string
	Port int
	// TLS is "auto" (or empty: implicit TLS on port 465, otherwise STARTTLS
	// when the server offers it), "implicit", "starttls" (required) or "none".
	TLS      string
	Username string
	Password string
}

// SendRaw delivers an already rendered RFC 5322 message, honouring ctx for
// the dial and all I/O.
//
// The SMTP host is operator input like any other outbound target: it is
// dialed through the SSRF policy so a relay cannot be pointed at loopback,
// metadata or internal addresses unless security.allow_private_targets /
// allowed_networks permit it. The dial is pinned to the validated IP, while
// TLS still verifies the configured host name.
func SendRaw(ctx context.Context, srv Server, from string, to []string, msg []byte) error {
	host := srv.Host
	if host == "" {
		return fmt.Errorf("smtp_host is empty")
	}
	for _, addr := range append([]string{from}, to...) {
		if strings.ContainsAny(addr, "\r\n") {
			return fmt.Errorf("invalid email address %q", addr)
		}
	}
	tlsMode := strings.ToLower(srv.TLS)
	if tlsMode == "" || tlsMode == "auto" {
		tlsMode = "auto"
		if srv.Port == 465 {
			tlsMode = "implicit"
		}
	}
	addr := net.JoinHostPort(host, strconv.Itoa(srv.Port))
	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}

	conn, err := probe.SafeDialContext(&net.Dialer{})(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	if tlsMode == "implicit" {
		tlsConn := tls.Client(conn, tlsConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			conn.Close()
			return err
		}
		conn = tlsConn
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return err
	}
	defer client.Close()

	if tlsMode == "auto" || tlsMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(tlsConfig); err != nil {
				return fmt.Errorf("STARTTLS: %w", err)
			}
		} else if tlsMode == "starttls" {
			return fmt.Errorf("server %s does not offer STARTTLS", host)
		}
	}
	if srv.Username != "" {
		if ok, _ := client.Extension("AUTH"); !ok {
			return fmt.Errorf("server %s does not support AUTH but credentials are configured", host)
		}
		if err := client.Auth(&plainAuth{username: srv.Username, password: srv.Password, host: host}); err != nil {
			return err
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// plainAuth implements SMTP AUTH PLAIN.
type plainAuth struct {
	identity string
	username string
	password string
	host     string
}

// isLocalSMTPHost mirrors net/smtp's isLocalhost: the standard library allows PLAIN
// credentials on a local relay without TLS.
func isLocalSMTPHost(name string) bool {
	switch name {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// Start answers the SMTP AUTH PLAIN challenge. net/smtp's PlainAuth refuses to send
// credentials over a connection that is neither TLS-protected nor local, and
// refuses a server presenting a name other than the configured host; this keeps
// both guards, so credentials are never sent in the clear to a server that
// advertises AUTH but not STARTTLS.
//
// A nil ServerInfo means the caller did not describe the connection; net/smtp always
// supplies one, so the checks apply on every real send.
func (a *plainAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if server != nil {
		if !server.TLS && !isLocalSMTPHost(server.Name) {
			return "", nil, fmt.Errorf("refusing to send SMTP credentials over an unencrypted connection to %q", server.Name)
		}
		if server.Name != a.host {
			return "", nil, fmt.Errorf("refusing to send SMTP credentials to %q, which is not the configured host %q", server.Name, a.host)
		}
	}
	return "PLAIN", []byte(a.identity + "\x00" + a.username + "\x00" + a.password), nil
}

func (a *plainAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	return nil, nil
}
