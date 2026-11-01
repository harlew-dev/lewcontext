package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"lewcontext/api/internal/observe"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uptrace/bun"
)

// The Performance page's queries (docs/api/monitoring.md#performance):
// routes, the database, the event worker and each server, from the
// metrics every server writes once a minute (request_minutes,
// metric_minutes, and their hourly sums for a window longer than the
// minutes are kept), and PostgreSQL's own pg_stat_statements where the
// database has it.

// RouteStats are a route pattern's requests in a window, every server's:
// the answers by class, the timeouts, and their durations per histogram
// bound (observe.DurationBounds and one above).
type RouteStats struct {
	Route     string  `json:"route" bun:"route"`
	Requests  int64   `json:"requests" bun:"requests"`
	Status4xx int64   `json:"status4xx" bun:"status_4xx"`
	Status429 int64   `json:"status429" bun:"status_429"`
	Status5xx int64   `json:"status5xx" bun:"status_5xx"`
	Timeouts  int64   `json:"timeouts" bun:"timeouts"`
	Durations []int64 `json:"-" bun:"-"`
}

// IndexRouteStats totals each route's requests in [from, to), with its
// duration histogram, most requests first. At most MaxRoutes (300) routes
// are ever counted (observe), so the list is bounded by construction.
func (s *Store) IndexRouteStats(ctx context.Context, from, to time.Time) ([]RouteStats, error) {
	rows, err := s.requestRows(ctx, from, to)
	if err != nil {
		return nil, err
	}
	out := []RouteStats{}
	if err := s.DB.NewSelect().TableExpr("(?) AS x", rows("m.route, m.requests, m.status_4xx, m.status_429, m.status_5xx, m.timeouts")).
		ColumnExpr("x.route").
		ColumnExpr("sum(x.requests)::bigint AS requests").
		ColumnExpr("sum(x.status_4xx)::bigint AS status_4xx").
		ColumnExpr("sum(x.status_429)::bigint AS status_429").
		ColumnExpr("sum(x.status_5xx)::bigint AS status_5xx").
		ColumnExpr("sum(x.timeouts)::bigint AS timeouts").
		GroupExpr("x.route").
		OrderExpr("requests DESC, x.route").
		Scan(ctx, &out); err != nil {
		return nil, err
	}
	var slots []struct {
		Route string `bun:"route"`
		Slot  int    `bun:"slot"`
		Count int64  `bun:"count"`
	}
	if err := s.DB.NewSelect().TableExpr("(?) AS x", rows("m.route, m.duration_buckets")).
		TableExpr("unnest(x.duration_buckets) WITH ORDINALITY AS d(n, slot)").
		ColumnExpr("x.route, d.slot::int AS slot, sum(d.n)::bigint AS count").
		GroupExpr("1, 2").
		Scan(ctx, &slots); err != nil {
		return nil, err
	}
	index := map[string]int{}
	for i := range out {
		out[i].Durations = make([]int64, durationSlots)
		index[out[i].Route] = i
	}
	for _, x := range slots {
		if i, ok := index[x.Route]; ok && x.Slot >= 1 && x.Slot <= durationSlots {
			out[i].Durations[x.Slot-1] += x.Count
		}
	}
	return out, nil
}

// HistogramTotal is a histogram's series summed over a window, by the
// value of one of its labels: how many observations, and how many per
// bound and one above.
type HistogramTotal struct {
	Label   string  `json:"label"`
	Count   int64   `json:"count"`
	Buckets []int64 `json:"-"`
}

// SummarizeHistograms sums a histogram metric's series in [from, to) by
// the value of label ("" sums them all into one), most observations
// first.
func (s *Store) SummarizeHistograms(ctx context.Context, metric, label string, from, to time.Time) ([]HistogramTotal, error) {
	src, err := s.metricRows(ctx, metric, from, to)
	if err != nil {
		return nil, err
	}
	var slots []struct {
		Labels string `bun:"labels"`
		Slot   int    `bun:"slot"`
		Count  int64  `bun:"count"`
	}
	if err := s.DB.NewSelect().TableExpr("(?) AS x", src).
		TableExpr("unnest(x.buckets) WITH ORDINALITY AS d(n, slot)").
		ColumnExpr("x.labels, d.slot::int AS slot, sum(d.n)::bigint AS count").
		Where("x.buckets IS NOT NULL").
		GroupExpr("1, 2").
		Scan(ctx, &slots); err != nil {
		return nil, err
	}
	byLabel := map[string]*HistogramTotal{}
	for _, x := range slots {
		value := ""
		if label != "" {
			value = labelValue(x.Labels, label)
		}
		t, ok := byLabel[value]
		if !ok {
			t = &HistogramTotal{Label: value, Buckets: make([]int64, durationSlots)}
			byLabel[value] = t
		}
		if x.Slot >= 1 && x.Slot <= durationSlots {
			t.Buckets[x.Slot-1] += x.Count
			t.Count += x.Count
		}
	}
	out := make([]HistogramTotal, 0, len(byLabel))
	for _, t := range byLabel {
		out = append(out, *t)
	}
	slices.SortFunc(out, func(a, b HistogramTotal) int {
		if a.Count != b.Count {
			return int(b.Count - a.Count)
		}
		return strings.Compare(a.Label, b.Label)
	})
	return out, nil
}

