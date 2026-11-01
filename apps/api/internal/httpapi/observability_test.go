package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/observe"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// syncBuffer is a log destination safe for concurrent writes.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureLogs sends every log line to the returned buffer, as JSON at
// level, until the test ends.
func captureLogs(t testing.TB, level slog.Level) *syncBuffer {
	t.Helper()
	out := &syncBuffer{}
	previous := slog.Default()
	slog.SetDefault(observe.NewLogger(out, observe.FormatJSON, level))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return out
}

// logLines decodes captured JSON lines.
func logLines(t testing.TB, out *syncBuffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("not one JSON object per line: %q", line)
		}
		lines = append(lines, m)
	}
	return lines
}

var requestIDShape = regexp.MustCompile(`^req_[0-9A-Za-z]{14}$`)

// Every request gets a new ID of ours, sent back in X-Request-ID, whatever
// it was sent. A trusted proxy's own X-Request-ID, in the safe shape and
// sent once, goes on the access line as proxyRequestId only; anyone else's,
// or a malformed one, nowhere.
func TestRequestIDs(t *testing.T) {
	out := captureLogs(t, slog.LevelInfo)
	s := &Server{}
	s.Config.ClientIPHeader = "X-Real-Ip"
	s.Config.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	var seen string
	h := s.ClientAddress(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = observe.RequestID(r.Context()) }))
	send := func(remote string, ids ...string) (string, map[string]any) {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = remote
		for _, id := range ids {
			r.Header.Add("X-Request-ID", id)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		got := w.Header().Get("X-Request-ID")
		if got != seen || !requestIDShape.MatchString(got) {
			t.Fatalf("X-Request-ID %q, handler saw %q", got, seen)
		}
		lines := logLines(t, out)
		line := lines[len(lines)-1]
		if line["requestId"] != got {
			t.Fatalf("access line: %v", line)
		}
		return got, line
	}
	first, _ := send("198.51.100.1:1234")
	if again, _ := send("198.51.100.1:1234"); again == first {
		t.Fatalf("a repeated ID: %q", first)
	}
	id, line := send("10.1.2.3:443", "lb-7f3a.9_X")
	if id == "lb-7f3a.9_X" || line["proxyRequestId"] != "lb-7f3a.9_X" {
		t.Fatalf("a trusted proxy's ID: ours %q, line %v", id, line)
	}
	if _, line := send("10.1.2.3:443"); line["proxyRequestId"] != nil {
		t.Fatalf("no proxy ID sent: %v", line)
	}
	ignored := map[string][]string{
		"from an untrusted peer":  {"lb-7f3a.9_X"},
		"with a newline":          {"lb\nforged"},
		"with a space":            {"lb forged"},
		"with a quote":            {`lb"forged`},
		"too long":                {strings.Repeat("a", 65)},
		"empty":                   {""},
		"sent twice":              {"one", "two"},
		"non-ASCII":               {"lbé"},
		"with a CRLF and a field": {"x\r\nX-Admin-Key: y"},
	}
	for name, ids := range ignored {
		remote := "10.1.2.3:443"
		if name == "from an untrusted peer" {
			remote = "198.51.100.1:1234"
		}
		if _, line := send(remote, ids...); line["proxyRequestId"] != nil {
			t.Errorf("%s: logged %v", name, line["proxyRequestId"])
		}
	}
}

