package observe

import (
	"context"
	"log/slog"
	"math"
	"time"
)

// Once a minute each server writes what its registry counted during the
// minute just ended (docs/api/observability.md#metrics-in-the-database):
// a row per route with traffic (request_minutes) and a row per other
// series that changed, plus every gauge (metric_minutes), in one statement,
// with its heartbeat. What it writes depends on how many routes and series
// there are, never on the traffic. Insert only, and never retried: a minute
// whose write fails is dropped, and the totals stay in memory for /metrics.

// Minute is what one server counted during one minute.
type Minute struct {
	// Minute is the minute's start (UTC, truncated to the minute).
	Minute time.Time
	// Routes are the routes with traffic during the minute.
	Routes []RouteTotals
	// Series are the other series that changed, and every gauge.
	Series []SeriesMinute
}

// SeriesMinute is one series' change during a minute.
type SeriesMinute struct {
	Metric string
	// Labels are the series' labels, name="value" pairs sorted by name, or
	// "" (family.labelText).
	Labels string
	// Count is a counter's increase, a histogram's observations, or 1 for
	// a gauge.
	Count int64
	// Sum is a histogram's sum in milliseconds, or a gauge's value.
	Sum float64
	// Buckets are a histogram's counts per bound and one above (not
	// cumulative); nil for counters and gauges.
	Buckets []int64
}

// minuteState is what the last minute's reading saw, to take differences
// from. Only the flush (TakeMinute) touches it.
type minuteState struct {
	routes map[string]RouteTotals
	series map[*series]seriesReading
}

type seriesReading struct {
	count   int64
	sum     int64
	buckets []int64
}

// TakeMinute is what changed since the last call, labelled with minute:
// the routes with traffic, the series that changed, and every gauge. Called
// by one goroutine only (the minute's flush).
func (r *Registry) TakeMinute(minute time.Time) Minute {
	state := &r.minutes
	if state.routes == nil {
		state.routes = map[string]RouteTotals{}
		state.series = map[*series]seriesReading{}
	}
	m := Minute{Minute: minute.UTC().Truncate(time.Minute)}
	for _, s := range r.Requests.sorted() {
		now := s.totals()
		change := now.minus(state.routes[s.route])
		state.routes[s.route] = now
		if change.Requests > 0 {
			m.Routes = append(m.Routes, change)
		}
	}
	for _, f := range r.gather() {
		for _, s := range f.sorted() {
			if row, ok := takeSeries(f, s, state); ok {
				m.Series = append(m.Series, row)
			}
		}
	}
	return m
}

// takeSeries is one series' row for the minute, if it has one: a gauge
// always, a counter or histogram when it changed.
func takeSeries(f *family, s *series, state *minuteState) (SeriesMinute, bool) {
	row := SeriesMinute{Metric: f.name, Labels: f.labelText(s.values)}
	if f.kind == gaugeKind {
		row.Count, row.Sum = 1, math.Float64frombits(s.gauge.Load())
		return row, true
	}
	now := seriesReading{count: s.count.Load(), sum: s.sum.Load()}
	for i := range s.buckets {
		now.buckets = append(now.buckets, s.buckets[i].Load())
	}
	before := state.series[s]
	state.series[s] = now
	row.Count = now.count - before.count
	if row.Count <= 0 {
		return row, false
	}
	if f.kind == histogramKind {
		row.Sum = float64(now.sum-before.sum) / float64(time.Millisecond)
		row.Buckets = make([]int64, len(now.buckets))
		for i := range now.buckets {
			row.Buckets[i] = now.buckets[i]
			if i < len(before.buckets) {
				row.Buckets[i] -= before.buckets[i]
			}
		}
	}
	return row, true
}

// MinuteFlushTimeout bounds one minute's write: past it the minute is
// dropped, so a slow database can't make writes pile up.
const MinuteFlushTimeout = 10 * time.Second

// MinuteFlush writes a minute's totals once a minute (Run), at the start of
// each minute, with the minute just ended.
type MinuteFlush struct {
	Registry *Registry
	// Store writes a minute and the server's heartbeat in one statement
	// (store.RecordMetrics). It is called every minute even when Discard
	// is set, for the heartbeat.
	Store func(ctx context.Context, m Minute) error
	// Before, when set, runs before each minute is taken, to set gauges
	// read from the database (deliveries_pending), within the write's
	// time.
	Before func(ctx context.Context)
	// Discard (METRICS_STORE=off) writes no metrics: Store gets the minute
	// without its routes and series, so only the heartbeat is written.
	Discard bool
	// Now is time.Now unless a test sets it.
	Now func() time.Time
	// last is the last minute written, so no minute is written twice.
	last time.Time
	// after is time.After unless a test sets it.
	after func(time.Duration) <-chan time.Time
}

// Run writes each minute just ended, at the start of the next, until ctx
// is done. A failed write is logged and dropped: nothing is retried, so a
// database outage costs one attempt a minute.
func (f *MinuteFlush) Run(ctx context.Context) {
	for {
		now := f.now()
		next := now.Truncate(time.Minute).Add(time.Minute)
		select {
		case <-ctx.Done():
			return
		case <-f.wait(next.Sub(now)):
		}
		f.write(ctx, next.Add(-time.Minute))
	}
}

// Flush writes what was counted since the last write as the current
// minute: the last one before a shutdown, which would otherwise be lost.
func (f *MinuteFlush) Flush(ctx context.Context) error {
	return f.flush(ctx, f.now().Truncate(time.Minute))
}

func (f *MinuteFlush) write(ctx context.Context, minute time.Time) {
	write, cancel := context.WithTimeout(ctx, MinuteFlushTimeout)
	defer cancel()
	if err := f.flush(write, minute); err != nil && ctx.Err() == nil {
		slog.Warn("storing a minute's metrics failed; that minute is dropped", ErrorAttr(err))
	}
}

// flush takes the minute's totals (so a failed write drops them) and
// stores them. A minute already written is moved on to the next, so the
// process never writes one twice (its primary key would refuse it).
func (f *MinuteFlush) flush(ctx context.Context, minute time.Time) error {
	minute = minute.UTC().Truncate(time.Minute)
	if !f.last.IsZero() && !minute.After(f.last) {
		minute = f.last.Add(time.Minute)
	}
	f.last = minute
	if f.Before != nil {
		f.Before(ctx)
	}
	m := f.Registry.TakeMinute(minute)
	if f.Discard {
		m.Routes, m.Series = nil, nil
	}
	return f.Store(ctx, m)
}

func (f *MinuteFlush) wait(d time.Duration) <-chan time.Time {
	if f.after != nil {
		return f.after(d)
	}
	return time.After(d)
}

func (f *MinuteFlush) now() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}
