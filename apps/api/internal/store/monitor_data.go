package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"slices"
	"strings"
	"time"
)

// The Data page's queries (docs/api/monitoring.md#data): how big the
// database and each table are, what's connected and waiting right now,
// growth from the daily sizes (table_stats_daily), what fills the events
// table, and the last prune run per target. PostgreSQL's statistics views
// are read through the builder like any table. From pg_stat_activity only
// counts and durations are read: never a query's text, a role's name or a
// client's address. Table names from the catalog are only this schema's
// and only those in DataTables.

// FindDatabaseSize is the database's size on disk, in bytes
// (pg_database_size): every schema in it, indexes and TOAST included.
func (s *Store) FindDatabaseSize(ctx context.Context) (int64, error) {
	var n int64
	err := s.DB.NewSelect().ColumnExpr("pg_database_size(current_database())").Scan(ctx, &n)
	return n, err
}

// ConnectionCount is how many of the database's client connections are in
// a state. "hidden" counts another role's connections, whose state
// PostgreSQL shows only to a superuser or pg_read_all_stats.
type ConnectionCount struct {
	State string `json:"state" bun:"state"`
	Count int64  `json:"count" bun:"count"`
}

// Activity is what's connected to the database right now: connections by
// state, the longest-running query and transaction (in seconds; the
// connection reading them left out), connections idle in a transaction,
// and how many are waiting for a lock. Counts and durations only.
type Activity struct {
	MaxConnections            int64             `json:"maxConnections"`
	Connections               []ConnectionCount `json:"connections"`
	LongestQuerySeconds       float64           `json:"longestQuerySeconds"`
	LongestTransactionSeconds float64           `json:"longestTransactionSeconds"`
	IdleInTransaction         int64             `json:"idleInTransaction"`
	LockWaits                 int64             `json:"lockWaits"`
}

// activityRow is one state's connections and their longest durations.
type activityRow struct {
	State       string  `bun:"state"`
	Count       int64   `bun:"count"`
	Query       float64 `bun:"longest_query"`
	Transaction float64 `bun:"longest_transaction"`
}

// FindActivity reads pg_stat_activity and pg_locks for this database's
// client connections (every server's pool, and anyone else connected):
// counts by state, the longest durations, and the connections waiting for
// a lock.
func (s *Store) FindActivity(ctx context.Context) (Activity, error) {
	out := Activity{Connections: []ConnectionCount{}}
	if err := s.DB.NewSelect().ColumnExpr("current_setting('max_connections')::bigint").Scan(ctx, &out.MaxConnections); err != nil {
		return out, err
	}
	var rows []activityRow
	err := s.DB.NewSelect().TableExpr("pg_stat_activity AS a").
		ColumnExpr("coalesce(a.state, 'hidden') AS state").
		ColumnExpr("count(*) AS count").
		ColumnExpr("coalesce(extract(epoch FROM max(now() - a.query_start) FILTER (WHERE a.state = 'active' AND a.pid <> pg_backend_pid())), 0)::float8 AS longest_query").
		ColumnExpr("coalesce(extract(epoch FROM max(now() - a.xact_start) FILTER (WHERE a.pid <> pg_backend_pid())), 0)::float8 AS longest_transaction").
		Where("a.datname = current_database()").
		Where("a.backend_type = 'client backend'").
		GroupExpr("1").
		OrderExpr("2 DESC, 1").
		Scan(ctx, &rows)
	if err != nil {
		return out, err
	}
	for _, r := range rows {
		out.Connections = append(out.Connections, ConnectionCount{State: r.State, Count: r.Count})
		out.LongestQuerySeconds = max(out.LongestQuerySeconds, r.Query)
		out.LongestTransactionSeconds = max(out.LongestTransactionSeconds, r.Transaction)
		if strings.HasPrefix(r.State, "idle in transaction") {
			out.IdleInTransaction += r.Count
		}
	}
	err = s.DB.NewSelect().TableExpr("pg_locks AS l").
		Join("JOIN pg_stat_activity AS a ON a.pid = l.pid").
		ColumnExpr("count(DISTINCT l.pid)").
		Where("NOT l.granted").
		Where("a.datname = current_database()").
		Scan(ctx, &out.LockWaits)
	return out, err
}

