package observe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"
)

// requestContext is a request's context with id, as httpapi.ClientAddress
// makes it.
func requestContext(id string) context.Context {
	q := Begin(nil, "203.0.113.9")
	q.setID(id)
	return WithRequest(context.Background(), q)
}

// Both formats carry the request's ID on a line logged with its context,
// once, and nothing outside a request.
func TestHandlersCarryTheRequestID(t *testing.T) {
	for _, format := range []string{FormatJSON, FormatText} {
		var out bytes.Buffer
		logger := NewLogger(&out, format, slog.LevelInfo)
		ctx := requestContext("req_0123456789abcd")
		logger.InfoContext(ctx, "inside", "a", 1)
		logger.LogAttrs(ctx, slog.LevelInfo, "request", slog.String(RequestIDKey, "req_0123456789abcd"))
		logger.Info("outside")
		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		if len(lines) != 3 {
			t.Fatalf("%s: %q", format, out.String())
		}
		for i, line := range lines[:2] {
			if strings.Count(line, "req_0123456789abcd") != 1 || !strings.Contains(line, "requestId") {
				t.Errorf("%s line %d: want the ID once: %s", format, i, line)
			}
		}
		if strings.Contains(lines[2], "requestId") {
			t.Errorf("%s: a line outside a request has an ID: %s", format, lines[2])
		}
	}
	// JSON is one object per line, with the ID as a field.
	var out bytes.Buffer
	NewLogger(&out, FormatJSON, slog.LevelInfo).InfoContext(requestContext("req_0123456789abcd"), "inside")
	var line map[string]any
	if err := json.Unmarshal(out.Bytes(), &line); err != nil || line[RequestIDKey] != "req_0123456789abcd" || line["msg"] != "inside" {
		t.Fatalf("JSON line: %v %s", err, out.String())
	}
}

// The level filters, below LOG_LEVEL nothing is written.
func TestHandlerLevel(t *testing.T) {
	var out bytes.Buffer
	logger := NewLogger(&out, FormatJSON, slog.LevelWarn)
	logger.Info("quiet")
	logger.Debug("quieter")
	logger.Warn("loud")
	if strings.Count(out.String(), "\n") != 1 || !strings.Contains(out.String(), "loud") {
		t.Fatalf("level: %q", out.String())
	}
}

// The backstop: a key that looks secret, in any case, inside groups or a
// group named like one, through With too, is redacted; so is a request,
// its headers, cookies or a URL, whatever the key. Safe keys and the
// request's ID are kept.
func TestRedaction(t *testing.T) {
	secret := "s3cr3t-value"
	header := http.Header{"Cookie": {"lewcontext_session=" + secret}}
	link, _ := url.Parse("https://app.example.com/auth/forgot-password/" + secret + "?q=" + secret)
	for _, format := range []string{FormatJSON, FormatText} {
		var out bytes.Buffer
		logger := NewLogger(&out, format, slog.LevelDebug)
		ctx := requestContext("req_0123456789abcd")
		logger.InfoContext(ctx, "keys",
			"password", secret, "newPassword", secret, "csrfToken", secret, "X-CSRF-Token", secret,
			"APP_KEY", secret, "adminKey", secret, "X-Admin-Key", secret, "Set-Cookie", secret,
			"Authorization", secret, "code", secret, "recoveryCode", secret, "SECRET", secret,
		)
		logger.InfoContext(ctx, "groups",
			slog.Group("request", slog.Group("headers", slog.String("Cookie", secret), slog.String("ok", "visible-1"))),
			slog.Group("token", slog.String("value", secret)),
		)
		logger.With("sessionToken", secret).With(slog.Group("outer", slog.String("apiKey", secret))).InfoContext(ctx, "with")
		logger.WithGroup("cookie").InfoContext(ctx, "grouped", "value", secret)
		logger.InfoContext(ctx, "values", "headers", header, "url", link, "query", link.Query(), "request", &http.Request{URL: link, Header: header})
		logger.InfoContext(ctx, "safe", "route", "GET /api/workspaces/{workspace}", "status", 500, "user", "usr_0123456789abcd", "ip", "203.0.113.9", "error", errors.New("visible-2"))
		got := out.String()
		if strings.Contains(got, secret) {
			t.Errorf("%s: a secret was logged:\n%s", format, got)
		}
		for _, want := range []string{"visible-1", "visible-2", "GET /api/workspaces/{workspace}", "usr_0123456789abcd", "203.0.113.9", "req_0123456789abcd", Redacted} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: %q missing:\n%s", format, want, got)
			}
		}
	}
}

