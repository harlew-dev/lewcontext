package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/uptrace/bun"
)

// The admin Monitoring pages' queries (docs/api/monitoring.md): what each
// page and signal reads, always bounded by a window of 7 days at most and
// read through an index, under the page's deadline (internal/monitor sets
// it). Shared helpers here; each page's queries in monitor_<page>.go.

// MonitorSeries is a chart's numbers: a column per bucket of the window,
// its counts lining up with Series.
type MonitorSeries struct {
	Series  []string        `json:"series"`
	Buckets []SummaryBucket `json:"buckets"`
}

// AccountRef names an account in a review list: its public ID and name,
// never its email. ID alone once it's deleted.
type AccountRef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// ipNetwork is the address an event came from as signals count it: an
// IPv4 address, or an IPv6 address's /64 (as per-IP limits count it), so
// one client changing the last bits is still one. The text is what the
// Events page's ip filter takes.
const ipNetwork = "CASE WHEN family(e.ip) = 4 THEN host(e.ip) ELSE text(network(set_masklen(e.ip, 64))) END"

// signInNetwork is an address's network as "a network the account has
// used" means it: an IPv4 /24 or an IPv6 /48, as admin.session.moved does.
const signInNetwork = "network(set_masklen(%[1]s.ip, CASE WHEN family(%[1]s.ip) = 4 THEN 24 ELSE 48 END))"

// Sign-in results (auth.login_attempted's result).
var (
	signInSucceeded = []string{"succeeded", "mfa_required"}
	signInFailed    = []string{"wrong_password", "unknown_account"}
)

// eventsIn selects events named names in [from, to): the name index
// (events_name_created_idx) finds them.
func (s *Store) eventsIn(from, to time.Time, names ...string) *bun.SelectQuery {
	return s.DB.NewSelect().Model((*eventRecord)(nil)).
		Where("e.name IN (?)", bun.In(names)).
		Where("e.created_at >= ?", from).
		Where("e.created_at < ?", to)
}

// countEvents counts what q selects.
func countEvents(ctx context.Context, q *bun.SelectQuery) (int64, error) {
	n, err := q.Count(ctx)
	return int64(n), err
}

// countGroups counts the groups a grouped query returns.
func (s *Store) countGroups(ctx context.Context, grouped *bun.SelectQuery) (int64, error) {
	var n int64
	err := s.DB.NewSelect().TableExpr("(?) AS g", grouped).ColumnExpr("count(*)").Scan(ctx, &n)
	return n, err
}

// bucketedCounts counts events named names in w per bucket, keyed by
// keyExpr (a fixed SQL expression over e, never from input; its args go
// through placeholders), and folds them into a column per bucket with a
// count per key in order. Keys not in order are left out.
func (s *Store) bucketedCounts(ctx context.Context, w SummaryWindow, names []string, order []string, keyExpr string, args ...any) (MonitorSeries, error) {
	var rows []bucketCount
	err := s.summaryBase(w).
		ColumnExpr(keyExpr+" AS key", args...).
		ColumnExpr("count(*) AS count").
		Where("e.name IN (?)", bun.In(names)).
		GroupExpr("1, 2").
		Scan(ctx, &rows)
	if err != nil {
		return MonitorSeries{}, err
	}
	return foldSeries(w, order, rows), nil
}

// foldSeries puts grouped counts in a column per bucket of w, a count per
// series in order; counts of other keys are left out.
func foldSeries(w SummaryWindow, order []string, rows []bucketCount) MonitorSeries {
	out := MonitorSeries{Series: order, Buckets: zeroBuckets(BucketStarts(w), len(order))}
	addCounts(out.Buckets, w.Bucket, rows, func(key string) int { return slices.Index(order, key) })
	return out
}

// topKeys orders keys by their total in rows, largest first (ties by
// name); past max, the largest max-1 and SeriesOther, which then counts
// every key left out.
func topKeys(rows []bucketCount, max int) []string {
	totals := map[string]int{}
	for _, r := range rows {
		totals[r.Key] += r.Count
	}
	keys := make([]string, 0, len(totals))
	for k := range totals {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		if totals[a] != totals[b] {
			return totals[b] - totals[a]
		}
		return strings.Compare(a, b)
	})
	if len(keys) > max {
		return append(keys[:max-1:max-1], SeriesOther)
	}
	return keys
}

