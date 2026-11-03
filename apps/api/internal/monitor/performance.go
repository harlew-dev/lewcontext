package monitor

import (
	"context"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/observe"
	"lewcontext/api/internal/store"
	"slices"
	"time"
)

// The Performance page (docs/api/monitoring.md#performance): which routes
// are slow, is the database or the worker saturated? From the minutes
// every server writes (and their hourly sums), all servers summed; each
// server's own gauges last.

// PagePerformance is the Performance page's name.
const PagePerformance = "performance"

// Signal windows of the database's numbers.
const (
	// pingWindow: the round trip is measured as the page is read.
	pingWindow = time.Minute
	// poolWaitWindow: pool waits count when they last 5 of the last 6
	// minutes (the minute just ended may not be written yet).
	poolWaitWindow = 6 * time.Minute
)

// slowRouteSample: a route's p95 means something from this many requests.
const slowRouteSample = 20

// PerformanceSignals are the Performance page's signals.
var PerformanceSignals = []Signal{
	{
		Name: "http.requests_per_minute", Page: PagePerformance, Label: "Requests per minute", Unit: UnitPerMin,
		Window: requestWindow,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			t, err := requestTotals(ctx, env, sp)
			minutes := sp.To.Sub(sp.From).Minutes()
			return Reading{Value: float64(t.Requests) / minutes, Sample: t.Requests}, err
		},
	},
	{
		Name: "http.server_error_share", Page: PagePerformance, Label: "Server errors", Unit: UnitPercent,
		Window: requestWindow, Warn: 1, Critical: 5, MinSample: 100,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			t, err := requestTotals(ctx, env, sp)
			return share(t.ServerErrors, t.Requests), err
		},
	},
	{
		Name: "http.latency_p95", Page: PagePerformance, Label: "Latency, 95th percentile", Unit: UnitMs,
		Window: requestWindow, Warn: 1000, Critical: 3000, MinSample: 20,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			t, err := requestTotals(ctx, env, sp)
			return Reading{Value: Percentile(t.Durations, 0.95), Sample: t.Requests}, err
		},
	},
	{
		// The slowest route's p95, of those with slowRouteSample requests.
		Name: "http.slowest_route_p95", Page: PagePerformance, Label: "Slowest route, 95th percentile", Unit: UnitMs,
		Window: requestWindow, Warn: 2000, Critical: 5000, MinSample: slowRouteSample,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			routes, err := env.Store.IndexRouteStats(ctx, sp.From, sp.To)
			var r Reading
			for _, route := range routes {
				if p95 := Percentile(route.Durations, 0.95); route.Requests >= slowRouteSample && p95 > r.Value {
					r = Reading{Value: p95, Sample: route.Requests}
				}
			}
			return r, err
		},
	},
	{
		// A round trip to the database from this server, now: taking a
		// connection from the pool included.
		Name: "db.round_trip", Page: PagePerformance, Label: "Database round trip", Unit: UnitMs,
		Window: pingWindow, Critical: 500,
		Measure: func(ctx context.Context, env Env, _ Span) (Reading, error) {
			start := time.Now()
			err := env.Store.DB.PingContext(ctx)
			return Reading{Value: float64(time.Since(start).Microseconds()) / 1000, Sample: 1}, err
		},
	},
	{
		// Waits for a connection in most minutes: the pool (10) is the
		// bottleneck.
		Name: "db.pool_wait_minutes", Page: PagePerformance, Label: "Minutes with pool waits", Unit: UnitCount,
		Window: poolWaitWindow, Warn: 5,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			waits, err := env.Store.SummarizePoolWaits(ctx, sp.From, sp.To)
			return count(waits.Minutes, err)
		},
	},
	{
		// How long a wait took on average (the running totals keep no
		// histogram, so no percentile).
		Name: "db.pool_wait_time", Page: PagePerformance, Label: "Average pool wait", Unit: UnitMs,
		Window: time.Hour, Critical: 1000, MinSample: 1,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			waits, err := env.Store.SummarizePoolWaits(ctx, sp.From, sp.To)
			if err != nil || waits.Waits == 0 {
				return Reading{}, err
			}
			return Reading{Value: waits.WaitedMs / float64(waits.Waits), Sample: waits.Waits}, nil
		},
	},
	{
		// The histogram's bounds end at 10 s, so a p95 at 10 s means 10 s
		// or more; a stalled worker shows on the oldest waiting delivery.
		Name: "events.delivery_lag_p95", Page: PagePerformance, Label: "Delivery lag, 95th percentile", Unit: UnitMs,
		Window: time.Hour, Warn: observe.DurationBounds[len(observe.DurationBounds)-1],
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			lag, err := env.Store.SummarizeHistograms(ctx, "delivery_lag_ms", "", sp.From, sp.To)
			if err != nil || len(lag) == 0 {
				return Reading{}, err
			}
			return Reading{Value: Percentile(lag[0].Buckets, 0.95), Sample: lag[0].Count}, nil
		},
	},
	{
		// Sign-ins and password checks refused because every hashing slot
		// was taken: load, or an attack on sign-in.
		Name: "auth.password_hash_busy", Page: PagePerformance, Label: "Password checks refused, slots busy", Unit: UnitCount,
		Window: time.Hour, Warn: 1,
		Measure: func(ctx context.Context, env Env, sp Span) (Reading, error) {
			totals, err := env.Store.SummarizeMetricTotals(ctx, "password_hash_busy_total", "", sp.From, sp.To)
			var n int64
			for _, t := range totals {
				n += t.Count
			}
			return count(n, err)
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

// Metric storage, as the live servers have it (METRICS_STORE): with it off
// nothing above is written, and the numbers read zero.
const (
	MetricsStoreOn    = "on"
	MetricsStoreOff   = "off"
	MetricsStoreMixed = "mixed"
)

// metricsStore is how the live servers store metrics: on, off, mixed, or
// "" with none live.
func metricsStore(servers []store.ServerInstance, now time.Time) string {
	live := liveServers(servers, now)
	off := 0
	for _, si := range live {
		if configOf(si).MetricsStore == config.MetricsStoreOff {
			off++
		}
	}
	switch {
	case len(live) == 0:
		return ""
	case off == len(live):
		return MetricsStoreOff
	case off > 0:
		return MetricsStoreMixed
	}
	return MetricsStoreOn
}

// Latencies are a histogram's percentiles, in milliseconds.
type Latencies struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
}

func latenciesOf(counts []int64) Latencies {
	return Latencies{P50: Percentile(counts, 0.5), P95: Percentile(counts, 0.95), P99: Percentile(counts, 0.99)}
}

// RequestPoint is a bucket's requests: how many, the server errors, and
// their latencies.
type RequestPoint struct {
	Start        time.Time `json:"start"`
	Requests     int64     `json:"requests"`
	ServerErrors int64     `json:"serverErrors"`
	Latencies
}

// RouteRow is a route's requests in the window with its latencies.
type RouteRow struct {
	store.RouteStats
	Latencies
}

// maxRouteRows is how many routes the page lists, by requests.
const maxRouteRows = 100

// LatencyRow is a histogram's percentiles for one label value.
type LatencyRow struct {
	Label string `json:"label"`
	Count int64  `json:"count"`
	Latencies
}

// ServerRow is a server's latest gauges (from its last minutes), with when
// it started and was last heard from.
type ServerRow struct {
	ID          string     `json:"id"`
	Hostname    string     `json:"hostname"`
	Version     string     `json:"version"`
	StartedAt   time.Time  `json:"startedAt"`
	LastSeenAt  time.Time  `json:"lastSeenAt"`
	Live        bool       `json:"live"`
	Goroutines  *float64   `json:"goroutines"`
	HeapBytes   *float64   `json:"heapBytes"`
	MemoryBytes *float64   `json:"memoryBytes"`
	GCPauseMs   *float64   `json:"gcPauseMs"`
	PoolOpen    *float64   `json:"poolOpen"`
	PoolInUse   *float64   `json:"poolInUse"`
	PoolIdle    *float64   `json:"poolIdle"`
	MeasuredAt  *time.Time `json:"measuredAt"`
}

// serverGauges are the gauges each server's row shows.
var serverGauges = []string{"go_goroutines", "go_heap_bytes", "go_memory_bytes", "go_gc_pause_ms", "db_pool_open", "db_pool_in_use", "db_pool_idle"}

// serverGaugeWindow is how far back a server's latest gauges are looked for.
const serverGaugeWindow = 5 * time.Minute

// performanceSections are the Performance page's charts and tables.
type performanceSections struct {
	// MetricsStore is how the live servers store metrics: with it off,
	// everything here reads zero.
	MetricsStore string         `json:"metricsStore"`
	Requests     []RequestPoint `json:"requests"`
	Routes       []RouteRow     `json:"routes"`
	Database     struct {
		Operations  []LatencyRow          `json:"operations"`
		SlowQueries []store.MetricTotal   `json:"slowQueries"`
		InUse       []store.GaugePoint    `json:"inUse"`
		Waits       store.PoolWaits       `json:"waits"`
		Statements  *store.StatementStats `json:"statements"`
	} `json:"database"`
	Worker struct {
		Deliveries store.MonitorSeries `json:"deliveries"`
		Lag        []LatencyRow        `json:"lag"`
		Pending    []store.GaugePoint  `json:"pending"`
	} `json:"worker"`
	// SlotsBusy: sign-ins and password checks refused, every hashing
	// slot taken.
	SlotsBusy int64       `json:"slotsBusy"`
	Servers   []ServerRow `json:"servers"`
}

// performancePage reads the Performance page over w.
func performancePage(ctx context.Context, env Env, w Window) (Sections, error) {
	s := env.Store
	out := &performanceSections{Requests: []RequestPoint{}, Routes: []RouteRow{}, Servers: []ServerRow{}}
	out.Database.Operations, out.Worker.Lag = []LatencyRow{}, []LatencyRow{}
	return readSections(ctx, env, out, []section{
		{"servers", func(ctx context.Context) error {
			servers, err := s.IndexServerInstances(ctx)
			if err != nil {
				return err
			}
			out.MetricsStore = metricsStore(servers, env.Now)
			gauges, err := s.IndexServerGauges(ctx, env.Now.Add(-serverGaugeWindow), serverGauges)
			out.Servers = serverRows(servers, gauges, env.Now)
			return err
		}},
		{"requests", func(ctx context.Context) error {
			buckets, err := s.SummarizeRequests(ctx, w.SummaryWindow)
			for _, b := range buckets {
				out.Requests = append(out.Requests, RequestPoint{Start: b.Start, Requests: b.Requests, ServerErrors: b.ServerErrors, Latencies: latenciesOf(b.Durations)})
			}
			return err
		}},
		{"routes", func(ctx context.Context) error {
			routes, err := s.IndexRouteStats(ctx, w.From, w.To)
			for i, r := range routes {
				if i == maxRouteRows {
					break
				}
				out.Routes = append(out.Routes, RouteRow{RouteStats: r, Latencies: latenciesOf(r.Durations)})
			}
			return err
		}},
		{"database", func(ctx context.Context) error {
			ops, err := s.SummarizeHistograms(ctx, "db_query_duration_ms", "operation", w.From, w.To)
			out.Database.Operations = latencyRows(ops)
			return err
		}},
		{"database", func(ctx context.Context) (err error) {
			out.Database.SlowQueries, err = s.SummarizeMetricTotals(ctx, "db_slow_queries_total", "table", w.From, w.To)
			return
		}},
		{"database", func(ctx context.Context) (err error) {
			out.Database.InUse, err = s.SummarizeGauge(ctx, "db_pool_in_use", w.SummaryWindow)
			return
		}},
		{"database", func(ctx context.Context) (err error) {
			out.Database.Waits, err = s.SummarizePoolWaits(ctx, w.From, w.To)
			return
		}},
		{"statements", func(ctx context.Context) error {
			stats, err := s.SummarizeStatements(ctx)
			out.Database.Statements = &stats
			return err
		}},
		{"worker", func(ctx context.Context) (err error) {
			out.Worker.Deliveries, err = s.SummarizeMetric(ctx, "deliveries_total", "outcome", nil, w.SummaryWindow)
			return
		}},
		{"worker", func(ctx context.Context) error {
			lag, err := s.SummarizeHistograms(ctx, "delivery_lag_ms", "listener", w.From, w.To)
			out.Worker.Lag = latencyRows(lag)
			return err
		}},
		{"worker", func(ctx context.Context) (err error) {
			out.Worker.Pending, err = s.SummarizeGauge(ctx, "deliveries_pending", w.SummaryWindow)
			return
		}},
		{"hashing", func(ctx context.Context) error {
			totals, err := s.SummarizeMetricTotals(ctx, "password_hash_busy_total", "", w.From, w.To)
			for _, t := range totals {
				out.SlotsBusy += t.Count
			}
			return err
		}},
	})
}

// latencyRows are histograms' percentiles, by label.
func latencyRows(totals []store.HistogramTotal) []LatencyRow {
	out := make([]LatencyRow, len(totals))
	for i, t := range totals {
		out[i] = LatencyRow{Label: t.Label, Count: t.Count, Latencies: latenciesOf(t.Buckets)}
	}
	return out
}

// serverRows are the servers with their latest gauges, live ones first.
func serverRows(servers []store.ServerInstance, gauges []store.ServerGauge, now time.Time) []ServerRow {
	out := make([]ServerRow, len(servers))
	index := map[string]int{}
	for i, si := range servers {
		out[i] = ServerRow{ID: si.ID, Hostname: si.Hostname, Version: si.Version, StartedAt: si.StartedAt, LastSeenAt: si.LastSeenAt,
			Live: si.LastSeenAt.After(store.ServerLiveSince(now))}
		index[si.ID] = i
	}
	for _, g := range gauges {
		i, ok := index[g.Server]
		if !ok {
			continue
		}
		value, minute := g.Value, g.Minute
		row := &out[i]
		if row.MeasuredAt == nil || minute.After(*row.MeasuredAt) {
			row.MeasuredAt = &minute
		}
		switch g.Metric {
		case "go_goroutines":
			row.Goroutines = &value
		case "go_heap_bytes":
			row.HeapBytes = &value
		case "go_memory_bytes":
			row.MemoryBytes = &value
		case "go_gc_pause_ms":
			row.GCPauseMs = &value
		case "db_pool_open":
			row.PoolOpen = &value
		case "db_pool_in_use":
			row.PoolInUse = &value
		case "db_pool_idle":
			row.PoolIdle = &value
		}
	}
	slices.SortStableFunc(out, func(a, b ServerRow) int {
		if a.Live != b.Live {
			if a.Live {
				return -1
			}
			return 1
		}
		return 0
	})
	return out
}
