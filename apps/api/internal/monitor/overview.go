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
// levels, a sparkline over the window, and the page they belong to. Tiles
// whose page isn't built yet still show their numbers where the data is
// already there (metrics, the mail queue, grouped errors, last seen);
// Integrity and Alerts come with their pages.

// PageOverview is the Overview's name.
const PageOverview = "overview"

// Pages tiles belong to that aren't built yet (plans/features/admin/
// monitoring.md); the web app links only the built ones.
const (
	pagePerformance   = "performance"
	pageErrors        = "errors"
	pageUsage         = "usage"
	pageConfiguration = "configuration"
	pageMail          = "mail"
)

// requestWindow is what the request signals count over.
const requestWindow = time.Hour

// OverviewSignals are the signals of tiles from pages not built yet, read
// by the Overview; the others are on their own pages.
var OverviewSignals = []Signal{
	{
		Name: "servers.live", Page: pageConfiguration, Label: "Live servers", Unit: UnitCount,
		// Heard from in the last 2 minutes. A dashboard can't say there
		// are none: the external uptime check does.
		Window: store.ServerLiveFor,
		Measure: func(ctx context.Context, env Env, _ Span) (Reading, error) {
			live, err := env.Store.FindLiveServers(ctx)
			return Reading{Value: float64(live.Count), Sample: live.Count}, err
		},
	},
	{
		// More than one version live: a release in progress, or stopped
		// halfway.
		Name: "servers.versions", Page: pageConfiguration, Label: "Versions live", Unit: UnitCount,
		Window: store.ServerLiveFor, Warn: 2,
		Measure: func(ctx context.Context, env Env, _ Span) (Reading, error) {
			live, err := env.Store.FindLiveServers(ctx)
			return Reading{Value: float64(len(live.Versions)), Sample: live.Count}, err
		},
	},
	{
		Name: "http.requests_per_minute", Page: pagePerformance, Label: "Requests per minute", Unit: UnitPerMin,
		Window: requestWindow,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			t, err := requestTotals(ctx, env, sp)
			minutes := sp.To.Sub(sp.From).Minutes()
			return Reading{Value: float64(t.Requests) / minutes, Sample: t.Requests}, err
		},
	},
	{
		Name: "http.server_error_share", Page: pagePerformance, Label: "Server errors", Unit: UnitPercent,
		Window: requestWindow, Warn: 1, Critical: 5, MinSample: 100,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			t, err := requestTotals(ctx, env, sp)
			return share(t.ServerErrors, t.Requests), err
		},
	},
	{
		Name: "http.latency_p95", Page: pagePerformance, Label: "Latency, 95th percentile", Unit: UnitMs,
		Window: requestWindow, Warn: 1000, Critical: 3000, MinSample: 20,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			t, err := requestTotals(ctx, env, sp)
			return Reading{Value: Percentile(t.Durations, 0.95), Sample: t.Requests}, err
		},
	},
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
	{
		Name: "errors.new", Page: pageErrors, Label: "New error groups", Unit: UnitCount,
		Window: 24 * time.Hour, Warn: 1,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			g, err := env.Store.CountErrorGroups(ctx, sp.From)
			return count(g.New, err)
		},
	},
	{
		Name: "errors.new_panics", Page: pageErrors, Label: "New panics", Unit: UnitCount,
		Window: 24 * time.Hour, Critical: 1,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			g, err := env.Store.CountErrorGroups(ctx, sp.From)
			return count(g.NewPanics, err)
		},
	},
	{
		Name: "errors.open", Page: pageErrors, Label: "Open error groups", Unit: UnitCount,
		Window: 24 * time.Hour,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			g, err := env.Store.CountErrorGroups(ctx, sp.From)
			return count(g.Open, err)
		},
	},
	{
		Name: "usage.sign_ups", Page: pageUsage, Label: "Sign-ups", Unit: UnitCount,
		Window: 24 * time.Hour,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			return count(env.Store.CountEvents(ctx, sp.From, sp.To, "auth.registered"))
		},
		Drill: drill("auth.registered"),
	},
	{
		Name: "usage.active_accounts", Page: pageUsage, Label: "Active accounts", Unit: UnitCount,
		Window: 24 * time.Hour,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			return count(env.Store.CountActiveAccounts(ctx, sp.From))
		},
	},
}

// requestTotals totals every server's requests in sp.
func requestTotals(ctx context.Context, env Env, sp Span) (store.RequestTotals, error) {
	buckets, err := env.Store.SummarizeRequests(ctx, store.SummaryWindow{From: sp.From, To: sp.To})
	if err != nil || len(buckets) == 0 {
		return store.RequestTotals{Durations: make([]int64, len(observe.DurationBounds)+1)}, err
	}
	return buckets[0].RequestTotals, nil
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

// overviewSections are the Overview's tiles.
type overviewSections struct {
	Tiles []Tile `json:"tiles"`
}

// overviewTiles are the tiles, in order, by their signals.
var overviewTiles = []Tile{
	{ID: "servers", Label: "Servers", Page: pageConfiguration, Signals: []string{"servers.live", "servers.versions"}},
	{ID: "requests", Label: "Requests", Page: pagePerformance, Signals: []string{"http.requests_per_minute"}},
	{ID: "server_errors", Label: "Server errors", Page: pagePerformance, Signals: []string{"http.server_error_share"}},
	{ID: "latency", Label: "Latency", Page: pagePerformance, Signals: []string{"http.latency_p95"}},
	{ID: "mail", Label: "Mail", Page: pageMail, Signals: []string{"mail.oldest_pending", "mail.failed_deliveries"}},
	{ID: "errors", Label: "Errors", Page: pageErrors, Signals: []string{"errors.new", "errors.new_panics", "errors.open"}},
	{ID: "failed_sign_ins", Label: "Failed sign-ins", Page: PageSecurity, Signals: []string{"auth.failed_sign_ins"}},
	{ID: "limits", Label: "Shared limits", Page: PageLimits, Signals: []string{"limits.shared_reached"}},
	{ID: "admin_door", Label: "Admin door", Page: PageSecurity, Signals: []string{"admin.door_refusals", "admin.door_failures"}},
	{ID: "sign_ups", Label: "Sign-ups", Page: pageUsage, Signals: []string{"usage.sign_ups", "usage.active_accounts"}},
}

// overviewSignals are every tile's signals, in tile order.
func overviewSignals() []Signal {
	var out []Signal
	all := append(append(append([]Signal{}, OverviewSignals...), SecuritySignals...), LimitsSignals...)
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
	// Each sparkline is a part of its own, named by its tile (the request
	// totals feed three); a tile whose part timed out has none.
	read, err := readSections(ctx, env, nil, []section{
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
	read.Value = overviewSections{Tiles: tiles}
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
