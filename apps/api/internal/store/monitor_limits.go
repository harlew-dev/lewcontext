package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// The Rate limits page's queries (docs/api/monitoring.md#rate-limits):
// windows reached (rate_limit.reached, one per limit per window, by name),
// who reached them, the subjects at a limit right now (rate_limits), and
// refusals per route (request_minutes).

// reachedLimit is a rate_limit.reached event's limit without the level
// reached: auth:per-ip for auth:per-ip:block.
const reachedLimit = "split_part(e.payload->>'limit', ':', 1) || ':' || split_part(e.payload->>'limit', ':', 2)"

// blockReached keeps a limit's block level only: a limit "reached" here is
// one refusing requests. A challenge level crossed (auth:per-ip:challenge)
// only asks for a CAPTCHA, and register:global's at 200 an hour is an
// ordinary busy hour, so it isn't counted as reached; the 428s are counted
// in rate_limit_refused_total as <limit>:challenge.
const blockReached = "split_part(e.payload->>'limit', ':', 3) = 'block'"

// blockReachedLimit is reachedLimit for a block level, or ” (in no
// series) for a challenge level.
const blockReachedLimit = "CASE WHEN " + blockReached + " THEN " + reachedLimit + " ELSE '' END"

// maxLimitSeries is how many limits a chart stacks; the rest are Other.
const maxLimitSeries = 5

// SummarizeLimitsReached counts the windows reached (block levels only,
// blockReached) per bucket of w, by
// limit: the four limits reached most, then SeriesOther; with limits, a
// series for each of those, in that order, and no others.
func (s *Store) SummarizeLimitsReached(ctx context.Context, w SummaryWindow, limits ...string) (MonitorSeries, error) {
	if len(limits) > 0 {
		return s.bucketedCounts(ctx, w, []string{"rate_limit.reached"}, limits, blockReachedLimit)
	}
	var rows []bucketCount
	err := s.summaryBase(w).
		ColumnExpr(reachedLimit+" AS key").
		ColumnExpr("count(*) AS count").
		Where("e.name = 'rate_limit.reached'").
		Where(blockReached).
		GroupExpr("1, 2").
		Scan(ctx, &rows)
	if err != nil {
		return MonitorSeries{}, err
	}
	return foldTop(w, rows, maxLimitSeries), nil
}

// LimitReached is how many windows of a limit were reached in a window,
// and the last time.
type LimitReached struct {
	Limit       string    `json:"limit" bun:"limit_name"`
	Windows     int64     `json:"windows" bun:"windows"`
	LastReached time.Time `json:"lastReached" bun:"last_reached"`
}

// IndexLimitsReached lists every limit whose block level was reached in
// [from, to), most
// windows first; with limits, only those (by name).
func (s *Store) IndexLimitsReached(ctx context.Context, from, to time.Time, limits ...string) ([]LimitReached, error) {
	rows := []LimitReached{}
	q := s.eventsIn(from, to, "rate_limit.reached").
		ColumnExpr(reachedLimit + " AS limit_name").
		ColumnExpr("count(*) AS windows").
		ColumnExpr("max(e.created_at) AS last_reached").
		Where(blockReached).
		GroupExpr("1").
		OrderExpr("windows DESC, 1")
	if len(limits) > 0 {
		q = q.Where(reachedLimit+" IN (?)", bun.In(limits))
	}
	err := q.Scan(ctx, &rows)
	return rows, err
}

