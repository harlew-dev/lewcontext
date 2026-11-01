package admin_test

import (
	"context"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/monitor"
	"lewcontext/api/internal/store"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/uptrace/bun"
)

// The Data page (docs/api/monitoring.md#data).

// dataSections is the Data page's sections.
type dataSections struct {
	Database struct {
		SizeBytes int64 `json:"sizeBytes"`
		store.Activity
		CacheHitShare *float64 `json:"cacheHitShare"`
	} `json:"database"`
	Tables []monitor.TableRow `json:"tables"`
	Growth struct {
		Days           []monitor.GrowthDay   `json:"days"`
		Tables         []monitor.TableGrowth `json:"tables"`
		BytesPerDay    float64               `json:"bytesPerDay"`
		ProjectionDays int                   `json:"projectionDays"`
		ProjectedBytes *int64                `json:"projectedBytes"`
	} `json:"growth"`
	Log       store.EventLog            `json:"log"`
	Retention []monitor.RetentionTarget `json:"retention"`
}

// handMade is a table no migration makes: one made by hand in the
// schema, which the page must never name.
type handMade struct {
	bun.BaseModel `bun:"table:hand_made_secrets"`
	ID            int64 `bun:",pk"`
}

// Retention: each prune target's oldest row against its age and the
// grace (a non-audit event 96 days old is fine, 98 days overdue; an audit
// event 200 days old is fine for audit_events but not yet minimised), and
// its last confirmed run (2 days without one, or none while it has held a
// row that long, is "not running"). Both signals count the targets, and
// Configuration warns of the targets not running.
func TestAdminMonitoringDataRetention(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	ctx := context.Background()
	_, session := adminSignedIn(t, db, f)
	m := eventMaker{t, db, f}
	day := 24 * time.Hour
	m.make("project.created", map[string]any{"name": "Old"}, "", 96*day)
	m.make("auth.logged_in", map[string]any{"user": "usr_00000000000001"}, "", 200*day)
	m.make("prune.completed", map[string]any{"target": "events", "deleted": 12, "kept": 1, "cutoff": "2026-08-01T00:00:00Z"}, "", time.Hour)
	m.make("prune.completed", map[string]any{"target": "events", "deleted": 3, "kept": 0, "cutoff": "2026-07-31T00:00:00Z"}, "", 25*time.Hour)
	m.make("prune.completed", map[string]any{"target": "audit_events", "deleted": 0, "kept": 0, "cutoff": "2025-10-29T00:00:00Z"}, "", 3*day)
	apitest.Must(factory.AppErrors.State(factory.LastSeen(time.Now().Add(-40*day))).CreateOne(ctx, db, f))(t)
	apitest.Must(factory.MetricHours.State(func(mh *factory.MetricHour) { mh.Hour = time.Now().UTC().Truncate(time.Hour).Add(-10 * time.Hour) }).CreateOne(ctx, db, f))(t)
	apitest.Must(factory.TableStats.State(factory.StatOn("events", factory.Today().AddDate(0, 0, -3))).CreateOne(ctx, db, f))(t)

	type state struct{ overdue, notRunning bool }
	check := func(want map[string]state) {
		t.Helper()
		v := showMonitoring(t, h, session.Cookie(), "data", "")
		page := sections[dataSections](t, v)
		var overdue, notRunning float64
		for _, target := range page.Retention {
			w := want[target.Target]
			if target.Overdue != w.overdue || target.NotRunning != w.notRunning {
				t.Errorf("%s: overdue %v, not running %v; want %+v (oldest %v, last run %+v)", target.Target, target.Overdue, target.NotRunning, w, target.Oldest, target.LastRun)
			}
			if w.overdue {
				overdue++
			}
			if w.notRunning {
				notRunning++
			}
		}
		if len(page.Retention) != 7 {
			t.Fatalf("%d targets", len(page.Retention))
		}
		if got := v.signal(t, "data.retention_overdue"); got.Value != overdue || got.Level != monitor.LevelWarn {
			t.Errorf("retention overdue: %+v", got)
		}
		if got := v.signal(t, "data.prune_not_running"); got.Value != notRunning || got.Level != monitor.LevelWarn {
			t.Errorf("prune not running: %+v", got)
		}
		events := page.Retention[slices.IndexFunc(page.Retention, func(r monitor.RetentionTarget) bool { return r.Target == "events" })]
		if events.KeepDays != 90 || events.LastRun == nil || events.LastRun.Deleted != 12 || events.LastRun.Kept != 1 || !strings.HasPrefix(events.LastRun.Event, "evt_") {
			t.Fatalf("events' last run: %+v", events)
		}
	}
	notRunning := state{notRunning: true}
	both := state{overdue: true, notRunning: true}
	check(map[string]state{
		"audit_events": notRunning, "pseudonymise": both, "app_errors": both, "table_stats_daily": notRunning,
	})
	m.make("workspace.renamed", map[string]any{"name": "Older"}, "", 98*day)
	check(map[string]state{
		"events": {overdue: true}, "audit_events": notRunning, "pseudonymise": both, "app_errors": both, "table_stats_daily": notRunning,
	})

	page := sections[configurationSections](t, showMonitoring(t, h, session.Cookie(), "configuration", ""))
	if !slices.ContainsFunc(page.Warnings, func(w monitor.Warning) bool { return w.ID == monitor.WarnPruneNotRunning && w.Count == 4 }) {
		t.Fatalf("configuration warnings: %+v", page.Warnings)
	}
}

