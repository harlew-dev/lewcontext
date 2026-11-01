package config

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestMetricsSettings(t *testing.T) {
	setEnv(t, nil)
	c, err := Load()
	if err != nil || c.MetricsStore != MetricsStoreOn || c.MetricsAddr != "" || c.MetricsToken != "" || c.SlowQuery != 500*time.Millisecond {
		t.Fatalf("defaults: %+v %v", c.Metrics, err)
	}
	setEnv(t, map[string]string{"METRICS_STORE": "off", "METRICS_ADDR": "127.0.0.1:9090", "SLOW_QUERY_MS": "0"})
	if c, err := Load(); err != nil || c.MetricsStore != MetricsStoreOff || c.MetricsAddr != "127.0.0.1:9090" || c.SlowQuery != 0 {
		t.Fatalf("set: %+v %v", c.Metrics, err)
	}
	token := strings.Repeat("t0k", 11)
	for name, vars := range map[string]map[string]string{
		"store maybe":       {"METRICS_STORE": "maybe"},
		"no port":           {"METRICS_ADDR": "127.0.0.1"},
		"port 0":            {"METRICS_ADDR": "127.0.0.1:0"},
		"a path":            {"METRICS_ADDR": "http://127.0.0.1:9090/metrics"},
		"the API's address": {"METRICS_ADDR": "127.0.0.1:8080"},
		"a short token":     {"METRICS_TOKEN": "short"},
		"slow query -1":     {"SLOW_QUERY_MS": "-1"},
		"slow query 2 min":  {"SLOW_QUERY_MS": "120000"},
		"slow query text":   {"SLOW_QUERY_MS": "fast"},
	} {
		setEnv(t, vars)
		if _, err := Load(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// In production, /metrics on every interface needs a token.
	production := map[string]string{"APP_ENV": "production", "DATABASE_URL": "postgresql://u:p@db/app?sslmode=verify-full"}
	for _, addr := range []string{":9090", "0.0.0.0:9090", "[::]:9090", "[::ffff:0.0.0.0]:9090", "metrics.internal:9090",
		// A zone hides the unspecified address from a plain check, and
		// still listens on every interface.
		"[::%lo]:9090", "[::%eth0]:9090", "[0::0%1]:9090", "[::1%lo]:9090",
		// Public and link-local addresses are reachable beyond a private
		// network.
		"203.0.113.5:9090", "[2001:db8::5]:9090", "[fe80::1]:9090", "169.254.0.5:9090",
		// A name, localhost too: the resolver could point it anywhere.
		"localhost:9090"} {
		vars := map[string]string{"METRICS_ADDR": addr}
		for k, v := range production {
			vars[k] = v
		}
		setEnv(t, vars)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "METRICS_TOKEN") || !strings.Contains(err.Error(), "127.0.0.1") {
			t.Errorf("production %s without a token: %v", addr, err)
		}
		vars["METRICS_TOKEN"] = token
		setEnv(t, vars)
		if c, err := Load(); err != nil || c.MetricsAddr != addr {
			t.Errorf("production %s with a token: %v", addr, err)
		}
	}
	for _, addr := range []string{"127.0.0.1:9090", "[::1]:9090", "10.0.0.5:9090", "192.168.1.5:9090", "172.16.0.5:9090", "[fd00::5]:9090", "[::ffff:10.0.0.5]:9090"} {
		vars := map[string]string{"METRICS_ADDR": addr}
		for k, v := range production {
			vars[k] = v
		}
		setEnv(t, vars)
		if _, err := Load(); err != nil {
			t.Errorf("production %s: %v", addr, err)
		}
	}
	// Outside production a public bind is allowed (a warning is the
	// operator's to heed).
	setEnv(t, map[string]string{"METRICS_ADDR": "0.0.0.0:9090"})
	if _, err := Load(); err != nil {
		t.Errorf("development 0.0.0.0: %v", err)
	}
}

// The configuration summary stored in server_instances holds no secret,
// no part of one, and no digest of one: built from a configuration full of
// known secrets, its JSON contains none of them.
func TestSummaryHoldsNoSecret(t *testing.T) {
	secrets := map[string]string{
		"APP_KEY":                  testAppKey,
		"ADMIN_ROUTE_KEY":          "Qx7vT2mZp9LwK4rB8nYc3HdF6jS1aGe5UoXi0VbN",
		"ADMIN_ROUTE_KEY_PREVIOUS": "Wm3zR8tPq5NvJ2kD7hYx4LcB9fG1sAe6UoTi0QpZ",
		"METRICS_TOKEN":            "Jt5nW9qX2vB7mK3rZ8pL4cY6hD1fG0sA-eUoTiQ",
		"SMTP_USERNAME":            "zq8vkx3m7tpw@rl9.qmx",
		"SMTP_PASSWORD":            "Hy4!Rk9wQz2#Lm7xVt5pBn3cJd8",
		"DB_PASSWORD":              "Pq2Xz7Wk4Vm9Lt3Rn8Bc5Jh1Gd6",
	}
	setEnv(t, map[string]string{
		"APP_KEY":                  secrets["APP_KEY"],
		"ADMIN_ROUTE_KEY":          secrets["ADMIN_ROUTE_KEY"],
		"ADMIN_ROUTE_KEY_PREVIOUS": secrets["ADMIN_ROUTE_KEY_PREVIOUS"],
		"ADMIN_ALLOWED_IPS":        "203.0.113.0/24",
		"METRICS_TOKEN":            secrets["METRICS_TOKEN"],
		"METRICS_ADDR":             "127.0.0.1:9090",
		"MAIL_DRIVER":              "smtp",
		"SMTP_HOST":                "smtp.example.com",
		"SMTP_USERNAME":            secrets["SMTP_USERNAME"],
		"SMTP_PASSWORD":            secrets["SMTP_PASSWORD"],
		"MAIL_FROM_ADDRESS":        "no-reply@example.com",
		"EMAIL_VERIFICATION":       "on",
		"CLIENT_IP_HEADER":         "X-Forwarded-For",
		"TRUSTED_PROXIES":          "10.0.0.0/8,192.168.0.0/16",
		"DATABASE_URL":             "postgresql://dbuser:" + secrets["DB_PASSWORD"] + "@db.internal/app?sslmode=verify-full",
	})
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	summary, err := json.Marshal(c.Summary(MigrationsSummary{Applied: 25, Embedded: 25}))
	if err != nil {
		t.Fatal(err)
	}
	text := string(summary)
	for name, secret := range secrets {
		raw := []byte(secret)
		if name == "APP_KEY" {
			raw = c.AppKey
		}
		sum := sha256.Sum256(raw)
		forms := []string{
			secret, string(raw), hex.EncodeToString(sum[:]), base64.StdEncoding.EncodeToString(sum[:]),
			base64.RawURLEncoding.EncodeToString(sum[:]), base64.StdEncoding.EncodeToString(raw),
		}
		for _, form := range forms {
			// No 8 characters in a row of any form.
			for i := 0; i+8 <= len(form); i++ {
				if strings.Contains(text, form[i:i+8]) {
					t.Fatalf("the summary holds part of %s (%q): %s", name, form[i:i+8], text)
				}
			}
		}
	}
	for _, word := range []string{"dbuser", "db.internal", "no-reply", "203.0.113", "10.0.0.0"} {
		if strings.Contains(text, word) {
			t.Errorf("the summary holds %q: %s", word, text)
		}
	}
	// What it does say.
	var got Summary
	if err := json.Unmarshal(summary, &got); err != nil {
		t.Fatal(err)
	}
	if !got.AdminRouteKey || !got.AdminRouteKeyPrev || got.AdminAllowedIPs != 1 || got.TrustedProxies != 2 || got.ClientIPHeader != "X-Forwarded-For" ||
		!got.MetricsAddr || !got.MetricsToken || got.SMTP == nil || got.SMTP.Host != "smtp.example.com" || got.SMTP.Port != 587 ||
		got.EmailVerification != "optional" || got.Migrations.Applied != 25 || got.SlowQueryMs != 500 {
		t.Fatalf("summary: %s", text)
	}
}
