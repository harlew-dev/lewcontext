package config

import (
	"strings"
	"testing"
)

// smtpEnv is a working SMTP setup; tests change one thing at a time.
func smtpEnv(vars map[string]string) map[string]string {
	base := map[string]string{
		"MAIL_DRIVER":       "smtp",
		"MAIL_FROM_ADDRESS": "no-reply@example.com",
		"SMTP_HOST":         "smtp.example.com",
		"SMTP_USERNAME":     "resend",
		"SMTP_PASSWORD":     "re_secret_value",
	}
	for k, v := range vars {
		base[k] = v
	}
	return base
}

func TestMailDriver(t *testing.T) {
	// smtp is the default everywhere, so without settings the server refuses
	// to start, in production too, rather than running without email.
	for _, appEnv := range []string{"", "production"} {
		setEnv(t, map[string]string{"APP_ENV": appEnv, "MAIL_DRIVER": ""})
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "SMTP_HOST") || !strings.Contains(err.Error(), "MAIL_DRIVER=off") {
			t.Fatalf("APP_ENV=%q started without SMTP settings: %v", appEnv, err)
		}
	}
	setEnv(t, map[string]string{"APP_ENV": "production", "MAIL_DRIVER": "off"})
	if c, err := Load(); err != nil || c.MailDriver != MailOff {
		t.Fatalf("off: %+v %v", c.Mail, err)
	}
	setEnv(t, map[string]string{"MAIL_DRIVER": "log"})
	if c, err := Load(); err != nil || c.MailDriver != MailLog || c.MailFile != "tmp/mail.log" {
		t.Fatalf("log: %+v %v", c.Mail, err)
	}
	// The mailbox holds working reset links, so production refuses it.
	setEnv(t, map[string]string{"APP_ENV": "production", "MAIL_DRIVER": "log"})
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "MAIL_DRIVER") {
		t.Fatalf("production accepted the log driver: %v", err)
	}
	for _, unknown := range []string{"file", "ses", "SMTP"} {
		setEnv(t, map[string]string{"MAIL_DRIVER": unknown})
		if _, err := Load(); err == nil {
			t.Fatalf("MAIL_DRIVER=%s accepted", unknown)
		}
	}
}

func TestSMTPSettings(t *testing.T) {
	setEnv(t, smtpEnv(map[string]string{"APP_ENV": "production"}))
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := SMTP{Host: "smtp.example.com", Port: 587, TLS: SMTPStartTLS, Username: "resend", Password: "re_secret_value"}
	if c.SMTP != want || c.MailFromAddress != "no-reply@example.com" || c.MailFromName != "lewcontext" {
		t.Fatalf("defaults: %+v", c.Mail)
	}
	setEnv(t, smtpEnv(map[string]string{"SMTP_TLS": "tls", "APP_NAME": "Acme", "MAIL_FROM_NAME": ""}))
	if c, err := Load(); err != nil || c.SMTP.Port != 465 || c.MailFromName != "Acme" {
		t.Fatalf("tls default port and APP_NAME: %+v %v", c.Mail, err)
	}
	setEnv(t, smtpEnv(map[string]string{"SMTP_PORT": "2525", "MAIL_FROM_NAME": "Acme Support"}))
	if c, err := Load(); err != nil || c.SMTP.Port != 2525 || c.MailFromName != "Acme Support" {
		t.Fatalf("explicit port and name: %+v %v", c.Mail, err)
	}
	// Mailpit: no TLS and no credentials, outside production.
	setEnv(t, smtpEnv(map[string]string{"SMTP_TLS": "none", "SMTP_PORT": "1025", "SMTP_USERNAME": "", "SMTP_PASSWORD": ""}))
	if c, err := Load(); err != nil || c.SMTP.TLS != SMTPNoTLS || c.SMTP.Port != 1025 {
		t.Fatalf("Mailpit: %+v %v", c.Mail, err)
	}
}

func TestSMTPSettingsRefused(t *testing.T) {
	refused := map[string]map[string]string{
		"no host":                 {"SMTP_HOST": ""},
		"no sender":               {"MAIL_FROM_ADDRESS": ""},
		"a display name":          {"MAIL_FROM_ADDRESS": "Acme <no-reply@example.com>"},
		"a header in the sender":  {"MAIL_FROM_ADDRESS": "no-reply@example.com\r\nBcc: eve@example.com"},
		"a header in the name":    {"MAIL_FROM_NAME": "Acme\r\nBcc: eve@example.com"},
		"a host with spaces":      {"SMTP_HOST": "smtp.example.com extra"},
		"credentials in the host": {"SMTP_HOST": "alice:hunter2@smtp.example.com"},
		"an unknown TLS mode":     {"SMTP_TLS": "ssl"},
		"a port that isn't one":   {"SMTP_PORT": "smtp"},
		"port zero":               {"SMTP_PORT": "0"},
		"a port too large":        {"SMTP_PORT": "70000"},
		"none without a port":     {"SMTP_TLS": "none", "SMTP_USERNAME": "", "SMTP_PASSWORD": ""},
		"a username alone":        {"SMTP_PASSWORD": ""},
		"a password alone":        {"SMTP_USERNAME": ""},
		"credentials without TLS": {"SMTP_TLS": "none", "SMTP_PORT": "1025"},
		"none in production":      {"APP_ENV": "production", "SMTP_TLS": "none", "SMTP_PORT": "1025", "SMTP_USERNAME": "", "SMTP_PASSWORD": ""},
	}
	for name, change := range refused {
		setEnv(t, smtpEnv(change))
		_, err := Load()
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		// Messages name settings, never their values.
		if strings.Contains(err.Error(), "re_secret_value") {
			t.Errorf("%s: the error shows the password: %v", name, err)
		}
	}
}