// SummarizeMetric counts a counter metric per bucket of w, a series per
// value of its label by (the four largest, then Other), keeping only the
// series whose labels have the values in match.
func (s *Store) SummarizeMetric(ctx context.Context, metric, by string, match map[string]string, w SummaryWindow) (MonitorSeries, error) {
	src, err := s.metricRows(ctx, metric, w.From, w.To)
	if err != nil {
		return MonitorSeries{}, err
	}
	var rows []struct {
		Bucket time.Time `bun:"bucket"`
		Labels string    `bun:"labels"`
		Count  int64     `bun:"count"`
	}
	interval := fmt.Sprintf("%d seconds", int(w.Bucket.Seconds()))
	if err := s.DB.NewSelect().TableExpr("(?) AS x", src).
		ColumnExpr("date_bin(?::interval, x.t, "+summaryOrigin+") AS bucket", interval).
		ColumnExpr("x.labels, sum(x.count)::bigint AS count").
		GroupExpr("1, 2").
		Scan(ctx, &rows); err != nil {
		return MonitorSeries{}, err
	}
	counts := make([]bucketCount, 0, len(rows))
	for _, r := range rows {
		if matchLabels(r.Labels, match) {
			counts = append(counts, bucketCount{Bucket: r.Bucket, Key: labelValue(r.Labels, by), Count: int(r.Count)})
		}
	}
	return foldTop(w, counts, maxChartSeries), nil
}

// maxChartSeries is how many series a chart stacks (chart.tsx).
const maxChartSeries = 5

// matchLabels reports whether a series' labels have every value in match.
func matchLabels(labels string, match map[string]string) bool {
	for name, value := range match {
		if labelValue(labels, name) != value {
			return false
		}
	}
	return true
}

// GaugePoint is a gauge's average in the bucket starting at Start.
type GaugePoint struct {
	Start time.Time `json:"start" bun:"bucket"`
	Value float64   `json:"value" bun:"value"`
}

// SummarizeGauge averages a gauge per bucket of w, over every server's
// minutes (each server's own value, averaged: "per server"). Every bucket
// is listed, empty ones as zeros.
func (s *Store) SummarizeGauge(ctx context.Context, metric string, w SummaryWindow) ([]GaugePoint, error) {
	src, err := s.metricRows(ctx, metric, w.From, w.To)
	if err != nil {
		return nil, err
	}
	var rows []GaugePoint
	interval := fmt.Sprintf("%d seconds", int(w.Bucket.Seconds()))
	if err := s.DB.NewSelect().TableExpr("(?) AS x", src).
		ColumnExpr("date_bin(?::interval, x.t, "+summaryOrigin+") AS bucket", interval).
		ColumnExpr("sum(x.sum) / sum(x.count) AS value").
		GroupExpr("1").
		Scan(ctx, &rows); err != nil {
		return nil, err
	}
	starts := BucketStarts(w)
	out := make([]GaugePoint, len(starts))
	for i, start := range starts {
		out[i] = GaugePoint{Start: start}
	}
	for _, r := range rows {
		if len(starts) > 0 {
			if i := int(r.Start.Sub(starts[0]) / w.Bucket); i >= 0 && i < len(out) {
				out[i].Value = r.Value
			}
		}
	}
	return out, nil
}

// ServerGauge is a gauge's latest value on one server.
type ServerGauge struct {
	Server string    `bun:"server"`
	Metric string    `bun:"metric"`
	Value  float64   `bun:"value"`
	Minute time.Time `bun:"minute"`
}