func TestSensitiveKeys(t *testing.T) {
	for key, want := range map[string]bool{
		"password": true, "PASSWORD": true, "currentPassword": true, "token": true, "csrfToken": true,
		"secret": true, "cookie": true, "Set-Cookie": true, "authorization": true, "code": true,
		"recoveryCode": true, "key": true, "X-Admin-Key": true, "APP_KEY": true,
		"pass": true, "SMTP_PASS": true, "session": true, "sessionToken": true, "csrf": true, "X-CSRF": true,
		"otp": true, "totp": true, "mfa": true, "mfaSecret": true, "digest": true, "tokenDigest": true,
		"hash": true, "password_hash": true, "email": true, "toEmail": true,
		// Over-redaction, accepted: none is a key the code logs today.
		"passed": true, "bypass": true,
		// Keys the code logs: none may be blanked.
		"requestId": false, "proxyRequestId": false, "route": false, "user": false, "ip": false, "error": false,
		"listener": false, "template": false, "message_id": false, "header": false, "": false,
		"method": false, "status": false, "durationMs": false, "bytes": false, "aborted": false, "panic": false,
		"stack": false, "event": false, "id": false, "attempt": false, "limit": false, "window": false,
		"dropped": false, "address": false, "pages": false, "trustedProxies": false, "ranges": false,
		"host": false, "port": false, "tls": false, "from": false, "file": false, "clientIP": false, "delay": false,
		"occurrences": false, "groups": false,
	} {
		if got := sensitive(key) && !safeKeys[key]; got != want {
			t.Errorf("%q: redacted %v, want %v", key, got, want)
		}
	}
}

// Neither format lets a value break its line or start a new one.
func TestNoLogInjection(t *testing.T) {
	forged := "x\n{\"msg\":\"forged\"}\rlevel=ERROR msg=forged"
	for _, format := range []string{FormatJSON, FormatText} {
		var out bytes.Buffer
		NewLogger(&out, format, slog.LevelInfo).InfoContext(requestContext("req_0123456789abcd"), "request", "route", forged, "method", forged)
		if n := strings.Count(out.String(), "\n"); n != 1 || strings.Contains(out.String(), "\r") {
			t.Errorf("%s: %d lines: %q", format, n, out.String())
		}
	}
}

func TestContainsFold(t *testing.T) {
	for _, c := range []struct {
		s, sub string
		want   bool
	}{{"X-Admin-Key", "key", true}, {"KEY", "key", true}, {"ke", "key", false}, {"", "key", false}, {"monkeys", "key", true}, {"kEy", "key", true}} {
		if got := containsFold(c.s, c.sub); got != c.want {
			t.Errorf("containsFold(%q, %q) = %v", c.s, c.sub, got)
		}
	}
}

type recorded []Problem

func (r *recorded) Report(_ context.Context, p Problem) { *r = append(*r, p) }

// Problems reach the reporter once one is set, and nowhere before.
func TestReport(t *testing.T) {
	Report(context.Background(), Problem{Source: SourceRequest})
	var got recorded
	SetReporter(&got)
	defer SetReporter(nil)
	Report(context.Background(), Problem{Source: SourcePanic, Route: "GET /x"})
	if len(got) != 1 || got[0].Source != SourcePanic {
		t.Fatalf("reported: %+v", got)
	}
	SetReporter(nil)
	Report(context.Background(), Problem{Source: SourceWorker})
	if len(got) != 1 {
		t.Fatalf("reported after removal: %+v", got)
	}
}

// A panic's value is logged as a message only when it is one, cut short;
// what could carry a request is redacted; anything else is its type.
func TestPanicValue(t *testing.T) {
	r := httptest.NewRequest("GET", "/auth/forgot-password/tok?q=x", nil)
	for _, c := range []struct {
		value any
		want  string
	}{
		{r, Redacted}, {r.Header, Redacted}, {r.URL, Redacted}, {*r.URL, Redacted}, {r.URL.Query(), Redacted},
		{&http.Cookie{}, Redacted}, {[]*http.Cookie{}, Redacted},
		{"plain message", "plain message"}, {errors.New("an error"), "an error"},
		{42, "int"}, {struct{ Token string }{"tok"}, "struct { Token string }"}, {map[string]string{"k": "v"}, "map[string]string"},
	} {
		if got := PanicValue(c.value); got != c.want {
			t.Errorf("PanicValue(%T) = %q, want %q", c.value, got, c.want)
		}
	}
	// Scrubbed before it's cut, so a token at the cut doesn't leave a piece.
	if cut := PanicValue(strings.Repeat("a ", 250) + "Zx9_aB3-qL0vT7mK2pR5sW8yE1nH4jD6fG9cV0bX3zA"); strings.Contains(cut, "Zx9") || !strings.HasSuffix(cut, "<token>") {
		t.Errorf("a token at the cut: %q", cut)
	}
	long := PanicValue(strings.Repeat("é", 400))
	if len(long) > maxPanicValue+len("…") || !strings.HasSuffix(long, "…") || !utf8.ValidString(long) {
		t.Errorf("a long message: %d bytes, valid %v", len(long), utf8.ValidString(long))
	}
}

func TestMethod(t *testing.T) {
	for method, want := range map[string]string{"GET": "GET", "DELETE": "DELETE", "PATCH": "PATCH", "get": "OTHER", "PROPFIND": "OTHER", strings.Repeat("X", 9000): "OTHER", "": "OTHER"} {
		if got := Method(method); got != want {
			t.Errorf("Method(%.10q) = %q", method, got)
		}
	}
}
