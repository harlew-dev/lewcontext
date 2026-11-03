package monitor

import (
	"cmp"
	"context"
	"lewcontext/api/internal/prune"
	"lewcontext/api/internal/store"
	"slices"
	"strings"
	"time"
)

// The Data page (docs/api/monitoring.md#data): how big is the database,
// what's growing, is pruning running? The database's size and what's
// connected and waiting now (counts and durations from PostgreSQL's
// statistics, never a query's text, a role or a client's address), each
// known table's sizes and upkeep, growth from the daily sizes
// (table_stats_daily), what fills the events table, and each prune
// target's oldest row against its retention with its last confirmed run
// (prune.completed).

// PageData is the Data page's name.
const PageData = "data"

// Retention levels: a target's oldest row may be up to retentionGrace past
// its age (the plan's 97 days for 90) before it's overdue, and its last
// confirmed run up to pruneEvery old (a daily cron and a day's grace)
// before prune isn't running.
const (
	retentionGrace = 7 * 24 * time.Hour
	pruneEvery     = 48 * time.Hour
)

// Table levels: a table is mostly read by sequential scans (an index is
// probably missing) when it has scannedRows live rows or more and more
// sequential scans than index scans; its dead rows are worth a look at
// deadShareLevel percent of its rows, with deadRowsLevel or more.
const (
	scannedRows    = 10_000
	deadShareLevel = 20
	deadRowsLevel  = 1_000
)

// growthDays is how far back the growth reads the daily sizes, and
// projectionDays how far ahead it projects them.
const (
	growthDays     = 30
	projectionDays = 90
)

// DataSignals are the Data page's signals.
var DataSignals = []Signal{
	{
		Name: "data.database_size", Page: PageData, Label: "Database size", Unit: UnitBytes,
		Window: checkNow,
		Measure: func(ctx context.Context, env Env, _ Span) (Reading, error) {
			n, err := env.Store.FindDatabaseSize(ctx)
			return Reading{Value: float64(n), Sample: 1}, err
		},
	},
	{
		// A target whose oldest row is past its age and the grace: the
		// retention schedule isn't kept (docs/compliance.md#retention).
		Name: "data.retention_overdue", Page: PageData, Label: "Targets past their retention", Unit: UnitCount,
		Window: checkNow, Warn: 1,
		Measure: func(ctx context.Context, env Env, _ Span) (Reading, error) {
			targets, err := readRetention(ctx, env)
			return count(countTargets(targets, func(t RetentionTarget) bool { return t.Overdue }), err)
		},
	},
	{
		// A target not run in 2 days, or never run though it has held rows
		// for 2 days: prune isn't scheduled, or its cron fails.
		Name: "data.prune_not_running", Page: PageData, Label: "Prune targets not running", Unit: UnitCount,
		Window: checkNow, Warn: 1,
		Measure: func(ctx context.Context, env Env, _ Span) (Reading, error) {
			targets, err := readRetention(ctx, env)
			return count(countTargets(targets, func(t RetentionTarget) bool { return t.NotRunning }), err)
		},
	},
	{
		// An open transaction holds back vacuum on every table and may
		// hold locks others wait for.
		Name: "db.longest_transaction", Page: PageData, Label: "Longest open transaction", Unit: UnitMinutes,
		Window: checkNow, Warn: 5, Critical: 60,
		Measure: func(ctx context.Context, env Env, _ Span) (Reading, error) {
			a, err := env.Store.FindActivity(ctx)
			return Reading{Value: a.LongestTransactionSeconds / 60, Sample: 1}, err
		},
	},
	{
		Name: "db.lock_waits", Page: PageData, Label: "Connections waiting for a lock", Unit: UnitCount,
		Window: checkNow, Warn: 5,
		Measure: func(ctx context.Context, env Env, _ Span) (Reading, error) {
			a, err := env.Store.FindActivity(ctx)
			return count(a.LockWaits, err)
		},
	},
}

// RetentionTarget is a prune target as the Data page shows it: its age
// (prune's default), its oldest row that a run would prune (nil: none),
// and its last confirmed run. Overdue: the oldest row is past its age and
// the grace. NotRunning: no confirmed run in pruneEvery, or none ever
// while it has held such a row that long.
type RetentionTarget struct {
	Target      string          `json:"target"`
	Description string          `json:"description"`
	KeepDays    int             `json:"keepDays"`
	Minimises   bool            `json:"minimises"`
	Oldest      *time.Time      `json:"oldest"`
	Overdue     bool            `json:"overdue"`
	LastRun     *store.PruneRun `json:"lastRun"`
	NotRunning  bool            `json:"notRunning"`
}