// IndexServerGauges reads each server's latest value of each gauge in
// metrics, from its minutes since since (the primary key's range).
func (s *Store) IndexServerGauges(ctx context.Context, since time.Time, metrics []string) ([]ServerGauge, error) {
	out := []ServerGauge{}
	err := s.DB.NewSelect().TableExpr("metric_minutes AS m").
		ColumnExpr("DISTINCT ON (m.server, m.metric) m.server, m.metric, m.sum AS value, m.minute").
		Where("m.minute >= ?", since).
		Where("m.metric IN (?)", bun.In(metrics)).
		OrderExpr("m.server, m.metric, m.minute DESC").
		Scan(ctx, &out)
	return out, err
}

// PoolWaits are the database pool's waits for a connection in a window,
// every server's: how many, how long in all, and in how many minutes any
// server waited. From the minutes only (each server's running totals, as
// sql.DBStats keeps them), so at most MinuteRetention back: From is where
// they were read from.
type PoolWaits struct {
	From     time.Time `json:"from"`
	Waits    int64     `json:"waits"`
	WaitedMs float64   `json:"waitedMs"`
	Minutes  int64     `json:"minutes"`
}

// SummarizePoolWaits sums the pool's waits in [from, to) from each
// server's running totals (db_pool_wait_count, db_pool_wait_ms): the rise
// from one of its minutes to the next.
func (s *Store) SummarizePoolWaits(ctx context.Context, from, to time.Time) (PoolWaits, error) {
	if oldest := to.Add(-MinuteRetention); from.Before(oldest) {
		from = oldest
	}
	out := PoolWaits{From: from}
	deltas := s.DB.NewSelect().TableExpr("metric_minutes AS m").
		ColumnExpr("m.minute, m.metric").
		ColumnExpr("m.sum - lag(m.sum) OVER (PARTITION BY m.server, m.metric ORDER BY m.minute) AS delta").
		Where("m.minute >= ? AND m.minute < ?", from.Add(-time.Minute), to).
		Where("m.metric IN ('db_pool_wait_count', 'db_pool_wait_ms')")
	err := s.DB.NewSelect().TableExpr("(?) AS d", deltas).
		ColumnExpr("coalesce(sum(d.delta) FILTER (WHERE d.metric = 'db_pool_wait_count' AND d.delta > 0), 0)::bigint").
		ColumnExpr("coalesce(sum(d.delta) FILTER (WHERE d.metric = 'db_pool_wait_ms' AND d.delta > 0), 0)").
		ColumnExpr("count(DISTINCT d.minute) FILTER (WHERE d.metric = 'db_pool_wait_count' AND d.delta > 0)").
		Where("d.minute >= ?", from).
		Scan(ctx, &out.Waits, &out.WaitedMs, &out.Minutes)
	return out, err
}

// Statement is one of pg_stat_statements' statements: its text as
// PostgreSQL normalised it (every constant a $n placeholder), and its
// totals since the statistics were last reset.
type Statement struct {
	Query   string  `json:"query" bun:"query"`
	Calls   int64   `json:"calls" bun:"calls"`
	TotalMs float64 `json:"totalMs" bun:"total_ms"`
	MeanMs  float64 `json:"meanMs" bun:"mean_ms"`
	Rows    int64   `json:"rows" bun:"rows"`
}

// StatementStats are the statements taking the most time in all, or
// Available false, with Unavailable saying why, when they can't be read
// (StatementsNotInstalled, StatementsNotLoaded, StatementsUnreadable).
// Hidden counts those read but not shown: past the first statementsShown,
// or refused by SafeStatement.
type StatementStats struct {
	Available   bool        `json:"available"`
	Unavailable string      `json:"unavailable,omitempty"`
	Statements  []Statement `json:"statements"`
	Hidden      int         `json:"hidden"`
}

// Why the statements aren't available: the extension isn't installed in
// this database, it's installed but not loaded (shared_preload_libraries),
// or reading it failed otherwise (a revoked grant, an older version's
// columns; logged with its SQLSTATE).
const (
	StatementsNotInstalled = "not_installed"
	StatementsNotLoaded    = "not_loaded"
	StatementsUnreadable   = "unreadable"
)

// Bounds of the statements read and shown.
const (
	statementsRead  = 50
	statementsShown = 10
	// MaxStatement is how much of a statement's text is shown.
	MaxStatement = 1000
)

