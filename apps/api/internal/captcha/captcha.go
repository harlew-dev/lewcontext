// Package captcha checks CAPTCHA tokens for rate limits' challenge levels
// (docs/api/rate-limits.md#challenges). Handlers depend only on Provider;
// Turnstile is the one implementation, and another provider is one more.
package captcha

import (
	"context"
	"errors"
	"fmt"
	"lewcontext/api/internal/config"
	"net/http"
)

// Provider checks CAPTCHA tokens with one service.
type Provider interface {
	// Name is what the 428 tells the web app to load: "turnstile".
	Name() string
	// SiteKey is public: the 428 sends it to the browser.
	SiteKey() string
	// Verify checks a token solved for one action, from the request's IP:
	// nil, ErrRejected, or ErrUnavailable when the service can't answer.
	Verify(ctx context.Context, token, action, ip string) error
	// CSP lists the origins the provider's widget needs, per directive.
	CSP() Sources
}

// Sources are origins a page's Content-Security-Policy must allow.
type Sources struct{ Script, Frame []string }

var (
	// ErrRejected: the token is missing its mark, made for another site or
	// action, expired, or already used.
	ErrRejected = errors.New("captcha token rejected")
	// ErrUnavailable: the service couldn't be reached or answered oddly.
	ErrUnavailable = errors.New("captcha service unavailable")
)

// MaxToken bounds the token header, checked before any call.
const MaxToken = 2048

// New returns the configured provider, or nil for "off". host is the
// hostname tokens must have been solved on (APP_ORIGIN's).
func New(c config.Captcha, client *http.Client, host string) (Provider, error) {
	switch c.CaptchaProvider {
	case config.CaptchaOff, "":
		return nil, nil
	case config.CaptchaTurnstile:
		return NewTurnstile(c.CaptchaSiteKey, c.CaptchaSecretKey, host, client), nil
	}
	return nil, fmt.Errorf("unknown CAPTCHA provider %q", c.CaptchaProvider)
}
