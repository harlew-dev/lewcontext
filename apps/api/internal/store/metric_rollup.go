package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/database"
	"time"
)

// The hourly rollup (docs/api/observability.md#metrics-in-the-database):
// one server at a time, holding an advisory lock, sums each finished hour of
// minutes from every server into request_hours and metric_hours, then
// deletes minutes older than MinuteRetention. So the minute tables stay
// bounded even if nobody runs make prune.

// Rollup timings.
const (
	// MinuteRetention is how long minute rows are kept.
	MinuteRetention = 48 * time.Hour
	// MinuteCeiling is the oldest a minute row can get, summed or not: if
	// the hours can't be summed (a run keeps failing), minutes past it are
	// deleted anyway, so the minute tables stay bounded whatever happens.
	MinuteCeiling = 7 * 24 * time.Hour
	// RollupDelay is how long after an hour ends it's summed: every
	// server's last minute of it is written by then (a minute's write
	// takes MinuteFlushTimeout at most).
	RollupDelay = 5 * time.Minute
	// maxRollupHours is the most hours one run sums, so a run after a long
	// pause stays short; the next run carries on.
	maxRollupHours = 72
	// minuteDeleteBatch and maxMinuteDeleteBatches bound one run's
	// deletes: 500,000 rows a table at most, the rest next time.
	minuteDeleteBatch      = 10000
	maxMinuteDeleteBatches = 50
)

// MetricsRollup is what one rollup run did.
type MetricsRollup struct {
	// Locked is false when another server held the lock: this run did
	// nothing.
	Locked bool
	// Hours are the hours summed; RequestRows and MetricRows the hour rows
	// written.
	Hours, RequestRows, MetricRows int
	// MinutesDeleted counts the minute rows deleted, both tables.
	MinutesDeleted int
}

// ceilingTimeout bounds the fallback delete of minutes past MinuteCeiling,
// which runs on a context of its own once a run's summing failed (its own
// context may be the one that ran out).
const ceilingTimeout = 30 * time.Second

// afterRollupHour runs after each hour's transaction commits; tests use it
// to end a run part way.
var afterRollupHour func()

// SummarizeMetrics runs the rollup, if no other server is running it. Each
// finished hour not summed yet (from the earliest minute after the last
// hour summed, up to RollupDelay before the database's clock) is summed in
// a short transaction of its own, which takes the rollup lock
// (database.TryLockMetricsRollup) again, so a run that runs out of time
// keeps the hours it finished and the next run carries on; at most
// maxRollupHours a run. Then, in one more transaction under the lock,
// minutes older than MinuteRetention are deleted, but never a minute not
// summed yet. An hour is written once: a second run finds it summed, and
// the tables' unique keys refuse it anyway. A minute that arrives after
// its hour was summed (later than RollupDelay) is left out of the hour.
// Times come from the database's clock, never this server's, so a server
// whose clock runs ahead can't sum hours that haven't happened.
//
// If summing or deleting fails, including on the run's own deadline,
// minutes older than MinuteCeiling are deleted anyway, summed or not, on a
// context of its own (ceilingTimeout) and under the same lock; the error
// is still returned. Not when ctx was canceled: the server is stopping.
func (s *Store) SummarizeMetrics(ctx context.Context) (MetricsRollup, error) {
	out, err := s.summarizeHours(ctx)
	if err == nil && out.Locked {
		err = s.deleteSummedMinutes(ctx, &out)
	}
	if err == nil || errors.Is(ctx.Err(), context.Canceled) {
		return out, err
	}
	ceiling, cancel := context.WithTimeout(context.WithoutCancel(ctx), ceilingTimeout)
	defer cancel()
	deleted, ceilingErr := s.deleteMinutesPastCeiling(ceiling)
	out.MinutesDeleted += deleted
	return out, errors.Join(err, ceilingErr)
}

// summarizeHours sums pending hours, each in a transaction of its own
// holding the lock, until none is left, the lock is another server's, or
// maxRollupHours are done.
func (s *Store) summarizeHours(ctx context.Context) (MetricsRollup, error) {
	var out MetricsRollup
	for range maxRollupHours {
		more := false
		err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			locked, err := database.TryLockMetricsRollup(ctx, tx)
			if err != nil || !locked {
				return err
			}
			out.Locked = true
			now, err := databaseNow(ctx, tx)
			if err != nil {
				return err
			}
			cutoff := now.Add(-RollupDelay).Truncate(time.Hour)
			requests, err := summarizeRequestHour(ctx, tx, cutoff, &out)
			if err != nil {
				return err
			}
			metrics, err := summarizeSeriesHour(ctx, tx, cutoff, &out)
			more = requests || metrics
			return err
		})
		if err != nil {
			return out, err
		}
		if !more || !out.Locked {
			return out, nil
		}
		if afterRollupHour != nil {
			afterRollupHour()
		}
	}
	return out, nil
}

