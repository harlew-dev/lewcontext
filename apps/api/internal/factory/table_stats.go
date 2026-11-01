package factory

import (
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"time"
)

type TableStat struct {
	bun.BaseModel `bun:"table:table_stats_daily,alias:tsd"`
	ID            int64     `bun:",pk,autoincrement"`
	Day           time.Time `bun:"type:date"`
	TableName     string
	LiveRows      int64
	TotalBytes    int64
}

// statTables are tables the app has, for fake daily sizes.
var statTables = []string{"events", "projects", "users", "workspaces", "sessions", "request_hours"}

// TableStats builds a day's size of a table in the last 400 days. Give
// several their own day or table (Sequence, StatOn): a table has one row
// a day. Only the monitoring seeder makes any, for days before today (the
// server writes today's).
var TableStats = Define(func(f *gofakeit.Faker) TableStat {
	rows := int64(f.IntRange(0, 1_000_000))
	return TableStat{
		Day:        Today().AddDate(0, 0, -f.IntRange(1, 400)),
		TableName:  f.RandomString(statTables),
		LiveRows:   rows,
		TotalBytes: 16384 + rows*200,
	}
})

// StatOn sets a row's table and day.
func StatOn(table string, day time.Time) func(*TableStat) {
	return func(s *TableStat) { s.TableName, s.Day = table, day }
}

// Today is the current day in UTC, as the database's sessions (pinned to
// UTC) see it.
func Today() time.Time {
	return time.Now().UTC().Truncate(24 * time.Hour)
}
