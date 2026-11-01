package httpapi_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/ratelimit"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/observe"
	"lewcontext/api/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 10,000 requests to one route are one request_minutes row once the
// minute is written: what is stored depends on the routes, never the
// traffic.
func TestTenThousandRequestsAreOneRow(t *testing.T) {
	s, db := server(t)
	s.Metrics = observe.NewRegistry()
	h := router.New(s)
	for range 10000 {
		if w := apitest.Get(h, "GET", "/api/health/live", nil); w.Code != 200 {
			t.Fatalf("live: %d", w.Code)
		}
	}
	instance := testInstance()
	flush := &observe.MinuteFlush{Registry: s.Metrics, Store: func(ctx context.Context, m observe.Minute) error {
		return s.Repo.RecordMetrics(ctx, instance, m)
	}}
	if err := flush.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Route    string
		Requests int64
	}
	if err := db.NewSelect().Table("request_minutes").Column("route", "requests").Scan(context.Background(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Route != "GET /api/health/live" || rows[0].Requests != 10000 {
		t.Fatalf("request_minutes: %+v", rows)
	}
}

// METRICS_STORE=off writes no metric: the minute's write carries only the
// heartbeat.
func TestMetricsStoreOffWritesNothing(t *testing.T) {
	s, db := server(t)
	s.Metrics = observe.NewRegistry()
	h := router.New(s)
	apitest.Get(h, "GET", "/api/health/live", nil)
	instance := testInstance()
	flush := &observe.MinuteFlush{Registry: s.Metrics, Discard: true, Store: func(ctx context.Context, m observe.Minute) error {
		return s.Repo.RecordMetrics(ctx, instance, m)
	}}
	if err := flush.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	for table, want := range map[string]int64{"request_minutes": 0, "metric_minutes": 0, "server_instances": 1} {
		if n, err := db.NewSelect().Table(table).Count(context.Background()); err != nil || n != want {
			t.Errorf("%s: %d rows (%v), want %d", table, n, err, want)
		}
	}
}

// No request can make a series: random paths, invented methods, page
// paths holding tokens and wrong admin keys all count under the patterns
// and fixed labels, and nothing the client sent reaches /metrics.
func TestClientsCantMintSeries(t *testing.T) {
	s, _ := server(t)
	s.Metrics = observe.NewRegistry()
	s.Pages = apitest.Pages(t)
	h := router.New(s)
	random := func() string {
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		return "zq" + hex.EncodeToString(b)
	}
	for range 300 {
		for _, c := range []struct{ method, path string }{
			{"GET", "/api/" + random()},
			{"GET", "/api/workspaces/" + random() + "/projects"},
			{"PATCH", "/api/health/" + random()},
			{random(), "/api/health"},
			{"GET", "/auth/reset-password/" + random()},
			{"GET", "/admin/" + random()},
			{"GET", "/" + random() + ".js"},
			{"GET", "/app/" + random()},
		} {
			r := httptest.NewRequest("GET", c.path, nil)
			r.Method = c.method
			h.ServeHTTP(httptest.NewRecorder(), r)
		}
	}
	if n := s.Metrics.Requests.Len(); n > 12 {
		t.Fatalf("%d routes from random requests", n)
	}
	var out bytes.Buffer
	if err := s.Metrics.WritePrometheus(&out); err != nil {
		t.Fatal(err)
	}
	if err := observe.Metrics.WritePrometheus(&out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if strings.Contains(text, "zq") {
		t.Fatalf("a client's value reached /metrics:\n%s", text)
	}
	for _, label := range []string{`route="unmatched"`, `route="page /auth/*"`} {
		if !strings.Contains(text, label) {
			t.Errorf("no %s in:\n%s", label, text)
		}
	}
}

// A client that went away is counted as aborted, never as a 5xx; a
// request whose deadline passed is flagged as timed out.
func TestAbortedAndTimedOutRequests(t *testing.T) {
	s, _ := server(t)
	s.Metrics = observe.NewRegistry()
	gone := s.ClientAddress(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observe.SetRoute(r.Context(), "GET /api/thing")
		httpapi.InternalError(w, r, context.Canceled)
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	gone.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/thing", nil).WithContext(ctx))

	slow := s.ClientAddress(s.Security(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observe.SetRoute(r.Context(), "GET /api/slow")
		// As a handler whose query ran past the deadline would see it.
		deadline, cancel := context.WithDeadline(r.Context(), time.Now().Add(-time.Second))
		defer cancel()
		<-deadline.Done()
		httpapi.InternalError(w, r, deadline.Err())
	})))
	slow.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/slow", nil))

	// The request's own deadline (Security's, which a parent's past
	// deadline ends at once).
	late := s.ClientAddress(s.Security(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observe.SetRoute(r.Context(), "GET /api/late")
		<-r.Context().Done()
		httpapi.InternalError(w, r, r.Context().Err())
	})))
	past, cancelPast := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelPast()
	late.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/late", nil).WithContext(past))

	m := s.Metrics.TakeMinute(time.Now())
	byRoute := map[string]observe.RouteTotals{}
	for _, r := range m.Routes {
		byRoute[r.Route] = r
	}
	if g := byRoute["GET /api/thing"]; g.Requests != 1 || g.Aborted != 1 || g.Status5xx != 0 {
		t.Fatalf("client gone: %+v", g)
	}
	if s := byRoute["GET /api/slow"]; s.Requests != 1 || s.Status5xx != 1 || s.Timeouts != 0 {
		t.Fatalf("a handler's own deadline isn't the request's: %+v", s)
	}
	if l := byRoute["GET /api/late"]; l.Requests != 1 || l.Status5xx != 1 || l.Timeouts != 1 {
		t.Fatalf("timed out: %+v", l)
	}
}

// testInstance is a server_instances row for a test's minute writes.
func testInstance() store.ServerInstance {
	now := time.Now()
	return store.ServerInstance{ID: observe.NewServerID(), Hostname: "test", Version: "dev", StartedAt: now, LastSeenAt: now, Config: []byte(`{}`)}
}

// metricLine is a sample's value in the process's /metrics, or "".
func metricLine(t *testing.T, sample string) string {
	t.Helper()
	var out bytes.Buffer
	if err := observe.Metrics.WritePrometheus(&out); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if value, ok := strings.CutPrefix(line, sample+" "); ok {
			return value
		}
	}
	return ""
}

// Every refusal is counted in rate_limit_refused_total, not only the first
// in a window (which is all rate_limit.reached records).
func TestEveryRefusalIsCounted(t *testing.T) {
	s, _ := server(t)
	l := ratelimit.Limit{Name: "metrics-test:per-user", Max: 1, Window: time.Minute, InMemory: true}
	for range 4 {
		r := httptest.NewRequest("GET", "/api/x", nil)
		s.Allow(httptest.NewRecorder(), r, l, l.Key("usr_00000000000001"))
	}
	if got := metricLine(t, `rate_limit_refused_total{limit="metrics-test:per-user:block"}`); got != "3" {
		t.Fatalf("refusals counted: %q", got)
	}
}