// deleteSummedMinutes deletes minutes older than MinuteRetention from both
// tables, but never one not summed yet, holding the lock.
func (s *Store) deleteSummedMinutes(ctx context.Context, out *MetricsRollup) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		locked, err := database.TryLockMetricsRollup(ctx, tx)
		if err != nil || !locked {
			return err
		}
		now, err := databaseNow(ctx, tx)
		if err != nil {
			return err
		}
		cutoff := now.Add(-RollupDelay).Truncate(time.Hour)
		for _, table := range []struct {
			minutes, hours any
			keyNames       string
		}{
			{(*requestMinute)(nil), (*requestHour)(nil), requestMinuteKey},
			{(*metricMinute)(nil), (*metricHour)(nil), metricMinuteKey},
		} {
			before := now.Add(-MinuteRetention)
			next, pending, err := nextHour(ctx, tx, table.minutes, table.hours, cutoff)
			if err != nil {
				return err
			}
			if pending && next.Before(before) {
				before = next
			}
			n, err := deleteMinutes(ctx, tx, table.minutes, table.keyNames, before)
			out.MinutesDeleted += n
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// deleteMinutesPastCeiling deletes minutes older than MinuteCeiling (by
// the database's clock) from both tables, summed or not, holding the
// rollup's lock: the fallback when a run can't sum.
func (s *Store) deleteMinutesPastCeiling(ctx context.Context) (int, error) {
	total := 0
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		locked, err := database.TryLockMetricsRollup(ctx, tx)
		if err != nil || !locked {
			return err
		}
		now, err := databaseNow(ctx, tx)
		if err != nil {
			return err
		}
		before := now.Add(-MinuteCeiling)
		for _, t := range []struct {
			model    any
			keyNames string
		}{
			{(*requestMinute)(nil), requestMinuteKey},
			{(*metricMinute)(nil), metricMinuteKey},
		} {
			n, err := deleteMinutes(ctx, tx, t.model, t.keyNames, before)
			total += n
			if err != nil {
				return err
			}
		}
		return nil
	})
	return total, err
}

// databaseNow is the database's clock, which every server shares.
func databaseNow(ctx context.Context, db bun.IDB) (time.Time, error) {
	var now time.Time
	err := db.NewSelect().ColumnExpr("now()").Scan(ctx, &now)
	return now.UTC(), err
}

// The minute tables' primary keys, for deleting rows in batches.
const (
	requestMinuteKey = "minute, route, server"
	metricMinuteKey  = "minute, metric, labels, server"
)

// nextHour is the next hour to sum in a minute table: the hour of its
// earliest minute after the last hour summed, if that's before cutoff.
// Minutes dated after cutoff (a server whose clock runs ahead) are never
// summed into an hour.
func nextHour(ctx context.Context, tx bun.Tx, minutes, hours any, cutoff time.Time) (hour time.Time, ok bool, err error) {
	var last, first sql.NullTime
	if err := tx.NewSelect().Model(hours).ColumnExpr("max(hour)").Scan(ctx, &last); err != nil {
		return hour, false, err
	}
	q := tx.NewSelect().Model(minutes).ColumnExpr("min(minute)").Where("minute < ?", cutoff)
	if last.Valid {
		q = q.Where("minute >= ?", last.Time.Add(time.Hour))
	}
	if err := q.Scan(ctx, &first); err != nil || !first.Valid {
		return hour, false, err
	}
	return first.Time.UTC().Truncate(time.Hour), true, nil
}

