// Package mailtest provides a minimal in-process SMTP server for tests.
package mailtest

import (
	"bufio"
	"context"
	"io"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"strings"
	"sync"
	"testing"
	"time"
)

// Message is one message the server accepted.
type Message struct {
	From string
	To   []string
	Data string
}

// Header returns a header of the received message.
func (m Message) Header(name string) string {
	msg, err := mail.ReadMessage(strings.NewReader(m.Data))
	if err != nil {
		return ""
	}
	return msg.Header.Get(name)
}

// Body returns the message body with quoted-printable soft breaks and
// escapes decoded, so tests can search it for links.
func (m Message) Body() string {
	idx := strings.Index(m.Data, "\r\n\r\n")
	if idx < 0 {
		return m.Data
	}
	raw := m.Data[idx+4:]
	decoded, err := readAllQP(raw)
	if err != nil {
		return raw
	}
	return decoded
}

func readAllQP(raw string) (string, error) {
	b, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(raw)))
	return string(b), err
}

// Server is a fake SMTP relay listening on 127.0.0.1.
type Server struct {
	ln net.Listener

	mu       sync.Mutex
	messages []Message
	failNext int
	notify   chan struct{}
}

// NewServer starts a server and stops it when t ends.
func NewServer(t testing.TB) *Server {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("mailtest: listen: %v", err)
	}
	s := &Server{ln: ln, notify: make(chan struct{}, 1024)}
	go s.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

// Host is the listening IP.
func (s *Server) Host() string { return "127.0.0.1" }

// Port is the listening port.
func (s *Server) Port() int { return s.ln.Addr().(*net.TCPAddr).Port }

// FailNext makes the next n transactions fail with a transient 451 reply.
func (s *Server) FailNext(n int) {
	s.mu.Lock()
	s.failNext = n
	s.mu.Unlock()
}

// Messages returns the messages accepted so far.
func (s *Server) Messages() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Message(nil), s.messages...)
}

// WaitFor waits until at least n messages arrived or the timeout passes,
// and returns the messages accepted so far.
func (s *Server) WaitFor(n int, timeout time.Duration) []Message {
	deadline := time.After(timeout)
	for {
		if msgs := s.Messages(); len(msgs) >= n {
			return msgs
		}
		select {
		case <-s.notify:
		case <-deadline:
			return s.Messages()
		}
	}
}

func (s *Server) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	r := bufio.NewReader(conn)
	reply := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }
	reply("220 mailtest ESMTP")
	var cur Message
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			reply("250-mailtest")
			reply("250 8BITMIME")
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			s.mu.Lock()
			fail := s.failNext > 0
			if fail {
				s.failNext--
			}
			s.mu.Unlock()
			if fail {
				reply("451 try again later")
				continue
			}
			cur = Message{From: angleAddr(line[10:])}
			reply("250 OK")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			cur.To = append(cur.To, angleAddr(line[8:]))
			reply("250 OK")
		case cmd == "DATA":
			reply("354 go ahead")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				l = strings.TrimPrefix(l, ".")
				b.WriteString(l)
			}
			cur.Data = b.String()
			s.mu.Lock()
			s.messages = append(s.messages, cur)
			s.mu.Unlock()
			select {
			case s.notify <- struct{}{}:
			default:
			}
			cur = Message{}
			reply("250 queued")
		case cmd == "RSET":
			cur = Message{}
			reply("250 OK")
		case cmd == "NOOP":
			reply("250 OK")
		case cmd == "QUIT":
			reply("221 bye")
			return
		default:
			reply("502 not implemented")
		}
	}
}

// angleAddr extracts the address between < and > from a MAIL/RCPT argument.
func angleAddr(arg string) string {
	start := strings.IndexByte(arg, '<')
	end := strings.IndexByte(arg, '>')
	if start < 0 || end < start {
		return strings.TrimSpace(arg)
	}
	return arg[start+1 : end]
}
