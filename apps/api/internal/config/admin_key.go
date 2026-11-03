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
// admin dashboard (/admin/<key>), and the admin dashboard's other settings.
// Unset turns admin off. The key hides the door; it isn't authentication
// (docs/api/admin.md). Messages never show it.
func (c *Config) loadAdminRouteKey() error {
	key, err := adminRouteKey("ADMIN_ROUTE_KEY", c.Production)
	if err != nil {
		return err
	}
	c.AdminRouteKey = key
	if err := c.loadPreviousAdminRouteKey(); err != nil {
		return err
	}
	c.AdminAllowedIPs, err = parseRanges("ADMIN_ALLOWED_IPS", os.Getenv("ADMIN_ALLOWED_IPS"))
	return err
}

// loadPreviousAdminRouteKey reads ADMIN_ROUTE_KEY_PREVIOUS, the key being
// retired after a rotation: admin sessions made under it keep working until
// they end, but it never opens the sign-in. It needs a current key, and must
// differ from it.
func (c *Config) loadPreviousAdminRouteKey() error {
	previous, err := adminRouteKey("ADMIN_ROUTE_KEY_PREVIOUS", c.Production)
	if err != nil || previous == "" {
		return err
	}
	if c.AdminRouteKey == "" {
		return errors.New("ADMIN_ROUTE_KEY_PREVIOUS is set without ADMIN_ROUTE_KEY: set the new key as ADMIN_ROUTE_KEY, and the old one as ADMIN_ROUTE_KEY_PREVIOUS")
	}
	if previous == c.AdminRouteKey {
		return errors.New("ADMIN_ROUTE_KEY_PREVIOUS is the same as ADMIN_ROUTE_KEY: set a new ADMIN_ROUTE_KEY, and the old one as ADMIN_ROUTE_KEY_PREVIOUS")
	}
	c.AdminRouteKeyPrevious = previous
	return nil
}

// adminRouteKey reads one admin key variable, empty when unset, with the
// same rules for both.
func adminRouteKey(name string, production bool) (string, error) {
	help := strings.Replace(adminRouteKeyHelp, "ADMIN_ROUTE_KEY", name, 1)
	key := strings.TrimSpace(os.Getenv(name))
	if key == "" {
		return "", nil
	}
	if !adminRouteKeyShape.MatchString(key) {
		return "", errors.New(help)
	}
	if strings.Count(key, key[:1]) == len(key) {
		return "", errors.New(name + " is one repeated character. " + help)
	}
	if production && key == templateAdminRouteKey {
		return "", errors.New(name + " is the example key from .env.example; production needs its own. " + help)
	}
	return key, nil
}
