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

// SummarizeMetrics runs the rollup, if no other server is running it: in
// one transaction holding the rollup lock (database.TryLockMetricsRollup),
// it sums each finished hour not summed yet, from the earliest minute after
// the last hour summed, up to RollupDelay ago (at most maxRollupHours a
// run), then deletes minutes older than MinuteRetention, but never a minute
// it hasn't summed. An hour is written once: a second run finds it summed,
// and the tables' unique keys refuse it anyway. A minute that arrives after
// its hour was summed (later than RollupDelay) is left out of the hour.
//
// If summing fails, the run is rolled back and, in a transaction of its
// own under the same lock, minutes older than MinuteCeiling are deleted
// anyway; the error is still returned.
func (s *Store) SummarizeMetrics(ctx context.Context, now time.Time) (MetricsRollup, error) {
	out, err := s.summarizeMetrics(ctx, now)
	if err == nil || ctx.Err() != nil {
		return out, err
	}
	deleted, ceilingErr := s.deleteMinutesPastCeiling(ctx, now)
	out.MinutesDeleted = deleted
	return out, errors.Join(err, ceilingErr)
}

func (s *Store) summarizeMetrics(ctx context.Context, now time.Time) (MetricsRollup, error) {
	var out MetricsRollup
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		locked, err := database.TryLockMetricsRollup(ctx, tx)
		if err != nil || !locked {
			return err
		}
		out = MetricsRollup{Locked: true}
		cutoff := now.UTC().Add(-RollupDelay).Truncate(time.Hour)
		requests, err := summarizeRequests(ctx, tx, cutoff, &out)
		if err != nil {
			return err
		}
		metrics, err := summarizeSeries(ctx, tx, cutoff, &out)
		if err != nil {
			return err
		}
		retention := now.Add(-MinuteRetention)
		for _, table := range []struct {
			model    any
			summed   time.Time
			keyNames string
		}{
			{(*requestMinute)(nil), requests, requestMinuteKey},
			{(*metricMinute)(nil), metrics, metricMinuteKey},
		} {
			before := retention
			if table.summed.Before(before) {
				before = table.summed
			}
			n, err := deleteMinutes(ctx, tx, table.model, table.keyNames, before)
			out.MinutesDeleted += n
			if err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

// deleteMinutesPastCeiling deletes minutes older than MinuteCeiling from
// both tables, summed or not, holding the rollup's lock: the fallback when
// a run can't sum.
func (s *Store) deleteMinutesPastCeiling(ctx context.Context, now time.Time) (int, error) {
	total := 0
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		locked, err := database.TryLockMetricsRollup(ctx, tx)
		if err != nil || !locked {
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

// The minute tables' primary keys, for deleting rows in batches.
const (
	requestMinuteKey = "minute, route, server"
	metricMinuteKey  = "minute, metric, labels, server"
)

// pendingHours are the hours to sum in a minute table: from the hour of
// its earliest minute after the last hour summed, to cutoff, at most
// maxRollupHours. summed is how far the table is summed once they are.
func pendingHours(ctx context.Context, tx bun.Tx, minutes, hours any, cutoff time.Time) (pending []time.Time, summed time.Time, err error) {
	var last, first sql.NullTime
	if err := tx.NewSelect().Model(hours).ColumnExpr("max(hour)").Scan(ctx, &last); err != nil {
		return nil, cutoff, err
	}
	q := tx.NewSelect().Model(minutes).ColumnExpr("min(minute)").Where("minute < ?", cutoff)
	if last.Valid {
		q = q.Where("minute >= ?", last.Time.Add(time.Hour))
	}
	if err := q.Scan(ctx, &first); err != nil || !first.Valid {
		return nil, cutoff, err
	}
	for h := first.Time.UTC().Truncate(time.Hour); h.Before(cutoff); h = h.Add(time.Hour) {
		if len(pending) == maxRollupHours {
			return pending, h, nil
		}
		pending = append(pending, h)
	}
	return pending, cutoff, nil
}

// summarizeRequests sums request_minutes into request_hours, an hour at a
// time, and returns how far it's summed.
func summarizeRequests(ctx context.Context, tx bun.Tx, cutoff time.Time, out *MetricsRollup) (time.Time, error) {
	pending, summed, err := pendingHours(ctx, tx, (*requestMinute)(nil), (*requestHour)(nil), cutoff)
	if err != nil {
		return time.Time{}, err
	}
	for _, hour := range pending {
		var minutes []requestMinute
		if err := tx.NewSelect().Model(&minutes).
			Where("minute >= ? AND minute < ?", hour, hour.Add(time.Hour)).
			Scan(ctx); err != nil {
			return time.Time{}, err
		}
		rows := sumRequestMinutes(hour, minutes)
		if len(rows) > 0 {
			if _, err := tx.NewInsert().Model(&rows).On("CONFLICT (hour, route) DO NOTHING").Exec(ctx); err != nil {
				return time.Time{}, err
			}
		}
		out.Hours++
		out.RequestRows += len(rows)
	}
	return summed, nil
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

// summarizeSeries sums metric_minutes into metric_hours, an hour at a
// time, and returns how far it's summed.
func summarizeSeries(ctx context.Context, tx bun.Tx, cutoff time.Time, out *MetricsRollup) (time.Time, error) {
	pending, summed, err := pendingHours(ctx, tx, (*metricMinute)(nil), (*metricHour)(nil), cutoff)
	if err != nil {
		return time.Time{}, err
	}
	for _, hour := range pending {
		var minutes []metricMinute
		if err := tx.NewSelect().Model(&minutes).
			Where("minute >= ? AND minute < ?", hour, hour.Add(time.Hour)).
			Scan(ctx); err != nil {
			return time.Time{}, err
		}
		rows := sumMetricMinutes(hour, minutes)
		if len(rows) > 0 {
			if _, err := tx.NewInsert().Model(&rows).On("CONFLICT (hour, metric, labels) DO NOTHING").Exec(ctx); err != nil {
				return time.Time{}, err
			}
		}
		out.MetricRows += len(rows)
	}
	return summed, nil
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
