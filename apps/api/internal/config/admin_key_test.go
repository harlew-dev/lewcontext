package config

import (
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
