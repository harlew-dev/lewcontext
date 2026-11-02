package config

import (
	"net/netip"
	"slices"
	"strings"
	"testing"
)

func TestAdminRouteKey(t *testing.T) {
	setEnv(t, nil)
	if c, err := Load(); err != nil || c.AdminRouteKey != "" {
		t.Fatalf("unset turns admin off: %q %v", c.AdminRouteKey, err)
	}
	accepted := []string{
		"8a1f0c52-6d3e-4b7a-9c2d-5e8f1a3b7c90", // uuidgen
		"ZPycpaTc6D_ZYGNirUyhk42QUAMY-uT4M3ji3CEnzWx",
		strings.Repeat("ab", 64),
	}
	for _, key := range accepted {
		setEnv(t, map[string]string{"ADMIN_ROUTE_KEY": key})
		if c, err := Load(); err != nil || c.AdminRouteKey != key {
			t.Errorf("%s: %q %v", key, c.AdminRouteKey, err)
		}
	}
	setEnv(t, map[string]string{"ADMIN_ROUTE_KEY": templateAdminRouteKey})
	if _, err := Load(); err != nil {
		t.Fatalf("the template key outside production: %v", err)
	}

	refused := map[string]map[string]string{
		"31 characters":           {"ADMIN_ROUTE_KEY": strings.Repeat("ab", 15) + "c"},
		"129 characters":          {"ADMIN_ROUTE_KEY": strings.Repeat("ab", 64) + "c"},
		"a slash":                 {"ADMIN_ROUTE_KEY": strings.Repeat("ab", 16) + "/x"},
		"a dot":                   {"ADMIN_ROUTE_KEY": strings.Repeat("ab", 16) + ".x"},
		"a space":                 {"ADMIN_ROUTE_KEY": strings.Repeat("ab", 16) + " x"},
		"one repeated character":  {"ADMIN_ROUTE_KEY": strings.Repeat("a", 40)},
		"template, in production": {"ADMIN_ROUTE_KEY": templateAdminRouteKey, "APP_ENV": "production"},
	}
	for name, vars := range refused {
		setEnv(t, vars)
		_, err := Load()
		if err == nil || !strings.Contains(err.Error(), "ADMIN_ROUTE_KEY") {
			t.Errorf("%s: %v, want an ADMIN_ROUTE_KEY error", name, err)
			continue
		}
		if strings.Contains(err.Error(), vars["ADMIN_ROUTE_KEY"]) {
			t.Errorf("%s: the message shows the key", name)
		}
	}
}

func TestPreviousAdminRouteKey(t *testing.T) {
	current := "ZPycpaTc6D_ZYGNirUyhk42QUAMY-uT4M3ji3CEnzWx"
	previous := "8a1f0c52-6d3e-4b7a-9c2d-5e8f1a3b7c90"
	setEnv(t, map[string]string{"ADMIN_ROUTE_KEY": current})
	if c, err := Load(); err != nil || c.AdminRouteKeyPrevious != "" {
		t.Fatalf("unset: %q %v", c.AdminRouteKeyPrevious, err)
	}
	setEnv(t, map[string]string{"ADMIN_ROUTE_KEY": current, "ADMIN_ROUTE_KEY_PREVIOUS": previous})
	if c, err := Load(); err != nil || c.AdminRouteKeyPrevious != previous || c.AdminRouteKey != current {
		t.Fatalf("set: %+v %v", c, err)
	}
	refused := map[string]map[string]string{
		"without a current key":   {"ADMIN_ROUTE_KEY_PREVIOUS": previous},
		"equal to the current":    {"ADMIN_ROUTE_KEY": current, "ADMIN_ROUTE_KEY_PREVIOUS": current},
		"too short":               {"ADMIN_ROUTE_KEY": current, "ADMIN_ROUTE_KEY_PREVIOUS": "abcdef"},
		"a slash":                 {"ADMIN_ROUTE_KEY": current, "ADMIN_ROUTE_KEY_PREVIOUS": strings.Repeat("ab", 16) + "/x"},
		"one repeated character":  {"ADMIN_ROUTE_KEY": current, "ADMIN_ROUTE_KEY_PREVIOUS": strings.Repeat("b", 40)},
		"template, in production": {"ADMIN_ROUTE_KEY": current, "ADMIN_ROUTE_KEY_PREVIOUS": templateAdminRouteKey, "APP_ENV": "production"},
	}
	for name, vars := range refused {
		setEnv(t, vars)
		_, err := Load()
		if err == nil || !strings.Contains(err.Error(), "ADMIN_ROUTE_KEY_PREVIOUS") {
			t.Errorf("%s: %v, want an ADMIN_ROUTE_KEY_PREVIOUS error", name, err)
			continue
		}
		for _, key := range []string{vars["ADMIN_ROUTE_KEY"], vars["ADMIN_ROUTE_KEY_PREVIOUS"]} {
			if key != "" && strings.Contains(err.Error(), key) {
				t.Errorf("%s: the message shows a key", name)
			}
		}
	}
}

func TestAdminAllowedIPs(t *testing.T) {
	setEnv(t, nil)
	if c, err := Load(); err != nil || c.AdminAllowedIPs != nil {
		t.Fatalf("unset allows any address: %v %v", c.AdminAllowedIPs, err)
	}
	setEnv(t, map[string]string{"ADMIN_ALLOWED_IPS": "203.0.113.10, 198.51.100.7/24,2001:db8:1::/48,::ffff:192.0.2.1"})
	c, err := Load()
	want := []netip.Prefix{
		netip.MustParsePrefix("203.0.113.10/32"), netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("2001:db8:1::/48"), netip.MustParsePrefix("192.0.2.1/32"),
	}
	if err != nil || !slices.Equal(c.AdminAllowedIPs, want) {
		t.Fatalf("parsed: %v %v", c.AdminAllowedIPs, err)
	}
	for name, list := range map[string]string{
		"bad address":        "203.0.113.10,office",
		"bad range":          "10.0.0.0/33",
		"every IPv4 address": "203.0.113.10,0.0.0.0/0",
		"every IPv6 address": "::/0",
		"IPv4-mapped range":  "::ffff:10.0.0.0/104",
	} {
		setEnv(t, map[string]string{"ADMIN_ALLOWED_IPS": list})
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "ADMIN_ALLOWED_IPS") {
			t.Errorf("%s: %v", name, err)
		}
	}
}
