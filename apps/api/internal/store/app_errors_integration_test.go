package store

import (
	"context"
	"errors"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/observe"
	"strings"
	"testing"
	"time"
)

// errorRows reads app_errors by fingerprint.
func errorRows(t *testing.T, s *Store) map[string]appErrorRecord {
	t.Helper()
	var rows []appErrorRecord
	if err := s.DB.NewSelect().Model(&rows).Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	byFingerprint := map[string]appErrorRecord{}
	for _, r := range rows {
		byFingerprint[r.Fingerprint] = r
	}
	return byFingerprint
}

// Groups are inserted once and then counted: one row per fingerprint,
// however many flushes; a resolved group that happens again is open again.
func TestRecordErrorsUpsertsOneRowPerGroup(t *testing.T) {
	s, ctx := queueStore(t, nil)
	stored := func(groups []observe.ErrorGroup) {
		t.Helper()
		if err := s.RecordErrors(ctx, groups); err != nil {
			t.Fatal(err)
		}
	}
	collector := observe.NewCollector(s.RecordErrors, "abc123def456")
	start := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	now := start
	collector.Now = func() time.Time { return now }
	report := func(requestID string) {
		collector.Report(ctx, observe.Problem{Source: observe.SourceRequest, Route: "GET /api/workspaces", RequestID: requestID,
			Err: errors.New(`ERROR: invalid input syntax for type uuid: "ada@example.com" (SQLSTATE 22P02)`)})
	}
	report("req_0123456789abcd")
	now = now.Add(time.Minute)
	report("req_ZZZZZZZZZZZZZZ")
	if err := collector.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	rows := errorRows(t, s)
	if len(rows) != 1 {
		t.Fatalf("%d rows", len(rows))
	}
	var first appErrorRecord
	for _, r := range rows {
		first = r
	}
	if first.Count != 2 || first.Message != `ERROR: invalid input syntax for type uuid: "…" (SQLSTATE 22P02)` || first.LastRequestID != "req_ZZZZZZZZZZZZZZ" ||
		!first.FirstSeenAt.Equal(start) || !first.LastSeenAt.Equal(start.Add(time.Minute)) || first.LastVersion != "abc123def456" || first.Source != "request" {
		t.Fatalf("first flush: %+v", first)
	}

	// Resolved by an admin, then it happens again: counted and open.
	if _, err := s.DB.NewUpdate().Model((*appErrorRecord)(nil)).Set("resolved_at = now()").Where("id = ?", first.ID).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	for range 3 {
		report("")
	}
	if err := collector.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	again := errorRows(t, s)[first.Fingerprint]
	if len(errorRows(t, s)) != 1 || again.Count != 5 || !again.ResolvedAt.IsZero() || again.LastRequestID != "" ||
		!again.FirstSeenAt.Equal(start) || !again.LastSeenAt.Equal(start.Add(2*time.Minute)) {
		t.Fatalf("second flush: %+v", again)
	}

	// An older flush (a server whose flush was delayed) adds its count but
	// doesn't move the latest occurrence back.
	older := observe.ErrorGroup{Fingerprint: first.Fingerprint, Source: first.Source, Route: first.Route, Message: first.Message, Stack: "old.f(…)",
		Count: 4, FirstSeen: start.Add(-time.Hour), LastSeen: start, LastRequestID: "req_0000000000000A", LastVersion: "old"}
	stored([]observe.ErrorGroup{older})
	late := errorRows(t, s)[first.Fingerprint]
	if late.Count != 9 || !late.FirstSeenAt.Equal(start.Add(-time.Hour)) || !late.LastSeenAt.Equal(start.Add(2*time.Minute)) ||
		late.LastRequestID != "" || late.LastVersion != "abc123def456" || late.Stack != "" {
		t.Fatalf("a late flush: %+v", late)
	}
	stored(nil) // nothing to do
}

// The table's CHECKs hold what the collector promises: a fingerprint's
// shape, a known source, bounded text, our own request IDs only.
func TestAppErrorChecks(t *testing.T) {
	s, ctx := queueStore(t, nil)
	f := factory.TestFaker(t)
	if _, err := factory.AppErrors.Create(ctx, s.DB, f, 3); err != nil {
		t.Fatal(err)
	}
	for name, state := range map[string]func(*factory.AppError){
		"fingerprint":     func(e *factory.AppError) { e.Fingerprint = "not-hex" },
		"source":          func(e *factory.AppError) { e.Source = "browser" },
		"empty message":   func(e *factory.AppError) { e.Message = "" },
		"long message":    func(e *factory.AppError) { e.Message = strings.Repeat("x", 1001) },
		"proxy's request": func(e *factory.AppError) { e.LastRequestID = "abc-123" },
		"version":         func(e *factory.AppError) { e.LastVersion = "<script>" },
	} {
		if _, err := factory.AppErrors.State(state).CreateOne(ctx, s.DB, f); err == nil {
			t.Errorf("%s: a bad row was stored", name)
		}
	}
}

// One bad group never stops the others, nor every later flush: a group
// that breaks the table's rules (a NUL, which PostgreSQL can't store; a
// control character in its route; an unknown source) is never sent, and
// one the database refuses for its data (a time out of its range) is
// dropped after the others are stored one by one. Neither comes back for
// a retry; a transient failure would (observe's collector tests).
func TestRecordErrorsDropsBadGroups(t *testing.T) {
	s, ctx := queueStore(t, nil)
	now := time.Now().UTC().Truncate(time.Microsecond)
	group := func(message string) observe.ErrorGroup {
		return observe.ErrorGroup{Fingerprint: observe.Fingerprint(observe.SourceRequest, "GET /x", message), Source: observe.SourceRequest,
			Route: "GET /x", Message: message, Count: 1, FirstSeen: now, LastSeen: now}
	}
	good, alsoGood := group("first"), group("second")
	nul := group("nul")
	nul.Stack = "\tx\x00.go:1"
	control := group("control")
	control.Route = "GET /x\r\nforged"
	source := group("source")
	source.Source = "browser"
	err := s.RecordErrors(ctx, []observe.ErrorGroup{good, nul, control, source})
	var partial *observe.FlushError
	if !errors.As(err, &partial) || partial.Dropped != 3 || len(partial.Retry) != 0 {
		t.Fatalf("invalid groups: %v", err)
	}
	outOfRange := group("out of range")
	outOfRange.FirstSeen = time.Date(300000, 1, 1, 0, 0, 0, 0, time.UTC)
	err = s.RecordErrors(ctx, []observe.ErrorGroup{alsoGood, outOfRange})
	if !errors.As(err, &partial) || partial.Dropped != 1 || len(partial.Retry) != 0 {
		t.Fatalf("a group the database refuses: %v", err)
	}
	rows := errorRows(t, s)
	if len(rows) != 2 || rows[good.Fingerprint].Message != "first" || rows[alsoGood.Fingerprint].Message != "second" {
		t.Fatalf("stored: %+v", rows)
	}
}
