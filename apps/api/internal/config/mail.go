package config

import (
	"errors"
	"net/mail"
	"os"
	"strconv"
	"strings"
)

// Mail is how email is delivered. MailSMTP sends it through an SMTP server;
// MailLog appends it to MailFile, a development mailbox; MailOff sends nothing,
// so password reset and invitations are unavailable.
type Mail struct {
	MailDriver string
	// MailFile is the MailLog mailbox.
	MailFile string
	// MailFromAddress and MailFromName make the From header. The address is
	// bare (no display name), so neither can add a header.
	MailFromAddress, MailFromName string
	SMTP                          SMTP
}

// SMTP is how to reach the SMTP server. Error messages name these settings
// but never print their values, so the password can't reach a log.
type SMTP struct {
	Host               string
	Port               int
	TLS                string // SMTPStartTLS, SMTPImplicitTLS or SMTPNoTLS
	Username, Password string
}

const (
	MailSMTP = "smtp"
	MailLog  = "log"
	MailOff  = "off"
)

const (
	// SMTPStartTLS upgrades a plain connection, and refuses a server that
	// doesn't offer STARTTLS.
	SMTPStartTLS = "starttls"
	// SMTPImplicitTLS speaks TLS from the first byte (usually port 465).
	SMTPImplicitTLS = "tls"
	// SMTPNoTLS is for a local test inbox (Mailpit) only.
	SMTPNoTLS = "none"
)

// loadMail reads MAIL_* and, for the smtp driver, SMTP_*. smtp is the default
// everywhere, production included, so a server with no SMTP settings refuses
// to start rather than silently running without email.
func (c *Config) loadMail() error {
	c.MailDriver = env("MAIL_DRIVER", MailSMTP)
	c.MailFile = env("MAIL_FILE", "tmp/mail.log")
	switch c.MailDriver {
	case MailOff:
		return nil
	case MailLog:
		// The mailbox holds working password reset links: anyone who can read
		// it could take over any account.
		if c.Production {
			return errors.New("MAIL_DRIVER=log is for development only; use smtp, or off to run without email")
		}
		return nil
	case MailSMTP:
		return c.loadSMTP()
	case "file":
		return errors.New("MAIL_DRIVER=file is now called log; change it in .env (or use smtp with Mailpit: see .env.example)")
	}
	return errors.New("MAIL_DRIVER must be smtp, log or off")
}

func (c *Config) loadSMTP() error {
	from := os.Getenv("MAIL_FROM_ADDRESS")
	c.SMTP.Host = os.Getenv("SMTP_HOST")
	if from == "" || c.SMTP.Host == "" {
		return errors.New("MAIL_DRIVER is smtp (the default): set SMTP_HOST and MAIL_FROM_ADDRESS, or MAIL_DRIVER=off to run without email")
	}
	if !bareAddress(from) {
		return errors.New("MAIL_FROM_ADDRESS must be a bare email address, such as no-reply@example.com")
	}
	c.MailFromAddress = from
	c.MailFromName = env("MAIL_FROM_NAME", c.AppName)
	if strings.ContainsAny(c.MailFromName, "\r\n") || strings.ContainsAny(c.SMTP.Host, " \t\r\n/") {
		return errors.New("MAIL_FROM_NAME and SMTP_HOST can't contain line breaks")
	}
	c.SMTP.TLS = env("SMTP_TLS", SMTPStartTLS)
	if err := c.loadSMTPPort(); err != nil {
		return err
	}
	c.SMTP.Username, c.SMTP.Password = os.Getenv("SMTP_USERNAME"), os.Getenv("SMTP_PASSWORD")
	if (c.SMTP.Username == "") != (c.SMTP.Password == "") {
		return errors.New("SMTP_USERNAME and SMTP_PASSWORD must be set together")
	}
	if c.SMTP.TLS == SMTPNoTLS {
		if c.Production {
			return errors.New("SMTP_TLS=none is for a local test inbox; production must use starttls or tls")
		}
		// The password would cross the network unencrypted.
		if c.SMTP.Username != "" {
			return errors.New("SMTP_TLS=none can't send SMTP_USERNAME and SMTP_PASSWORD; use starttls or tls")
		}
	}
	return nil
}

// loadSMTPPort reads SMTP_TLS's port: 587 for starttls and 465 for tls unless
// SMTP_PORT says otherwise, and SMTP_PORT always for none.
func (c *Config) loadSMTPPort() error {
	defaults := map[string]int{SMTPStartTLS: 587, SMTPImplicitTLS: 465, SMTPNoTLS: 0}
	port, known := defaults[c.SMTP.TLS]
	if !known {
		return errors.New("SMTP_TLS must be starttls, tls or none")
	}
	if raw := os.Getenv("SMTP_PORT"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 65535 {
			return errors.New("SMTP_PORT must be a port from 1 to 65535")
		}
		port = parsed
	}
	if port == 0 {
		return errors.New("SMTP_TLS=none needs SMTP_PORT (Mailpit listens on 1025)")
	}
	c.SMTP.Port = port
	return nil
}

// bareAddress accepts an address with no display name and no line breaks.
func bareAddress(s string) bool {
	if strings.ContainsAny(s, "\r\n") {
		return false
	}
	address, err := mail.ParseAddress(s)
	return err == nil && address.Address == s
}
