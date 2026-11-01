package store

import (
	"context"
	"encoding/json"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/observe"
	"regexp"
	"time"
	"unicode/utf8"
)

// Metrics in the database (docs/api/observability.md#metrics-in-the-database):
// each server's minute (request_minutes, metric_minutes) with its heartbeat
// (server_instances) in one statement, and the hourly rollup into
// request_hours and metric_hours under an advisory lock.

// ServerInstance is a running server (server_instances): made at startup,
// its LastSeenAt moved by each minute's heartbeat.
type ServerInstance struct {
	bun.BaseModel `bun:"table:server_instances,alias:si"`
	// ID is srv_ and 14 base62, new at every start (observe.NewServerID).
	ID         string `bun:",pk"`
	Hostname   string
	Version    string
	StartedAt  time.Time
	LastSeenAt time.Time
	// Config is the configuration's summary (config.Summary): never a
	// secret's value or digest.
	Config json.RawMessage `bun:"type:jsonb"`
}

type requestMinute struct {
	bun.BaseModel   `bun:"table:request_minutes,alias:rm"`
	Minute          time.Time
	Server          string
	Route           string
	Requests        int64
	Status4xx       int64 `bun:"status_4xx"`
	Status429       int64 `bun:"status_429"`
	Status5xx       int64 `bun:"status_5xx"`
	Timeouts        int64
	Aborted         int64
	DurationSumMs   float64
	DurationBuckets []int64 `bun:",array"`
}

type requestHour struct {
	bun.BaseModel   `bun:"table:request_hours,alias:rh"`
	ID              int64 `bun:",pk,autoincrement"`
	Hour            time.Time
	Route           string
	Requests        int64
	Status4xx       int64 `bun:"status_4xx"`
	Status429       int64 `bun:"status_429"`
	Status5xx       int64 `bun:"status_5xx"`
	Timeouts        int64
	Aborted         int64
	DurationSumMs   float64
	DurationBuckets []int64 `bun:",array"`
}

type metricMinute struct {
	bun.BaseModel `bun:"table:metric_minutes,alias:mm"`
	Minute        time.Time
	Server        string
	Metric        string
	Labels        string
	Count         int64
	Sum           float64
	Buckets       []int64 `bun:",array"`
}

type metricHour struct {
	bun.BaseModel `bun:"table:metric_hours,alias:mh"`
	ID            int64 `bun:",pk,autoincrement"`
	Hour          time.Time
	Metric        string
	Labels        string
	Count         int64
	Sum           float64
	Buckets       []int64 `bun:",array"`
}

// RecordMetrics writes one server's minute and its heartbeat in one
// statement: a request_minutes row per route with traffic, a
// metric_minutes row per series, and the server's server_instances row
// (inserted if it isn't there, its last_seen_at moved to now if it is).
// Insert only for the minutes: a minute already written is left as it was
// (the flush never writes one twice). A row that breaks the tables' rules
// is left out rather than failing the minute. Nothing here retries.
//
// Times are the database's: the heartbeat is its now(), and a minute more
// than maxMinuteSkew from its clock (a server whose clock is wrong) is
// dropped, heartbeat kept, so it can't land in an hour long summed or one
// that hasn't happened. Checking costs one more query, only for a minute
// with rows.
func (s *Store) RecordMetrics(ctx context.Context, server ServerInstance, m observe.Minute) error {
	q := s.DB.NewInsert().Model(&server).
		Value("last_seen_at", "now()").
		On("CONFLICT (id) DO UPDATE").
		Set("last_seen_at = EXCLUDED.last_seen_at")
	requests, series := requestMinuteRows(server.ID, m), metricMinuteRows(server.ID, m)
	if len(requests) > 0 || len(series) > 0 {
		now, err := databaseNow(ctx, s.DB)
		if err != nil {
			return err
		}
		if !minuteInTime(m.Minute, now) {
			requests, series = nil, nil
		}
	}
	if len(requests) > 0 {
		q = q.With("request_rows", s.DB.NewInsert().Model(&requests).On("CONFLICT DO NOTHING"))
	}
	if len(series) > 0 {
		q = q.With("metric_rows", s.DB.NewInsert().Model(&series).On("CONFLICT DO NOTHING"))
	}
	_, err := q.Exec(ctx)
	return err
}

