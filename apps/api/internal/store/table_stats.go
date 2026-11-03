package store

import (
	"context"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/database"
	"time"
)

// The daily table sizes (docs/api/monitoring.md#data): once a day, after
// the hourly rollup, one server (whichever takes the advisory lock) writes
// each known table's live rows and bytes into table_stats_daily, for the
// Data page's growth. One row per table per day, never updated; pruned at
// 400 days (make prune table_stats_daily).

// tableStatRecord is a table_stats_daily row.
type tableStatRecord struct {
	bun.BaseModel `bun:"table:table_stats_daily,alias:tsd"`
	ID            int64     `bun:",pk,autoincrement"`
	Day           time.Time `bun:"type:date"`
	TableName     string
	LiveRows      int64
	TotalBytes    int64
}

// TableStatsRun is what one RecordTableStats did: Locked false when
// another server held the lock, Rows the rows written (none when today's
// were written already).
type TableStatsRun struct {
	Locked bool
	Rows   int
}

// RecordTableStats writes today's size of every known table (DataTables)
// in this schema, once a day: under the table-stats lock
// (database.TryLockTableStats), never waiting for it, and only when no row
// for today exists yet; the (day, table_name) key refuses a second anyway.
// The day is the database's (UTC), so every server agrees on it.
func (s *Store) RecordTableStats(ctx context.Context) (TableStatsRun, error) {
	var out TableStatsRun
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		locked, err := database.TryLockTableStats(ctx, tx)
		if err != nil || !locked {
			return err
		}
		out.Locked = true
		var today time.Time
		if err := tx.NewSelect().ColumnExpr("current_date").Scan(ctx, &today); err != nil {
			return err
		}
		written, err := tx.NewSelect().Model((*tableStatRecord)(nil)).Where("tsd.day = ?", today).Exists(ctx)
		if err != nil || written {
			return err
		}
		var rows []tableStatRecord
		err = tx.NewSelect().TableExpr("pg_stat_user_tables AS t").
			ColumnExpr("? AS day", today).
			ColumnExpr("t.relname AS table_name").
			ColumnExpr("greatest(t.n_live_tup, 0) AS live_rows").
			ColumnExpr("pg_total_relation_size(t.relid) AS total_bytes").
			Where("t.schemaname = current_schema()").
			Where("t.relname IN (?)", bun.In(DataTables)).
			OrderExpr("t.relname").
			Scan(ctx, &rows)
		if err != nil || len(rows) == 0 {
			return err
		}
		if _, err := tx.NewInsert().Model(&rows).On("CONFLICT (day, table_name) DO NOTHING").Exec(ctx); err != nil {
			return err
		}
		out.Rows = len(rows)
		return nil
	})
	return out, err
}
