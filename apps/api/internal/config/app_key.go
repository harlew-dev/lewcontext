package config

import (
	"bytes"
	"encoding/base64"
	"errors"
	"lewcontext/api/internal/auth"
	"os"
	"strings"
)

// templateAppKey is the throwaway key in .env.example. Anyone can read
// it, so production refuses it.
const templateAppKey = "O1Uo2N3TlBVsoKbrO0rTLwuE7DArCGLH2tWmaL8jMQA="

const appKeyHelp = "APP_KEY must be 32 random bytes in base64: make app-key writes one into .env, or print one with go run ./cmd/admin app-key generate"

// loadAppKey reads APP_KEY, which seals MFA secrets at rest
// (auth.Keys). It's required everywhere. Messages never show the value.
// Changing it later makes every sealed secret unreadable, locking out every
// account with MFA on.
func (c *Config) loadAppKey() error {
	raw := strings.TrimSpace(os.Getenv("APP_KEY"))
	if raw == "" && os.Getenv("ENCRYPTION_KEY") != "" {
		return errors.New("ENCRYPTION_KEY is now called APP_KEY: rename it in .env, keeping its value")
	}
	if raw == "" {
		return errors.New("APP_KEY is required. " + appKeyHelp)
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(key) != auth.AppKeySize {
		return errors.New(appKeyHelp)
	}
	if bytes.Count(key, key[:1]) == len(key) {
		return errors.New("APP_KEY is one repeated byte. " + appKeyHelp)
	}
	if c.Production && raw == templateAppKey {
		return errors.New("APP_KEY is the example key from .env.example; production needs its own. " + appKeyHelp)
	}
	c.AppKey = key
	return nil
}
