package prune

import (
	"context"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/database/dbtest"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/store"
	"strings"
	"testing"
	"time"
)

// eventsStore has, relative to now: old events (100 days) whose deliveries
// are done, delivered, failed or still pending, and a recent one (10 days).
func eventsStore(t *testing.T, oldDone int) (*store.Store, time.Time) {
	t.Helper()
	db := dbtest.Open(t)
	ctx := context.Background()
	if _, err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	f := factory.TestFaker(t)
	now := time.Now()
	at := func(age time.Duration) factory.Factory[factory.Event] {
		return factory.Events.State(func(e *factory.Event) { e.CreatedAt = now.Add(-age) })
	}
	old, recent := 100*24*time.Hour, 10*24*time.Hour
	if _, err := at(old).Create(ctx, db, f, oldDone); err != nil {
		t.Fatal(err)
	}
	withDelivery := func(age time.Duration, state func(*factory.EventDelivery)) {
		event, err := at(age).CreateOne(ctx, db, f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := factory.DeliveryOf(event.ID, "test.listener").State(state).CreateOne(ctx, db, f); err != nil {
			t.Fatal(err)
		}
	}
	withDelivery(old, func(d *factory.EventDelivery) { d.DeliveredAt = now })
	withDelivery(old, func(d *factory.EventDelivery) { d.FailedAt = now })
	withDelivery(old, func(*factory.EventDelivery) {}) // pending: kept
	if _, err := at(recent).CreateOne(ctx, db, f); err != nil {
		t.Fatal(err)
	}
	return &store.Store{DB: db}, now
}

func count(t *testing.T, s *store.Store, table string) int {
	t.Helper()
	n, err := s.DB.NewSelect().Table(table).Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
}

func TestPreviewDeletesNothing(t *testing.T) {
	s, now := eventsStore(t, 2)
	events, _ := Find("events")
	r, err := Run(context.Background(), s, events, Options{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	// 2 done + delivered + failed would go; the pending one is kept.
	if r.Deleted != 4 || r.Kept != 1 || r.Confirmed || count(t, s, "events") != 6 {
		t.Fatalf("preview: %+v, %d events left", r, count(t, s, "events"))
	}
	if text := r.String(); !strings.HasPrefix(text, "events: would delete 4 older than ") || !strings.HasSuffix(text, "(90d); kept 1 with pending deliveries") {
		t.Fatalf("preview text: %q", text)
	}
}

func TestConfirmDeletesOnlyOldEventsWithoutPendingDeliveries(t *testing.T) {
	s, now := eventsStore(t, 7)
	events, _ := Find("events")
	// A batch of 3 needs several rounds for 9 rows.
	r, err := Run(context.Background(), s, events, Options{Now: now, Confirm: true, Batch: 3})
	if err != nil {
		t.Fatal(err)
	}
	if r.Deleted != 9 || r.Kept != 1 || !strings.HasPrefix(r.String(), "events: deleted 9 ") {
		t.Fatalf("report: %+v", r)
	}
	// Left: the old event with a pending delivery, and the recent one;
	// and the run's evidence, prune.completed.
	if n := countApp(t, s); n != 2 {
		t.Fatalf("%d events left, want 2", n)
	}
	completed := completedRuns(t, s)
	if len(completed) != 1 || completed[0] != `{"kept": 1, "cutoff": "`+now.Add(-90*day).UTC().Format(time.RFC3339)+`", "target": "events", "deleted": 9}` {
		t.Fatalf("prune.completed: %v", completed)
	}
	if n := count(t, s, "event_deliveries"); n != 1 {
		t.Fatalf("%d deliveries left, want only the pending one", n)
	}
	// A shorter age now reaches the recent event too.
	r, err = Run(context.Background(), s, events, Options{Now: now, Confirm: true, Age: 24 * time.Hour})
	if err != nil || r.Deleted != 1 || countApp(t, s) != 1 || len(completedRuns(t, s)) != 2 {
		t.Fatalf("with 1d: %+v, %v", r, err)
	}
	// A preview records nothing.
	if _, err := Run(context.Background(), s, events, Options{Now: now}); err != nil || len(completedRuns(t, s)) != 2 {
		t.Fatalf("a preview recorded prune.completed: %v", err)
	}
}

// countApp counts the events other than prune's own record.
func countApp(t *testing.T, s *store.Store) int {
	t.Helper()
	n, err := s.DB.NewSelect().Table("events").Where("name <> 'prune.completed'").Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
}

// completedRuns are the prune.completed payloads, oldest first.
func completedRuns(t *testing.T, s *store.Store) []string {
	t.Helper()
	var payloads []string
	err := s.DB.NewSelect().Table("events").ColumnExpr("payload::text").Where("name = 'prune.completed'").OrderExpr("id").Scan(context.Background(), &payloads)
	if err != nil {
		t.Fatal(err)
	}
	return payloads
}

func TestRunRefusesAgesUnderAnHourAndStopsWhenCancelled(t *testing.T) {
	s, now := eventsStore(t, 5)
	events, _ := Find("events")
	if _, err := Run(context.Background(), s, events, Options{Now: now, Confirm: true, Age: time.Minute}); err == nil {
		t.Fatal("pruned with a one-minute age")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, s, events, Options{Now: now, Confirm: true}); err == nil {
		t.Fatal("a cancelled run reported success")
	}
	if n := count(t, s, "events"); n != 9 || len(completedRuns(t, s)) != 0 {
		t.Fatalf("%d events left after refused runs, want all 9 and no prune.completed", n)
	}
}

// app_errors goes by when a group was last seen (30 days by default),
// whenever it first happened; only with a target and confirmation, and
// never under an hour.
func TestPruneAppErrors(t *testing.T) {
	db := dbtest.Open(t)
	ctx := context.Background()
	if _, err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	f := factory.TestFaker(t)
	now := time.Now()
	seen := func(age time.Duration, n int) {
		if _, err := factory.AppErrors.State(factory.LastSeen(now.Add(-age))).Create(ctx, db, f, n); err != nil {
			t.Fatal(err)
		}
	}
	seen(40*24*time.Hour, 3) // stale
	seen(10*24*time.Hour, 2) // recent
	// First seen long ago, but seen again yesterday: kept.
	if _, err := factory.AppErrors.State(func(e *factory.AppError) {
		e.FirstSeenAt, e.LastSeenAt = now.AddDate(0, 0, -200), now.Add(-24*time.Hour)
	}).CreateOne(ctx, db, f); err != nil {
		t.Fatal(err)
	}
	s := &store.Store{DB: db}
	target, ok := Find("app_errors")
	if !ok || target.DefaultAge != 30*day {
		t.Fatalf("app_errors target: %+v", target)
	}
	r, err := Run(ctx, s, target, Options{Now: now})
	if err != nil || r.Deleted != 3 || count(t, s, "app_errors") != 6 {
		t.Fatalf("preview: %+v %v", r, err)
	}
	if _, err := Run(ctx, s, target, Options{Now: now, Confirm: true, Age: 30 * time.Minute}); err == nil {
		t.Fatal("pruned with a 30-minute age")
	}
	r, err = Run(ctx, s, target, Options{Now: now, Confirm: true, Batch: 2})
	if err != nil || r.Deleted != 3 || count(t, s, "app_errors") != 3 || !strings.HasPrefix(r.String(), "app_errors: deleted 3 ") {
		t.Fatalf("confirmed: %+v %v, %d left", r, err, count(t, s, "app_errors"))
	}
}

// The hour tables go after 90 days; the minute tables need no target (the
// rollup keeps them to 48 hours).
func TestPruneMetricHours(t *testing.T) {
	db := dbtest.Open(t)
	ctx := context.Background()
	if _, err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	f := factory.TestFaker(t)
	now := time.Now().UTC().Truncate(time.Hour)
	if _, err := factory.RequestHours.Sequence(func(i int, h *factory.RequestHour) {
		h.Hour = now.Add(-time.Duration(24*(80+5*i)) * time.Hour) // 80, 85, 90, 95 and 100 days: the last three go
	}).Create(ctx, db, f, 5); err != nil {
		t.Fatal(err)
	}
	if _, err := factory.MetricHours.Sequence(func(i int, h *factory.MetricHour) {
		h.Hour = now.Add(-time.Duration(24*(89+2*i)) * time.Hour) // 89, 91 days
	}).Create(ctx, db, f, 2); err != nil {
		t.Fatal(err)
	}
	s := &store.Store{DB: db}
	for _, c := range []struct {
		target string
		want   int
	}{{"request_hours", 3}, {"metric_hours", 1}} {
		target, ok := Find(c.target)
		if !ok || target.DefaultAge != 90*day {
			t.Fatalf("%s target: %+v", c.target, target)
		}
		r, err := Run(ctx, s, target, Options{Now: now.Add(time.Minute), Confirm: true})
		if err != nil || r.Deleted != c.want {
			t.Fatalf("%s: %+v %v", c.target, r, err)
		}
	}
	if count(t, s, "request_hours") != 2 || count(t, s, "metric_hours") != 1 {
		t.Fatal("pruned the wrong hours")
	}
}

// The daily table sizes go after 400 days: a day 399 days old stays, one
// 401 days old goes.
func TestPruneTableStats(t *testing.T) {
	db := dbtest.Open(t)
	ctx := context.Background()
	if _, err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	f := factory.TestFaker(t)
	today := factory.Today()
	if _, err := factory.TableStats.Sequence(func(i int, s *factory.TableStat) {
		s.TableName, s.Day = "events", today.AddDate(0, 0, -[]int{399, 401}[i])
	}).Create(ctx, db, f, 2); err != nil {
		t.Fatal(err)
	}
	target, ok := Find("table_stats_daily")
	if !ok || target.DefaultAge != 400*day {
		t.Fatalf("table_stats_daily target: %+v", target)
	}
	s := &store.Store{DB: db}
	r, err := Run(ctx, s, target, Options{Now: today.Add(12 * time.Hour), Confirm: true})
	if err != nil || r.Deleted != 1 {
		t.Fatalf("table_stats_daily: %+v %v", r, err)
	}
	if count(t, s, "table_stats_daily") != 1 {
		t.Fatal("pruned the wrong day")
	}
}
