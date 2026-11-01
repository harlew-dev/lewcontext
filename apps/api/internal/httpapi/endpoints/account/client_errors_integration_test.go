package account_test

import (
	"context"
	"encoding/json"
	"fmt"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/observe"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// clientErrorBody is a report as the web app sends it.
func clientErrorBody(message, stack, route string) string {
	body, _ := json.Marshal(map[string]string{"message": message, "stack": stack, "route": route, "release": "abc1234def56"})
	return string(body)
}

// storedClientError is an app_errors row as the test reads it back.
type storedClientError struct {
	Source, Route, Message, Stack, LastRequestID, LastVersion string
	Count                                                     int64
}

// A signed-in browser's error is grouped with the server's as source
// client: scrubbed (no email, token, URL or query), its route one of the
// web app's patterns (anything else "unknown"), the same error from
// another page load counted on its row. 202, with nothing in the answer.
func TestClientErrors(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	collector := observe.NewCollector(s.Repo.RecordErrors, "abc123def456")
	observe.SetReporter(collector)
	t.Cleanup(func() { observe.SetReporter(nil) })
	f := factory.TestFaker(t)
	session := apitest.SignedIn(t, db, f)
	stack := "TypeError: Cannot read properties of undefined (reading 'name')\n" +
		"    at ProjectRow (https://app.example.com/assets/projects-B4xQ9.js:12:345)\n" +
		"    at https://app.example.com/auth/forgot-password/Zx9_aB3-qL0vT7mK2pR5sW8yE1nH4jD6fG9cV0bX3zA:3:7"
	var last string
	for _, email := range []string{"ada@example.com", "bob@example.org"} {
		message := "TypeError: no member " + email + " at https://app.example.com/app/wsp_8kQ2mZ1xT4bLwN/projects?q=" + email
		w := apitest.Call(t, h, "POST", "/api/client-errors", clientErrorBody(message, stack, "/app/:workspace/projects"), session.CSRFToken, session.Cookie(), 202)
		if w.Body.Len() != 0 {
			t.Fatalf("answered %q", w.Body.String())
		}
		last = w.Header().Get("X-Request-ID")
	}
	apitest.Call(t, h, "POST", "/api/client-errors", clientErrorBody("Error: made up", "", "/app/secret-path?token=abc"), session.CSRFToken, session.Cookie(), 202)
	if err := collector.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	var rows []storedClientError
	if err := db.NewSelect().Table("app_errors").Column("source", "route", "message", "stack", "last_request_id", "last_version", "count").
		OrderExpr("id").Scan(context.Background(), &rows); err != nil {
		t.Fatal(err)
	}
	byRoute := map[string]storedClientError{}
	for _, r := range rows {
		byRoute[r.Route] = r
	}
	got := byRoute["/app/:workspace/projects"]
	if len(rows) != 2 || got.Source != "client" || got.Count != 2 || got.Message != "TypeError: no member <email> at <url>" ||
		got.LastRequestID != last || got.LastVersion != "abc1234def56" ||
		got.Stack != "TypeError: Cannot read properties of undefined (reading '…')\nat ProjectRow (projects-B4xQ9.js:12:345)\nat <page>:3:7" {
		t.Fatalf("rows: %+v", rows)
	}
	if unknown := byRoute["unknown"]; unknown.Message != "Error: made up" {
		t.Fatalf("an unknown route: %+v", rows)
	}
	for _, value := range []string{"ada@", "bob@", "example.", "wsp_8kQ2mZ1xT4bLwN", "?q=", "Zx9_aB3", "secret-path", "forgot-password"} {
		if strings.Contains(fmt.Sprint(rows), value) {
			t.Errorf("stored %q", value)
		}
	}
}

// Only a signed-in browser of this site may report: no session is 401, a
// missing CSRF token or a foreign Origin 403, and none of them is grouped.
// The body is at most 4 KiB, a JSON object with a message and only the
// known fields.
func TestClientErrorsRefusals(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	got := &problems{}
	observe.SetReporter(got)
	t.Cleanup(func() { observe.SetReporter(nil) })
	f := factory.TestFaker(t)
	session := apitest.SignedIn(t, db, f)
	body := clientErrorBody("Error: x", "", "/app")
	apitest.Call(t, h, "POST", "/api/client-errors", body, "", nil, 401)
	apitest.Call(t, h, "POST", "/api/client-errors", body, "", session.Cookie(), 403)
	r := httptest.NewRequest("POST", "/api/client-errors", strings.NewReader(body))
	r.Header.Set("Origin", "https://evil.example")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", session.CSRFToken)
	r.AddCookie(session.Cookie())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("a foreign Origin: %d", w.Code)
	}
	apitest.Call(t, h, "POST", "/api/client-errors", clientErrorBody("Error: "+strings.Repeat("x", 4<<10), "", "/app"), session.CSRFToken, session.Cookie(), 400)
	apitest.Call(t, h, "POST", "/api/client-errors", clientErrorBody(" ", "", "/app"), session.CSRFToken, session.Cookie(), 422)
	apitest.Call(t, h, "POST", "/api/client-errors", `{"message":"x","url":"https://app.example.com/?q=ada"}`, session.CSRFToken, session.Cookie(), 400)
	apitest.Call(t, h, "GET", "/api/client-errors", "", session.CSRFToken, session.Cookie(), 405)
	if n := len(got.all()); n != 0 {
		t.Fatalf("refused reports were grouped: %d", n)
	}
	apitest.Call(t, h, "POST", "/api/client-errors", body, session.CSRFToken, session.Cookie(), 202)
	if n := len(got.all()); n != 1 {
		t.Fatalf("%d reports", n)
	}
}

