package seed

import (
	"fmt"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/store"
	"time"
)

// The monitoring seeder's last part: what the Data page looks for. Prune
// runs (prune.completed) for most targets in the last day, request_hours'
// last run four days ago (so "prune not running" warns), and a month of
// daily table sizes before today, grown up to each table's size now (the
// server writes today's real row; these days are made up, so Growth has
// a curve to show). Nothing is deleted or minimised.

// seedPruneRuns are the targets planted as run, with how long ago.
var seedPruneRuns = []struct {
	target string
	ago    time.Duration
}{
	{"pseudonymise", 5 * time.Hour},
	{"events", 5*time.Hour - 5*time.Minute},
	{"audit_events", 5*time.Hour - 10*time.Minute},
	{"app_errors", 5*time.Hour - 15*time.Minute},
	{"request_hours", 4*24*time.Hour + 5*time.Hour},
}

// seedGrowthDays is how many days of table sizes are planted.
const seedGrowthDays = 30

// seedData plants the Data page's patterns, returning what it planted.
func seedData(a abuse) (string, error) {
	for i, run := range seedPruneRuns {
		at := a.now.Add(-run.ago)
		a.add("prune.completed", run.ago, "", map[string]any{
			"target": run.target, "deleted": 40 * (i + 1), "kept": 0,
			"cutoff": at.Add(-90 * 24 * time.Hour).UTC().Format(time.RFC3339),
		})
	}
	days, err := a.tableSizes()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d prune runs (request_hours' 4 days old), %d days of table sizes", len(seedPruneRuns), days), nil
}

// tableSize is a table's size now, as the daily sizes record it.
type tableSize struct {
	Name  string `bun:"name"`
	Rows  int64  `bun:"rows"`
	Bytes int64  `bun:"bytes"`
}

// tableSizes plants seedGrowthDays of sizes before today for each known
// table (store.DataTables) in this schema, each day smaller than the next (events growing
// fastest), skipping a day a table already has.
func (a abuse) tableSizes() (int, error) {
	var now []tableSize
	err := a.tx.NewSelect().TableExpr("pg_stat_user_tables AS t").
		ColumnExpr("t.relname AS name").
		ColumnExpr("greatest(t.n_live_tup, 0) AS rows").
		ColumnExpr("pg_total_relation_size(t.relid) AS bytes").
		Where("t.schemaname = current_schema()").
		Where("t.relname IN (?)", bun.In(store.DataTables)).
		Scan(a.ctx, &now)
	if err != nil || len(now) == 0 {
		return 0, err
	}
	today := factory.Today()
	var rows []factory.TableStat
	for _, table := range now {
		growth := 0.01
		if table.Name == "events" {
			growth = 0.03
		}
		for d := seedGrowthDays; d >= 1; d-- {
			shrink := 1 - growth*float64(d)
			rows = append(rows, factory.TableStat{
				Day: today.AddDate(0, 0, -d), TableName: table.Name,
				LiveRows: int64(float64(table.Rows) * shrink), TotalBytes: int64(float64(table.Bytes) * shrink),
			})
		}
	}
	_, err = a.tx.NewInsert().Model(&rows).On("CONFLICT (day, table_name) DO NOTHING").Exec(a.ctx)
	return seedGrowthDays, err
}
