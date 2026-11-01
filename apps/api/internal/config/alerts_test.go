package config

import (
	"strings"
	"testing"
)

// The alerts' URLs: optional; an absolute http(s) URL, https in
// production, with no credentials or fragment; in production no loopback,
// private or link-local host unless ALERT_ALLOW_PRIVATE_TARGETS; the
// secret only with the webhook, and long. No message repeats a value.
func TestAlertSettings(t *testing.T) {
	setEnv(t, nil)
	c, err := Load()
	if err != nil || c.AlertWebhookURL != "" || c.AlertHeartbeatURL != "" || c.AlertWebhookFormat != AlertWebhookJSON || c.AlertAllowPrivateTargets {
		t.Fatalf("defaults: %+v %v", c.Alerts, err)
	}
	secret := strings.Repeat("s3cr", 8)
	setEnv(t, map[string]string{"ALERT_WEBHOOK_URL": "http://127.0.0.1:9000/hook", "ALERT_WEBHOOK_FORMAT": "slack", "ALERT_WEBHOOK_SECRET": secret, "ALERT_HEARTBEAT_URL": "https://hc-ping.example.net/abc"})
	if c, err := Load(); err != nil || c.AlertWebhookURL != "http://127.0.0.1:9000/hook" || c.AlertWebhookFormat != "slack" || c.AlertWebhookSecret != secret || c.AlertHeartbeatURL == "" {
		t.Fatalf("development: %+v %v", c.Alerts, err)
	}
	for name, vars := range map[string]map[string]string{
		"a relative URL":          {"ALERT_WEBHOOK_URL": "/hooks/abc"},
		"another scheme":          {"ALERT_WEBHOOK_URL": "ftp://hooks.example.net/abc"},
		"no host":                 {"ALERT_HEARTBEAT_URL": "https:///abc"},
		"a user and password":     {"ALERT_WEBHOOK_URL": "https://user:pass@hooks.example.net/abc"},
		"a fragment":              {"ALERT_WEBHOOK_URL": "https://hooks.example.net/abc#x"},
		"a line break":            {"ALERT_HEARTBEAT_URL": "https://hc-ping.example.net/a\nb"},
		"an unknown format":       {"ALERT_WEBHOOK_URL": "https://hooks.example.net/abc", "ALERT_WEBHOOK_FORMAT": "xml"},
		"a short secret":          {"ALERT_WEBHOOK_URL": "https://hooks.example.net/abc", "ALERT_WEBHOOK_SECRET": "short"},
		"a secret without a hook": {"ALERT_WEBHOOK_SECRET": secret},
		"private targets maybe":   {"ALERT_ALLOW_PRIVATE_TARGETS": "yes"},
	} {
		setEnv(t, vars)
		_, err := Load()
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		for _, v := range vars {
			if len(v) > 8 && strings.Contains(err.Error(), v) {
				t.Errorf("%s: the message repeats the value: %v", name, err)
			}
		}
	}
	production := map[string]string{"APP_ENV": "production", "DATABASE_URL": "postgresql://u:p@db/app?sslmode=verify-full"}
	with := func(more map[string]string) map[string]string {
		vars := map[string]string{}
		for k, v := range production {
			vars[k] = v
		}
		for k, v := range more {
			vars[k] = v
		}
		return vars
	}
	for _, u := range []string{"http://hooks.example.net/abc", "https://127.0.0.1/hook", "https://localhost/hook", "https://10.0.0.5/hook",
		"https://[::1]/hook", "https://169.254.169.254/latest", "https://[fe80::1]/hook", "https://192.168.1.10:8443/hook", "https://[::ffff:10.0.0.1]/hook", "https://0.0.0.0/hook"} {
		setEnv(t, with(map[string]string{"ALERT_WEBHOOK_URL": u}))
		if _, err := Load(); err == nil {
			t.Errorf("production %s: accepted", u)
		}
		setEnv(t, with(map[string]string{"ALERT_HEARTBEAT_URL": u}))
		if _, err := Load(); err == nil {
			t.Errorf("production heartbeat %s: accepted", u)
		}
	}
	setEnv(t, with(map[string]string{"ALERT_WEBHOOK_URL": "https://10.0.0.5/hook", "ALERT_ALLOW_PRIVATE_TARGETS": "true"}))
	if c, err := Load(); err != nil || !c.AlertAllowPrivateTargets {
		t.Errorf("production, private allowed: %v", err)
	}
	setEnv(t, with(map[string]string{"ALERT_WEBHOOK_URL": "https://hooks.slack.com/services/T0/B0/x", "ALERT_HEARTBEAT_URL": "https://203.0.113.10/ping"}))
	if _, err := Load(); err != nil {
		t.Errorf("production, public: %v", err)
	}
}
