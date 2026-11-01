package observe

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

// No label value a client could send makes a new series past MaxSeries:
// the rest go to the overflow series, every label Unmatched. A value that
// isn't a plain label (a control character, too long) is Unmatched too.
func TestSeriesAreBounded(t *testing.T) {
	r := NewRegistry()
	c := r.Counter("things_total", "Things.", "kind", "route")
	for i := range 5000 {
		c.Inc(fmt.Sprintf("kind-%d", i), "GET /x")
	}
	c.Inc("bad\nvalue", "GET /x")
	c.Inc(strings.Repeat("x", maxLabelValue+1), "GET /x")
	series := *c.f.series.Load()
	if len(series) != MaxSeries+1 {
		t.Fatalf("%d series", len(series))
	}
	over, ok := series[labelValues{Unmatched, Unmatched}]
	if !ok || over.count.Load() != 5000-MaxSeries+2 {
		t.Fatalf("overflow series: %v %+v", ok, over)
	}
	// An existing series still counts as itself.
	c.Inc("kind-0", "GET /x")
	if got := series[labelValues{"kind-0", "GET /x"}].count.Load(); got != 2 {
		t.Fatalf("kind-0: %d", got)
	}
	// Labels the metric doesn't have are ignored; missing ones are Unmatched.
	g := r.Gauge("level", "Level.")
	g.Set(3, "anything")
	if s := *g.f.series.Load(); len(s) != 1 || s[labelValues{}] == nil {
		t.Fatalf("unlabelled gauge: %v", s)
	}
}

// The request table holds at most MaxRoutes routes; past it, and for a
// route that isn't a plain label, requests count as Unmatched.
func TestRequestRoutesAreBounded(t *testing.T) {
	q := NewRegistry().Requests
	for i := range 2000 {
		q.Observe(fmt.Sprintf("GET /api/things/%d", i), 200, time.Millisecond, false, false)
	}
	q.Observe("GET /api/\x00", 200, time.Millisecond, false, false)
	q.Observe("", 200, time.Millisecond, false, false)
	if q.Len() != MaxRoutes+1 {
		t.Fatalf("%d routes", q.Len())
	}
	unmatched := (*q.routes.Load())[Unmatched].totals()
	if unmatched.Requests != 2000-MaxRoutes+2 {
		t.Fatalf("unmatched: %+v", unmatched)
	}
}

// Recording into a series that exists allocates nothing: it's on every
// request's path.
func TestRecordingDoesntAllocate(t *testing.T) {
	r := NewRegistry()
	c := r.Counter("hits_total", "Hits.", "limit")
	h := r.Histogram("wait_ms", "Wait.", DurationBounds, "operation")
	c.Inc("auth:per-ip:block")
	h.Observe(time.Millisecond, "select")
	r.Requests.Observe("GET /api/auth/me", 200, time.Millisecond, false, false)
	for name, record := range map[string]func(){
		"counter":   func() { c.Inc("auth:per-ip:block") },
		"histogram": func() { h.Observe(3*time.Millisecond, "select") },
		"request":   func() { r.Requests.Observe("GET /api/auth/me", 200, 7*time.Millisecond, false, false) },
	} {
		if n := testing.AllocsPerRun(100, record); n != 0 {
			t.Errorf("%s: %v allocations", name, n)
		}
	}
}

// A duration on a bound counts in that bound's bucket (le), one above the
// last bound in the extra bucket; status classes, 429s, timeouts and
// aborted requests are counted apart, an aborted one in no class.
func TestRequestTotals(t *testing.T) {
	q := NewRegistry().Requests
	q.Observe("GET /x", 200, 5*time.Millisecond, false, false)
	q.Observe("GET /x", 200, 5*time.Millisecond+1, false, false)
	q.Observe("GET /x", 429, time.Second, false, false)
	q.Observe("GET /x", 500, 11*time.Second, true, false)
	q.Observe("GET /x", 0, 2*time.Second, false, true)
	q.Observe("GET /x", 304, 0, false, false)
	got := (*q.routes.Load())["GET /x"].totals()
	want := RouteTotals{Route: "GET /x", Requests: 6, Status2xx: 2, Status3xx: 1, Status4xx: 1, Status429: 1, Status5xx: 1, Timeouts: 1, Aborted: 1,
		DurationSum: 10*time.Millisecond + 1 + time.Second + 11*time.Second + 2*time.Second,
		Buckets:     [12]int64{2, 1, 0, 0, 0, 0, 0, 1, 1, 0, 0, 1}}
	if got != want {
		t.Fatalf("totals:\n got %+v\nwant %+v", got, want)
	}
}

