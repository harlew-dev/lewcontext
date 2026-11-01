package config

import (
	"errors"
	"os"
	"regexp"
)

// Captcha is the CAPTCHA provider a rate limit's challenge level asks for
// (internal/captcha, docs/api/rate-limits.md#challenges).
type Captcha struct {
	// CaptchaProvider is CAPTCHA_PROVIDER: "turnstile" or "off". Outside
	// production it defaults to turnstile with Cloudflare's test keys, which
	// always pass; production must set it.
	CaptchaProvider string
	// CaptchaSiteKey is public: the 428 sends it to the browser.
	CaptchaSiteKey string
	// CaptchaSecretKey verifies tokens with the provider. A secret: never
	// logged or sent anywhere but the provider.
	CaptchaSecretKey string
}

const (
	CaptchaTurnstile = "turnstile"
	CaptchaOff       = "off"
)

// Cloudflare's published Turnstile test keys (always pass, always fail,
// force an interaction…): the defaults outside production, refused in it.
const (
	TurnstileTestSiteKey   = "1x00000000000000000000AA"
	TurnstileTestSecretKey = "1x0000000000000000000000000000000AA"
)

var turnstileTestKey = regexp.MustCompile(`^[123]x0+[A-F]{2}$`)

// IsTurnstileTestKey reports whether key is one of Cloudflare's published
// Turnstile test keys (site or secret), which production refuses.
func IsTurnstileTestKey(key string) bool {
	return turnstileTestKey.MatchString(key)
}

// loadCaptcha reads CAPTCHA_PROVIDER, CAPTCHA_SITE_KEY, CAPTCHA_SECRET_KEY
// and PER_IP_LIMITS.
func (c *Config) loadCaptcha() error {
	switch env("PER_IP_LIMITS", "on") {
	case "on":
	case "off":
		c.PerIPLimitsOff = true
	default:
		return errors.New("PER_IP_LIMITS must be on or off")
	}
	provider := os.Getenv("CAPTCHA_PROVIDER")
	if provider == "" && c.Production {
		return errors.New("CAPTCHA_PROVIDER is required in production: turnstile, or off to run without CAPTCHAs")
	}
	if provider == "" {
		provider = CaptchaTurnstile
	}
	if provider != CaptchaTurnstile && provider != CaptchaOff {
		return errors.New("CAPTCHA_PROVIDER must be turnstile or off")
	}
	c.CaptchaProvider = provider
	if provider == CaptchaOff {
		return nil
	}
	c.CaptchaSiteKey, c.CaptchaSecretKey = os.Getenv("CAPTCHA_SITE_KEY"), os.Getenv("CAPTCHA_SECRET_KEY")
	if !c.Production {
		if c.CaptchaSiteKey == "" {
			c.CaptchaSiteKey = TurnstileTestSiteKey
		}
		if c.CaptchaSecretKey == "" {
			c.CaptchaSecretKey = TurnstileTestSecretKey
		}
		return nil
	}
	if c.CaptchaSiteKey == "" || c.CaptchaSecretKey == "" {
		return errors.New("CAPTCHA_PROVIDER=turnstile needs CAPTCHA_SITE_KEY and CAPTCHA_SECRET_KEY")
	}
	if IsTurnstileTestKey(c.CaptchaSiteKey) || IsTurnstileTestKey(c.CaptchaSecretKey) {
		return errors.New("CAPTCHA_SITE_KEY and CAPTCHA_SECRET_KEY are Cloudflare's test keys: production needs your own")
	}
	return nil
}
