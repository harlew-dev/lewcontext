package captcha

import (
	"context"
	"errors"
	"io"
	"lewcontext/api/internal/config"
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
	if form.Get("secret") != "the-secret" || form.Get("response") != "tok" || form.Get("remoteip") != "203.0.113.7" || len(form.Get("idempotency_key")) != 43 {
		t.Fatalf("sent %v", form)
	}
	for name, c := range map[string]struct {
		status int
		body   string
		action string
		want   error
	}{
		"another action (a sign-up token spent on login)": {200, ok, "register", ErrRejected},
		"another site":                      {200, `{"success":true,"hostname":"evil.example","action":"login"}`, "login", ErrRejected},
		"used or expired":                   {200, `{"success":false,"error-codes":["timeout-or-duplicate"]}`, "login", ErrRejected},
		"a test key's answer, for any host": {200, `{"success":true,"hostname":"example.com","metadata":{"result_with_testing_key":true}}`, "login", nil},
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