// foldTop folds rows into a column per bucket, a series per key of
// topKeys, the rest counted in SeriesOther.
func foldTop(w SummaryWindow, rows []bucketCount, max int) MonitorSeries {
	order := topKeys(rows, max)
	out := MonitorSeries{Series: order, Buckets: zeroBuckets(BucketStarts(w), len(order))}
	other := slices.Index(order, SeriesOther)
	addCounts(out.Buckets, w.Bucket, rows, func(key string) int {
		if i := slices.Index(order, key); i >= 0 {
			return i
		}
		return other
	})
	return out
}

// accountRefs names accounts by internal ID: public ID and name.
func (s *Store) accountRefs(ctx context.Context, ids []int64) (map[int64]AccountRef, error) {
	out := map[int64]AccountRef{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		ID       int64  `bun:"id"`
		PublicID string `bun:"public_id"`
		Name     string `bun:"name"`
	}
	err := s.DB.NewSelect().Model((*User)(nil)).Column("u.id", "u.public_id", "u.name").
		Where("u.id IN (?)", bun.In(ids)).Scan(ctx, &rows)
	for _, r := range rows {
		out[r.ID] = AccountRef{ID: r.PublicID, Name: r.Name}
	}
	return out, err
}

// accountRefsByPublicID names accounts by public ID (an event payload's
// user); one that's gone keeps its ID alone.
func (s *Store) accountRefsByPublicID(ctx context.Context, ids []string) (map[string]AccountRef, error) {
	out := map[string]AccountRef{}
	for _, id := range ids {
		out[id] = AccountRef{ID: id}
	}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		PublicID string `bun:"public_id"`
		Name     string `bun:"name"`
	}
	err := s.DB.NewSelect().Model((*User)(nil)).Column("u.public_id", "u.name").
		Where("u.public_id IN (?)", bun.In(ids)).Scan(ctx, &rows)
	for _, r := range rows {
		out[r.PublicID] = AccountRef{ID: r.PublicID, Name: r.Name}
	}
	return out, err
}

// LimitState is a rate limit's current window in rate_limits: hits so far
// and when the window ends. A limit shared by everyone has one row, keyed
// by its bare name.
type LimitState struct {
	Name     string    `json:"name"`
	Hits     int64     `json:"hits"`
	ResetsAt time.Time `json:"resetsAt"`
}

// FindLimitStates reads the live windows of limits shared by everyone,
// by name (their key is the bare name); a limit with no live window is
// left out.
func (s *Store) FindLimitStates(ctx context.Context, names []string) (map[string]LimitState, error) {
	out := map[string]LimitState{}
	if len(names) == 0 {
		return out, nil
	}
	var rows []rateLimitRecord
	err := s.DB.NewSelect().Model(&rows).
		Where("l.key IN (?)", bun.In(names)).
		Where("l.resets_at > now()").
		Scan(ctx)
	for _, r := range rows {
		out[r.Key] = LimitState{Name: r.Key, Hits: int64(r.Hits), ResetsAt: r.ResetsAt}
	}
	return out, err
}

// requestRows is where request totals come from for [from, to): the
// minutes, which are kept 48 hours; for a longer window, the hours summed
// so far, then the minutes after the last hour summed, so nothing is
// counted twice. The cutoff between them is read once, here: every query
// built from the returned source splits at the same hour.
func (s *Store) requestRows(ctx context.Context, from, to time.Time) (func(columns string) *bun.SelectQuery, error) {
	minutes := func(columns string) *bun.SelectQuery {
		return s.DB.NewSelect().TableExpr("request_minutes AS m").
			ColumnExpr("m.minute AS t, "+columns).
			Where("m.minute >= ?", from).Where("m.minute < ?", to)
	}
	if to.Sub(from) <= MinuteRetention-time.Hour {
		return minutes, nil
	}
	cutoff, err := s.summedUntil(ctx, (*requestHour)(nil))
	if err != nil || cutoff.IsZero() {
		return minutes, err
	}
	return func(columns string) *bun.SelectQuery {
		hours := s.DB.NewSelect().TableExpr("request_hours AS m").
			ColumnExpr("m.hour AS t, "+columns).
			Where("m.hour >= ?", from).Where("m.hour < ?", cutoff)
		return hours.UnionAll(minutes(columns).Where("m.minute >= ?", cutoff))
	}, nil
}

