package config

import (
	"strings"
	"testing"
)

func setEnv(t *testing.T, vars map[string]string) {
	t.Helper()
	base := map[string]string{
		"DATABASE_URL": "postgresql://u:p@db/app?sslmode=verify-full",
		"APP_ORIGIN":   "https://app.example.com",
		"APP_ENV":      "",
		"MAIL_DRIVER":  "",
		"MAIL_FILE":    "",
	}
	for k, v := range vars {
		base[k] = v
	}
	for k, v := range base {
		t.Setenv(k, v)
	}
}

func TestMailDriver(t *testing.T) {
	setEnv(t, nil)
	c, err := Load()
	if err != nil || c.MailDriver != MailFile || c.MailFile != "tmp/mail.log" {
		t.Fatalf("development default: %+v %v", c, err)
	}
	setEnv(t, map[string]string{"APP_ENV": "production"})
	if c, err := Load(); err != nil || c.MailDriver != MailOff {
		t.Fatalf("production default: %+v %v", c, err)
	}
	// The mailbox holds working reset links, so production refuses it.
	setEnv(t, map[string]string{"APP_ENV": "production", "MAIL_DRIVER": "file"})
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "MAIL_DRIVER") {
		t.Fatalf("production accepted the file driver: %v", err)
	}
	setEnv(t, map[string]string{"MAIL_DRIVER": "smtp"})
	if _, err := Load(); err == nil {
		t.Fatal("unknown driver accepted")
	}
}