// SummarizeStatements reads this database's and this role's statements
// with the most total time from pg_stat_statements, when the extension is
// installed (in whichever schema) and loaded; other databases' and roles'
// statements are never read. Only statements SafeStatement passes are
// shown, the first statementsShown of them. Reading it is never the
// page's failure: any error but ctx's own (its budget, a client gone)
// answers Available false with the reason, the error logged.
func (s *Store) SummarizeStatements(ctx context.Context) (StatementStats, error) {
	out := StatementStats{Statements: []Statement{}}
	var schema string
	err := s.DB.NewSelect().TableExpr("pg_extension AS x").
		Join("JOIN pg_namespace AS n ON n.oid = x.extnamespace").
		ColumnExpr("n.nspname").
		Where("x.extname = 'pg_stat_statements'").
		Scan(ctx, &schema)
	if errors.Is(err, sql.ErrNoRows) {
		out.Unavailable = StatementsNotInstalled
		return out, nil
	}
	if err != nil {
		return statementsFailed(ctx, out, err)
	}
	var rows []Statement
	err = s.DB.NewSelect().TableExpr("?.pg_stat_statements AS st", bun.Ident(schema)).
		ColumnExpr("st.query, st.calls, st.total_exec_time AS total_ms, st.mean_exec_time AS mean_ms, st.rows").
		Where("st.dbid = (SELECT oid FROM pg_database WHERE datname = current_database())").
		Where("st.userid = (SELECT oid FROM pg_roles WHERE rolname = current_user)").
		OrderExpr("st.total_exec_time DESC").
		Limit(statementsRead).
		Scan(ctx, &rows)
	if notLoaded(err) {
		out.Unavailable = StatementsNotLoaded
		return out, nil
	}
	if err != nil {
		return statementsFailed(ctx, out, err)
	}
	out.Available = true
	for _, r := range rows {
		query, ok := SafeStatement(r.Query)
		if !ok || len(out.Statements) == statementsShown {
			out.Hidden++
			continue
		}
		r.Query = query
		out.Statements = append(out.Statements, r)
	}
	return out, nil
}

// statementsFailed answers a failed read of pg_stat_statements: ctx's
// own end is returned (the part's budget, or the request gone), anything
// else logged and answered as unreadable.
func statementsFailed(ctx context.Context, out StatementStats, err error) (StatementStats, error) {
	if ctx.Err() != nil {
		return out, err
	}
	code := ""
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		code = pgErr.Code
	}
	slog.WarnContext(ctx, "reading pg_stat_statements failed; the Performance page shows it as unavailable", "sqlstate", code, observe.ErrorAttr(err))
	out.Unavailable = StatementsUnreadable
	return out, nil
}

// notLoaded reports whether pg_stat_statements is installed but not
// loaded (shared_preload_libraries): SQLSTATE 55000.
func notLoaded(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "55000"
}

// statementStarts are the statements PostgreSQL normalises, constants and
// all: utility statements (SET, CREATE, COPY …) keep their text as run,
// values included, so they're never shown.
var statementStarts = []string{"select", "insert", "update", "delete", "with"}

// SafeStatement is a normalised statement as it may be shown, or false
// when it might hold a value: only a query or a data change (never a
// utility statement), with no string left in it (a quote, a dollar quote:
// a constant PostgreSQL didn't replace with $n) and no comment (kept as
// first seen), valid UTF-8, on one line, at most MaxStatement characters.
func SafeStatement(query string) (string, bool) {
	q := strings.Join(strings.Fields(query), " ")
	if !utf8.ValidString(q) || strings.ContainsAny(q, "'`\\\x00") || strings.Contains(q, "--") || strings.Contains(q, "/*") || !onlyPlaceholders(q) {
		return "", false
	}
	first, _, _ := strings.Cut(strings.ToLower(q), " ")
	if !slices.Contains(statementStarts, strings.TrimLeft(first, "(")) {
		return "", false
	}
	if utf8.RuneCountInString(q) > MaxStatement {
		q = string([]rune(q)[:MaxStatement]) + "…"
	}
	return q, true
}

// onlyPlaceholders reports whether every $ in q starts a placeholder ($1),
// never a dollar quote ($$, $tag$).
func onlyPlaceholders(q string) bool {
	for i := 0; i < len(q); i++ {
		if q[i] == '$' && (i+1 == len(q) || q[i+1] < '0' || q[i+1] > '9') {
			return false
		}
	}
	return true
}