// FindCacheHitShare is the share of the database's block reads found in
// PostgreSQL's buffer cache since its statistics were last reset, in
// percent; nil before any read.
func (s *Store) FindCacheHitShare(ctx context.Context) (*float64, error) {
	var share sql.NullFloat64
	err := s.DB.NewSelect().TableExpr("pg_stat_database AS d").
		ColumnExpr("100.0 * d.blks_hit / nullif(d.blks_hit + d.blks_read, 0)").
		Where("d.datname = current_database()").
		Scan(ctx, &share)
	if err != nil || !share.Valid {
		return nil, err
	}
	return &share.Float64, nil
}

// TableStat is a table as PostgreSQL's statistics see it: its sizes, its
// live and dead rows (running counts: close, not exact), when it was last
// vacuumed and analyzed (by hand or by autovacuum), and how it was read
// (sequential scans against index scans since the statistics were reset).
type TableStat struct {
	Name        string     `json:"name" bun:"name"`
	TotalBytes  int64      `json:"totalBytes" bun:"total_bytes"`
	TableBytes  int64      `json:"tableBytes" bun:"table_bytes"`
	IndexBytes  int64      `json:"indexBytes" bun:"index_bytes"`
	LiveRows    int64      `json:"liveRows" bun:"live_rows"`
	DeadRows    int64      `json:"deadRows" bun:"dead_rows"`
	LastVacuum  *time.Time `json:"lastVacuum" bun:"last_vacuum"`
	LastAnalyze *time.Time `json:"lastAnalyze" bun:"last_analyze"`
	SeqScans    int64      `json:"seqScans" bun:"seq_scans"`
	IndexScans  int64      `json:"indexScans" bun:"index_scans"`
}

// IndexTableStats lists this schema's tables that are in DataTables,
// largest first. pg_stat_user_tables lists every schema's tables; anything
// else (another schema's, a table made by hand) is never read.
func (s *Store) IndexTableStats(ctx context.Context) ([]TableStat, error) {
	out := []TableStat{}
	err := s.DB.NewSelect().TableExpr("pg_stat_user_tables AS t").
		ColumnExpr("t.relname AS name").
		ColumnExpr("pg_total_relation_size(t.relid) AS total_bytes").
		ColumnExpr("pg_table_size(t.relid) AS table_bytes").
		ColumnExpr("pg_indexes_size(t.relid) AS index_bytes").
		ColumnExpr("greatest(t.n_live_tup, 0) AS live_rows").
		ColumnExpr("greatest(t.n_dead_tup, 0) AS dead_rows").
		ColumnExpr("greatest(t.last_vacuum, t.last_autovacuum) AS last_vacuum").
		ColumnExpr("greatest(t.last_analyze, t.last_autoanalyze) AS last_analyze").
		ColumnExpr("coalesce(t.seq_scan, 0) AS seq_scans").
		ColumnExpr("coalesce(t.idx_scan, 0) AS index_scans").
		Where("t.schemaname = current_schema()").
		Where("t.relname IN (?)", bun.In(DataTables)).
		OrderExpr("total_bytes DESC, name").
		Scan(ctx, &out)
	return out, err
}

// TableSize is one table's size on one day (table_stats_daily).
type TableSize struct {
	Day        time.Time `bun:"day"`
	Table      string    `bun:"table_name"`
	LiveRows   int64     `bun:"live_rows"`
	TotalBytes int64     `bun:"total_bytes"`
}

// IndexTableSizes lists the daily sizes from the day since on, oldest
// first, of the tables in DataTables: one row per table a day, so 31 days
// are at most 31 rows a table.
func (s *Store) IndexTableSizes(ctx context.Context, since time.Time) ([]TableSize, error) {
	out := []TableSize{}
	err := s.DB.NewSelect().Model((*tableStatRecord)(nil)).
		ColumnExpr("tsd.day, tsd.table_name, tsd.live_rows, tsd.total_bytes").
		Where("tsd.day >= ?::date", since.UTC().Format(time.DateOnly)).
		Where("tsd.table_name IN (?)", bun.In(DataTables)).
		OrderExpr("tsd.day, tsd.table_name").
		Scan(ctx, &out)
	return out, err
}

// maxLogSeries is how many event names the log chart stacks; the rest are
// Other. maxLogNames is how many names the list keeps.
const (
	maxLogSeries = 5
	maxLogNames  = 10
)