// metricRows is requestRows for a metric's series: minutes, or hours then
// minutes for a window longer than the minutes are kept.
func (s *Store) metricRows(ctx context.Context, metric string, from, to time.Time) (*bun.SelectQuery, error) {
	const columns = "m.labels, m.count, m.sum, m.buckets"
	minutes := s.DB.NewSelect().TableExpr("metric_minutes AS m").
		ColumnExpr("m.minute AS t, "+columns).
		Where("m.minute >= ?", from).Where("m.minute < ?", to).Where("m.metric = ?", metric)
	if to.Sub(from) <= MinuteRetention-time.Hour {
		return minutes, nil
	}
	cutoff, err := s.summedUntil(ctx, (*metricHour)(nil))
	if err != nil || cutoff.IsZero() {
		return minutes, err
	}
	hours := s.DB.NewSelect().TableExpr("metric_hours AS m").
		ColumnExpr("m.hour AS t, "+columns).
		Where("m.hour >= ?", from).Where("m.hour < ?", cutoff).Where("m.metric = ?", metric)
	minutes = minutes.Where("m.minute >= ?", cutoff)
	return hours.UnionAll(minutes), nil
}

// summedUntil is the end of the last hour summed in an hour table, or zero
// when none is.
func (s *Store) summedUntil(ctx context.Context, hours any) (time.Time, error) {
	var last sql.NullTime
	if err := s.DB.NewSelect().Model(hours).ColumnExpr("max(hour)").Scan(ctx, &last); err != nil || !last.Valid {
		return time.Time{}, err
	}
	return last.Time.Add(time.Hour), nil
}

// MetricTotal is a labelled series' total in a window: Label is the value
// of the label asked for.
type MetricTotal struct {
	Label string `json:"label"`
	Count int64  `json:"count"`
}

// SummarizeMetricTotals totals a counter's series in [from, to) by the
// value of one of its labels (label="value"), largest first.
func (s *Store) SummarizeMetricTotals(ctx context.Context, metric, label string, from, to time.Time) ([]MetricTotal, error) {
	rows, err := s.metricRows(ctx, metric, from, to)
	if err != nil {
		return nil, err
	}
	var totals []struct {
		Labels string `bun:"labels"`
		Count  int64  `bun:"count"`
	}
	if err := s.DB.NewSelect().TableExpr("(?) AS x", rows).
		ColumnExpr("x.labels, sum(x.count)::bigint AS count").
		GroupExpr("x.labels").Scan(ctx, &totals); err != nil {
		return nil, err
	}
	byValue := map[string]int64{}
	for _, t := range totals {
		byValue[labelValue(t.Labels, label)] += t.Count
	}
	out := make([]MetricTotal, 0, len(byValue))
	for v, n := range byValue {
		out = append(out, MetricTotal{Label: v, Count: n})
	}
	slices.SortFunc(out, func(a, b MetricTotal) int {
		if a.Count != b.Count {
			return int(b.Count - a.Count)
		}
		return strings.Compare(a.Label, b.Label)
	})
	return out, nil
}

// labelValue is the value of name in a series' labels as stored
// (name="value" pairs, sorted, comma-separated; values from fixed sets, so
// they hold no quote or comma), or "" without it.
func labelValue(labels, name string) string {
	prefix := fmt.Sprintf("%s=\"", name)
	for _, pair := range strings.Split(labels, ",") {
		if v, ok := strings.CutPrefix(pair, prefix); ok {
			return strings.TrimSuffix(v, "\"")
		}
	}
	return ""
}
