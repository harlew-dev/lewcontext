package store

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/uptrace/bun"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/observe"
)

func incidentErrorFixture(t *testing.T, s *Store) (User, factory.AppError, factory.IncidentCycle) {
	t.Helper()
	ctx := context.Background()
	f := factory.TestFaker(t)
	u, err := factory.Users.CreateOne(ctx, s.DB, f)
	if err != nil {
		t.Fatal(err)
	}
	group, err := factory.AppErrors.State(factory.LastSeen(time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond))).CreateOne(ctx, s.DB, f)
	if err != nil {
		t.Fatal(err)
	}
	cycle, err := factory.IncidentCycles.State(func(c *factory.IncidentCycle) { c.AppErrorID = group.ID }).CreateOne(ctx, s.DB, f)
	if err != nil {
		t.Fatal(err)
	}
	return User{ID: u.ID, PublicID: u.PublicID, Name: u.Name}, group, cycle
}

func TestIncidentResolutionAndDelayedBatches(t *testing.T) {
	s, ctx := queueStore(t, nil)
	admin, g, c := incidentErrorFixture(t, s)
	target := IncidentTarget{Kind: "error", PublicID: g.PublicID}
	msg, err := s.CreateIncidentMessage(ctx, target, c.PublicID, "  Reviewed 👩‍💻\n<script>plain text</script>  ", admin)
	if err != nil {
		t.Fatal(err)
	}
	if *msg.Body != "Reviewed 👩‍💻\n<script>plain text</script>" {
		t.Fatal("accepted text was changed")
	}
	if err := s.ResolveErrorGroup(ctx, g.PublicID, c.PublicID, msg.PublicID, g.LastSeenAt.Add(-time.Second), admin); !errors.Is(err, ErrIncidentConflict) {
		t.Fatalf("stale snapshot: %v", err)
	}
	if err := s.ResolveErrorGroup(ctx, g.PublicID, c.PublicID, msg.PublicID, g.LastSeenAt, admin); err != nil {
		t.Fatal(err)
	}
	var resolved appErrorRecord
	if err := s.DB.NewSelect().Model(&resolved).Where("ae.id = ?", g.ID).Scan(ctx); err != nil {
		t.Fatal(err)
	}
	batch := observe.ErrorGroup{Fingerprint: g.Fingerprint, Source: g.Source, Route: g.Route, Message: g.Message, Count: 2, FirstSeen: g.FirstSeenAt, LastSeen: resolved.ResolvedAt}
	if err := s.RecordErrors(ctx, []observe.ErrorGroup{batch}); err != nil {
		t.Fatal(err)
	}
	thread, err := s.FindIncidentThread(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if len(thread.Cycles) != 1 || thread.CurrentCycle.ResolvedAt == nil {
		t.Fatal("equal delayed batch reopened resolution")
	}
	batch.LastSeen = resolved.ResolvedAt.Add(-time.Microsecond)
	if err := s.RecordErrors(ctx, []observe.ErrorGroup{batch}); err != nil {
		t.Fatal(err)
	}
	batch.LastSeen = resolved.ResolvedAt.Add(time.Microsecond)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- s.RecordErrors(ctx, []observe.ErrorGroup{batch}) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	thread, err = s.FindIncidentThread(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if len(thread.Cycles) != 2 || thread.CurrentCycle.Number != 2 || thread.CurrentCycle.ResolvedAt != nil || thread.Cycles[1].ResolutionMessage == nil || *thread.Cycles[1].ResolutionMessage != msg.PublicID {
		t.Fatalf("recurrence lost history: %+v", thread)
	}
	var row appErrorRecord
	if err := s.DB.NewSelect().Model(&row).Where("ae.id = ?", g.ID).Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if row.Count != g.Count+20 {
		t.Fatalf("counts: %d", row.Count)
	}
	if err := s.ResolveErrorGroup(ctx, g.PublicID, c.PublicID, msg.PublicID, row.LastSeenAt, admin); !errors.Is(err, ErrIncidentConflict) {
		t.Fatalf("old cycle reused: %v", err)
	}
}

func TestIncidentPagingScopingQuotaAndRedaction(t *testing.T) {
	s, ctx := queueStore(t, nil)
	admin, g, c := incidentErrorFixture(t, s)
	target := IncidentTarget{Kind: "error", PublicID: g.PublicID}
	_, other, otherCycle := incidentErrorFixture(t, s)
	otherTarget := IncidentTarget{Kind: "error", PublicID: other.PublicID}
	f := factory.TestFaker(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	rows, err := factory.IncidentMessages.State(func(m *factory.IncidentMessage) { m.CycleID = c.ID; m.AuthorID = &admin.ID; m.CreatedAt = at }).Create(ctx, s.DB, f, 3)
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.IndexIncidentMessages(ctx, target, c.PublicID, "", 2)
	if err != nil || len(page.Data) != 2 || page.Next == nil {
		t.Fatalf("page: %+v %v", page, err)
	}
	older, err := s.IndexIncidentMessages(ctx, target, c.PublicID, *page.Next, 2)
	if err != nil || len(older.Data) != 1 || older.Data[0].ID != rows[0].ID || older.Next != nil {
		t.Fatalf("tied cursor: %+v %v", older, err)
	}
	if _, err := s.IndexIncidentMessages(ctx, otherTarget, c.PublicID, "", 2); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("foreign cycle: %v", err)
	}
	if _, err := s.IndexIncidentMessages(ctx, otherTarget, otherCycle.PublicID, rows[0].PublicID, 2); !errors.Is(err, ErrNoCursor) {
		t.Fatalf("foreign cursor: %v", err)
	}
	if err := s.ResolveErrorGroup(ctx, g.PublicID, c.PublicID, rows[0].PublicID, g.LastSeenAt, admin); err != nil {
		t.Fatal(err)
	}
	redacted, err := s.UpdateIncidentMessageRedaction(ctx, target, rows[0].PublicID, AdminAction{Admin: admin, Reason: "Removed sensitive investigation text"})
	if err != nil || redacted.Body != nil || redacted.RedactedAt == nil {
		t.Fatalf("redaction: %+v %v", redacted, err)
	}
	thread, err := s.FindIncidentThread(ctx, target)
	if err != nil || thread.CurrentCycle.ResolvedAt == nil || thread.CurrentCycle.ResolutionMessage == nil {
		t.Fatalf("redaction changed resolution: %+v %v", thread, err)
	}
	if _, err := s.UpdateIncidentMessageRedaction(ctx, otherTarget, rows[0].PublicID, AdminAction{Admin: admin, Reason: "A sufficient reason"}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("foreign redaction: %v", err)
	}
	if _, err := factory.IncidentMessages.State(func(m *factory.IncidentMessage) { m.CycleID = c.ID }).Create(ctx, s.DB, f, MaxIncidentMessages-3); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateIncidentMessage(ctx, target, c.PublicID, "quota counts tombstones", admin); err == nil {
		t.Fatal("quota ignored tombstone")
	}
	// Deleting an author nulls references; explicit resolution source survives.
	if _, err := s.DB.NewDelete().Model((*User)(nil)).Where("id = ?", admin.ID).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	thread, err = s.FindIncidentThread(ctx, target)
	if err != nil || thread.CurrentCycle.Resolver != nil || thread.CurrentCycle.ResolutionSource == nil || *thread.CurrentCycle.ResolutionSource != "admin" {
		t.Fatalf("deleted resolver misattributed: %+v %v", thread, err)
	}
}

func TestIncidentConcurrentResolution(t *testing.T) {
	s, ctx := queueStore(t, nil)
	admin, g, c := incidentErrorFixture(t, s)
	target := IncidentTarget{Kind: "error", PublicID: g.PublicID}
	msg, err := s.CreateIncidentMessage(ctx, target, c.PublicID, "Handled and verified", admin)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for range 2 {
		go func() { results <- s.ResolveErrorGroup(ctx, g.PublicID, c.PublicID, msg.PublicID, g.LastSeenAt, admin) }()
	}
	a, b := <-results, <-results
	if !((a == nil && errors.Is(b, ErrIncidentConflict)) || (b == nil && errors.Is(a, ErrIncidentConflict))) {
		t.Fatalf("resolution winners: %v %v", a, b)
	}
	count, err := s.DB.NewSelect().TableExpr("events").Where("name = ?", "admin.error.resolved").Count(ctx)
	if err != nil || count != 1 {
		t.Fatalf("atomic event count %d %v", count, err)
	}
	// A held parent forces a collector flush to observe the committed
	// resolution timestamp; an older buffered batch must keep it closed.
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.NewSelect().Model((*appErrorRecord)(nil)).Where("ae.id = ?", g.ID).For("UPDATE").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- s.RecordErrors(ctx, []observe.ErrorGroup{{Fingerprint: g.Fingerprint, Source: g.Source, Route: g.Route, Message: g.Message, Count: 1, FirstSeen: g.FirstSeenAt, LastSeen: g.LastSeenAt}})
	}()
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	thread, err := s.FindIncidentThread(ctx, target)
	if err != nil || thread.CurrentCycle.ResolvedAt == nil {
		t.Fatalf("delayed flush reopened: %+v %v", thread, err)
	}
}

func TestIncidentPruningActivity(t *testing.T) {
	s, ctx := queueStore(t, nil)
	admin, g, c := incidentErrorFixture(t, s)
	target := IncidentTarget{Kind: "error", PublicID: g.PublicID}
	msg, err := s.CreateIncidentMessage(ctx, target, c.PublicID, "Keep unresolved investigation", admin)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-400 * 24 * time.Hour)
	if _, err := s.DB.NewUpdate().Model((*appErrorRecord)(nil)).Set("first_seen_at = ?", old).Set("last_seen_at = ?", old).Where("id = ?", g.ID).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Prune(ctx, PrunableAppErrors, time.Now().Add(-30*24*time.Hour), 100); err != nil || n != 0 {
		t.Fatalf("unresolved discussion pruned %d %v", n, err)
	}
	// Set a legacy closure to exercise retention independently of snapshot.
	if err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewUpdate().Model((*appErrorRecord)(nil)).Set("resolved_at = ?", old).Where("id = ?", g.ID).Exec(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Prune(ctx, PrunableAppErrors, time.Now().Add(-30*24*time.Hour), 100); err != nil || n != 0 {
		t.Fatalf("recent comment pruned %d %v", n, err)
	}
	if _, err := s.DB.NewUpdate().Model((*IncidentMessage)(nil)).Set("created_at = ?", old).Where("public_id = ?", msg.PublicID).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Prune(ctx, PrunableAppErrors, time.Now().Add(-30*24*time.Hour), 100); err != nil || n != 1 {
		t.Fatalf("expired thread not pruned %d %v", n, err)
	}
	if _, err := s.FindIncidentThread(ctx, target); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("parent survived cascade: %v", err)
	}
}
