package captcha

import (
	"bytes"
	"context"
	"errors"
	"io"
	"lewcontext/api/internal/config"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fake is siteverify, answering with body and recording each form sent.
func fake(t *testing.T, status int, body string) (*Turnstile, *[]url.Values) {
	t.Helper()
	var sent []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		sent = append(sent, r.PostForm)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	ts := NewTurnstile("site-key", "the-secret", "app.example.com", srv.Client())
	ts.endpoint = srv.URL
	return ts, &sent
}

func TestTurnstileVerify(t *testing.T) {
	ctx := context.Background()
	ok := `{"success":true,"hostname":"app.example.com","action":"login"}`
	ts, sent := fake(t, 200, ok)
	if err := ts.Verify(ctx, "tok", "login", "203.0.113.7"); err != nil {
		t.Fatalf("a good token: %v", err)
	}
	form := (*sent)[0]
	if form.Get("secret") != "the-secret" || form.Get("response") != "tok" || form.Get("remoteip") != "203.0.113.7" || form.Has("idempotency_key") || len(form) != 3 {
		t.Fatalf("sent %v", form)
	}
	for name, c := range map[string]struct {
		status int
		body   string
		action string
		want   error
	}{
		"another action (a sign-up token spent on login)": {200, ok, "register", ErrRejected},
		"another site":    {200, `{"success":true,"hostname":"evil.example","action":"login"}`, "login", ErrRejected},
		"used or expired": {200, `{"success":false,"error-codes":["timeout-or-duplicate"]}`, "login", ErrRejected},
		// Our secret isn't a test secret, so a testing answer gets no pass.
		"a testing answer to a real secret": {200, `{"success":true,"hostname":"evil.example","action":"register","metadata":{"result_with_testing_key":true}}`, "login", ErrRejected},
		"a server error":                    {500, `oops`, "login", ErrUnavailable},
		"not JSON":                          {200, `<html>`, "login", ErrUnavailable},
		"an endless body":                   {200, `{"success":true,"hostname":"app.example.com","action":"` + strings.Repeat("x", 100<<10) + `"}`, "login", ErrUnavailable},
	} {
		ts, _ := fake(t, c.status, c.body)
		if err := ts.Verify(ctx, "tok", c.action, "203.0.113.7"); !errors.Is(err, c.want) && !(c.want == nil && err == nil) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
	// No call at all for a missing or oversized token.
	ts, sent = fake(t, 200, ok)
	for _, token := range []string{"", strings.Repeat("a", MaxToken+1)} {
		if err := ts.Verify(ctx, token, "login", "203.0.113.7"); !errors.Is(err, ErrRejected) {
			t.Errorf("token of %d bytes: %v", len(token), err)
		}
	}
	if len(*sent) != 0 {
		t.Fatal("siteverify was called for a token refused locally")
	}
}

func TestTurnstileTimesOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the body, so the server notices the client hanging up.
		_, _ = io.ReadAll(r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()
	ts := NewTurnstile("k", "s", "app.example.com", srv.Client())
	ts.endpoint = srv.URL
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := ts.Verify(ctx, "tok", "login", "203.0.113.7"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("a hung siteverify: %v", err)
	}
}

func TestNew(t *testing.T) {
	if p, err := New(config.Captcha{CaptchaProvider: "off"}, nil, "h"); p != nil || err != nil {
		t.Fatalf("off: %v %v", p, err)
	}
	if _, err := New(config.Captcha{CaptchaProvider: "hcaptcha"}, nil, "h"); err == nil {
		t.Fatal("an unknown provider accepted")
	}
	p, err := New(config.Captcha{CaptchaProvider: "turnstile", CaptchaSiteKey: "site", CaptchaSecretKey: "secret"}, nil, "h")
	if err != nil || p.Name() != "turnstile" || p.SiteKey() != "site" {
		t.Fatalf("turnstile: %v %v", p, err)
	}
	if csp := p.CSP(); len(csp.Script) != 1 || csp.Script[0] != "https://challenges.cloudflare.com" || len(csp.Frame) != 1 || csp.Frame[0] != "https://challenges.cloudflare.com" {
		t.Fatalf("CSP %+v", csp)
	}
}

// Cloudflare's test secrets answer for example.com and no action: taken as
// they are, but only when our secret is one of them.
func TestTurnstileTestSecrets(t *testing.T) {
	answer := `{"success":true,"hostname":"example.com","metadata":{"result_with_testing_key":true}}`
	ts, _ := fake(t, 200, answer)
	if err := ts.Verify(context.Background(), "tok", "login", "203.0.113.7"); !errors.Is(err, ErrRejected) {
		t.Fatalf("a testing answer to a real secret: %v", err)
	}
	ts.secret, ts.testSecret = config.TurnstileTestSecretKey, true
	if err := ts.Verify(context.Background(), "tok", "login", "203.0.113.7"); err != nil {
		t.Fatalf("a testing answer to the test secret: %v", err)
	}
	if !NewTurnstile("k", config.TurnstileTestSecretKey, "h", nil).testSecret || !NewTurnstile("k", "2x0000000000000000000000000000000AA", "h", nil).testSecret || NewTurnstile("k", "0x4AAAAAAAreal", "h", nil).testSecret {
		t.Fatal("test secrets recognised wrongly")
	}
}

// A redirect is never followed, so the secret goes to siteverify only.
func TestTurnstileNeverFollowsRedirects(t *testing.T) {
	followed := false
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		followed = true
		_, _ = io.WriteString(w, `{"success":true,"hostname":"app.example.com","action":"login"}`)
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	// Even a client whose own policy follows redirects.
	ts := NewTurnstile("k", "the-secret", "app.example.com", &http.Client{})
	ts.endpoint = srv.URL
	if err := ts.Verify(context.Background(), "tok", "login", "203.0.113.7"); !errors.Is(err, ErrUnavailable) || followed {
		t.Fatalf("a redirect: %v, followed %v", err, followed)
	}
}

// siteverify's codes for our mistakes or its outage are ErrUnavailable
// (503); the token's are ErrRejected. Both are logged with the codes, never
// the secret or the token.
func TestTurnstileErrorCodes(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previous)
	for code, want := range map[string]error{
		"missing-input-secret":   ErrUnavailable,
		"invalid-input-secret":   ErrUnavailable,
		"bad-request":            ErrUnavailable,
		"internal-error":         ErrUnavailable,
		"missing-input-response": ErrRejected,
		"invalid-input-response": ErrRejected,
		"timeout-or-duplicate":   ErrRejected,
		"something-new":          ErrRejected,
	} {
		logs.Reset()
		ts, _ := fake(t, 200, `{"success":false,"error-codes":["`+code+`"]}`)
		err := ts.Verify(context.Background(), "the-token", "login", "203.0.113.7")
		level := "level=WARN"
		if want == ErrUnavailable {
			level = "level=ERROR"
		}
		line := logs.String()
		if !errors.Is(err, want) || !strings.Contains(line, code) || !strings.Contains(line, level) || strings.Contains(line, "the-secret") || strings.Contains(line, "the-token") {
			t.Errorf("%s: %v, logged %q", code, err, line)
		}
	}
}
