package config

import (
	"errors"
	"os"
	"regexp"
	"strings"
)

// templateAdminRouteKey is the throwaway key in .env.example. Anyone can
// read it, so production refuses it.
const templateAdminRouteKey = "ZPycpaTc6D_ZYGNirUyhk42QUAMY-uT4M3ji3CEnzWw"

const adminRouteKeyHelp = "ADMIN_ROUTE_KEY must be a UUIDv4 (uuidgen) or 32–128 characters of A–Z, a–z, 0–9, _ and -: print one with go run ./cmd/admin route-key generate"

// adminRouteKeyShape is what ADMIN_ROUTE_KEY may hold. A UUID (36 hex
// digits and hyphens) is one of them.
var adminRouteKeyShape = regexp.MustCompile(`^[A-Za-z0-9_-]{32,128}$`)

// loadAdminRouteKey reads ADMIN_ROUTE_KEY, the secret path segment of the
// admin dashboard (/admin/<key>). Unset turns admin off. The key hides the
// door; it isn't authentication (docs/api/admin.md). Messages never show it.
func (c *Config) loadAdminRouteKey() error {
	key := strings.TrimSpace(os.Getenv("ADMIN_ROUTE_KEY"))
	if key == "" {
		return nil
	}
	if !adminRouteKeyShape.MatchString(key) {
		return errors.New(adminRouteKeyHelp)
	}
	if strings.Count(key, key[:1]) == len(key) {
		return errors.New("ADMIN_ROUTE_KEY is one repeated character. " + adminRouteKeyHelp)
	}
	if c.Production && key == templateAdminRouteKey {
		return errors.New("ADMIN_ROUTE_KEY is the example key from .env.example; production needs its own. " + adminRouteKeyHelp)
	}
	c.AdminRouteKey = key
	return nil
}