// One line per request: the pattern or label as its route, never the path
// or query, with status, size, user and address; health checks at debug,
// and what ACCESS_LOG leaves out.
func TestAccessLine(t *testing.T) {
	out := captureLogs(t, slog.LevelDebug)
	s := &Server{}
	h := s.ClientAddress(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			observe.SetRoute(r.Context(), "GET /api/health")
		case "/fail":
			observe.SetRoute(r.Context(), "GET /fail")
			observe.SetUser(r.Context(), "usr_0123456789abcd")
			http.Error(w, "nope", http.StatusBadRequest)
			return
		default:
			observe.SetRoute(r.Context(), "page /auth/*")
		}
		_, _ = w.Write([]byte("hello"))
	}))
	get := func(target string) {
		r := httptest.NewRequest("GET", target, nil)
		r.RemoteAddr = "203.0.113.9:1234"
		h.ServeHTTP(httptest.NewRecorder(), r)
	}
	get("/auth/forgot-password/sometoken?q=searched")
	get("/api/health")
	get("/fail?email=ada@example.com")
	lines := logLines(t, out)
	if len(lines) != 3 {
		t.Fatalf("lines: %v", lines)
	}
	first := lines[0]
	if first["msg"] != "request" || first["route"] != "page /auth/*" || first["status"] != 200.0 || first["bytes"] != 5.0 ||
		first["method"] != "GET" || first["ip"] != "203.0.113.9" || first["user"] != "" || first["level"] != "INFO" ||
		!requestIDShape.MatchString(first["requestId"].(string)) || first["durationMs"] == nil {
		t.Errorf("access line: %v", first)
	}
	if lines[1]["level"] != "DEBUG" || lines[1]["route"] != "GET /api/health" {
		t.Errorf("health line: %v", lines[1])
	}
	if lines[2]["status"] != 400.0 || lines[2]["user"] != "usr_0123456789abcd" {
		t.Errorf("failure line: %v", lines[2])
	}
	for _, leak := range []string{"sometoken", "searched", "ada@example.com", "/auth/forgot-password", "?"} {
		if strings.Contains(out.String(), leak) {
			t.Errorf("%q logged: %s", leak, out.String())
		}
	}

	// An invented method is logged as OTHER, however long.
	odd := httptest.NewRequest("GET", "/auth/x", nil)
	odd.Method = strings.Repeat("Z", 1000)
	h.ServeHTTP(httptest.NewRecorder(), odd)
	if lines := logLines(t, out); lines[len(lines)-1]["method"] != "OTHER" {
		t.Fatalf("an invented method: %v", lines[len(lines)-1])
	}

	// ACCESS_LOG=errors keeps only failures (and slow requests); off, none.
	s.Config.AccessLog = config.AccessLogErrors
	start := len(logLines(t, out))
	get("/auth/login")
	get("/fail")
	if lines := logLines(t, out)[start:]; len(lines) != 1 || lines[0]["status"] != 400.0 {
		t.Errorf("errors only: %v", lines)
	}
	s.Config.AccessLog = config.AccessLogOff
	start = len(logLines(t, out))
	get("/fail")
	if lines := logLines(t, out)[start:]; len(lines) != 0 {
		t.Errorf("off: %v", lines)
	}
	// At info, health checks aren't logged at all.
	s.Config.AccessLog = config.AccessLogAll
	slog.SetDefault(observe.NewLogger(out, observe.FormatJSON, slog.LevelInfo))
	start = len(logLines(t, out))
	get("/api/health")
	if lines := logLines(t, out)[start:]; len(lines) != 0 {
		t.Errorf("health at info: %v", lines)
	}
}

