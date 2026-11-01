package config

import (
	"strings"
	"testing"
)

func TestCaptcha(t *testing.T) {
	// Outside production: Turnstile with the always-passing test keys.
	setEnv(t, map[string]string{"CAPTCHA_PROVIDER": ""})
	c, err := Load()
	if err != nil || c.CaptchaProvider != CaptchaTurnstile || c.CaptchaSiteKey != TurnstileTestSiteKey || c.CaptchaSecretKey != TurnstileTestSecretKey {
		t.Fatalf("default: %+v %v", c.Captcha, err)
	}
	// off needs no keys.
	setEnv(t, map[string]string{"CAPTCHA_PROVIDER": "off", "APP_ENV": "production"})
	if c, err := Load(); err != nil || c.CaptchaProvider != CaptchaOff || c.CaptchaSecretKey != "" {
		t.Fatalf("off in production: %+v %v", c.Captcha, err)
	}
	// Production with its own keys.
	own := map[string]string{"APP_ENV": "production", "CAPTCHA_PROVIDER": "turnstile", "CAPTCHA_SITE_KEY": "0x4AAAAAAAreal", "CAPTCHA_SECRET_KEY": "0x4AAAAAAArealsecret"}
	setEnv(t, own)
	if c, err := Load(); err != nil || c.CaptchaSiteKey != "0x4AAAAAAAreal" {
		t.Fatalf("production keys: %+v %v", c.Captcha, err)
	}
	refused := map[string]map[string]string{
		"production without a choice": {"APP_ENV": "production", "CAPTCHA_PROVIDER": ""},
		"an unknown provider":         {"CAPTCHA_PROVIDER": "hcaptcha"},
		"production without keys":     {"APP_ENV": "production", "CAPTCHA_PROVIDER": "turnstile"},
		"production with no secret":   {"APP_ENV": "production", "CAPTCHA_PROVIDER": "turnstile", "CAPTCHA_SITE_KEY": "0x4AAAAAAAreal"},
		"the always-pass test keys":   {"APP_ENV": "production", "CAPTCHA_PROVIDER": "turnstile", "CAPTCHA_SITE_KEY": TurnstileTestSiteKey, "CAPTCHA_SECRET_KEY": TurnstileTestSecretKey},
		"a forced-challenge test key": {"APP_ENV": "production", "CAPTCHA_PROVIDER": "turnstile", "CAPTCHA_SITE_KEY": "3x00000000000000000000FF", "CAPTCHA_SECRET_KEY": "0x4AAAAAAArealsecret"},
		"a failing test secret":       {"APP_ENV": "production", "CAPTCHA_PROVIDER": "turnstile", "CAPTCHA_SITE_KEY": "0x4AAAAAAAreal", "CAPTCHA_SECRET_KEY": "2x0000000000000000000000000000000AA"},
	}
	for name, vars := range refused {
		setEnv(t, vars)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "CAPTCHA") {
			t.Errorf("%s: accepted (%v)", name, err)
		}
	}
}

func TestPerIPLimits(t *testing.T) {
	setEnv(t, nil)
	if c, err := Load(); err != nil || c.PerIPLimitsOff {
		t.Fatalf("default: %v %v", c.PerIPLimitsOff, err)
	}
	setEnv(t, map[string]string{"PER_IP_LIMITS": "off"})
	if c, err := Load(); err != nil || !c.PerIPLimitsOff {
		t.Fatalf("off: %v %v", c.PerIPLimitsOff, err)
	}
	setEnv(t, map[string]string{"PER_IP_LIMITS": "no"})
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "PER_IP_LIMITS") {
		t.Fatalf("no accepted: %v", err)
	}
}