// EventLog is what filled the events table in a window: a column per
// bucket by name (the four recorded most, then SeriesOther), the names
// recorded most with their counts, and how many in all.
type EventLog struct {
	Chart MonitorSeries `json:"chart"`
	Names []NameCount   `json:"names"`
	Total int           `json:"total"`
}

// SummarizeEventLog counts the events recorded in w by bucket and name,
// from events_created_idx alone (it holds the name), as the Events page's
// chart does.
func (s *Store) SummarizeEventLog(ctx context.Context, w SummaryWindow) (EventLog, error) {
	var rows []bucketCount
	err := s.summaryBase(w).
		ColumnExpr("e.name AS key").
		ColumnExpr("count(*) AS count").
		GroupExpr("1, 2").
		Scan(ctx, &rows)
	if err != nil {
		return EventLog{}, err
	}
	totals := map[string]int{}
	out := EventLog{Chart: foldTop(w, rows, maxLogSeries), Names: []NameCount{}}
	for _, r := range rows {
		totals[r.Key] += r.Count
		out.Total += r.Count
	}
	for name, n := range totals {
		out.Names = append(out.Names, NameCount{Name: name, Count: n})
	}
	slices.SortFunc(out.Names, func(a, b NameCount) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(a.Name, b.Name)
	})
	if len(out.Names) > maxLogNames {
		out.Names = out.Names[:maxLogNames]
	}
	return out, nil
}

// SummarizeEventsRecorded counts the events recorded per bucket of w, as
// one series ("events"), from events_created_idx alone: the Overview's
// Data tile.
func (s *Store) SummarizeEventsRecorded(ctx context.Context, w SummaryWindow) (MonitorSeries, error) {
	var rows []bucketCount
	err := s.summaryBase(w).
		ColumnExpr("'events' AS key").
		ColumnExpr("count(*) AS count").
		GroupExpr("1, 2").
		Scan(ctx, &rows)
	if err != nil {
		return MonitorSeries{}, err
	}
	return foldSeries(w, []string{"events"}, rows), nil
}

// PruneRun is a target's last confirmed prune run, from its
// prune.completed: when, the event's ID, and what the run reported.
type PruneRun struct {
	Target  string    `json:"target"`
	At      time.Time `json:"at"`
	Event   string    `json:"event"`
	Deleted int64     `json:"deleted"`
	Kept    int64     `json:"kept"`
	Cutoff  string    `json:"cutoff"`
}

// pruneRunRow is a prune.completed as read, its payload decoded after.
type pruneRunRow struct {
	Target   string          `bun:"target"`
	At       time.Time       `bun:"created_at"`
	PublicID string          `bun:"public_id"`
	Payload  json.RawMessage `bun:"payload"`
}

// IndexPruneRuns finds each target's newest prune.completed, leaving out a
// target with none. Each is the newest end of the name's range in
// events_name_created_idx with that target: prune.completed is recorded
// only by an operator's confirmed runs (a few a day), and kept a year.
func (s *Store) IndexPruneRuns(ctx context.Context, targets []string) ([]PruneRun, error) {
	last := s.DB.NewSelect().Model((*eventRecord)(nil)).
		ColumnExpr("e.public_id, e.created_at, e.payload").
		Where("e.name = 'prune.completed'").
		Where("e.payload->>'target' = t.target").
		OrderExpr("e.created_at DESC").
		Limit(1)
	var rows []pruneRunRow
	err := s.DB.NewSelect().TableExpr("unnest(?::text[]) AS t(target)", pgdialect.Array(targets)).
		Join("CROSS JOIN LATERAL (?) AS p", last).
		ColumnExpr("t.target, p.public_id, p.created_at, p.payload").
		OrderExpr("t.target").
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	out := make([]PruneRun, 0, len(rows))
	for _, r := range rows {
		var p struct {
			Deleted int64  `json:"deleted"`
			Kept    int64  `json:"kept"`
			Cutoff  string `json:"cutoff"`
		}
		// A payload that doesn't decode still says the run happened.
		_ = json.Unmarshal(r.Payload, &p)
		out = append(out, PruneRun{Target: r.Target, At: r.At.UTC(), Event: r.PublicID, Deleted: p.Deleted, Kept: p.Kept, Cutoff: p.Cutoff})
	}
	return out, nil
}
