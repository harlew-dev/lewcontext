package mail

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"io"
	"math/big"
	"mime"
	"mime/multipart"
	"net"
	netmail "net/mail"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSMTP is just enough of an SMTP server to see what the driver sends:
// EHLO, optional STARTTLS (or TLS from the start), AUTH PLAIN, MAIL, RCPT and
// DATA, each answered with 250 unless a test sets a refusal.
type fakeSMTP struct {
	addr     *net.TCPAddr
	cert     tls.Certificate
	startTLS bool // offer STARTTLS
	implicit bool // TLS from the first byte
	auth     bool // offer AUTH PLAIN
	silent   bool // accept connections and never answer
	replies  map[string]string

	mu  sync.Mutex
	got received
}

// received is what the server was sent, read through fakeSMTP.received.
type received struct {
	mailFrom, rcptTo []string
	data             string
	authPlain        string // decoded \x00user\x00pass
	authOverTLS      bool
}

func (s *fakeSMTP) received() received {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.got
}

func newFakeSMTP(t *testing.T, configure func(*fakeSMTP)) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeSMTP{addr: ln.Addr().(*net.TCPAddr), cert: testCertificate(t), replies: map[string]string{}}
	if configure != nil {
		configure(s)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(conn)
		}
	}()
	return s
}

func (s *fakeSMTP) reply(command, fallback string) string {
	if r, ok := s.replies[command]; ok {
		return r
	}
	return fallback
}

func (s *fakeSMTP) serve(conn net.Conn) {
	defer conn.Close()
	if s.silent {
		_, _ = io.Copy(io.Discard, conn)
		return
	}
	encrypted := false
	if s.implicit {
		conn = tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{s.cert}})
		encrypted = true
	}
	r := bufio.NewReader(conn)
	write := func(line string) { _, _ = io.WriteString(conn, line+"\r\n") }
	write("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		verb := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		switch {
		case verb == "EHLO" || verb == "HELO":
			lines := []string{"fake"}
			if s.startTLS && !encrypted {
				lines = append(lines, "STARTTLS")
			}
			if s.auth {
				lines = append(lines, "AUTH PLAIN")
			}
			lines = append(lines, "8BITMIME")
			for i, l := range lines {
				sep := "-"
				if i == len(lines)-1 {
					sep = " "
				}
				write("250" + sep + l)
			}
		case verb == "STARTTLS":
			write("220 go ahead")
			tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{s.cert}})
			if tlsConn.Handshake() != nil {
				return
			}
			conn, r, encrypted = tlsConn, bufio.NewReader(tlsConn), true
		case verb == "AUTH":
			fields := strings.Fields(line)
			decoded, _ := base64.StdEncoding.DecodeString(fields[len(fields)-1])
			s.mu.Lock()
			s.got.authPlain, s.got.authOverTLS = string(decoded), encrypted
			s.mu.Unlock()
			write(s.reply("AUTH", "235 2.7.0 accepted"))
		case verb == "MAIL":
			s.mu.Lock()
			s.got.mailFrom = append(s.got.mailFrom, line)
			s.mu.Unlock()
			write(s.reply("MAIL", "250 ok"))
		case verb == "RCPT":
			s.mu.Lock()
			s.got.rcptTo = append(s.got.rcptTo, line)
			s.mu.Unlock()
			write(s.reply("RCPT", "250 ok"))
		case verb == "DATA":
			write("354 go ahead")
			var body strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				body.WriteString(strings.TrimPrefix(l, "."))
			}
			s.mu.Lock()
			s.got.data = body.String()
			s.mu.Unlock()
			write(s.reply("DATA", "250 queued"))
		case verb == "QUIT":
			write("221 bye")
			return
		case verb == "NOOP" || verb == "RSET":
			write("250 ok")
		default:
			write("502 unknown command")
		}
	}
}

