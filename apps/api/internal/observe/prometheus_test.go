package observe

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// goldenRegistry is a registry with one of each kind of metric and a few
// requests, for the golden file.
func goldenRegistry() *Registry {
	r := NewRegistry()
	refused := r.Counter("rate_limit_refused_total", "Requests refused by a rate limit, by limit and level.", "limit")
	refused.Add(3, "auth:per-ip:block")
	refused.Inc("project:per-user:block")
	r.Gauge("deliveries_pending", "Event deliveries waiting to run.").Set(2)
	r.Gauge("build_info", "This server's build.", "version", "go").Set(1, "abc123def456", "go1.27.1")
	lag := r.Histogram("delivery_lag_ms", "Time to a listener's first attempt, in milliseconds.", []float64{10, 100}, "listener")
	lag.Observe(4*time.Millisecond, "mail.registration")
	lag.Observe(40*time.Millisecond, "mail.registration")
	lag.Observe(400*time.Millisecond, "mail.registration")
	r.Counter("errors_total", "Failures, by source and route.", "source", "route").Inc("request", `GET /api/"quoted"\path`)
	r.Requests.Observe("GET /api/auth/me", 200, 3*time.Millisecond, false, false)
	r.Requests.Observe("GET /api/auth/me", 401, 7*time.Millisecond, false, false)
	r.Requests.Observe("POST /api/workspaces/{workspace}/projects", 500, 11*time.Second, true, false)
	r.Requests.Observe("POST /api/workspaces/{workspace}/projects", 0, 30*time.Millisecond, false, true)
	return r
}

// /metrics' text is pinned by a golden file. To change it on purpose:
// UPDATE_GOLDEN=1 go test ./internal/observe -run TestPrometheusGolden
func TestPrometheusGolden(t *testing.T) {
	var out bytes.Buffer
	if err := goldenRegistry().WritePrometheus(&out); err != nil {
		t.Fatal(err)
	}
	const golden = "testdata/metrics.golden"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(golden, out.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), want) {
		t.Fatalf("/metrics differs from %s:\n%s", golden, out.String())
	}
}

// /metrics answers GET and HEAD on /metrics only; with a token, only a
// request bearing it.
func TestMetricsHandler(t *testing.T) {
	const token = "aVeryLongMetricsTokenOfAtLeast32Characters"
	open := MetricsHandler(goldenRegistry(), "")
	guarded := MetricsHandler(goldenRegistry(), token)
	for _, c := range []struct {
		name          string
		h             http.Handler
		method, path  string
		authorization string
		want          int
	}{
		{"open", open, "GET", "/metrics", "", 200},
		{"head", open, "HEAD", "/metrics", "", 200},
		{"another path", open, "GET", "/metrics/x", "", 404},
		{"the API's", open, "GET", "/api/health", "", 404},
		{"post", open, "POST", "/metrics", "", 405},
		{"no token", guarded, "GET", "/metrics", "", 401},
		{"wrong token", guarded, "GET", "/metrics", "Bearer " + token + "x", 401},
		{"a prefix", guarded, "GET", "/metrics", "Bearer " + token[:31], 401},
		{"empty", guarded, "GET", "/metrics", "Bearer ", 401},
		{"basic", guarded, "GET", "/metrics", "Basic " + token, 401},
		{"bare", guarded, "GET", "/metrics", token, 401},
		{"right token", guarded, "GET", "/metrics", "Bearer " + token, 200},
		{"lower-case scheme", guarded, "GET", "/metrics", "bearer " + token, 200},
	} {
		r := httptest.NewRequest(c.method, c.path, nil)
		if c.authorization != "" {
			r.Header.Set("Authorization", c.authorization)
		}
		w := httptest.NewRecorder()
		c.h.ServeHTTP(w, r)
		if w.Code != c.want {
			t.Errorf("%s: %d, want %d", c.name, w.Code, c.want)
			continue
		}
		body := w.Body.String()
		if c.want == 200 && c.method == "GET" && !strings.Contains(body, "http_requests_total{") {
			t.Errorf("%s: no metrics:\n%s", c.name, body)
		}
		if c.want != 200 && strings.Contains(body, "http_requests_total") {
			t.Errorf("%s: metrics in a refusal", c.name)
		}
		if c.want == 401 && w.Header().Get("WWW-Authenticate") == "" {
			t.Errorf("%s: no WWW-Authenticate", c.name)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: cacheable", c.name)
		}
	}
}

// The listener has its own timeouts and header limit.
func TestMetricsServer(t *testing.T) {
	s := MetricsServer("127.0.0.1:0", http.NotFoundHandler())
	if s.ReadHeaderTimeout == 0 || s.ReadTimeout == 0 || s.WriteTimeout == 0 || s.IdleTimeout == 0 || s.MaxHeaderBytes == 0 {
		t.Fatalf("%+v", s)
	}
}
