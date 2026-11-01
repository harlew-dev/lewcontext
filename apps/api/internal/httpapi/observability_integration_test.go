package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/observe"
	"lewcontext/api/internal/store"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// logCapture holds every line logged, as JSON at debug, until the test ends.
type logCapture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *logCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}

func (c *logCapture) text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

func (c *logCapture) lines(t *testing.T) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(c.text()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("not a JSON line: %q", line)
		}
		lines = append(lines, m)
	}
	return lines
}

func captureLogs(t *testing.T) *logCapture {
	t.Helper()
	c := &logCapture{}
	previous := slog.Default()
	slog.SetDefault(observe.NewLogger(c, observe.FormatJSON, slog.LevelDebug))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return c
}

// request is one request the log test sends, and the route its access line
// must name.
type request struct {
	method, target, body, route string
	headers                     map[string]string
	cookies                     []*http.Cookie
	user                        string
}

func (q request) send(h http.Handler) *httptest.ResponseRecorder {
	r := httptest.NewRequest(q.method, q.target, strings.NewReader(q.body))
	r.RemoteAddr = "203.0.113.9:1234"
	r.Header.Set("User-Agent", apitest.UserAgent)
	if q.method != "GET" {
		r.Header.Set("Origin", apitest.Origin)
		r.Header.Set("Content-Type", "application/json")
	}
	for k, v := range q.headers {
		r.Header.Set(k, v)
	}
	for _, c := range q.cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// No log line ever holds a secret: a link's token in a page path, the admin
// key in its pages' paths and X-Admin-Key, the assets pass, session and CSRF
// cookies and headers, passwords and codes in bodies, searches and emails
// in query strings. Every request gets one access line naming its route by
// pattern or label, with its user.
func TestNothingSecretInTheLogs(t *testing.T) {
	s, db := server(t)
	s.Pages = apitest.Pages(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	logs := captureLogs(t)

	session := apitest.SignedIn(t, db, f)
	userID := apitest.PublicIDOf(t, db, session.UserID)
	workspace := apitest.OwnedWorkspace(t, db, f, session.UserID)
	reset := apitest.Must(factory.PasswordResets.State(factory.ResetFor(session.UserID)).CreateOne(ctx, db, f))(t)
	invitation := apitest.Must(factory.InvitationsTo(workspace.ID).State(factory.Sent).CreateOne(ctx, db, f))(t)
	blockLink := apitest.Must(factory.EmailBlockLinks.State(factory.BlockLinkFor("blocked-person@example.com")).CreateOne(ctx, db, f))(t)
	admin := apitest.WithMFA(t, db, f)
	apitest.Must(factory.Admins.State(factory.AdminFor(admin.User.ID)).CreateOne(ctx, db, f))(t)
	adminSession := apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(admin.User.ID)).CreateOne(ctx, db, f))(t)
	key := factory.AdminRouteKey
	door := apitest.Get(h, "GET", "/admin/"+key, nil)
	pass := apitest.CookieNamed(door, "lewcontext_admin_assets")
	if door.Code != 200 || pass == nil {
		t.Fatalf("no assets pass: %d", door.Code)
	}
	searched, emailSearched, password, code := "needle-searched-for", "looked-up@example.com", "typed-password-123", "246810"
	adminHeaders := map[string]string{"X-Admin-Key": key, "X-CSRF-Token": adminSession.CSRFToken}
	appHeaders := map[string]string{"X-CSRF-Token": session.CSRFToken}
	app, adminCookie := []*http.Cookie{session.Cookie()}, []*http.Cookie{adminSession.Cookie()}

	requests := []request{
		{method: "GET", target: "/auth/forgot-password/" + reset.Token, route: "page /auth/*"},
		{method: "GET", target: "/auth/invitations/" + invitation.Token + "?next=" + url.QueryEscape("/auth/invitations/"+invitation.Token), route: "page /auth/*"},
		{method: "GET", target: "/auth/email-blocks/" + blockLink.Token, route: "page /auth/*"},
		{method: "GET", target: "/auth/login?next=" + url.QueryEscape("/auth/forgot-password/"+reset.Token), route: "page /auth/*"},
		{method: "GET", target: "/admin/" + key, route: "page /admin/*"},
		{method: "GET", target: "/admin/" + key + "/users?q=" + searched, route: "page /admin/*", cookies: adminCookie},
		{method: "GET", target: "/admin/" + key + "/users", route: "page /admin/*"},
		{method: "GET", target: "/admin/" + key + "x/users", route: "page 404"},
		{method: "GET", target: apitest.AdminAsset, route: "admin asset", cookies: []*http.Cookie{pass}},
		{method: "GET", target: apitest.AdminAsset, route: "page 404"},
		{method: "GET", target: "/app/" + workspace.PublicID + "/projects?q=" + searched, route: "page /app/*", cookies: app, user: userID},
		{method: "GET", target: "/favicon.svg?token=" + reset.Token, route: "page 404"},
		{method: "GET", target: "/", route: "page /"},
		{method: "GET", target: "/api/admin/users?q=" + searched, route: "GET /api/admin/users", headers: adminHeaders, cookies: adminCookie, user: observe.AdminUser},
		{method: "GET", target: "/api/admin/events?email=" + url.QueryEscape(emailSearched), route: "GET /api/admin/events", headers: adminHeaders, cookies: adminCookie, user: observe.AdminUser},
		{method: "GET", target: "/api/admin/users", route: "GET /api/admin/users", headers: map[string]string{"X-Admin-Key": key + "x"}, cookies: adminCookie},
		{method: "POST", target: "/api/admin/step-up", body: `{"code":"` + code + `"}`, route: "POST /api/admin/step-up", headers: adminHeaders, cookies: adminCookie, user: observe.AdminUser},
		{method: "GET", target: apitest.WorkspacePath(workspace.PublicID, "/members?q="+searched), route: "GET /api/workspaces/{workspace}/members", cookies: app, user: userID},
		{method: "GET", target: "/api/auth/me", route: "GET /api/auth/me", cookies: app, user: userID},
		{method: "POST", target: "/api/auth/password/verify", body: `{"password":"` + password + `"}`, route: "POST /api/auth/password/verify", headers: appHeaders, cookies: app, user: userID},
		{method: "POST", target: "/api/auth/login", body: `{"email":"` + emailSearched + `","password":"` + password + `"}`, route: "POST /api/auth/login"},
		{method: "POST", target: "/api/auth/password-reset/check", body: `{"token":"` + reset.Token + `"}`, route: "POST /api/auth/password-reset/check"},
		{method: "POST", target: "/api/invitations/check", body: `{"token":"` + invitation.Token + `"}`, route: "POST /api/invitations/check"},
		{method: "POST", target: "/api/email-blocks/check", body: `{"token":"` + blockLink.Token + `"}`, route: "POST /api/email-blocks/check"},
		{method: "GET", target: "/api/nowhere/" + reset.Token + "?q=" + searched, route: observe.Unmatched},
		{method: "GET", target: "/api/health?token=" + reset.Token, route: "GET /api/health"},
		{method: "GET", target: "/api/health/live", route: "GET /api/health/live"},
	}
	start := len(logs.lines(t))
	for _, q := range requests {
		q.send(h)
	}
	secrets := []string{
		reset.Token, invitation.Token, blockLink.Token, key, pass.Value,
		session.Token, session.CSRFToken, adminSession.Token, adminSession.CSRFToken,
		searched, emailSearched, url.QueryEscape(emailSearched), password, code,
		"blocked-person@example.com", admin.User.Email,
	}
	logged := logs.text()
	for _, secret := range secrets {
		if strings.Contains(logged, secret) {
			t.Errorf("a secret (%.12s…) is in the log:\n%s", secret, logged)
		}
	}
	var access []map[string]any
	for _, line := range logs.lines(t)[start:] {
		if line["msg"] == "request" {
			access = append(access, line)
		}
	}
	if len(access) != len(requests) {
		t.Fatalf("%d access lines for %d requests", len(access), len(requests))
	}
	for i, line := range access {
		q := requests[i]
		if line["route"] != q.route || line["user"] != q.user || line["method"] != q.method || line["ip"] != "203.0.113.9" {
			t.Errorf("%s %.40s: want route %q user %q, got %v", q.method, q.target, q.route, q.user, line)
		}
	}
}

