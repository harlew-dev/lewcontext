package config

import (
	"errors"
	"net/url"
	"os"
)

type Config struct {
	DatabaseURL, Origin, Address string
	Production                   bool
	// MailDriver is how email is delivered: MailFile writes it to MailFile,
	// a development mailbox; MailOff sends nothing, so password reset is
	// unavailable.
	MailDriver, MailFile string
}

const (
	MailFile = "file"
	MailOff  = "off"
)

func Load() (Config, error) {
	c := Config{DatabaseURL: os.Getenv("DATABASE_URL"), Origin: env("APP_ORIGIN", "http://localhost:3000"), Address: env("API_ADDR", "127.0.0.1:8080"), Production: os.Getenv("APP_ENV") == "production"}
	c.MailDriver = env("MAIL_DRIVER", MailFile)
	if c.Production {
		c.MailDriver = env("MAIL_DRIVER", MailOff)
	}
	c.MailFile = env("MAIL_FILE", "tmp/mail.log")
	if c.MailDriver != MailFile && c.MailDriver != MailOff {
		return c, errors.New("MAIL_DRIVER must be file or off")
	}
	// The mailbox holds working password reset links: anyone who can read it
	// could take over any account.
	if c.Production && c.MailDriver == MailFile {
		return c, errors.New("MAIL_DRIVER=file is for development only; production has no email delivery yet, so use off")
	}
	if c.DatabaseURL == "" {
		return c, errors.New("DATABASE_URL is required")
	}
	u, err := url.Parse(c.Origin)
	if err != nil || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return c, errors.New("APP_ORIGIN must be an exact http(s) origin without trailing slash")
	}
	if c.Production && u.Scheme != "https" {
		return c, errors.New("production APP_ORIGIN requires HTTPS")
	}
	if c.Production {
		db, err := url.Parse(c.DatabaseURL)
		if err != nil || db.Query().Get("sslmode") != "verify-full" {
			return c, errors.New("production DATABASE_URL requires sslmode=verify-full")
		}
	}
	return c, nil
}
func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
