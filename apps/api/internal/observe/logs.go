package observe

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"unicode/utf8"
)

// Log formats (LOG_FORMAT): one JSON object per line, for a log collector,
// or slog's key=value text, for people.
const (
	FormatJSON = "json"
	FormatText = "text"
)

// Redacted replaces the value of an attribute the redaction backstop drops.
const Redacted = "[redacted]"

// NewLogger is the one logger every command installs with slog.SetDefault:
// format (FormatJSON or FormatText) at level and above, written to w, with
// the request's ID on every line logged with a request's context, and
// secret-looking attributes redacted (redact).
func NewLogger(w io.Writer, format string, level slog.Level) *slog.Logger {
	return slog.New(NewHandler(w, format, level))
}

// NewHandler is NewLogger's handler.
func NewHandler(w io.Writer, format string, level slog.Level) slog.Handler {
	options := &slog.HandlerOptions{Level: level, ReplaceAttr: redact}
	if format == FormatText {
		return contextHandler{slog.NewTextHandler(w, options)}
	}
	return contextHandler{slog.NewJSONHandler(w, options)}
}

// UseForCommand installs NewLogger for a command run by hand (migrate,
// seed, prune, admin), writing to stderr, and keeps the log package's
// output as it was: plain lines on stderr. Those commands print their
// results and refusals with log.Fatal and log.Printf, which slog.SetDefault
// would otherwise send through the handler at info, where LOG_LEVEL=warn
// would hide why a command failed.
func UseForCommand(format string, level slog.Level) {
	slog.SetDefault(NewLogger(os.Stderr, format, level))
	log.SetOutput(os.Stderr)
	log.SetFlags(log.LstdFlags)
}

// contextHandler adds the request's ID (RequestID) to every line logged
// with a request's context, as requestId, unless the line already starts
// with it (the access line does). Request code logs with the *Context
// functions (slog.ErrorContext(r.Context(), …)) to get it.
type contextHandler struct {
	inner slog.Handler
}

func (h contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := RequestID(ctx); id != "" && !startsWithRequestID(r) {
		r.AddAttrs(slog.String(RequestIDKey, id))
	}
	return h.inner.Handle(ctx, r)
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{h.inner.WithAttrs(attrs)}
}

// WithGroup nests what follows, the request's ID included: a line logged
// through a grouped logger carries it inside the group.
func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{h.inner.WithGroup(name)}
}

// RequestIDKey is the request's ID's key on every line.
const RequestIDKey = "requestId"

// ProxyRequestIDKey is the access line's key for the X-Request-ID a trusted
// proxy sent, kept apart from ours for correlation only.
const ProxyRequestIDKey = "proxyRequestId"

func startsWithRequestID(r slog.Record) bool {
	first := false
	r.Attrs(func(a slog.Attr) bool {
		first = a.Key == RequestIDKey
		return false
	})
	return first
}

// sensitiveKeyParts are what an attribute's key (or a group around it)
// contains, in any case, when its value is redacted: code shouldn't log
// such values at all, and this catches a mistake. Broad on purpose: a key
// such as "passed" or "bypass" is redacted too, and a key that is safe
// despite holding a part goes in safeKeys.
var sensitiveKeyParts = []string{
	"password", "pass", "token", "secret", "cookie", "authorization", "session", "csrf",
	"code", "otp", "mfa", "key", "digest", "hash", "email",
}

// safeKeys are logged as they are without checking: the access line's, the
// request's IDs, and what a panic's line holds (its value is made safe by
// PanicValue first). None holds a sensitive part today; listing them keeps
// a later part (or a key renamed to hold one) from blanking them.
var safeKeys = map[string]bool{
	RequestIDKey:      true,
	ProxyRequestIDKey: true,
	"method":          true,
	"route":           true,
	"status":          true,
	"durationMs":      true,
	"bytes":           true,
	"user":            true,
	"ip":              true,
	"aborted":         true,
	"panic":           true,
	"stack":           true,
}

// redact is every handler's ReplaceAttr, the backstop for a secret logged
// by mistake: an attribute whose key, or any group it's in, holds a
// sensitive part (sensitiveKeyParts) is logged as Redacted, and so is a
// value that is a request, its headers, cookies or a URL (which can hold a
// token in its path or query), whatever its key. slog calls it for every
// attribute inside groups too, with the groups' names.
func redact(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 && builtin(a.Key) {
		// With a ReplaceAttr, slog hands the JSON handler the level as a
		// value it marshals through encoding/json; as a string it costs no
		// allocation and reads the same ("INFO").
		if a.Key == slog.LevelKey && a.Value.Kind() == slog.KindAny {
			if level, ok := a.Value.Any().(slog.Level); ok {
				return slog.String(slog.LevelKey, level.String())
			}
		}
		return a
	}
	if a.Value.Kind() == slog.KindAny && revealing(a.Value.Any()) {
		return slog.String(a.Key, Redacted)
	}
	for _, group := range groups {
		if sensitive(group) {
			return slog.String(a.Key, Redacted)
		}
	}
	if !safeKeys[a.Key] && sensitive(a.Key) {
		return slog.String(a.Key, Redacted)
	}
	return a
}

// builtin reports whether key is one of slog's own: the time, level,
// message and source of a line.
func builtin(key string) bool {
	return key == slog.TimeKey || key == slog.LevelKey || key == slog.MessageKey || key == slog.SourceKey
}

// revealing reports whether v is something whose logged form would carry
// what a request was sent with: a request, its headers or cookies, a URL or
// a query.
func revealing(v any) bool {
	switch v.(type) {
	case *http.Request, http.Header, *http.Cookie, []*http.Cookie, *url.URL, url.URL, url.Values:
		return true
	}
	return false
}

// maxPanicValue is how much of a panic's message is logged and reported.
const maxPanicValue = 512

// PanicValue is what may be logged of a panic's value p: Redacted for a
// request, its headers, cookies or a URL (revealing); the message of a
// string or an error, scrubbed (Scrub) and then cut to 512 bytes, so a
// token cut in half can't slip past the scrubber; for anything else only
// its type, since its printed form could hold anything. The handler's
// redaction can't see inside a value already formatted, so every panic
// goes through this first.
func PanicValue(p any) string {
	if revealing(p) {
		return Redacted
	}
	switch v := p.(type) {
	case string:
		return truncate(Scrub(v), maxPanicValue)
	case error:
		return truncate(ScrubError(v), maxPanicValue)
	}
	return fmt.Sprintf("%T", p)
}

// truncate cuts s to at most n bytes, on a UTF-8 boundary.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}

// sensitive reports whether key holds one of sensitiveKeyParts, ignoring
// ASCII case, without allocating.
func sensitive(key string) bool {
	for _, part := range sensitiveKeyParts {
		if containsFold(key, part) {
			return true
		}
	}
	return false
}

// containsFold reports whether s contains lower (lower-case ASCII),
// ignoring the case of ASCII letters in s.
func containsFold(s, lower string) bool {
	for i := 0; i+len(lower) <= len(s); i++ {
		match := true
		for j := 0; j < len(lower); j++ {
			c := s[i+j]
			if 'A' <= c && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != lower[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
