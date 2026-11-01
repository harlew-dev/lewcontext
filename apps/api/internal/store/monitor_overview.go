package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// The Overview's own queries (docs/api/monitoring.md#overview): what its
// tiles read besides the other pages' signals: live servers, requests and
// their latency (request_minutes, request_hours), the mail queue, grouped
// errors and active accounts.

// ServerLiveFor is how recently a server must have been heard from (its
// minute's heartbeat) to count as live.
const ServerLiveFor = 2 * time.Minute

// LiveServers are the servers heard from in the last ServerLiveFor, and
// their versions, most servers first.
type LiveServers struct {
	Count    int64           `json:"count"`
	Versions []VersionServer `json:"versions"`
}

// VersionServer is how many live servers run a version.
type VersionServer struct {
	Version string `json:"version" bun:"version"`
	Servers int64  `json:"servers" bun:"servers"`
}

// FindLiveServers counts the live servers by version
// (server_instances_last_seen_idx).
func (s *Store) FindLiveServers(ctx context.Context) (LiveServers, error) {
	out := LiveServers{Versions: []VersionServer{}}
	err := s.DB.NewSelect().Model((*ServerInstance)(nil)).
		ColumnExpr("si.version, count(*) AS servers").
		Where("si.last_seen_at > now() - ?::interval", fmt.Sprintf("%d seconds", int(ServerLiveFor.Seconds()))).
		GroupExpr("si.version").
		OrderExpr("servers DESC, si.version").
		Scan(ctx, &out.Versions)
	for _, v := range out.Versions {
		out.Count += v.Servers
	}
	return out, err
}

// RequestTotals are every server's requests in a window: how many, the
// server errors (5xx) among them, and their durations per histogram bound
// (observe.DurationBounds and one above).
type RequestTotals struct {
	Requests     int64   `json:"requests"`
	ServerErrors int64   `json:"serverErrors"`
	Durations    []int64 `json:"-"`
}

// RequestBucket is RequestTotals for one bucket of a window.
type RequestBucket struct {
	Start time.Time `json:"start"`
	RequestTotals
}

// durationSlots is how many counts a duration histogram has.
const durationSlots = 12

// SummarizeRequests totals every server's requests in w per bucket (one
// bucket of the whole window when w.Bucket is zero), with their duration
// histograms, from the minutes (and the hours, for a window longer than
// the minutes are kept). Every bucket is listed, empty ones as zeros.
func (s *Store) SummarizeRequests(ctx context.Context, w SummaryWindow) ([]RequestBucket, error) {
	if w.Bucket == 0 {
		w.Bucket = w.To.Sub(w.From)
	}
	starts := BucketStarts(SummaryWindow{From: w.From, To: w.To, Bucket: w.Bucket})
	if w.Bucket == w.To.Sub(w.From) {
		starts = []time.Time{w.From}
	}
	out := make([]RequestBucket, len(starts))
	for i, start := range starts {
		out[i] = RequestBucket{Start: start, RequestTotals: RequestTotals{Durations: make([]int64, durationSlots)}}
	}
	slot := func(t time.Time) int {
		if len(starts) == 1 {
			return 0
		}
		return int(t.Sub(starts[0]) / w.Bucket)
	}
	bin := "date_bin(?::interval, x.t, ?)"
	interval := fmt.Sprintf("%d seconds", int(w.Bucket.Seconds()))
	origin := starts[0]
	rows, err := s.requestRows(ctx, w.From, w.To)
	if err != nil {
		return nil, err
	}
	source := rows("m.requests, m.status_5xx")
	var totals []struct {
		Bucket       time.Time `bun:"bucket"`
		Requests     int64     `bun:"requests"`
		ServerErrors int64     `bun:"server_errors"`
	}
	if err := s.DB.NewSelect().TableExpr("(?) AS x", source).
		ColumnExpr(bin+" AS bucket", interval, origin).
		ColumnExpr("sum(x.requests)::bigint AS requests").
		ColumnExpr("sum(x.status_5xx)::bigint AS server_errors").
		GroupExpr("1").Scan(ctx, &totals); err != nil {
		return nil, err
	}
	for _, t := range totals {
		if i := slot(t.Bucket); i >= 0 && i < len(out) {
			out[i].Requests += t.Requests
			out[i].ServerErrors += t.ServerErrors
		}
	}
	// The histograms, summed slot by slot.
	source = rows("m.duration_buckets")
	var slots []struct {
		Bucket time.Time `bun:"bucket"`
		Slot   int       `bun:"slot"`
		Count  int64     `bun:"count"`
	}
	if err := s.DB.NewSelect().TableExpr("(?) AS x", source).
		TableExpr("unnest(x.duration_buckets) WITH ORDINALITY AS d(n, slot)").
		ColumnExpr(bin+" AS bucket", interval, origin).
		ColumnExpr("d.slot::int AS slot").
		ColumnExpr("sum(d.n)::bigint AS count").
		GroupExpr("1, 2").Scan(ctx, &slots); err != nil {
		return nil, err
	}
	for _, x := range slots {
		if i := slot(x.Bucket); i >= 0 && i < len(out) && x.Slot >= 1 && x.Slot <= durationSlots {
			out[i].Durations[x.Slot-1] += x.Count
		}
	}
	return out, nil
}

