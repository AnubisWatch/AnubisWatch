package mail

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	netmail "net/mail"
	"sort"
	"strings"
	"time"
)

// ErrHeaderInjection reports an address or header value carrying a line
// break, which would let the value start a header of its own.
var ErrHeaderInjection = errors.New("mail: line break in header value")

// Message is one email to a single recipient. Text is required; HTML is
// optional and sent as a multipart/alternative part.
type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
	// Headers are extra headers (for example List-Unsubscribe). Names must be
	// header tokens; values must not contain line breaks.
	Headers map[string]string
	// Kind labels the message in logs ("subscription_confirm", ...). It is
	// never sent.
	Kind string
}

// rendered is a message ready for the SMTP envelope.
type rendered struct {
	from  string
	to    string
	bytes []byte
}

// render builds the RFC 5322 message from the sender address. Every header
// value is checked for CR/LF, and the subject is RFC 2047-encoded.
func (m Message) render(from *netmail.Address, now time.Time) (*rendered, error) {
	if strings.ContainsAny(m.To, "\r\n") {
		return nil, ErrHeaderInjection
	}
	to, err := netmail.ParseAddress(strings.TrimSpace(m.To))
	if err != nil {
		return nil, fmt.Errorf("mail: invalid recipient: %w", err)
	}
	if strings.ContainsAny(to.Address, "\r\n") || strings.ContainsAny(from.Address, "\r\n") {
		return nil, ErrHeaderInjection
	}
	if strings.ContainsAny(m.Subject, "\r\n") {
		return nil, ErrHeaderInjection
	}
	names := make([]string, 0, len(m.Headers))
	for name, value := range m.Headers {
		if !validHeaderName(name) {
			return nil, fmt.Errorf("mail: invalid header name %q", name)
		}
		if strings.ContainsAny(value, "\r\n") {
			return nil, ErrHeaderInjection
		}
		names = append(names, name)
	}
	sort.Strings(names)

	domain := "anubis.local"
	if at := strings.LastIndex(from.Address, "@"); at >= 0 && at < len(from.Address)-1 {
		domain = from.Address[at+1:]
	}

	var b bytes.Buffer
	writeHeader := func(name, value string) {
		b.WriteString(name + ": " + value + "\r\n")
	}
	writeHeader("From", from.String())
	writeHeader("To", to.String())
	writeHeader("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	writeHeader("Date", now.UTC().Format(time.RFC1123Z))
	writeHeader("Message-ID", "<"+randomHex(16)+"@"+domain+">")
	writeHeader("MIME-Version", "1.0")
	writeHeader("Auto-Submitted", "auto-generated")
	for _, name := range names {
		writeHeader(name, m.Headers[name])
	}

	if m.HTML == "" {
		writeHeader("Content-Type", "text/plain; charset=UTF-8")
		writeHeader("Content-Transfer-Encoding", "quoted-printable")
		b.WriteString("\r\n")
		if err := writeQP(&b, m.Text); err != nil {
			return nil, err
		}
	} else {
		boundary := "anubis-" + randomHex(12)
		writeHeader("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
		b.WriteString("\r\n")
		for _, part := range []struct{ ctype, body string }{
			{"text/plain; charset=UTF-8", m.Text},
			{"text/html; charset=UTF-8", m.HTML},
		} {
			b.WriteString("--" + boundary + "\r\n")
			writeHeader("Content-Type", part.ctype)
			writeHeader("Content-Transfer-Encoding", "quoted-printable")
			b.WriteString("\r\n")
			if err := writeQP(&b, part.body); err != nil {
				return nil, err
			}
			b.WriteString("\r\n")
		}
		b.WriteString("--" + boundary + "--\r\n")
	}
	return &rendered{from: from.Address, to: to.Address, bytes: b.Bytes()}, nil
}

func writeQP(b *bytes.Buffer, body string) error {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\n", "\r\n")
	w := quotedprintable.NewWriter(b)
	if _, err := w.Write([]byte(body)); err != nil {
		return err
	}
	return w.Close()
}

func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r <= ' ' || r >= 0x7f || r == ':' {
			return false
		}
	}
	return true
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand does not fail on supported platforms; fall back to time.
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
