package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	gomail "github.com/wneessen/go-mail"
)

// SMTPConfig is how to reach the SMTP server and who mail comes from. The
// values come from config, which has already checked them.
type SMTPConfig struct {
	Host               string
	Port               int
	TLS                string // "starttls", "tls" or "none"
	Username, Password string
	FromAddress        string // bare, e.g. no-reply@example.com
	FromName           string
}

// SMTPDriver sends each message over its own SMTP connection. TLS is
// required unless the config says none (a local test inbox), STARTTLS is
// never optional, and certificates are always verified.
type SMTPDriver struct {
	config SMTPConfig
	domain string // the From address's, for Message-IDs
	// Tests only: a CA to trust besides the system's, and a shorter timeout
	// for each step of the SMTP conversation.
	rootCAs *x509.CertPool
	timeout time.Duration
}

func NewSMTPDriver(c SMTPConfig) (*SMTPDriver, error) {
	at := strings.LastIndex(c.FromAddress, "@")
	if c.Host == "" || at < 0 {
		return nil, errors.New("mail: SMTP needs a host and a From address")
	}
	if c.TLS != "starttls" && c.TLS != "tls" && c.TLS != "none" {
		return nil, fmt.Errorf("mail: unknown SMTP TLS mode %q", c.TLS)
	}
	return &SMTPDriver{config: c, domain: c.FromAddress[at+1:], timeout: 10 * time.Second}, nil
}

// Deliver sends m within ctx (the listener's 30 s). An error, including a
// refusal from the server, leaves the delivery to the event worker's retries.
func (d *SMTPDriver) Deliver(ctx context.Context, m Message) error {
	msg, err := d.message(m)
	if err != nil {
		return err
	}
	// A client per message: listeners run concurrently, and a connection
	// per message is cheap at this volume.
	client, err := gomail.NewClient(d.config.Host, d.options()...)
	if err != nil {
		return fmt.Errorf("smtp: %w", err)
	}
	if err := client.DialAndSendWithContext(ctx, msg); err != nil {
		return fmt.Errorf("smtp: %w", err)
	}
	slog.Info("mail sent", "template", m.Template, "message_id", msg.GetMessageID())
	return nil
}

// message builds the email: From, To, Subject, Date, Message-ID and
// Auto-Submitted headers, List-Unsubscribe for a blockable email, the
// plain-text part and its HTML alternative, and the HTML's inline images
// (the logo). Nothing in a header comes from user data: List-Unsubscribe is
// APP_ORIGIN and a base64url token.
func (d *SMTPDriver) message(m Message) (*gomail.Msg, error) {
	msg := gomail.NewMsg(gomail.WithNoDefaultUserAgent())
	if err := msg.FromFormat(d.config.FromName, d.config.FromAddress); err != nil {
		return nil, fmt.Errorf("smtp: From: %w", err)
	}
	if err := msg.To(m.To); err != nil {
		return nil, fmt.Errorf("smtp: To: %w", err)
	}
	msg.Subject(m.Subject)
	msg.SetDate()
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	msg.SetMessageIDWithValue(hex.EncodeToString(id) + "@" + d.domain)
	// RFC 3834: tells out-of-office and other auto-replies not to answer.
	msg.SetGenHeader(gomail.Header("Auto-Submitted"), "auto-generated")
	if m.Unsubscribe != "" {
		if strings.ContainsAny(m.Unsubscribe, "\r\n<>") {
			return nil, errors.New("smtp: List-Unsubscribe: invalid link")
		}
		// RFC 2369's URL form only: the link opens a page that asks first.
		// No List-Unsubscribe-Post, whose one click would block at once.
		msg.SetGenHeader(gomail.HeaderListUnsubscribe, "<"+m.Unsubscribe+">")
	}
	msg.SetBodyString(gomail.TypeTextPlain, m.Text)
	msg.AddAlternativeString(gomail.TypeTextHTML, m.HTML)
	for _, image := range m.Inline {
		// go-mail writes a given Content-ID as it is; RFC 2392 wants <…>.
		err := msg.EmbedReader(image.Name, bytes.NewReader(image.Data),
			gomail.WithFileContentID("<"+image.ContentID+">"), gomail.WithFileContentType(gomail.ContentType("image/png")))
		if err != nil {
			return nil, fmt.Errorf("smtp: embed %s: %w", image.Name, err)
		}
	}
	return msg, nil
}

func (d *SMTPDriver) options() []gomail.Option {
	options := []gomail.Option{
		gomail.WithPort(d.config.Port),
		gomail.WithTimeout(d.timeout),
		gomail.WithTLSConfig(&tls.Config{ServerName: d.config.Host, MinVersion: tls.VersionTLS12, RootCAs: d.rootCAs}),
	}
	switch d.config.TLS {
	case "starttls":
		options = append(options, gomail.WithTLSPolicy(gomail.TLSMandatory))
	case "tls":
		options = append(options, gomail.WithSSL())
	case "none":
		options = append(options, gomail.WithTLSPolicy(gomail.NoTLS))
	}
	// PLAIN, which go-mail only sends over TLS; config refuses credentials
	// with none anyway.
	if d.config.Username != "" {
		options = append(options,
			gomail.WithSMTPAuth(gomail.SMTPAuthPlain),
			gomail.WithUsername(d.config.Username),
			gomail.WithPassword(d.config.Password))
	}
	return options
}