// TakeMinute returns what changed since the last call: routes and series
// with no change are left out, gauges are always in.
func TestTakeMinute(t *testing.T) {
	r := NewRegistry()
	c := r.Counter("refused_total", "Refused.", "limit")
	h := r.Histogram("lag_ms", "Lag.", []float64{10, 100}, "listener")
	g := r.Gauge("pending", "Pending.")
	r.Requests.Observe("GET /a", 200, 2*time.Millisecond, false, false)
	r.Requests.Observe("GET /a", 503, 20*time.Millisecond, false, false)
	c.Add(3, "auth:per-ip:block")
	h.Observe(50*time.Millisecond, "mail.registration")
	g.Set(7)
	minute := time.Date(2026, 11, 1, 10, 4, 30, 0, time.UTC)
	m := r.TakeMinute(minute)
	if !m.Minute.Equal(time.Date(2026, 11, 1, 10, 4, 0, 0, time.UTC)) || len(m.Routes) != 1 || m.Routes[0].Requests != 2 || m.Routes[0].Status5xx != 1 {
		t.Fatalf("first minute: %+v", m)
	}
	series := map[string]SeriesMinute{}
	for _, s := range m.Series {
		series[s.Metric+"{"+s.Labels+"}"] = s
	}
	if s := series[`refused_total{limit="auth:per-ip:block"}`]; s.Count != 3 || s.Buckets != nil {
		t.Errorf("counter: %+v", s)
	}
	if s := series[`lag_ms{listener="mail.registration"}`]; s.Count != 1 || s.Sum != 50 || fmt.Sprint(s.Buckets) != "[0 1 0]" {
		t.Errorf("histogram: %+v", s)
	}
	if s := series["pending{}"]; s.Count != 1 || s.Sum != 7 {
		t.Errorf("gauge: %+v", s)
	}
	// Nothing new: only the gauge.
	m = r.TakeMinute(minute.Add(time.Minute))
	if len(m.Routes) != 0 || len(m.Series) != 1 || m.Series[0].Metric != "pending" {
		t.Fatalf("quiet minute: %+v", m)
	}
	c.Inc("auth:per-ip:block")
	r.Requests.Observe("GET /a", 200, time.Millisecond, false, false)
	m = r.TakeMinute(minute.Add(2 * time.Minute))
	if len(m.Routes) != 1 || m.Routes[0].Requests != 1 || m.Routes[0].Status5xx != 0 || len(m.Series) != 2 {
		t.Fatalf("third minute: %+v", m)
	}
}

// minuteStore records what a MinuteFlush stored, failing while fail is set.
type minuteStore struct {
	minutes []Minute
	fail    bool
}

func (s *minuteStore) store(_ context.Context, m Minute) error {
	s.minutes = append(s.minutes, m)
	if s.fail {
		return errors.New("database down")
	}
	return nil
}

// A failed write drops its minute: the next one carries only what came
// after, nothing is retried, and Run tries once per minute. With Discard
// (METRICS_STORE=off) the store still gets each minute, for the heartbeat,
// but without its routes or series. A minute is never written twice.
func TestMinuteFlush(t *testing.T) {
	r := NewRegistry()
	s := &minuteStore{fail: true}
	now := time.Date(2026, 11, 1, 10, 4, 59, 0, time.UTC)
	ticks := make(chan time.Time)
	f := &MinuteFlush{Registry: r, Store: s.store, Now: func() time.Time { return now }, after: func(time.Duration) <-chan time.Time { return ticks }}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.Run(ctx)
	}()
	r.Requests.Observe("GET /a", 200, time.Millisecond, false, false)
	ticks <- now // the 10:05 boundary: writes 10:04, which fails
	r.Requests.Observe("GET /a", 200, time.Millisecond, false, false)
	ticks <- now // writes 10:04 again by the clock, moved on to 10:05
	cancel()
	<-done
	if len(s.minutes) != 2 {
		t.Fatalf("%d writes, want one per tick", len(s.minutes))
	}
	first, second := s.minutes[0], s.minutes[1]
	if !first.Minute.Equal(time.Date(2026, 11, 1, 10, 4, 0, 0, time.UTC)) || first.Routes[0].Requests != 1 {
		t.Fatalf("first: %+v", first)
	}
	if !second.Minute.Equal(first.Minute.Add(time.Minute)) || second.Routes[0].Requests != 1 {
		t.Fatalf("second, after a failure: %+v", second)
	}
	// The last flush before a shutdown takes the current minute, after
	// the last one written.
	s.fail = false
	r.Requests.Observe("GET /a", 200, time.Millisecond, false, false)
	if err := f.Flush(context.Background()); err != nil || !s.minutes[2].Minute.Equal(second.Minute.Add(time.Minute)) {
		t.Fatalf("flush: %v %+v", err, s.minutes[2])
	}
	f.Discard = true
	r.Requests.Observe("GET /a", 200, time.Millisecond, false, false)
	if err := f.Flush(context.Background()); err != nil || len(s.minutes[3].Routes) != 0 || len(s.minutes[3].Series) != 0 {
		t.Fatalf("discarded: %+v", s.minutes[3])
	}
}

// A metric is declared once, with at most two labels.
func TestDeclarations(t *testing.T) {
	r := NewRegistry()
	r.Counter("once_total", "Once.")
	for name, declare := range map[string]func(){
		"twice":        func() { r.Counter("once_total", "Again.") },
		"three labels": func() { r.Counter("many_total", "Many.", "a", "b", "c") },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: no panic", name)
				}
			}()
			declare()
		}()
	}
}

var serverIDShape = regexp.MustCompile(`^srv_[0-9A-Za-z]{14}$`)

// Server IDs are srv_ and 14 base62, new each time.
func TestServerIDs(t *testing.T) {
	a, b := NewServerID(), NewServerID()
	if !serverIDShape.MatchString(a) || a == b {
		t.Fatalf("%q %q", a, b)
	}
}

// What counting a request costs, on its own (part of
// BenchmarkRequestOverhead in httpapi).
func BenchmarkObserveRequest(b *testing.B) {
	q := NewRegistry().Requests
	b.ReportAllocs()
	for b.Loop() {
		q.Observe("GET /api/workspaces/{workspace}/projects", 200, 3*time.Millisecond, false, false)
	}
}
