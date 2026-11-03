package store

import (
	"context"
	"database/sql"
	"errors"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/factory"
	"testing"
	"time"
)

// lastSeen reads an account's users.last_seen_at.
func lastSeen(t *testing.T, s *Store, userID int64) *time.Time {
	t.Helper()
	var at *time.Time
	if err := s.DB.NewSelect().Model((*User)(nil)).Column("last_seen_at").Where("id = ?", userID).Scan(context.Background(), &at); err != nil {
		t.Fatal(err)
	}
	return at
}

// The session check moves users.last_seen_at at most once an hour per
// account, in the same statement, and never for a session that isn't live
// nor a suspended account's.
func TestSessionCheckMovesLastSeenHourly(t *testing.T) {
	s, ctx := queueStore(t, nil)
	f := factory.TestFaker(t)
	user, err := factory.Users.CreateOne(ctx, s.DB, f)
	if err != nil {
		t.Fatal(err)
	}
	session, err := factory.SessionFor(ctx, s.DB, f, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if lastSeen(t, s, user.ID) != nil {
		t.Fatal("a new account has been seen")
	}
	found, err := s.FindSession(ctx, auth.Digest(session.Token))
	if err != nil || found.User.ID != user.ID {
		t.Fatalf("session: %+v %v", found, err)
	}
	first := lastSeen(t, s, user.ID)
	if first == nil || time.Since(*first) > time.Minute {
		t.Fatalf("first request: last seen %v", first)
	}

	// Within the hour, more requests leave it alone.
	recent := time.Now().Add(-59 * time.Minute).UTC().Truncate(time.Microsecond)
	if _, err := s.DB.NewUpdate().Model((*User)(nil)).Set("last_seen_at = ?", recent).Where("id = ?", user.ID).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := s.FindSession(ctx, auth.Digest(session.Token)); err != nil {
			t.Fatal(err)
		}
	}
	if got := lastSeen(t, s, user.ID); got == nil || !got.Equal(recent) {
		t.Fatalf("within the hour: %v, want %v", got, recent)
	}

	// Over an hour old, the next request moves it.
	stale := time.Now().Add(-61 * time.Minute)
	if _, err := s.DB.NewUpdate().Model((*User)(nil)).Set("last_seen_at = ?", stale).Where("id = ?", user.ID).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FindSession(ctx, auth.Digest(session.Token)); err != nil {
		t.Fatal(err)
	}
	if got := lastSeen(t, s, user.ID); got == nil || time.Since(*got) > time.Minute {
		t.Fatalf("after an hour: %v", got)
	}

	// An expired session moves nothing, and nobody else's account moves.
	other, err := factory.Users.CreateOne(ctx, s.DB, f)
	if err != nil {
		t.Fatal(err)
	}
	expired, err := factory.Sessions.State(func(x *factory.Session) { x.UserID = other.ID }).State(factory.Expired).CreateOne(ctx, s.DB, f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.FindSession(ctx, auth.Digest(expired.Token)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expired session: %v", err)
	}
	if lastSeen(t, s, other.ID) != nil {
		t.Fatal("an expired session moved last_seen_at")
	}

	// A suspended account's live session isn't found (the suspension's
	// backstop, in the same statement), so it isn't seen either.
	suspended, err := factory.Users.CreateOne(ctx, s.DB, f)
	if err != nil {
		t.Fatal(err)
	}
	live, err := factory.SessionFor(ctx, s.DB, f, suspended.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := factory.UserSuspensions.State(factory.SuspensionOf(suspended.ID)).CreateOne(ctx, s.DB, f); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FindSession(ctx, auth.Digest(live.Token)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("a suspended account's session: %v", err)
	}
	if lastSeen(t, s, suspended.ID) != nil {
		t.Fatal("a suspended account's session moved last_seen_at")
	}
}

// The session check never waits for the account's row: a transaction
// holding it (as changing the password does, before deleting the
// sessions) leaves last_seen_at alone, and the check answers at once.
// Waiting would deadlock with that transaction's delete.
func TestSessionCheckSkipsALockedAccount(t *testing.T) {
	s, ctx := queueStore(t, nil)
	f := factory.TestFaker(t)
	user, err := factory.Users.CreateOne(ctx, s.DB, f)
	if err != nil {
		t.Fatal(err)
	}
	session, err := factory.SessionFor(ctx, s.DB, f, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.NewSelect().Model((*User)(nil)).Column("id").Where("id = ?", user.ID).For("UPDATE").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	quick, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	found, err := s.FindSession(quick, auth.Digest(session.Token))
	if err != nil || found.User.ID != user.ID {
		t.Fatalf("session check under a locked account: %v", err)
	}
	if lastSeen(t, s, user.ID) != nil {
		t.Fatal("last seen moved under someone else's lock")
	}
	// The holder goes on to delete the sessions, which FindSession's
	// statement no longer holds, and commits.
	if _, err := tx.NewDelete().Model((*sessionRecord)(nil)).Where("user_id = ?", user.ID).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// Free again, the next check moves it.
	if _, err := factory.SessionFor(ctx, s.DB, f, user.ID); err != nil {
		t.Fatal(err)
	}
}