// A panic is a 500 with the request's ID, never its value; it's logged at
// error with the ID and stack. Headers the handler set are dropped, a
// panic once the answer started cuts the connection rather than writing a
// second status, and http.ErrAbortHandler passes through.
func TestRecoverPanics(t *testing.T) {
	out := captureLogs(t, slog.LevelInfo)
	s := &Server{}
	s.Config.Origin = "http://localhost:3000"
	var reported []observe.Problem
	observe.SetReporter(reporterFunc(func(p observe.Problem) { reported = append(reported, p) }))
	defer observe.SetReporter(nil)
	panicking := func(after func(w http.ResponseWriter)) http.Handler {
		return s.ClientAddress(s.Security(RecoverPanics(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			observe.SetRoute(r.Context(), "GET /api/boom")
			after(w)
			panic("secret-panic-value")
		}))))
	}
	w := httptest.NewRecorder()
	panicking(func(w http.ResponseWriter) {
		http.SetCookie(w, &http.Cookie{Name: "lewcontext_session", Value: "half-made"})
	}).ServeHTTP(w, httptest.NewRequest("GET", "/api/boom", nil))
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 500 {
		t.Fatalf("panic answer: %d %q", w.Code, w.Body.String())
	}
	id := w.Header().Get("X-Request-ID")
	if body["error"] != somethingWentWrong || body["requestId"] != id || !requestIDShape.MatchString(id) || len(body) != 2 {
		t.Fatalf("panic body: %v", body)
	}
	if strings.Contains(w.Body.String(), "secret-panic-value") || strings.Contains(w.Body.String(), "goroutine") {
		t.Fatalf("the panic reached the client: %q", w.Body.String())
	}
	if w.Header().Get("Set-Cookie") != "" || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("panic headers: %v", w.Header())
	}
	lines := logLines(t, out)
	if len(lines) != 2 {
		t.Fatalf("want the panic and the access line: %v", lines)
	}
	logged := lines[0]
	if logged["level"] != "ERROR" || logged["requestId"] != id || logged["route"] != "GET /api/boom" || logged["panic"] != "secret-panic-value" ||
		!strings.Contains(logged["stack"].(string), "TestRecoverPanics") {
		t.Fatalf("panic line: %v", logged)
	}
	if lines[1]["status"] != 500.0 || lines[1]["requestId"] != id {
		t.Fatalf("access line: %v", lines[1])
	}
	if len(reported) != 1 || reported[0].Source != observe.SourcePanic || reported[0].RequestID != id || reported[0].Route != "GET /api/boom" || len(reported[0].Stack) == 0 {
		t.Fatalf("reported: %+v", reported)
	}

	// Once the answer has started, the connection is cut: one status only.
	started := &countingWriter{ResponseRecorder: httptest.NewRecorder()}
	func() {
		defer func() {
			if p := recover(); p != http.ErrAbortHandler {
				t.Errorf("want http.ErrAbortHandler, got %v", p)
			}
		}()
		panicking(func(w http.ResponseWriter) {
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"partial":`))
		}).ServeHTTP(started, httptest.NewRequest("GET", "/api/boom", nil))
	}()
	if started.headers != 1 || started.Code != 200 || strings.Contains(started.Body.String(), "Something went wrong") {
		t.Fatalf("after the answer started: %d statuses, %d %q", started.headers, started.Code, started.Body.String())
	}
	if lines := logLines(t, out); lines[len(lines)-1]["aborted"] != true || lines[len(lines)-1]["status"] != 200.0 {
		t.Fatalf("an aborted answer's access line: %v", lines[len(lines)-1])
	}
	// A flush starts the answer too, though no status was written: nothing
	// is added after it.
	flushed := &countingWriter{ResponseRecorder: httptest.NewRecorder()}
	func() {
		defer func() {
			if p := recover(); p != http.ErrAbortHandler {
				t.Errorf("after a flush: want http.ErrAbortHandler, got %v", p)
			}
		}()
		panicking(func(w http.ResponseWriter) {
			_, _ = w.Write([]byte("partial"))
			w.(http.Flusher).Flush()
		}).ServeHTTP(flushed, httptest.NewRequest("GET", "/api/boom", nil))
	}()
	if flushed.Body.String() != "partial" || flushed.headers != 0 || !flushed.Flushed {
		t.Fatalf("after a flush: %d statuses, %q", flushed.headers, flushed.Body.String())
	}
	before0 := len(logLines(t, out))
	func() {
		defer func() { _ = recover() }()
		panicking(func(w http.ResponseWriter) {
			if err := http.NewResponseController(w).Flush(); err != nil {
				t.Errorf("ResponseController.Flush: %v", err)
			}
		}).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/boom", nil))
	}()
	if lines := logLines(t, out)[before0:]; len(lines) != 2 || lines[1]["aborted"] != true || lines[1]["status"] != 200.0 {
		t.Fatalf("a flush through ResponseController: %v", lines)
	}
	// http.ErrAbortHandler isn't logged or answered: it passes through.
	before := len(logLines(t, out))
	func() {
		defer func() {
			if p := recover(); p != http.ErrAbortHandler {
				t.Errorf("want http.ErrAbortHandler through, got %v", p)
			}
		}()
		h := s.ClientAddress(RecoverPanics(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })))
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	}()
	if lines := logLines(t, out)[before:]; len(lines) != 1 || lines[0]["msg"] != "request" || lines[0]["status"] != 0.0 {
		t.Fatalf("an abort logged more than its access line: %v", lines)
	}
	// Without ClientAddress (a handler under test) it still answers once.
	w = httptest.NewRecorder()
	RecoverPanics(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(errors.New("x")) })).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 500 || strings.Contains(w.Body.String(), "requestId") {
		t.Fatalf("outside ClientAddress: %d %q", w.Code, w.Body.String())
	}
}

// A panic's value is logged only as observe.PanicValue allows: a request,
// its headers, cookies or URL redacted, any other non-message by its type,
// a message cut short; and so is what reaches the reporter.
func TestPanicValuesAreRedacted(t *testing.T) {
	out := captureLogs(t, slog.LevelInfo)
	var reported []observe.Problem
	observe.SetReporter(reporterFunc(func(p observe.Problem) { reported = append(reported, p) }))
	defer observe.SetReporter(nil)
	secret := "s3cr3t-token-value"
	type holder struct{ Token string }
	cases := map[string]func(r *http.Request) any{
		"the request": func(r *http.Request) any { return r },
		"its headers": func(r *http.Request) any { return r.Header },
		"its URL":     func(r *http.Request) any { return r.URL },
		"a cookie":    func(r *http.Request) any { return &http.Cookie{Name: "x", Value: secret} },
		"a struct":    func(r *http.Request) any { return holder{Token: secret} },
		"a long text": func(r *http.Request) any { return strings.Repeat("x ", 300) + secret },
	}
	want := map[string]string{"the request": observe.Redacted, "its headers": observe.Redacted, "its URL": observe.Redacted, "a cookie": observe.Redacted, "a struct": "httpapi.holder"}
	for name, value := range cases {
		s := &Server{}
		h := s.ClientAddress(RecoverPanics(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic(value(r)) })))
		r := httptest.NewRequest("GET", "/auth/forgot-password/"+secret+"?q="+secret, nil)
		r.Header.Set("X-Admin-Key", secret)
		r.AddCookie(&http.Cookie{Name: "lewcontext_session", Value: secret})
		start := len(logLines(t, out))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		lines := logLines(t, out)[start:]
		if w.Code != 500 || len(lines) != 2 {
			t.Fatalf("%s: %d %v", name, w.Code, lines)
		}
		got := lines[0]["panic"].(string)
		if expected, ok := want[name]; ok && got != expected {
			t.Errorf("%s: logged %q, want %q", name, got, expected)
		}
		if name == "a long text" && (len(got) > 520 || !strings.HasSuffix(got, "…")) {
			t.Errorf("a long text: %d bytes logged", len(got))
		}
	}
	if strings.Contains(out.String(), secret) {
		t.Fatalf("a secret in a panic's line:\n%s", out.String())
	}
	for _, p := range reported {
		if strings.Contains(p.Err.Error(), secret) {
			t.Fatalf("a secret reported: %v", p.Err)
		}
	}
}

// A panic outside RecoverPanics (in ClientAddress, Security or the
// dispatch) is caught by ClientAddress: logged at error through the same
// path, reported, and answered with the generic 500 and the request's ID;
// once the answer has started, the connection is cut.
func TestClientAddressRecovers(t *testing.T) {
	out := captureLogs(t, slog.LevelInfo)
	var reported []observe.Problem
	observe.SetReporter(reporterFunc(func(p observe.Problem) { reported = append(reported, p) }))
	defer observe.SetReporter(nil)
	s := &Server{}
	h := s.ClientAddress(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/started" {
			_, _ = w.Write([]byte("partial"))
		}
		panic(r.Header)
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/x", nil)
	r.Header.Set("X-Admin-Key", "s3cr3t-key")
	h.ServeHTTP(w, r)
	id := w.Header().Get("X-Request-ID")
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 500 || body["requestId"] != id || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("answer: %d %q %v", w.Code, w.Body.String(), w.Header())
	}
	lines := logLines(t, out)
	if len(lines) != 2 || lines[0]["msg"] != "request panicked" || lines[0]["level"] != "ERROR" || lines[0]["panic"] != observe.Redacted ||
		lines[0]["requestId"] != id || lines[1]["status"] != 500.0 || len(reported) != 1 || strings.Contains(out.String(), "s3cr3t-key") {
		t.Fatalf("lines: %v, reported %+v", lines, reported)
	}
	func() {
		defer func() {
			if p := recover(); p != http.ErrAbortHandler {
				t.Errorf("want http.ErrAbortHandler, got %v", p)
			}
		}()
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/started", nil))
	}()
	if lines := logLines(t, out)[2:]; len(lines) != 2 || lines[1]["aborted"] != true || lines[1]["status"] != 200.0 {
		t.Fatalf("after the answer started: %v", lines)
	}
}

type reporterFunc func(observe.Problem)

func (f reporterFunc) Report(_ context.Context, p observe.Problem) { f(p) }

// countingWriter counts status lines written.
type countingWriter struct {
	*httptest.ResponseRecorder
	headers int
}

func (w *countingWriter) WriteHeader(status int) {
	w.headers++
	w.ResponseRecorder.WriteHeader(status)
}

// InternalError answers the generic 500 with the request's ID, logs the
// error under it with the route, and reports it.
func TestInternalError(t *testing.T) {
	out := captureLogs(t, slog.LevelInfo)
	s := &Server{}
	h := s.ClientAddress(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observe.SetRoute(r.Context(), "GET /api/thing")
		InternalError(w, r, errors.New("the database said no"))
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/thing", nil))
	id := w.Header().Get("X-Request-ID")
	if w.Code != 500 || w.Body.String() != `{"error":"`+somethingWentWrong+`","requestId":"`+id+`"}`+"\n" {
		t.Fatalf("answer: %d %q", w.Code, w.Body.String())
	}
	lines := logLines(t, out)
	if len(lines) != 2 || lines[0]["msg"] != "request failed" || lines[0]["requestId"] != id || lines[0]["route"] != "GET /api/thing" || lines[0]["error"] != "the database said no" {
		t.Fatalf("lines: %v", lines)
	}
}

// reports collects what observe.Report is handed until the test ends.
func reports(t testing.TB) *reported {
	t.Helper()
	got := &reported{}
	observe.SetReporter(got)
	t.Cleanup(func() { observe.SetReporter(nil) })
	return got
}

type reported struct {
	mu       sync.Mutex
	problems []observe.Problem
}

func (r *reported) Report(_ context.Context, p observe.Problem) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.problems = append(r.problems, p)
}

func (r *reported) all() []observe.Problem {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]observe.Problem(nil), r.problems...)
}

// An error's text is scrubbed before it's logged: the log keeps the
// request's ID, which leads to everything else. The report gets the error
// itself, which the collector scrubs.
func TestInternalErrorLogsScrubbedText(t *testing.T) {
	out := captureLogs(t, slog.LevelInfo)
	got := reports(t)
	s := &Server{}
	failure := errors.New(`ERROR: invalid input syntax for type uuid: "ada@example.com" from 203.0.113.9 for usr_2yEHYLwiwPVhLW (SQLSTATE 22P02)`)
	h := s.ClientAddress(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observe.SetRoute(r.Context(), "GET /api/thing")
		InternalError(w, r, failure)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/thing", nil))
	line := logLines(t, out)[0]
	if line["error"] != `ERROR: invalid input syntax for type uuid: "…" from <ip> for <id> (SQLSTATE 22P02)` {
		t.Fatalf("logged %q", line["error"])
	}
	for _, value := range []string{"ada@example.com", "203.0.113.9 for", "usr_2yEHYLwiwPVhLW"} {
		if strings.Contains(out.String(), value) {
			t.Errorf("the log holds %q", value)
		}
	}
	if p := got.all(); len(p) != 1 || p[0].Source != observe.SourceRequest || p[0].Route != "GET /api/thing" || !errors.Is(p[0].Err, failure) {
		t.Fatalf("reported %+v", p)
	}
}

// A client that went away (its request's context canceled) is no failure:
// no 500, no report, no error line; the access line says aborted, and a
// debug line why. A timeout (the request's deadline) still is one.
func TestClientGoneIsNoError(t *testing.T) {
	out := captureLogs(t, slog.LevelDebug)
	got := reports(t)
	s := &Server{}
	var cause error
	h := s.ClientAddress(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observe.SetRoute(r.Context(), "GET /api/thing")
		InternalError(w, r, fmt.Errorf("select projects: %w", cause))
	}))

	cause = context.Canceled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/thing", nil).WithContext(ctx))
	if w.Body.Len() != 0 || len(got.all()) != 0 {
		t.Fatalf("answered %d %q, reported %+v", w.Code, w.Body.String(), got.all())
	}
	lines := logLines(t, out)
	if len(lines) != 2 || lines[0]["msg"] != "request aborted by the client" || lines[0]["level"] != "DEBUG" ||
		lines[1]["msg"] != "request" || lines[1]["aborted"] != true || lines[1]["status"] != float64(0) {
		t.Fatalf("lines: %v", lines)
	}

	// The request's own deadline is a failure, and so is a canceled error
	// while the client is still there.
	for _, c := range []struct {
		cause error
		ctx   func() (context.Context, context.CancelFunc)
	}{
		{context.DeadlineExceeded, func() (context.Context, context.CancelFunc) { return context.WithTimeout(context.Background(), 0) }},
		{context.Canceled, func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }},
	} {
		cause = c.cause
		ctx, cancel := c.ctx()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/thing", nil).WithContext(ctx))
		cancel()
		if w.Code != 500 {
			t.Errorf("%v: %d", c.cause, w.Code)
		}
	}
	if len(got.all()) != 2 {
		t.Fatalf("reported %+v", got.all())
	}
}

// BenchmarkRequestOverhead is what ClientAddress costs each request: the
// client address, the request's ID, the record and its context, and the
// access line, written as JSON to a discarding writer. The budget is 20 µs
// and 3 allocations per request beyond what it cost before the request ID
// and access line (docs/api/observability.md). Run it with
// go test ./internal/httpapi -run '^$' -bench RequestOverhead -benchmem.
func BenchmarkRequestOverhead(b *testing.B) {
	previous := slog.Default()
	slog.SetDefault(observe.NewLogger(io.Discard, observe.FormatJSON, slog.LevelInfo))
	defer slog.SetDefault(previous)
	s := &Server{Metrics: observe.NewRegistry()}
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observe.SetRoute(r.Context(), "GET /api/workspaces/{workspace}/projects")
		w.WriteHeader(http.StatusOK)
	})
	h := s.ClientAddress(ok)
	r := httptest.NewRequest("GET", "/api/workspaces/wsp_2yEHYLwiwPVhLW/projects", nil)
	r.RemoteAddr = "203.0.113.9:1234"
	w := &discardWriter{header: http.Header{}}
	b.ReportAllocs()
	for b.Loop() {
		clear(w.header)
		h.ServeHTTP(w, r)
	}
}

// discardWriter is a response writer that keeps nothing.
type discardWriter struct{ header http.Header }

func (w *discardWriter) Header() http.Header         { return w.header }
func (w *discardWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *discardWriter) WriteHeader(int)             {}
