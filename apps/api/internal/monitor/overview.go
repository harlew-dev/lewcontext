package monitor

import (
	"context"
	"lewcontext/api/internal/observe"
	"lewcontext/api/internal/store"
	"slices"
	"time"
)

// The Overview (docs/api/monitoring.md#overview): is everything all right,
// and if not, where? A grid of tiles, each one or more signals with their
// levels, a sparkline over the window, and the page they belong to (Mail
// is the admin dashboard's Mail page). Alerts come with their page.

// PageOverview is the Overview's name.
const PageOverview = "overview"

// pageMail is the Mail tile's page: the admin dashboard's Mail page, not
// a Monitoring one.
const pageMail = "mail"

// requestWindow is what the request signals count over.
const requestWindow = time.Hour

// OverviewSignals are the signals of tiles whose page isn't a Monitoring
// page (Mail), read by the Overview; the others are on their own pages.
var OverviewSignals = []Signal{
	{
		// How far the event worker is behind: every server down, or the
		// worker stalled.
		Name: "mail.oldest_pending", Page: pageMail, Label: "Oldest waiting delivery", Unit: UnitMinutes,
		Window: 24 * time.Hour, Warn: 2, Critical: 10,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			q, err := env.Store.FindMailQueue(ctx, sp.From)
			if err != nil || q.OldestPendingAt == nil {
				return Reading{}, err
			}
			return Reading{Value: env.Now.Sub(*q.OldestPendingAt).Minutes(), Sample: 1}, nil
		},
	},
	{
		Name: "mail.failed_deliveries", Page: pageMail, Label: "Deliveries failed", Unit: UnitCount,
		Window: 24 * time.Hour, Warn: 1, Critical: 10,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			q, err := env.Store.FindMailQueue(ctx, sp.From)
			return count(q.Failed, err)
		},
	},
}

// Percentile estimates the q quantile (0.95) of durations counted per
// histogram bound (observe.DurationBounds, then one above), in
// milliseconds: linear within its bucket, as Prometheus estimates it. A
// quantile above the last bound is that bound.
func Percentile(counts []int64, q float64) float64 {
	var total int64
	for _, n := range counts {
		total += n
	}
	if total == 0 {
		return 0
	}
	rank := q * float64(total)
	var seen int64
	lower := 0.0
	for i, n := range counts {
		if i >= len(observe.DurationBounds) {
			return observe.DurationBounds[len(observe.DurationBounds)-1]
		}
		upper := observe.DurationBounds[i]
		if n > 0 && float64(seen+n) >= rank {
			return lower + (upper-lower)*(rank-float64(seen))/float64(n)
		}
		seen += n
		lower = upper
	}
	return observe.DurationBounds[len(observe.DurationBounds)-1]
}

// Tile is one of the Overview's tiles: its signals (by name, in the
// response's signals), the page they belong to, and its sparkline.
type Tile struct {
	ID      string      `json:"id"`
	Label   string      `json:"label"`
	Page    string      `json:"page"`
	Signals []string    `json:"signals"`
	Series  *TileSeries `json:"series"`
}

// TileSeries is a tile's sparkline: a point per bucket of the window.
type TileSeries struct {
	Label  string  `json:"label"`
	Unit   string  `json:"unit"`
	Points []Point `json:"points"`
}

// Point is a sparkline's value for the bucket starting at Start.
type Point struct {
	Start time.Time `json:"start"`
	Value float64   `json:"value"`
}

// overviewSections are the Overview's tiles, and how the live servers
// store metrics (METRICS_STORE: with it off, the request and pool tiles
// read nothing).
type overviewSections struct {
	Tiles        []Tile `json:"tiles"`
	MetricsStore string `json:"metricsStore"`
}

// overviewTiles are the tiles, in order, by their signals.
var overviewTiles = []Tile{
	{ID: "servers", Label: "Servers", Page: PageConfiguration, Signals: []string{"servers.live", "servers.versions"}},
	{ID: "requests", Label: "Requests", Page: PagePerformance, Signals: []string{"http.requests_per_minute"}},
	{ID: "server_errors", Label: "Server errors", Page: PagePerformance, Signals: []string{"http.server_error_share"}},
	{ID: "latency", Label: "Latency", Page: PagePerformance, Signals: []string{"http.latency_p95"}},
	{ID: "database", Label: "Database", Page: PagePerformance, Signals: []string{"db.round_trip", "db.pool_wait_minutes"}},
	{ID: "data", Label: "Data", Page: PageData, Signals: []string{"data.database_size", "data.prune_not_running", "data.retention_overdue"}},
	{ID: "mail", Label: "Mail", Page: pageMail, Signals: []string{"mail.oldest_pending", "mail.failed_deliveries"}},
	{ID: "errors", Label: "Errors", Page: PageErrors, Signals: []string{"errors.new", "errors.new_panics", "errors.open"}},
	{ID: "integrity", Label: "Integrity", Page: PageIntegrity, Signals: []string{integrityCritical, integrityWarning}},
	{ID: "failed_sign_ins", Label: "Failed sign-ins", Page: PageSecurity, Signals: []string{"auth.failed_sign_ins"}},
	{ID: "limits", Label: "Shared limits", Page: PageLimits, Signals: []string{"limits.shared_reached"}},
	{ID: "admin_door", Label: "Admin door", Page: PageSecurity, Signals: []string{"admin.door_refusals", "admin.door_failures"}},
	{ID: "sign_ups", Label: "Sign-ups", Page: PageUsage, Signals: []string{"usage.sign_ups", "usage.active_accounts"}},
}