var requestIDShape = regexp.MustCompile(`^req_[0-9A-Za-z]{14}$`)

// An event recorded in a request carries the request's ID, the same as its
// X-Request-ID and its access line. It is always ours: a trusted proxy's
// X-Request-ID only goes on the access line, as proxyRequestId, and anyone
// else's nowhere, so a client can't put another request's ID on its events.
func TestEventsCarryTheRequestID(t *testing.T) {
	s, db := server(t)
	s.Config.ClientIPHeader = "X-Forwarded-For"
	s.Config.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	h := router.New(s)
	logs := captureLogs(t)
	login := func(remote, id string) string {
		r := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(apitest.LoginBody("nobody-"+remote+"@example.com")))
		r.RemoteAddr = remote + ":1234"
		r.Header.Set("Origin", apitest.Origin)
		r.Header.Set("Content-Type", "application/json")
		if id != "" {
			r.Header.Set("X-Request-ID", id)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("login: %d %s", w.Code, w.Body.String())
		}
		return w.Header().Get("X-Request-ID")
	}
	own := login("198.51.100.1", "")
	proxied := login("10.1.2.3", "lb-0001.a_b")
	forged := login("198.51.100.2", "forged-id")
	ids := []string{own, proxied, forged}
	for _, id := range ids {
		if !requestIDShape.MatchString(id) {
			t.Fatalf("IDs: %q", ids)
		}
	}
	attempts := apitest.EventsNamed(t, db, "auth.login_attempted")
	if len(attempts) != 3 {
		t.Fatalf("events: %+v", attempts)
	}
	for i, line := range logs.lines(t) {
		if line["requestId"] != ids[i] || attempts[i].RequestID != ids[i] {
			t.Errorf("request %d: line %v, event %q, want %s", i, line, attempts[i].RequestID, ids[i])
		}
		if wantProxy := map[int]any{1: "lb-0001.a_b"}[i]; line["proxyRequestId"] != wantProxy {
			t.Errorf("request %d: proxyRequestId %v", i, line["proxyRequestId"])
		}
	}
	if strings.Contains(logs.text(), "forged-id") {
		t.Error("an untrusted peer's ID was logged")
	}
	// Outside a request (the CLI, a listener) an event has none.
	reached := events.RateLimitReached{Limit: "auth:per-ip:block", Window: 900, ResetsAt: time.Now()}
	if err := s.Repo.Record(context.Background(), reached, store.EventScope{}); err != nil {
		t.Fatal(err)
	}
	if e := apitest.OnlyEvent(t, db, "rate_limit.reached"); e.RequestID != "" {
		t.Fatalf("an event outside a request: %+v", e)
	}
}