// maxMinuteSkew is how far a minute's start may be from the database's
// clock, beyond the minute itself: a flush writes the minute just ended
// (or, stopping, the one in progress) within seconds.
const maxMinuteSkew = 5 * time.Minute

// minuteInTime reports whether a minute starting at minute is close
// enough to the database's now to be stored.
func minuteInTime(minute, now time.Time) bool {
	return !minute.Before(now.Add(-maxMinuteSkew-time.Minute)) && !minute.After(now.Add(maxMinuteSkew))
}

// Bounds of the minute tables' columns (021, 023), checked before a row is
// sent.
var metricName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,99}$`)

const (
	maxMetricRoute  = 200
	maxMetricLabels = 1000
)

func requestMinuteRows(server string, m observe.Minute) []requestMinute {
	rows := make([]requestMinute, 0, len(m.Routes))
	for _, r := range m.Routes {
		n := utf8.RuneCountInString(r.Route)
		if r.Requests <= 0 || n == 0 || n > maxMetricRoute || !utf8.ValidString(r.Route) {
			continue
		}
		rows = append(rows, requestMinute{
			Minute: m.Minute, Server: server, Route: r.Route,
			Requests: r.Requests, Status4xx: r.Status4xx, Status429: r.Status429, Status5xx: r.Status5xx,
			Timeouts: r.Timeouts, Aborted: r.Aborted,
			DurationSumMs:   float64(r.DurationSum) / float64(time.Millisecond),
			DurationBuckets: r.Buckets[:],
		})
	}
	return rows
}

func metricMinuteRows(server string, m observe.Minute) []metricMinute {
	rows := make([]metricMinute, 0, len(m.Series))
	for _, x := range m.Series {
		if x.Count <= 0 || !metricName.MatchString(x.Metric) || utf8.RuneCountInString(x.Labels) > maxMetricLabels || !utf8.ValidString(x.Labels) {
			continue
		}
		rows = append(rows, metricMinute{
			Minute: m.Minute, Server: server, Metric: x.Metric, Labels: x.Labels,
			Count: x.Count, Sum: x.Sum, Buckets: x.Buckets,
		})
	}
	return rows
}

// RecordServerStart writes the server's row and records server.started,
// together.
func (s *Store) RecordServerStart(ctx context.Context, server ServerInstance) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(&server).Value("last_seen_at", "now()").On("CONFLICT (id) DO UPDATE").Set("last_seen_at = EXCLUDED.last_seen_at").Exec(ctx); err != nil {
			return err
		}
		return s.record(ctx, tx, events.ServerStarted{Server: server.ID, Version: server.Version}, EventScope{})
	})
}

// RecordServerStop records server.stopped with its reason, and moves the
// server's last_seen_at to now.
func (s *Store) RecordServerStop(ctx context.Context, server ServerInstance, reason string) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewUpdate().Model((*ServerInstance)(nil)).Set("last_seen_at = now()").Where("id = ?", server.ID).Exec(ctx); err != nil {
			return err
		}
		return s.record(ctx, tx, events.ServerStopped{Server: server.ID, Reason: reason}, EventScope{})
	})
}

// DeleteSilentServerInstances deletes the rows of servers not seen since
// before: they stopped (a server silent for 2 minutes is shown as gone).
func (s *Store) DeleteSilentServerInstances(ctx context.Context, before time.Time) (int, error) {
	result, err := s.DB.NewDelete().Model((*ServerInstance)(nil)).Where("last_seen_at < ?", before).Exec(ctx)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	return int(n), err
}

// CountPendingDeliveries counts the event deliveries waiting to run, for
// the deliveries_pending gauge.
func (s *Store) CountPendingDeliveries(ctx context.Context) (int, error) {
	n, err := s.DB.NewSelect().Model((*deliveryRecord)(nil)).
		Where("d.delivered_at IS NULL AND d.failed_at IS NULL").
		Count(ctx)
	return int(n), err
}
