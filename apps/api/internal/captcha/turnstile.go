package captcha

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"lewcontext/api/internal/auth"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// turnstileOrigin serves Turnstile's script and widget, and its siteverify.
const turnstileOrigin = "https://challenges.cloudflare.com"

// siteverify is Turnstile's verification endpoint.
const siteverify = turnstileOrigin + "/turnstile/v0/siteverify"

// verifyTimeout bounds each call; the response body is read up to
// maxResponse.
const (
	verifyTimeout = 5 * time.Second
	maxResponse   = 64 << 10
)

// Turnstile is Cloudflare Turnstile, with net/http only.
type Turnstile struct {
	siteKey, secret, host string
	endpoint              string
	client                *http.Client
}

// NewTurnstile verifies with Cloudflare; tokens must have been solved on
// host. A nil client uses http.DefaultClient (the timeout is per call).
func NewTurnstile(siteKey, secret, host string, client *http.Client) *Turnstile {
	if client == nil {
		client = http.DefaultClient
	}
	return &Turnstile{siteKey: siteKey, secret: secret, host: host, endpoint: siteverify, client: client}
}

func (t *Turnstile) Name() string    { return "turnstile" }
func (t *Turnstile) SiteKey() string { return t.siteKey }

// CSP: the script and the widget's frame both come from Cloudflare.
func (t *Turnstile) CSP() Sources {
	return Sources{Script: []string{turnstileOrigin}, Frame: []string{turnstileOrigin}}
}

type siteverifyAnswer struct {
	Success    bool     `json:"success"`
	Hostname   string   `json:"hostname"`
	Action     string   `json:"action"`
	ErrorCodes []string `json:"error-codes"`
	Metadata   struct {
		Testing bool `json:"result_with_testing_key"`
	} `json:"metadata"`
}

// Verify posts the token to siteverify with our secret, the client's IP and
// a fresh idempotency key. It must succeed, on our hostname, for action, so
// a token solved for sign-up can't be spent on login. Single use and the
// 300-second expiry are Cloudflare's (timeout-or-duplicate). Cloudflare's
// test secrets answer for any hostname and no action; production refuses
// them (config), so their answers are taken as they are.
func (t *Turnstile) Verify(ctx context.Context, token, action, ip string) error {
	if token == "" || len(token) > MaxToken {
		return ErrRejected
	}
	idempotency, err := auth.Token()
	if err != nil {
		return err
	}
	form := url.Values{"secret": {t.secret}, "response": {token}, "remoteip": {ip}, "idempotency_key": {idempotency}}
	ctx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := t.client.Do(req)
	if err != nil {
		return errors.Join(ErrUnavailable, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ErrUnavailable
	}
	var answer siteverifyAnswer
	if err := json.NewDecoder(io.LimitReader(res.Body, maxResponse)).Decode(&answer); err != nil {
		return errors.Join(ErrUnavailable, err)
	}
	switch {
	case !answer.Success:
		return ErrRejected
	case answer.Metadata.Testing:
		return nil
	case answer.Hostname != t.host || answer.Action != action:
		return ErrRejected
	}
	return nil
}