// testCertificate is a self-signed certificate for 127.0.0.1.
func testCertificate(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "fake smtp"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// smtpMailer sends through s with mode ("starttls", "tls" or "none"), trusting
// s's certificate when trust is set.
func smtpMailer(t *testing.T, s *fakeSMTP, mode string, trust bool, username, password string) (*Mailer, *SMTPDriver) {
	t.Helper()
	d, err := NewSMTPDriver(SMTPConfig{
		Host: "127.0.0.1", Port: s.addr.Port, TLS: mode,
		Username: username, Password: password,
		FromAddress: "no-reply@example.com", FromName: "Acme",
	})
	if err != nil {
		t.Fatal(err)
	}
	d.timeout = 2 * time.Second
	if trust {
		leaf, err := x509.ParseCertificate(s.cert.Certificate[0])
		if err != nil {
			t.Fatal(err)
		}
		d.rootCAs = x509.NewCertPool()
		d.rootCAs.AddCert(leaf)
	}
	m, err := New(testOptions(d, &blockList{}, "Acme"))
	if err != nil {
		t.Fatal(err)
	}
	return m, d
}

func sendReset(m *Mailer, name string) error {
	return m.Send(context.Background(), "ada@example.com", ForgotPassword, ForgotPasswordData{Name: name, URL: "https://app.example.com/auth/forgot-password/abc"})
}

func TestSMTPMessage(t *testing.T) {
	s := newFakeSMTP(t, nil)
	m, _ := smtpMailer(t, s, "none", false, "", "")
	// A name can't reach a header: it's only ever in the body.
	if err := sendReset(m, "Ada\r\nBcc: eve@example.com"); err != nil {
		t.Fatal(err)
	}
	got := s.received()
	if len(got.rcptTo) != 1 || !strings.Contains(got.rcptTo[0], "<ada@example.com>") {
		t.Fatalf("recipients: %v", got.rcptTo)
	}
	msg, err := netmail.ReadMessage(strings.NewReader(got.data))
	if err != nil {
		t.Fatal(err)
	}
	h := msg.Header
	from, err := netmail.ParseAddress(h.Get("From"))
	if err != nil || from.Address != "no-reply@example.com" || from.Name != "Acme" {
		t.Errorf("From: %q", h.Get("From"))
	}
	if h.Get("To") != "<ada@example.com>" || h.Get("Subject") != subjects[ForgotPassword] || h.Get("Auto-Submitted") != "auto-generated" {
		t.Errorf("To %q, Subject %q, Auto-Submitted %q", h.Get("To"), h.Get("Subject"), h.Get("Auto-Submitted"))
	}
	if _, err := h.Date(); err != nil {
		t.Errorf("Date: %v", err)
	}
	if id := h.Get("Message-Id"); !strings.HasSuffix(id, "@example.com>") || len(id) < 40 {
		t.Errorf("Message-ID: %q", id)
	}
	for _, name := range []string{"Bcc", "User-Agent", "X-Mailer"} {
		if h.Get(name) != "" {
			t.Errorf("unexpected %s header: %q", name, h.Get(name))
		}
	}
	// Plain text first, then HTML, both with the link, and the logo the HTML
	// shows embedded as a PNG with the Content-ID it refers to.
	leaves := mimeLeaves(t, h.Get("Content-Type"), msg.Body)
	if len(leaves) != 3 {
		t.Fatalf("%d parts: %+v", len(leaves), leaves)
	}
	link := "https://app.example.com/auth/forgot-password/abc"
	if leaves[0].contentType != "text/plain" || !strings.Contains(leaves[0].body, link) {
		t.Errorf("first part: %s\n%s", leaves[0].contentType, leaves[0].body)
	}
	if leaves[1].contentType != "text/html" || !strings.Contains(leaves[1].body, link) || !strings.Contains(leaves[1].body, `src="cid:logo"`) {
		t.Errorf("second part: %s\n%s", leaves[1].contentType, leaves[1].body)
	}
	if leaves[2].contentType != "image/png" || leaves[2].contentID != "<logo>" || !strings.HasPrefix(leaves[2].body, "\x89PNG") {
		t.Errorf("logo part: %s %s, %d bytes", leaves[2].contentType, leaves[2].contentID, len(leaves[2].body))
	}
}

// mimeLeaf is one non-multipart part of a message, decoded.
type mimeLeaf struct{ contentType, contentID, body string }

// mimeLeaves walks nested multipart bodies in order.
func mimeLeaves(t *testing.T, contentType string, body io.Reader) []mimeLeaf {
	t.Helper()
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		t.Fatalf("Content-Type %q: %v", contentType, err)
	}
	if !strings.HasPrefix(mediaType, "multipart/") {
		raw, _ := io.ReadAll(body)
		return []mimeLeaf{{contentType: mediaType, body: string(raw)}}
	}
	var leaves []mimeLeaf
	parts := multipart.NewReader(body, params["boundary"])
	for {
		part, err := parts.NextPart()
		if err == io.EOF {
			return leaves
		}
		if err != nil {
			t.Fatal(err)
		}
		var content io.Reader = part
		if strings.EqualFold(part.Header.Get("Content-Transfer-Encoding"), "base64") {
			content = base64.NewDecoder(base64.StdEncoding, part)
		}
		inner := mimeLeaves(t, part.Header.Get("Content-Type"), content)
		if len(inner) == 1 {
			inner[0].contentID = part.Header.Get("Content-Id")
		}
		leaves = append(leaves, inner...)
	}
}