// MailQueue is how the mail queue stands: the oldest delivery waiting (its
// event's time), and the deliveries that failed for good since a time.
type MailQueue struct {
	OldestPendingAt *time.Time `json:"oldestPendingAt"`
	Failed          int64      `json:"failed"`
}

// FindMailQueue reads the oldest pending delivery's event
// (event_deliveries_pending_idx) and counts deliveries failed since
// (event_deliveries_failed_idx).
func (s *Store) FindMailQueue(ctx context.Context, failedSince time.Time) (MailQueue, error) {
	var out MailQueue
	var oldest sql.NullTime
	if err := s.DB.NewSelect().Model((*deliveryRecord)(nil)).
		Join("JOIN events AS e ON e.id = d.event_id").
		ColumnExpr("min(e.created_at)").
		Where("d.delivered_at IS NULL AND d.failed_at IS NULL").
		Scan(ctx, &oldest); err != nil {
		return out, err
	}
	if oldest.Valid {
		out.OldestPendingAt = &oldest.Time
	}
	n, err := s.DB.NewSelect().Model((*deliveryRecord)(nil)).Where("d.failed_at >= ?", failedSince).Count(ctx)
	out.Failed = int64(n)
	return out, err
}

// ErrorGroups counts grouped errors (app_errors): open ones, and those
// first seen since a time, panics apart.
type ErrorGroups struct {
	Open      int64 `json:"open"`
	New       int64 `json:"new"`
	NewPanics int64 `json:"newPanics"`
	// NewFrequent are the new groups the server reported (not a
	// browser's) that happened FrequentErrors times or more.
	NewFrequent int64 `json:"newFrequent"`
}

// FrequentErrors is how many times a new group must have happened to be
// one of ErrorGroups.NewFrequent.
const FrequentErrors = 10

// CountErrorGroups counts the open error groups, and those first seen
// since since. The table holds at most a month of groups (make prune).
func (s *Store) CountErrorGroups(ctx context.Context, since time.Time) (ErrorGroups, error) {
	var out ErrorGroups
	err := s.DB.NewSelect().Model((*appErrorRecord)(nil)).
		ColumnExpr("count(*) FILTER (WHERE resolved_at IS NULL)").
		ColumnExpr("count(*) FILTER (WHERE first_seen_at >= ?)", since).
		ColumnExpr("count(*) FILTER (WHERE first_seen_at >= ? AND source = 'panic')", since).
		ColumnExpr("count(*) FILTER (WHERE first_seen_at >= ? AND source <> 'client' AND count >= ?)", since, FrequentErrors).
		Scan(ctx, &out.Open, &out.New, &out.NewPanics, &out.NewFrequent)
	return out, err
}

// CountActiveAccounts counts the accounts seen (a signed-in request) since
// since, from users.last_seen_at (users_last_seen_idx), which is moved at
// most hourly.
func (s *Store) CountActiveAccounts(ctx context.Context, since time.Time) (int64, error) {
	n, err := s.DB.NewSelect().Model((*User)(nil)).Where("u.last_seen_at >= ?", since).Count(ctx)
	return int64(n), err
}