// CountSubjectsAt counts, per limit counted in rate_limits, the subjects
// (accounts, addresses, emails: keys are digests, so how many, never who)
// whose live window is at max or over. Shared limits (a bare-name key)
// aren't subjects and aren't counted.
func (s *Store) CountSubjectsAt(ctx context.Context, max map[string]int) (map[string]int64, error) {
	out := map[string]int64{}
	if len(max) == 0 {
		return out, nil
	}
	lowest := 0
	for _, m := range max {
		if lowest == 0 || m < lowest {
			lowest = m
		}
	}
	var rows []struct {
		Name     string `bun:"name"`
		Hits     int    `bun:"hits"`
		Subjects int64  `bun:"subjects"`
	}
	err := s.DB.NewSelect().Model((*rateLimitRecord)(nil)).
		ColumnExpr("split_part(l.key, ':', 1) || ':' || split_part(l.key, ':', 2) AS name").
		ColumnExpr("l.hits").
		ColumnExpr("count(*) AS subjects").
		Where("split_part(l.key, ':', 3) <> ''").
		Where("l.resets_at > now()").
		Where("l.hits >= ?", lowest).
		GroupExpr("1, 2").
		Scan(ctx, &rows)
	for _, r := range rows {
		if m, ok := max[r.Name]; ok && r.Hits >= m {
			out[r.Name] += r.Subjects
		}
	}
	return out, err
}

// maxTopSubjects is how many accounts or addresses a top list holds.
const maxTopSubjects = 10

// AccountWindows is how many windows an account reached, of one limit or
// of any.
type AccountWindows struct {
	Account AccountRef `json:"account" bun:"-"`
	UserID  int64      `json:"-" bun:"user_id"`
	Limit   string     `json:"limit,omitempty" bun:"limit_name"`
	Windows int64      `json:"windows" bun:"windows"`
}

// IndexTopAccountsReaching lists the accounts that reached the most
// windows (block levels) in [from, to); with limits, of those limits only, a row per
// account and limit.
func (s *Store) IndexTopAccountsReaching(ctx context.Context, from, to time.Time, limits ...string) ([]AccountWindows, error) {
	rows := []AccountWindows{}
	q := s.eventsIn(from, to, "rate_limit.reached").
		ColumnExpr("e.user_id").
		ColumnExpr("count(*) AS windows").
		Where("e.user_id IS NOT NULL").
		Where(blockReached).
		OrderExpr("windows DESC, e.user_id").
		Limit(maxTopSubjects)
	if len(limits) > 0 {
		q = q.ColumnExpr(reachedLimit+" AS limit_name").Where(reachedLimit+" IN (?)", bun.In(limits)).GroupExpr("1, 3")
	} else {
		q = q.GroupExpr("1")
	}
	if err := q.Scan(ctx, &rows); err != nil {
		return nil, err
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.UserID
	}
	refs, err := s.accountRefs(ctx, ids)
	for i := range rows {
		rows[i].Account = refs[rows[i].UserID]
	}
	return rows, err
}

// IndexTopAddressesReaching lists the addresses (an IPv6 /64) that reached
// the most windows in [from, to), block levels only.
func (s *Store) IndexTopAddressesReaching(ctx context.Context, from, to time.Time) ([]AddressCount, error) {
	return s.indexTopAddresses(ctx, s.eventsIn(from, to, "rate_limit.reached").Where(blockReached))
}

// RouteRefusals is how many requests to a route were refused with 429 in
// a window, of how many.
type RouteRefusals struct {
	Route     string `json:"route" bun:"route"`
	Refused   int64  `json:"refused" bun:"refused"`
	Requests  int64  `json:"requests" bun:"requests"`
	ServerErr int64  `json:"serverErrors" bun:"server_errors"`
}

// IndexRouteRefusals lists the routes with 429s in [from, to), most
// first, from every server's minutes (and hours, for a long window).
func (s *Store) IndexRouteRefusals(ctx context.Context, from, to time.Time) ([]RouteRefusals, error) {
	requests, err := s.requestRows(ctx, from, to)
	if err != nil {
		return nil, err
	}
	source := requests("m.route, m.requests, m.status_429, m.status_5xx")
	rows := []RouteRefusals{}
	err = s.DB.NewSelect().TableExpr("(?) AS x", source).
		ColumnExpr("x.route").
		ColumnExpr("sum(x.status_429)::bigint AS refused").
		ColumnExpr("sum(x.requests)::bigint AS requests").
		ColumnExpr("sum(x.status_5xx)::bigint AS server_errors").
		GroupExpr("x.route").
		Having("sum(x.status_429) > 0").
		OrderExpr("refused DESC, x.route").
		Limit(maxTopSubjects).
		Scan(ctx, &rows)
	return rows, err
}