func TestSMTPStartTLSIsRequired(t *testing.T) {
	s := newFakeSMTP(t, nil) // offers no STARTTLS
	m, _ := smtpMailer(t, s, "starttls", true, "", "")
	if err := sendReset(m, "Ada"); err == nil {
		t.Fatal("sent without TLS")
	}
	if got := s.received(); len(got.mailFrom) != 0 {
		t.Fatalf("the message started: %v", got.mailFrom)
	}
}

func TestSMTPStartTLSAndAuth(t *testing.T) {
	s := newFakeSMTP(t, func(s *fakeSMTP) { s.startTLS, s.auth = true, true })
	m, _ := smtpMailer(t, s, "starttls", true, "resend", "re_secret")
	if err := sendReset(m, "Ada"); err != nil {
		t.Fatal(err)
	}
	got := s.received()
	if !got.authOverTLS || got.authPlain != "\x00resend\x00re_secret" {
		t.Fatalf("auth %q, over TLS %v", got.authPlain, got.authOverTLS)
	}
	if got.data == "" {
		t.Fatal("nothing sent")
	}
}

func TestSMTPUntrustedCertificate(t *testing.T) {
	s := newFakeSMTP(t, func(s *fakeSMTP) { s.startTLS = true })
	m, _ := smtpMailer(t, s, "starttls", false, "", "")
	if err := sendReset(m, "Ada"); err == nil {
		t.Fatal("accepted a certificate nobody trusts")
	}
	if got := s.received(); len(got.mailFrom) != 0 {
		t.Fatalf("the message started: %v", got.mailFrom)
	}
}

func TestSMTPImplicitTLS(t *testing.T) {
	s := newFakeSMTP(t, func(s *fakeSMTP) { s.implicit, s.auth = true, true })
	m, _ := smtpMailer(t, s, "tls", true, "user", "pass")
	if err := sendReset(m, "Ada"); err != nil {
		t.Fatal(err)
	}
	if got := s.received(); !got.authOverTLS || got.data == "" {
		t.Fatalf("auth over TLS %v, sent %v", got.authOverTLS, got.data != "")
	}
}

func TestSMTPRefusals(t *testing.T) {
	for command, refusal := range map[string]string{
		"AUTH": "535 5.7.8 authentication failed",
		"RCPT": "550 5.1.1 no such mailbox",
		"DATA": "451 4.3.0 try again later",
	} {
		s := newFakeSMTP(t, func(s *fakeSMTP) {
			s.startTLS, s.auth = true, true
			s.replies[command] = refusal
		})
		m, _ := smtpMailer(t, s, "starttls", true, "resend", "re_secret")
		err := sendReset(m, "Ada")
		if err == nil {
			t.Errorf("%s %q: no error", command, refusal)
			continue
		}
		if strings.Contains(err.Error(), "re_secret") {
			t.Errorf("%s: the error shows the password: %v", command, err)
		}
	}
}

func TestSMTPServerThatNeverAnswers(t *testing.T) {
	s := newFakeSMTP(t, func(s *fakeSMTP) { s.silent = true })
	m, d := smtpMailer(t, s, "none", false, "", "")
	d.timeout = 300 * time.Millisecond
	start := time.Now()
	if err := sendReset(m, "Ada"); err == nil {
		t.Fatal("no error from a silent server")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("took %v", elapsed)
	}
}
