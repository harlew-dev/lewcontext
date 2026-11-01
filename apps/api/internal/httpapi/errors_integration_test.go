package httpapi_test

import (
	"context"
	"fmt"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/observe"
	"lewcontext/api/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// collecting sets a collector storing into s for the test, as cmd/server
// does, and returns it.
func collecting(t *testing.T, s *store.Store) *observe.Collector {
	t.Helper()
	c := observe.NewCollector(s.ReportErrors, "abc123def456")
	observe.SetReporter(c)
	t.Cleanup(func() { observe.SetReporter(nil) })
	return c
}

// appError is one stored group, as the tests read it back.
type appError struct {
	Source, Route, Message, Stack, LastRequestID, LastVersion string
	Count                                                     int64
}

func appErrors(t *testing.T, s *store.Store) []appError {
	t.Helper()
	var rows []appError
	if err := s.DB.NewSelect().Table("app_errors").Column("source", "route", "message", "stack", "last_request_id", "last_version", "count").
		OrderExpr("id").Scan(context.Background(), &rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

// A handler's panic answers a 500 and, once flushed, is one app_errors row,
// whoever it hit: the same panic with another user's email in it counts on
// the same row. The row holds the scrubbed message, the stack's function
// names and lines, and the last request's ID; nothing of the requests.
func TestPanicsAreGroupedInAppErrors(t *testing.T) {
	s, _ := server(t)
	collector := collecting(t, s.Repo)
	h := s.ClientAddress(httpapi.RecoverPanics(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observe.SetRoute(r.Context(), "GET /api/thing")
		panic(fmt.Sprintf("no workspace for %s with token %s", r.URL.Query().Get("email"), r.Header.Get("X-Token")))
	})))
	var last string
	for i, email := range []string{"ada@example.com", "bob@example.org", "eve@example.net"} {
		r := httptest.NewRequest("GET", "/api/thing?email="+email, nil)
		r.Header.Set("X-Token", fmt.Sprintf("Zx9_aB3-qL0vT7mK2pR5sW8yE1nH4jD6fG9cV0bX3z%d", i))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 500 {
			t.Fatalf("panic: %d", w.Code)
		}
		last = w.Header().Get("X-Request-ID")
	}
	if err := collector.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows := appErrors(t, s.Repo)
	if len(rows) != 1 {
		t.Fatalf("%d rows: %+v", len(rows), rows)
	}
	row := rows[0]
	if row.Source != "panic" || row.Route != "GET /api/thing" || row.Count != 3 || row.Message != "no workspace for <email> with token <token>" ||
		row.LastRequestID != last || row.LastVersion != "abc123def456" {
		t.Fatalf("row: %+v", row)
	}
	if !strings.Contains(row.Stack, "TestPanicsAreGroupedInAppErrors") || strings.Contains(row.Stack, "0xc0") || strings.Contains(row.Stack, "goroutine") {
		t.Fatalf("stack:\n%s", row.Stack)
	}
	for _, value := range []string{"ada@", "bob@", "eve@", "Zx9_aB3", "example."} {
		if strings.Contains(fmt.Sprint(row), value) {
			t.Errorf("stored %q", value)
		}
	}
	// Again, in a later flush: the same row.
	r := httptest.NewRequest("GET", "/api/thing?email=zed@example.com", nil)
	r.Header.Set("X-Token", "aB3dE5fG7hJ9kL1mN3pQ5rS7tU9vW1xY3zA5bC7dE9f")
	h.ServeHTTP(httptest.NewRecorder(), r)
	if err := collector.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rows := appErrors(t, s.Repo); len(rows) != 1 || rows[0].Count != 4 {
		t.Fatalf("after a second flush: %+v", rows)
	}
}

// A failed query in the signed-in page gate is reported (the page is still
// served, to show its own outage page), as is the admin page gate's; a
// client that went away isn't.
func TestPageGateFailuresAreReported(t *testing.T) {
	down := apitest.Must(httpapi.New(&store.Store{DB: unreachable(t)}, apitest.Config()))(t)
	down.Pages = apitest.Pages(t)
	h := router.New(down)
	got := &problems{}
	observe.SetReporter(got)
	t.Cleanup(func() { observe.SetReporter(nil) })
	session := factory.Sessions.Make(factory.TestFaker(t), 1)[0]
	if w := apitest.Get(h, "GET", "/app/settings", session.Cookie()); w.Code != 200 {
		t.Fatalf("page in an outage: %d", w.Code)
	}
	reported := got.all()
	if len(reported) != 1 || reported[0].Source != observe.SourceRequest || reported[0].Route != "page /app/*" ||
		!strings.HasPrefix(reported[0].Err.Error(), "page session check: ") || !strings.HasPrefix(reported[0].RequestID, "req_") {
		t.Fatalf("reported: %+v", reported)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("GET", "/app/settings", nil).WithContext(ctx)
	r.AddCookie(session.Cookie())
	h.ServeHTTP(httptest.NewRecorder(), r)
	if n := len(got.all()); n != 1 {
		t.Fatalf("a client gone was reported: %d", n)
	}
}

type problems struct {
	mu   sync.Mutex
	list []observe.Problem
}

func (p *problems) Report(_ context.Context, problem observe.Problem) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.list = append(p.list, problem)
}

func (p *problems) all() []observe.Problem {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]observe.Problem(nil), p.list...)
}
