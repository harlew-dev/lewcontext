package captcha

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/observe"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
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

// ourFault are siteverify's error codes for a request we got wrong (our
// secret, our form) or its own failure: not the user's doing, so they're
// ErrUnavailable (503). Every other code is about the token: ErrRejected.
var ourFault = []string{"missing-input-secret", "invalid-input-secret", "bad-request", "internal-error"}

// Turnstile is Cloudflare Turnstile, with net/http only.
type Turnstile struct {
	siteKey, secret, host string
	// testSecret: the secret is one of Cloudflare's test secrets, whose
	// answers name no action and example.com.
	testSecret bool
	endpoint   string
	client     *http.Client
}

// NewTurnstile verifies with Cloudflare; tokens must have been solved on
// host. It uses a copy of client (nil for the default) that never follows
// a redirect, so the secret is only ever posted to siteverify.
func NewTurnstile(siteKey, secret, host string, client *http.Client) *Turnstile {
	noRedirects := http.Client{}
	if client != nil {
		noRedirects = *client
	}
	noRedirects.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Turnstile{
		siteKey: siteKey, secret: secret, host: host,
		testSecret: config.IsTurnstileTestKey(secret),
		endpoint:   siteverify, client: &noRedirects,
	}
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

// Verify posts the token to siteverify with our secret and the client's
// IP. It must succeed, on our hostname, for action, so a token solved for
// sign-up can't be spent on login. Single use and the 300-second expiry
// are Cloudflare's (timeout-or-duplicate). A redirect is ErrUnavailable,
// never followed. Answers to Cloudflare's test secrets name example.com
// and no action, so they're taken as they are, but only when our secret is
// one of them (production refuses those). Failures are logged with
// siteverify's error codes, never the secret or the token.
func (t *Turnstile) Verify(ctx context.Context, token, action, ip string) error {
	if token == "" || len(token) > MaxToken {
		return ErrRejected
	}
	answer, err := t.siteverify(ctx, token, ip)
	switch {
	case err != nil && errors.Is(err, context.Canceled) && errors.Is(ctx.Err(), context.Canceled):
		// The client went away (httpapi.ClientGone): not a failure.
		return errors.Join(ErrUnavailable, err)
	case err != nil:
		slog.ErrorContext(ctx, "captcha: siteverify couldn't be used", observe.ErrorAttr(err))
		return errors.Join(ErrUnavailable, err)
	}
	switch {
	case !answer.Success && slices.ContainsFunc(answer.ErrorCodes, func(code string) bool { return slices.Contains(ourFault, code) }):
		slog.ErrorContext(ctx, "captcha: siteverify refused our request", siteverifyErrors(answer))
		return ErrUnavailable
	case !answer.Success:
		slog.WarnContext(ctx, "captcha: token rejected", siteverifyErrors(answer))
		return ErrRejected
	case answer.Metadata.Testing && t.testSecret:
		return nil
	case answer.Hostname != t.host || answer.Action != action:
		slog.WarnContext(ctx, "captcha: token solved elsewhere", "hostname", observe.Scrub(answer.Hostname), "action", observe.Scrub(answer.Action), "want", action, "testing", answer.Metadata.Testing)
		return ErrRejected
	}
	return nil
}

// siteverifyErrors is the answer's error codes for the log, scrubbed like
// an error's text (they come from outside), under a key the log's redaction
// keeps ("codes" would be blanked, docs/api/observability.md).
func siteverifyErrors(answer siteverifyAnswer) slog.Attr {
	return slog.String("siteverifyErrors", observe.Scrub(strings.Join(answer.ErrorCodes, ",")))
}

// siteverify posts the token and reads the answer: an error when
// Cloudflare can't be reached, redirects, or answers other than 200 JSON.
func (t *Turnstile) siteverify(ctx context.Context, token, ip string) (siteverifyAnswer, error) {
	var answer siteverifyAnswer
	form := url.Values{"secret": {t.secret}, "response": {token}, "remoteip": {ip}}
	ctx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return answer, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := t.client.Do(req)
	if err != nil {
		return answer, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return answer, errors.New("siteverify answered " + res.Status)
	}
	err = json.NewDecoder(io.LimitReader(res.Body, maxResponse)).Decode(&answer)
	return answer, err
}