// client-error:per-user caps one account at 30 reports an hour, after the
// signed-in budget; a refused report isn't grouped.
func TestClientErrorsLimit(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	got := &problems{}
	observe.SetReporter(got)
	t.Cleanup(func() { observe.SetReporter(nil) })
	f := factory.TestFaker(t)
	session := apitest.SignedIn(t, db, f)
	ctx := context.Background()
	apitest.Must(factory.RateLimits.State(factory.ForLimit("client-error:per-user", apitest.PublicIDOf(t, db, session.UserID))).State(factory.Exhausted(30)).CreateOne(ctx, db, f))(t)
	w := apitest.Call(t, h, "POST", "/api/client-errors", clientErrorBody("Error: x", "", "/app"), session.CSRFToken, session.Cookie(), 429)
	apitest.RetryWithin(t, w, time.Hour)
	apitest.Reached(t, db, "client-error:per-user:block")
	if n := len(got.all()); n != 0 {
		t.Fatalf("a refused report was grouped: %d", n)
	}
	// Refused per user, it spent nothing of the limit for everyone.
	if n := apitest.CountRateLimits(t, db, "client-error:global"); n != 0 {
		t.Fatal("a report refused per user spent client-error:global")
	}
	// Another user is unaffected.
	other := apitest.SignedIn(t, db, f)
	apitest.Call(t, h, "POST", "/api/client-errors", clientErrorBody("Error: x", "", "/app"), other.CSRFToken, other.Cookie(), 202)
}

