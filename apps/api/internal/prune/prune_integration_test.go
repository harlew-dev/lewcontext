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
	// Left: the old event with a pending delivery, and the recent one.
	if n := count(t, s, "events"); n != 2 {
		t.Fatalf("%d events left, want 2", n)
	}
	if n := count(t, s, "event_deliveries"); n != 1 {
		t.Fatalf("%d deliveries left, want only the pending one", n)
	}
	// A shorter age now reaches the recent event too.
	r, err = Run(context.Background(), s, events, Options{Now: now, Confirm: true, Age: 24 * time.Hour})
	if err != nil || r.Deleted != 1 || count(t, s, "events") != 1 {
		t.Fatalf("with 1d: %+v, %v", r, err)
	}
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
	if n := count(t, s, "events"); n != 9 {
		t.Fatalf("%d events left after refused runs, want all 9", n)
	}
}
