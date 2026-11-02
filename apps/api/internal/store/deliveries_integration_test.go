package store

import (
	"context"
	"errors"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/database/dbtest"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/factory"
	"strings"
	"sync"
	"testing"
	"time"
)

// listeners is a fixed event → listener names table.
type listeners map[string][]string

func (l listeners) ListenersFor(event string) []string { return l[event] }

func queueStore(t *testing.T, l listeners) (*Store, context.Context) {
	t.Helper()
	db := dbtest.Open(t)
	ctx := context.Background()
	if _, err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	return &Store{DB: db, Listeners: l}, ctx
}

// pending counts deliveries not yet delivered or failed.
func pending(t *testing.T, s *Store) int {
	t.Helper()
	n, err := s.DB.NewSelect().Model((*deliveryRecord)(nil)).Where("delivered_at IS NULL AND failed_at IS NULL").Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
}

func TestRecordingQueuesADeliveryPerListener(t *testing.T) {
	s, ctx := queueStore(t, listeners{"auth.logged_out": {"test.audit", "test.mail"}})
	if err := s.Record(ctx, events.LoggedOut{User: "usr_00000000000001"}, EventScope{}); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(ctx, events.LoggedIn{User: "usr_00000000000001"}, EventScope{}); err != nil {
		t.Fatal(err)
	}
	claimed, err := s.Claim(ctx, 10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 2 || claimed[0].Event != "auth.logged_out" || claimed[0].Attempts != 1 || !strings.HasPrefix(claimed[0].PublicID, "evt_") ||
		!strings.Contains(string(claimed[0].Payload), "usr_00000000000001") {
		t.Fatalf("claimed: %+v", claimed)
	}
	// Without listeners, nothing is queued.
	s.Listeners = nil
	if err := s.Record(ctx, events.LoggedOut{}, EventScope{}); err != nil {
		t.Fatal(err)
	}
	if n := pending(t, s); n != 2 {
		t.Fatalf("want the 2 claimed deliveries pending, got %d", n)
	}
}

// Workers claiming at the same time never get the same delivery.
func TestConcurrentClaimsDontOverlap(t *testing.T) {
	s, ctx := queueStore(t, nil)
	f := factory.TestFaker(t)
	for range 40 {
		event, err := factory.Events.CreateOne(ctx, s.DB, f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := factory.DeliveryOf(event.ID, "test.listener").CreateOne(ctx, s.DB, f); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	seen := map[int64]int{}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				batch, err := s.Claim(ctx, 3, time.Minute)
				if err != nil {
					t.Error(err)
					return
				}
				if len(batch) == 0 {
					return
				}
				mu.Lock()
				for _, d := range batch {
					seen[d.EventID]++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(seen) != 40 {
		t.Fatalf("claimed %d of 40 deliveries", len(seen))
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("event %d claimed %d times", id, n)
		}
	}
}

func TestLeasesRetriesAndOutcomes(t *testing.T) {
	s, ctx := queueStore(t, listeners{"auth.logged_in": {"test.ok", "test.retry", "test.fail", "test.lease"}})
	if err := s.Record(ctx, events.LoggedIn{User: "usr_00000000000001"}, EventScope{}); err != nil {
		t.Fatal(err)
	}
	claimed, err := s.Claim(ctx, 10, 300*time.Millisecond)
	if err != nil || len(claimed) != 4 {
		t.Fatalf("claim: %v %+v", err, claimed)
	}
	byName := map[string]events.Delivery{}
	for _, d := range claimed {
		byName[d.Listener] = d
	}
	long := errors.New(strings.Repeat("é", 600) + "\x00")
	for _, err := range []error{
		s.Delivered(ctx, byName["test.ok"]),
		s.Retry(ctx, byName["test.retry"], time.Now().Add(time.Hour), errors.New("550 5.1.1 <ada@example.com>: Recipient address rejected")),
		s.Fail(ctx, byName["test.fail"], long),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	// Leased rows aren't due until the lease ends.
	if again, _ := s.Claim(ctx, 10, time.Minute); len(again) != 0 {
		t.Fatalf("claimed leased rows: %+v", again)
	}
	time.Sleep(400 * time.Millisecond)
	// Only the abandoned lease comes back; delivered, retrying-later and
	// failed rows don't.
	again, err := s.Claim(ctx, 10, time.Minute)
	if err != nil || len(again) != 1 || again[0].Listener != "test.lease" || again[0].Attempts != 2 {
		t.Fatalf("after the lease: %v %+v", err, again)
	}
	var lastError string
	if err := s.DB.NewSelect().Model((*deliveryRecord)(nil)).Column("last_error").Where("listener = ?", "test.fail").Scan(ctx, &lastError); err != nil {
		t.Fatal(err)
	}
	if len([]rune(lastError)) != maxLastError {
		t.Fatalf("last_error not cut to %d characters: %d", maxLastError, len([]rune(lastError)))
	}
	// Stored scrubbed: an SMTP refusal names its recipient.
	if err := s.DB.NewSelect().Model((*deliveryRecord)(nil)).Column("last_error").Where("listener = ?", "test.retry").Scan(ctx, &lastError); err != nil {
		t.Fatal(err)
	}
	if lastError != "<n> <n> <email>: Recipient address rejected" {
		t.Fatalf("last_error: %q", lastError)
	}
}

// A retry can't race pseudonymise: RequeueDelivery locks the event's row
// before it checks minimised_at, so while a run holds the row (minimising
// it) the retry waits, then refuses (ErrDeliveryMinimised); and the bulk
// retry, waiting the same way, leaves it out.
func TestRetryWaitsForAMinimisingEvent(t *testing.T) {
	s, ctx := queueStore(t, nil)
	f := factory.TestFaker(t)
	admin, err := factory.Users.CreateOne(ctx, s.DB, f)
	if err != nil {
		t.Fatal(err)
	}
	action := AdminAction{Admin: User{ID: admin.ID, PublicID: admin.PublicID}, Reason: "Retrying after the SMTP outage."}
	event, err := factory.Events.State(func(e *factory.Event) {
		e.Name, e.Payload = "auth.password_reset.requested", map[string]any{"email": "ada@example.test"}
	}).CreateOne(ctx, s.DB, f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := factory.DeliveryOf(event.ID, "mail.password_reset").State(func(d *factory.EventDelivery) {
		d.FailedAt, d.Attempts, d.LastError = time.Now(), 6, "smtp: timeout"
	}).CreateOne(ctx, s.DB, f); err != nil {
		t.Fatal(err)
	}
	var publicID string
	if err := s.DB.NewSelect().Model((*eventRecord)(nil)).Column("e.public_id").Where("e.id = ?", event.ID).Scan(ctx, &publicID); err != nil {
		t.Fatal(err)
	}
	retries := []struct {
		name  string
		retry func() error
		want  error
	}{
		{"one", func() error { return s.RequeueDelivery(ctx, publicID, "mail.password_reset", action) }, ErrDeliveryMinimised},
		{"bulk", func() error {
			_, err := s.RequeueFailedDeliveries(ctx, "mail.password_reset", time.Now().Add(-time.Hour), action)
			return err
		}, ErrNothingToRetry},
	}
	for _, c := range retries {
		// A pseudonymise run, mid-way: the row locked and minimised, not
		// yet committed.
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		// On a failure, the row is let go before the schema is dropped.
		t.Cleanup(func() { _ = tx.Rollback() })
		if _, err := tx.NewSelect().Model((*eventRecord)(nil)).Column("e.id").Where("e.id = ?", event.ID).For("UPDATE OF e SKIP LOCKED").Exec(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.NewUpdate().Model((*eventRecord)(nil)).Set("minimised_at = now()").Where("e.id = ?", event.ID).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- c.retry() }()
		select {
		case err := <-done:
			t.Fatalf("%s: the retry didn't wait for the run: %v", c.name, err)
		case <-time.After(300 * time.Millisecond):
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if err := <-done; !errors.Is(err, c.want) {
			t.Fatalf("%s: %v", c.name, err)
		}
		if _, err := s.DB.NewUpdate().Model((*eventRecord)(nil)).Set("minimised_at = NULL").Where("e.id = ?", event.ID).Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}
	// With no run holding it, an unminimised event's delivery is retried.
	if err := s.RequeueDelivery(ctx, publicID, "mail.password_reset", action); err != nil {
		t.Fatal(err)
	}
	if pending(t, s) != 1 {
		t.Fatal("not queued again")
	}
}
