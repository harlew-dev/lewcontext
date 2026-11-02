package config

import (
	"bytes"
	"strings"
	"testing"
)

// testAppKey is a valid APP_KEY for tests that don't care about it.
const testAppKey = "c3VwZXItc2VjcmV0LXRlc3Qta2V5LTMyLWJ5dGVzISE="

func TestAppKey(t *testing.T) {
	setEnv(t, nil)
	c, err := Load()
	if err != nil || !bytes.Equal(c.AppKey, []byte("super-secret-test-key-32-bytes!!")) {
		t.Fatalf("valid key: %q %v", c.AppKey, err)
	}
	setEnv(t, map[string]string{"APP_KEY": templateAppKey})
	if _, err := Load(); err != nil {
		t.Fatalf("the template key outside production: %v", err)
	}

	refused := map[string]map[string]string{
		"missing":                 {"APP_KEY": ""},
		"not base64":              {"APP_KEY": "not base64 at all!"},
		"16 bytes":                {"APP_KEY": "MDEyMzQ1Njc4OWFiY2RlZg=="},
		"33 bytes":                {"APP_KEY": "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWZn"},
		"one repeated byte":       {"APP_KEY": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="},
		"template, in production": {"APP_KEY": templateAppKey, "APP_ENV": "production"},
		"the old name":            {"APP_KEY": "", "ENCRYPTION_KEY": testAppKey},
	}
	for name, vars := range refused {
		setEnv(t, vars)
		_, err := Load()
		if err == nil || !strings.Contains(err.Error(), "APP_KEY") {
			t.Errorf("%s: %v, want an APP_KEY error", name, err)
			continue
		}
		if value := vars["APP_KEY"]; value != "" && strings.Contains(err.Error(), value) {
			t.Errorf("%s: the message shows the key", name)
		}
	}
}