// summarizeRequestHour sums request_minutes' next hour into request_hours,
// reporting whether there was one.
func summarizeRequestHour(ctx context.Context, tx bun.Tx, cutoff time.Time, out *MetricsRollup) (bool, error) {
	hour, ok, err := nextHour(ctx, tx, (*requestMinute)(nil), (*requestHour)(nil), cutoff)
	if err != nil || !ok {
		return false, err
	}
	var minutes []requestMinute
	if err := tx.NewSelect().Model(&minutes).
		Where("minute >= ? AND minute < ?", hour, hour.Add(time.Hour)).
		Scan(ctx); err != nil {
		return false, err
	}
	rows := sumRequestMinutes(hour, minutes)
	if len(rows) > 0 {
		if _, err := tx.NewInsert().Model(&rows).On("CONFLICT (hour, route) DO NOTHING").Exec(ctx); err != nil {
			return false, err
		}
	}
	out.Hours++
	out.RequestRows += len(rows)
	return true, nil
}

// sumRequestMinutes sums an hour's minutes, every server's, per route.
func sumRequestMinutes(hour time.Time, minutes []requestMinute) []requestHour {
	var rows []requestHour
	index := map[string]int{}
	for _, m := range minutes {
		i, ok := index[m.Route]
		if !ok {
			i = len(rows)
			index[m.Route] = i
			rows = append(rows, requestHour{Hour: hour, Route: m.Route, DurationBuckets: make([]int64, len(m.DurationBuckets))})
		}
		h := &rows[i]
		h.Requests += m.Requests
		h.Status4xx += m.Status4xx
		h.Status429 += m.Status429
		h.Status5xx += m.Status5xx
		h.Timeouts += m.Timeouts
		h.Aborted += m.Aborted
		h.DurationSumMs += m.DurationSumMs
		h.DurationBuckets = addBuckets(h.DurationBuckets, m.DurationBuckets)
	}
	return rows
}

// summarizeSeriesHour sums metric_minutes' next hour into metric_hours,
// reporting whether there was one.
func summarizeSeriesHour(ctx context.Context, tx bun.Tx, cutoff time.Time, out *MetricsRollup) (bool, error) {
	hour, ok, err := nextHour(ctx, tx, (*metricMinute)(nil), (*metricHour)(nil), cutoff)
	if err != nil || !ok {
		return false, err
	}
	var minutes []metricMinute
	if err := tx.NewSelect().Model(&minutes).
		Where("minute >= ? AND minute < ?", hour, hour.Add(time.Hour)).
		Scan(ctx); err != nil {
		return false, err
	}
	rows := sumMetricMinutes(hour, minutes)
	if len(rows) > 0 {
		if _, err := tx.NewInsert().Model(&rows).On("CONFLICT (hour, metric, labels) DO NOTHING").Exec(ctx); err != nil {
			return false, err
		}
	}
	out.MetricRows += len(rows)
	return true, nil
}

// sumMetricMinutes sums an hour's minutes, every server's, per series: a
// counter's or histogram's counts and sums add up; a gauge's count becomes
// its number of minutes and its sum their total, so sum / count is its
// average.
func sumMetricMinutes(hour time.Time, minutes []metricMinute) []metricHour {
	var rows []metricHour
	type key struct{ metric, labels string }
	index := map[key]int{}
	for _, m := range minutes {
		k := key{m.Metric, m.Labels}
		i, ok := index[k]
		if !ok {
			i = len(rows)
			index[k] = i
			rows = append(rows, metricHour{Hour: hour, Metric: m.Metric, Labels: m.Labels})
		}
		h := &rows[i]
		h.Count += m.Count
		h.Sum += m.Sum
		if m.Buckets != nil {
			h.Buckets = addBuckets(h.Buckets, m.Buckets)
		}
	}
	return rows
}

// addBuckets adds b to a, element by element, growing a if b is longer.
func addBuckets(a, b []int64) []int64 {
	for len(a) < len(b) {
		a = append(a, 0)
	}
	for i, n := range b {
		a[i] += n
	}
	return a
}

// deleteMinutes deletes a minute table's rows older than before, in
// batches of minuteDeleteBatch, at most maxMinuteDeleteBatches a run.
func deleteMinutes(ctx context.Context, tx bun.Tx, model any, keyNames string, before time.Time) (int, error) {
	total := 0
	for range maxMinuteDeleteBatches {
		batch := tx.NewSelect().Model(model).
			ColumnExpr(keyNames).
			Where("minute < ?", before).
			OrderExpr("minute").
			Limit(minuteDeleteBatch)
		result, err := tx.NewDelete().Model(model).
			Where("("+keyNames+") IN (?)", batch).
			Exec(ctx)
		if err != nil {
			return total, err
		}
		n, err := result.RowsAffected()
		total += int(n)
		if err != nil || n < minuteDeleteBatch {
			return total, err
		}
	}
	return total, nil
}