// The database's state is counts and durations only: a transaction held
// open for over a second and a connection waiting for its lock are
// counted, but nothing of the waiting query's text, the role or the
// client's address reaches the page. Tables are this schema's known ones,
// never one made by hand; growth comes from the daily sizes of known
// tables, with each table's rate and the database's projection; the log
// counts the window's events by name.
func TestAdminMonitoringData(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	ctx := context.Background()
	_, session := adminSignedIn(t, db, f)
	m := eventMaker{t, db, f}
	m.many(5, "project.created", func(int) map[string]any { return map[string]any{"name": "Plan"} }, same(""), time.Hour)
	apitest.Must(db.NewCreateTable().Model((*handMade)(nil)).Exec(ctx))(t)
	today := factory.Today()
	const mb = 1 << 20
	apitest.Must(factory.TableStats.Sequence(func(i int, st *factory.TableStat) {
		st.TableName = []string{"events", "events", "projects", "projects", "hand_made_secrets"}[i]
		st.Day = today.AddDate(0, 0, -[]int{3, 1, 3, 1, 1}[i])
		st.LiveRows = []int64{1000, 3000, 10, 10, 99}[i]
		st.TotalBytes = []int64{mb, 3 * mb, 8192, 8192, 4096}[i]
	}).Create(ctx, db, f, 5))(t)

	// A transaction holding a row, and another connection waiting for it,
	// its query holding a value that must never be shown.
	owner := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	marker := "secret-marker-" + f.LetterN(16)
	holder := apitest.Must(db.BeginTx(ctx, nil))(t)
	defer func() { _ = holder.Rollback() }()
	var id int64
	apitest.Must(0, holder.NewSelect().Model((*factory.User)(nil)).Column("id").Where("id = ?", owner.ID).For("UPDATE").Scan(ctx, &id))(t)
	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	waited := make(chan error, 1)
	go func() {
		var got string
		waited <- db.NewSelect().Model((*factory.User)(nil)).ColumnExpr("? AS marker", marker).Where("id = ?", owner.ID).For("UPDATE").Scan(waitCtx, &got)
	}()
	time.Sleep(1200 * time.Millisecond)

	w := adminCall(t, h, "GET", "/api/admin/monitoring/data", "", "", 200, session.Cookie())
	var role, client string
	apitest.Must(0, db.NewSelect().ColumnExpr("current_user, coalesce(host(inet_client_addr()), '')").Scan(ctx, &role, &client))(t)
	for _, never := range []string{marker, role, "hand_made_secrets", `"query"`, "usename", "client_addr"} {
		if strings.Contains(w.Body.String(), never) {
			t.Fatalf("the page holds %q: %s", never, w.Body.String())
		}
	}
	if client != "" && strings.Contains(w.Body.String(), client) {
		t.Fatalf("the page holds the client's address %s", client)
	}
	_ = holder.Rollback()
	if err := <-waited; err != nil {
		t.Fatalf("the waiting query: %v", err)
	}

	v := decodeAdmin[monitoringView](t, w.Body.Bytes())
	page := sections[dataSections](t, v)
	d := page.Database
	if d.SizeBytes <= 0 || d.MaxConnections <= 0 || len(d.Connections) == 0 || d.LongestTransactionSeconds < 1 || d.LockWaits < 1 {
		t.Fatalf("database: %+v", d)
	}
	if got := v.signal(t, "db.lock_waits"); got.Value < 1 {
		t.Fatalf("lock waits: %+v", got)
	}
	if got := v.signal(t, "db.longest_transaction"); got.Value < 1.0/60 {
		t.Fatalf("longest transaction: %+v", got)
	}
	if got := v.signal(t, "data.database_size"); got.Value <= 0 || got.Unit != monitor.UnitBytes {
		t.Fatalf("size: %+v", got)
	}

	if len(page.Tables) != len(store.DataTables) {
		t.Fatalf("%d tables, want %d", len(page.Tables), len(store.DataTables))
	}
	for _, table := range page.Tables {
		if !slices.Contains(store.DataTables, table.Name) || table.TotalBytes < table.TableBytes {
			t.Fatalf("table %+v", table)
		}
	}

	g := page.Growth
	if len(g.Days) != 2 || g.Days[1].TotalBytes != 3*mb+8192 || g.Days[1].LiveRows != 3010 {
		t.Fatalf("growth days: %+v", g.Days)
	}
	if len(g.Tables) != 2 || g.Tables[0].Name != "events" || g.Tables[0].BytesPerDay != mb || g.Tables[0].RowsPerDay != 1000 || g.Tables[1].BytesPerDay != 0 {
		t.Fatalf("growth tables: %+v", g.Tables)
	}
	if g.BytesPerDay != mb || g.ProjectedBytes == nil || *g.ProjectedBytes != 3*mb+8192+90*mb || g.ProjectionDays != 90 {
		t.Fatalf("projection: %+v", g)
	}

	i := slices.IndexFunc(page.Log.Names, func(n store.NameCount) bool { return n.Name == "project.created" })
	if i < 0 || page.Log.Names[i].Count != 5 || page.Log.Total < 5 || len(page.Log.Chart.Series) == 0 {
		t.Fatalf("log: %+v", page.Log)
	}
}