// readRetention reads every prune target's oldest row and last run.
func readRetention(ctx context.Context, env Env) ([]RetentionTarget, error) {
	pruners := prune.Pruners()
	names := make([]string, len(pruners))
	for i, p := range pruners {
		names[i] = p.Name
	}
	runs, err := env.Store.IndexPruneRuns(ctx, names)
	if err != nil {
		return nil, err
	}
	out := make([]RetentionTarget, len(pruners))
	for i, p := range pruners {
		oldest, err := env.Store.FindOldestPrunable(ctx, p.Table)
		if err != nil {
			return nil, err
		}
		t := RetentionTarget{Target: p.Name, Description: p.Description, KeepDays: int(p.DefaultAge / (24 * time.Hour)),
			Minimises: p.Minimises, Oldest: oldest}
		for _, r := range runs {
			if r.Target == p.Name {
				t.LastRun = &r
			}
		}
		t.Overdue = oldest != nil && env.Now.Sub(*oldest) > p.DefaultAge+retentionGrace
		if t.LastRun != nil {
			t.NotRunning = env.Now.Sub(t.LastRun.At) > pruneEvery
		} else {
			t.NotRunning = oldest != nil && env.Now.Sub(*oldest) > pruneEvery
		}
		out[i] = t
	}
	return out, nil
}

// countTargets counts the targets that are.
func countTargets(targets []RetentionTarget, is func(RetentionTarget) bool) int64 {
	var n int64
	for _, t := range targets {
		if is(t) {
			n++
		}
	}
	return n
}

// databaseState is the Data page's Database section: the size, what's
// connected now (store.Activity), and the cache hit share.
type databaseState struct {
	SizeBytes int64 `json:"sizeBytes"`
	store.Activity
	CacheHitShare *float64 `json:"cacheHitShare"`
}

// TableRow is a table as the Tables section lists it, with its dead rows'
// share of its rows and its two marks.
type TableRow struct {
	store.TableStat
	DeadShare float64 `json:"deadShare"`
	// Scanned: read mostly by sequential scans, at a size where an index
	// would matter.
	Scanned bool `json:"scanned"`
	// Bloated: dead rows past their level, waiting for a vacuum.
	Bloated bool `json:"bloated"`
}

// GrowthDay is the known tables' sizes summed on one day.
type GrowthDay struct {
	Day        string `json:"day"`
	LiveRows   int64  `json:"liveRows"`
	TotalBytes int64  `json:"totalBytes"`
}

// TableGrowth is a table's size on the latest day and how fast it grew
// per day between its first and latest day in the range.
type TableGrowth struct {
	Name        string  `json:"name"`
	LiveRows    int64   `json:"liveRows"`
	TotalBytes  int64   `json:"totalBytes"`
	RowsPerDay  float64 `json:"rowsPerDay"`
	BytesPerDay float64 `json:"bytesPerDay"`
}

// dataGrowth is the Growth section: the summed sizes per day over the
// last growthDays, the tables growing fastest, and, from two days apart
// or more, the database's rate and where it leads in projectionDays.
type dataGrowth struct {
	Days           []GrowthDay   `json:"days"`
	Tables         []TableGrowth `json:"tables"`
	BytesPerDay    float64       `json:"bytesPerDay"`
	ProjectionDays int           `json:"projectionDays"`
	ProjectedBytes *int64        `json:"projectedBytes"`
}

// dataSections are the Data page's sections.
type dataSections struct {
	Database  *databaseState    `json:"database"`
	Tables    []TableRow        `json:"tables"`
	Growth    *dataGrowth       `json:"growth"`
	Log       *store.EventLog   `json:"log"`
	Retention []RetentionTarget `json:"retention"`
}

