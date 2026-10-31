package config

import (
	"net/netip"
	"slices"
	"strings"
	"testing"
)

func setEnv(t *testing.T, vars map[string]string) {
	t.Helper()
	base := map[string]string{
		"DATABASE_URL": "postgresql://u:p@db/app?sslmode=verify-full",
		"APP_ORIGIN":   "https://app.example.com",
		"APP_ENV":      "",
		"MAIL_DRIVER":  "",
		"MAIL_FILE":    "",
		"RATE_LIMITS":  "",
		// Set to "" rather than left out, so the developer's own value
		// doesn't leak into a test.
		"CLIENT_IP_HEADER": "",
		"TRUSTED_PROXIES":  "",
	}
	for k, v := range vars {
		base[k] = v
	}
	for k, v := range base {
		t.Setenv(k, v)
	}
}

func TestMailDriver(t *testing.T) {
	setEnv(t, nil)
	c, err := Load()
	if err != nil || c.MailDriver != MailFile || c.MailFile != "tmp/mail.log" {
		t.Fatalf("development default: %+v %v", c, err)
	}
	setEnv(t, map[string]string{"APP_ENV": "production"})
	if c, err := Load(); err != nil || c.MailDriver != MailOff {
		t.Fatalf("production default: %+v %v", c, err)
	}
	// The mailbox holds working reset links, so production refuses it.
	setEnv(t, map[string]string{"APP_ENV": "production", "MAIL_DRIVER": "file"})
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "MAIL_DRIVER") {
		t.Fatalf("production accepted the file driver: %v", err)
	}
	setEnv(t, map[string]string{"MAIL_DRIVER": "smtp"})
	if _, err := Load(); err == nil {
		t.Fatal("unknown driver accepted")
	}
}

func TestRateLimits(t *testing.T) {
	setEnv(t, nil)
	if c, err := Load(); err != nil || c.RateLimits != RateLimitsOn {
		t.Fatalf("default: %+v %v", c, err)
	}
	setEnv(t, map[string]string{"RATE_LIMITS": "off"})
	if c, err := Load(); err != nil || c.RateLimits != RateLimitsOff {
		t.Fatalf("off outside production: %+v %v", c, err)
	}
	setEnv(t, map[string]string{"APP_ENV": "production", "RATE_LIMITS": "off"})
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "RATE_LIMITS") {
		t.Fatalf("production accepted RATE_LIMITS=off: %v", err)
	}
	setEnv(t, map[string]string{"RATE_LIMITS": "false"})
	if _, err := Load(); err == nil {
		t.Fatal("RATE_LIMITS=false accepted")
	}
}

func TestClientIP(t *testing.T) {
	setEnv(t, nil)
	if c, err := Load(); err != nil || c.ClientIPHeader != "" || c.TrustedProxies != nil {
		t.Fatalf("default: %+v %v", c, err)
	}
	// Names are case-insensitive; single addresses and ranges both parse.
	setEnv(t, map[string]string{"CLIENT_IP_HEADER": "cf-connecting-ip", "TRUSTED_PROXIES": "127.0.0.1, ::1,10.0.0.0/8,2001:db8::/32,10.1.2.3/16"})
	c, err := Load()
	want := []netip.Prefix{
		netip.MustParsePrefix("127.0.0.1/32"), netip.MustParsePrefix("::1/128"), netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("10.1.0.0/16"),
	}
	if err != nil || c.ClientIPHeader != "Cf-Connecting-Ip" || !slices.Equal(c.TrustedProxies, want) {
		t.Fatalf("parsed: %+v %v", c, err)
	}
	for name, vars := range map[string]map[string]string{
		"header without proxies": {"CLIENT_IP_HEADER": "X-Forwarded-For"},
		"proxies without header": {"TRUSTED_PROXIES": "127.0.0.1"},
		"only commas":            {"CLIENT_IP_HEADER": "X-Forwarded-For", "TRUSTED_PROXIES": " , "},
		"unknown header":         {"CLIENT_IP_HEADER": "Forwarded", "TRUSTED_PROXIES": "127.0.0.1"},
		"bad address":            {"CLIENT_IP_HEADER": "X-Real-IP", "TRUSTED_PROXIES": "127.0.0.1,localhost"},
		"bad range":              {"CLIENT_IP_HEADER": "X-Real-IP", "TRUSTED_PROXIES": "10.0.0.0/33"},
		"every IPv4 address":     {"CLIENT_IP_HEADER": "X-Real-IP", "TRUSTED_PROXIES": "127.0.0.1,0.0.0.0/0"},
		"every IPv6 address":     {"CLIENT_IP_HEADER": "X-Real-IP", "TRUSTED_PROXIES": "::/0"},
		"IPv4-mapped range":      {"CLIENT_IP_HEADER": "X-Real-IP", "TRUSTED_PROXIES": "::ffff:10.0.0.0/104"},
	} {
		setEnv(t, vars)
		if _, err := Load(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