// overviewSignals are every tile's signals, in tile order.
func overviewSignals() []Signal {
	var out []Signal
	all := Signals()
	for _, t := range overviewTiles {
		for _, name := range t.Signals {
			i := slices.IndexFunc(all, func(s Signal) bool { return s.Name == name })
			out = append(out, all[i])
		}
	}
	return out
}

// overviewPage reads the tiles' sparklines over w.
func overviewPage(ctx context.Context, env Env, w Window) (Sections, error) {
	s := env.Store
	series := map[string]*TileSeries{}
	metrics := ""
	// Each sparkline is a part of its own, named by its tile (the request
	// totals feed three); a tile whose part timed out has none.
	read, err := readSections(ctx, env, nil, []section{
		{"servers", func(ctx context.Context) error {
			servers, err := s.IndexServerInstances(ctx)
			metrics = metricsStore(servers, env.Now)
			return err
		}},
		{"requests", func(ctx context.Context) error {
			requests, err := s.SummarizeRequests(ctx, w.SummaryWindow)
			if err != nil {
				return err
			}
			series["requests"] = requestSeries("Requests", UnitCount, requests, func(b store.RequestBucket) float64 { return float64(b.Requests) })
			series["server_errors"] = requestSeries("Server errors", UnitPercent, requests, func(b store.RequestBucket) float64 {
				return share(b.ServerErrors, b.Requests).Value
			})
			series["latency"] = requestSeries("95th percentile", UnitMs, requests, func(b store.RequestBucket) float64 {
				return Percentile(b.Durations, 0.95)
			})
			return nil
		}},
		{"database", func(ctx context.Context) error {
			inUse, err := s.SummarizeGauge(ctx, "db_pool_in_use", w.SummaryWindow)
			points := make([]Point, len(inUse))
			for i, p := range inUse {
				points[i] = Point{Start: p.Start, Value: p.Value}
			}
			series["database"] = &TileSeries{Label: "Connections in use", Unit: UnitCount, Points: points}
			return err
		}},
		{"data", func(ctx context.Context) error {
			recorded, err := s.SummarizeEventsRecorded(ctx, w.SummaryWindow)
			series["data"] = eventSeries("Events recorded", recorded, "events")
			return err
		}},
		{"failed_sign_ins", func(ctx context.Context) error {
			signIns, err := s.SummarizeSignIns(ctx, w.SummaryWindow)
			series["failed_sign_ins"] = eventSeries("Failed sign-ins", signIns, "wrong_password", "unknown_account")
			return err
		}},
		{"limits", func(ctx context.Context) error {
			limits, err := s.SummarizeLimitsReached(ctx, w.SummaryWindow, globalLimits(env)...)
			series["limits"] = eventSeries("Shared limits reached", limits, limits.Series...)
			return err
		}},
		{"admin_door", func(ctx context.Context) error {
			door, err := s.SummarizeDoor(ctx, w.SummaryWindow)
			series["admin_door"] = eventSeries("Failures and refusals", door, "failed", "refused")
			return err
		}},
		{"sign_ups", func(ctx context.Context) error {
			signUps, err := s.SummarizeEventNames(ctx, w.SummaryWindow, "auth.registered")
			series["sign_ups"] = eventSeries("Sign-ups", signUps, "auth.registered")
			return err
		}},
	})
	if err != nil {
		return Sections{}, err
	}
	for _, name := range read.Unavailable {
		delete(series, name)
		if name == "requests" {
			delete(series, "server_errors")
			delete(series, "latency")
		}
	}
	tiles := make([]Tile, len(overviewTiles))
	for i, t := range overviewTiles {
		t.Series = series[t.ID]
		tiles[i] = t
	}
	read.Value = overviewSections{Tiles: tiles, MetricsStore: metrics}
	return read, nil
}

// requestSeries is a sparkline of a value per request bucket.
func requestSeries(label, unit string, buckets []store.RequestBucket, value func(store.RequestBucket) float64) *TileSeries {
	out := &TileSeries{Label: label, Unit: unit, Points: make([]Point, len(buckets))}
	for i, b := range buckets {
		out.Points[i] = Point{Start: b.Start, Value: value(b)}
	}
	return out
}

// eventSeries is a sparkline of the sum of some of a chart's series.
func eventSeries(label string, m store.MonitorSeries, keys ...string) *TileSeries {
	out := &TileSeries{Label: label, Unit: UnitCount, Points: make([]Point, len(m.Buckets))}
	for i, b := range m.Buckets {
		var n int
		for j, key := range m.Series {
			if slices.Contains(keys, key) {
				n += b.Counts[j]
			}
		}
		out.Points[i] = Point{Start: b.Start, Value: float64(n)}
	}
	return out
}