// dataPage reads the page's sections; the log over w, the rest as they
// stand now (the growth over the last growthDays).
func dataPage(ctx context.Context, env Env, w Window) (Sections, error) {
	s := env.Store
	out := &dataSections{Database: &databaseState{}, Tables: []TableRow{}, Retention: []RetentionTarget{}}
	return readSections(ctx, env, out, []section{
		{"database", func(ctx context.Context) (err error) {
			out.Database.SizeBytes, err = s.FindDatabaseSize(ctx)
			return err
		}},
		{"database", func(ctx context.Context) (err error) {
			out.Database.Activity, err = s.FindActivity(ctx)
			return err
		}},
		{"database", func(ctx context.Context) (err error) {
			out.Database.CacheHitShare, err = s.FindCacheHitShare(ctx)
			return err
		}},
		{"tables", func(ctx context.Context) error {
			tables, err := s.IndexTableStats(ctx)
			for _, t := range tables {
				out.Tables = append(out.Tables, tableRow(t))
			}
			return err
		}},
		{"growth", func(ctx context.Context) error {
			sizes, err := s.IndexTableSizes(ctx, env.Now.UTC().AddDate(0, 0, -growthDays))
			out.Growth = growthOf(sizes)
			return err
		}},
		{"log", func(ctx context.Context) error {
			log, err := s.SummarizeEventLog(ctx, w.SummaryWindow)
			out.Log = &log
			return err
		}},
		{"retention", func(ctx context.Context) (err error) {
			out.Retention, err = readRetention(ctx, env)
			return err
		}},
	})
}

// tableRow marks a table's dead rows and scans.
func tableRow(t store.TableStat) TableRow {
	r := TableRow{TableStat: t}
	if rows := t.LiveRows + t.DeadRows; rows > 0 {
		r.DeadShare = float64(t.DeadRows) * 100 / float64(rows)
	}
	r.Scanned = t.LiveRows >= scannedRows && t.SeqScans > t.IndexScans
	r.Bloated = t.DeadRows >= deadRowsLevel && r.DeadShare >= deadShareLevel
	return r
}

// maxGrowthTables is how many tables the Growth section lists.
const maxGrowthTables = 10

// growthOf sums the daily sizes per day and works out each table's rate
// between its first and latest day, and the database's (the known
// tables summed) with its projection.
func growthOf(sizes []store.TableSize) *dataGrowth {
	out := &dataGrowth{Days: []GrowthDay{}, Tables: []TableGrowth{}, ProjectionDays: projectionDays}
	first := map[string]store.TableSize{}
	latest := map[string]store.TableSize{}
	var order []string
	for _, sz := range sizes {
		day := sz.Day.UTC().Format(time.DateOnly)
		if n := len(out.Days); n == 0 || out.Days[n-1].Day != day {
			out.Days = append(out.Days, GrowthDay{Day: day})
		}
		d := &out.Days[len(out.Days)-1]
		d.LiveRows += sz.LiveRows
		d.TotalBytes += sz.TotalBytes
		if _, seen := first[sz.Table]; !seen {
			first[sz.Table] = sz
			order = append(order, sz.Table)
		}
		latest[sz.Table] = sz
	}
	for _, name := range order {
		a, b := first[name], latest[name]
		g := TableGrowth{Name: name, LiveRows: b.LiveRows, TotalBytes: b.TotalBytes}
		if days := b.Day.Sub(a.Day).Hours() / 24; days >= 1 {
			g.RowsPerDay = float64(b.LiveRows-a.LiveRows) / days
			g.BytesPerDay = float64(b.TotalBytes-a.TotalBytes) / days
		}
		out.Tables = append(out.Tables, g)
	}
	sortByGrowth(out.Tables)
	if len(out.Tables) > maxGrowthTables {
		out.Tables = out.Tables[:maxGrowthTables]
	}
	if len(out.Days) >= 2 {
		a, b := out.Days[0], out.Days[len(out.Days)-1]
		from, _ := time.Parse(time.DateOnly, a.Day)
		to, _ := time.Parse(time.DateOnly, b.Day)
		out.BytesPerDay = float64(b.TotalBytes-a.TotalBytes) / (to.Sub(from).Hours() / 24)
		projected := max(0, b.TotalBytes+int64(out.BytesPerDay*projectionDays))
		out.ProjectedBytes = &projected
	}
	return out
}

// sortByGrowth orders tables by bytes added per day, then by size, then
// by name.
func sortByGrowth(tables []TableGrowth) {
	slices.SortFunc(tables, func(a, b TableGrowth) int {
		return cmp.Or(cmp.Compare(b.BytesPerDay, a.BytesPerDay), cmp.Compare(b.TotalBytes, a.TotalBytes), strings.Compare(a.Name, b.Name))
	})
}
