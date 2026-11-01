package config

import "errors"

// loadPolicies reads POLICIES (off by default): on publishes the terms of
// service and privacy policy pages and makes sign-up agree to them
// (plans/features/implemented/policies.md). Whether the web build has them,
// and whether production may publish them, is checked once the pages are
// loaded (web.Pages.ServePolicies).
func (c *Config) loadPolicies() error {
	switch env("POLICIES", "off") {
	case "on":
		c.Policies = true
	case "off":
	default:
		return errors.New("POLICIES must be on or off")
	}
	return nil
}
