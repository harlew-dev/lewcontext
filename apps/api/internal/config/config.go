package config

import (
	"errors"
	"net/url"
	"os"
)

type Config struct {
	DatabaseURL, Origin, Address string
	Production                   bool
}

func Load() (Config, error) {
	c := Config{DatabaseURL: os.Getenv("DATABASE_URL"), Origin: env("APP_ORIGIN", "http://localhost:3000"), Address: env("API_ADDR", "127.0.0.1:8080"), Production: os.Getenv("APP_ENV") == "production"}
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
