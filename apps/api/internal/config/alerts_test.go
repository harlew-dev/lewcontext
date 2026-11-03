package config

import (
	"strings"
	"testing"
)

// ALERT_ALLOW_PRIVATE_TARGETS is the one alert setting left in the
// environment: where alerts go is managed in the dashboard (sealed in the
// database), so the old variables are no longer read.
func TestAlertSettings(t *testing.T) {
	setEnv(t, nil)
	c, err := Load()
	if err != nil || c.AlertAllowPrivateTargets {
		t.Fatalf("defaults: %+v %v", c.Alerts, err)
	}
	setEnv(t, map[string]string{"ALERT_ALLOW_PRIVATE_TARGETS": "true"})
	if c, err := Load(); err != nil || !c.AlertAllowPrivateTargets {
		t.Fatalf("allowed: %v", err)
	}
	setEnv(t, map[string]string{"ALERT_ALLOW_PRIVATE_TARGETS": "yes"})
	if _, err := Load(); err == nil {
		t.Fatal("accepted a value that isn't true or false")
	}
	// The removed variables are ignored, never an error and never used.
	setEnv(t, map[string]string{"ALERT_WEBHOOK_URL": "not a url", "ALERT_HEARTBEAT_URL": "ftp://x", "ALERT_WEBHOOK_SECRET": "short", "ALERT_WEBHOOK_FORMAT": "xml"})
	if _, err := Load(); err != nil {
		t.Fatalf("a removed variable is read: %v", err)
	}
}

// A receiver URL is checked as an operator saves it and before each use: an
// absolute http(s) URL (https in production) with no credentials or fragment,
// and in production no loopback, private or link-local host unless private
// targets are allowed. No message repeats the value.
func TestValidateAlertURL(t *testing.T) {
	for name, u := range map[string]string{
		"a relative URL":      "/hooks/abc",
		"another scheme":      "ftp://hooks.example.net/abc",
		"no host":             "https:///abc",
		"a user and password": "https://user:pass@hooks.example.net/abc",
		"a fragment":          "https://hooks.example.net/abc#x",
		"a line break":        "https://hc-ping.example.net/a\nb",
		"a bad port":          "https://hooks.example.net:99999/abc",
		"too long":            "https://hooks.example.net/" + strings.Repeat("a", 2100),
	} {
		err := ValidateAlertURL(u, false, false)
		if err == nil {
			t.Errorf("%s: accepted", name)
		} else if len(u) > 8 && strings.Contains(err.Error(), u) {
			t.Errorf("%s: the message repeats the value: %v", name, err)
		}
	}
	for _, u := range []string{"http://hooks.example.net/abc", "https://127.0.0.1/hook", "https://localhost/hook", "https://10.0.0.5/hook",
		"https://[::1]/hook", "https://169.254.169.254/latest", "https://[fe80::1]/hook", "https://192.168.1.10:8443/hook", "https://[::ffff:10.0.0.1]/hook", "https://0.0.0.0/hook"} {
		if ValidateAlertURL(u, true, false) == nil {
			t.Errorf("production %s: accepted", u)
		}
	}
	if ValidateAlertURL("https://10.0.0.5/hook", true, true) != nil {
		t.Error("production, private allowed: refused")
	}
	if ValidateAlertURL("http://127.0.0.1:9000/hook", false, false) != nil {
		t.Error("development http: refused")
	}
	if ValidateAlertURL("https://hooks.slack.com/services/T0/B0/x", true, false) != nil || ValidateAlertURL("https://203.0.113.10/ping", true, false) != nil {
		t.Error("production, public: refused")
	}
}