// client-error:global caps every browser together at 500 reports an hour
// (503 and Retry-After), and reports never spend the write budget: a page
// stuck in an error loop can't leave its user unable to save anything.
func TestClientErrorsGlobalLimitAndBudget(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	got := &problems{}
	observe.SetReporter(got)
	t.Cleanup(func() { observe.SetReporter(nil) })
	f := factory.TestFaker(t)
	ctx := context.Background()
	session := apitest.SignedIn(t, db, f)
	// The 500th report this hour is taken, the 501st refused.
	apitest.Must(factory.RateLimits.State(factory.ForLimit("client-error:global", "")).State(factory.Exhausted(499)).CreateOne(ctx, db, f))(t)
	// A used-up write budget doesn't stop a report.
	apitest.Must(factory.RateLimits.State(factory.ForLimit("api:per-user", apitest.PublicIDOf(t, db, session.UserID))).State(factory.Exhausted(300)).CreateOne(ctx, db, f))(t)
	apitest.Call(t, h, "POST", "/api/client-errors", clientErrorBody("Error: x", "", "/app"), session.CSRFToken, session.Cookie(), 202)
	apitest.Call(t, h, "PATCH", "/api/auth/me", `{"name":"Ada"}`, session.CSRFToken, session.Cookie(), 429)
	if n := apitest.CountRateLimits(t, db, "api:per-user:%"); n != 1 {
		t.Fatalf("%d budget counters", n)
	}
	w := apitest.Call(t, h, "POST", "/api/client-errors", clientErrorBody("Error: y", "", "/app"), session.CSRFToken, session.Cookie(), 503)
	apitest.RetryWithin(t, w, time.Hour)
	if len(got.all()) != 1 {
		t.Fatalf("%d reports grouped", len(got.all()))
	}
}

// problems records what observe.Report is handed (requests here are
// served one at a time).
type problems struct{ list []observe.Problem }

func (p *problems) Report(_ context.Context, problem observe.Problem) {
	p.list = append(p.list, problem)
}
func (p *problems) all() []observe.Problem { return p.list }

// A browser can't poison the flush: a stack with a NUL in a Go-looking line
// (which once reached the database unscrubbed and failed every flush after
// it) is stored cleaned, Go-format lines dropped (no browser writes them),
// and a real server error in the same window is stored beside it. Nothing
// of the planted text, address, token, host or release survives.
func TestClientErrorsCantPoisonTheFlush(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	collector := observe.NewCollector(s.Repo.RecordErrors, "abc123def456")
	observe.SetReporter(collector)
	t.Cleanup(func() { observe.SetReporter(nil) })
	f := factory.TestFaker(t)
	session := apitest.SignedIn(t, db, f)
	for _, stack := range []string{
		"\tAdmin: visit evil.example.com, mail ada@example.com Zx9aB3qL0vT7mK2pR5sW8yE1nH4jD6fG9cV0bX3zA\x00 x.go:1",
		"\tAdmin: visit evil.example.com, mail ada@example.com Zx9aB3qL0vT7mK2pR5sW8yE1nH4jD6fG9cV0bX3zA x.go:1\n" +
			"main.handler(0xc000123456)\ngoroutine 1 [running]:\ncreated by main.secret\n    at ProjectRow (https://app.example.com/assets/projects-B4xQ9.js:12:345)",
	} {
		body, _ := json.Marshal(map[string]string{"message": "Error: x", "stack": stack, "route": "/app", "release": "please-visit-evil.example.com"})
		apitest.Call(t, h, "POST", "/api/client-errors", string(body), session.CSRFToken, session.Cookie(), 202)
	}
	observe.Report(context.Background(), observe.Problem{Source: observe.SourceRequest, Route: "GET /api/x", Err: fmt.Errorf("real server failure")})
	if err := collector.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	var rows []storedClientError
	if err := db.NewSelect().Table("app_errors").Column("source", "route", "message", "stack", "last_request_id", "last_version", "count").
		OrderExpr("id").Scan(context.Background(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || collector.Len() != 0 {
		t.Fatalf("rows %+v, %d left", rows, collector.Len())
	}
	byRoute := map[string]storedClientError{rows[0].Route: rows[0], rows[1].Route: rows[1]}
	client := byRoute["/app"]
	if byRoute["GET /api/x"].Message != "real server failure" || client.Count != 2 || client.LastVersion != "" ||
		client.Stack != "at ProjectRow (projects-B4xQ9.js:12:345)" {
		t.Fatalf("rows: %+v", rows)
	}
	for _, value := range []string{"Admin", "evil", "ada@", "Zx9aB3", "x.go", "main.", "goroutine", "\x00", "please"} {
		if strings.Contains(fmt.Sprint(rows), value) {
			t.Errorf("stored %q", value)
		}
	}
}