// unreachable is a database nothing answers on, as in an outage.
func unreachable(t *testing.T) *bun.DB {
	t.Helper()
	cfg := apitest.Must(pgx.ParseConfig("postgresql://nobody:x@127.0.0.1:1/nothing?sslmode=disable&connect_timeout=1"))(t)
	db := bun.NewDB(stdlib.OpenDB(*cfg), pgdialect.New())
	t.Cleanup(func() { db.Close() })
	return db
}

// A 500 carries the request's ID, the one on its log line and in
// X-Request-ID; readiness fails while the database is down and liveness
// doesn't; readiness fails once shutdown has begun.
func TestOutagesAndShutdown(t *testing.T) {
	down := apitest.Must(httpapi.New(&store.Store{DB: unreachable(t)}, apitest.Config()))(t)
	h := router.New(down)
	logs := captureLogs(t)
	w := apitest.Get(h, "GET", "/api/auth/me", factory.Sessions.Make(factory.TestFaker(t), 1)[0].Cookie())
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 500 {
		t.Fatalf("outage: %d %s", w.Code, w.Body.String())
	}
	id := w.Header().Get("X-Request-ID")
	if !requestIDShape.MatchString(id) || body["requestId"] != id || strings.Contains(w.Body.String(), "nobody") {
		t.Fatalf("500: %v %q", body, id)
	}
	lines := logs.lines(t)
	if len(lines) != 2 || lines[0]["msg"] != "request failed" || lines[0]["requestId"] != id || lines[0]["route"] != "GET /api/auth/me" || lines[1]["requestId"] != id {
		t.Fatalf("lines: %v", lines)
	}
	if w := apitest.Get(h, "GET", "/api/health", nil); w.Code != 503 || !strings.Contains(w.Body.String(), "Database unavailable") {
		t.Fatalf("readiness in an outage: %d %s", w.Code, w.Body.String())
	}
	if w := apitest.Get(h, "GET", "/api/health/live", nil); w.Code != 200 || w.Body.String() != `{"status":"ok"}`+"\n" {
		t.Fatalf("liveness in an outage: %d %s", w.Code, w.Body.String())
	}

	s, _ := server(t)
	h = router.New(s)
	if w := apitest.Get(h, "GET", "/api/health", nil); w.Code != 200 {
		t.Fatalf("readiness: %d %s", w.Code, w.Body.String())
	}
	s.Drain()
	if w := apitest.Get(h, "GET", "/api/health", nil); w.Code != 503 || !strings.Contains(w.Body.String(), "Shutting down") {
		t.Fatalf("readiness while draining: %d %s", w.Code, w.Body.String())
	}
	if w := apitest.Get(h, "GET", "/api/health/live", nil); w.Code != 200 {
		t.Fatalf("liveness while draining: %d", w.Code)
	}
	// Other routes keep answering while in-flight work finishes.
	if w := apitest.Get(h, "GET", "/api/auth/me", nil); w.Code != 401 {
		t.Fatalf("a route while draining: %d", w.Code)
	}
}
