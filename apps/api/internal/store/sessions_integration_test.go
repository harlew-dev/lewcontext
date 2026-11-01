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
// account, in the same statement, and never for a session that isn't live.
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
}
