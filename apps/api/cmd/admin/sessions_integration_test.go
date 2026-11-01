package main

import (
	"bytes"
	"context"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/database/dbtest"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/store"
	"strings"
	"testing"
)

// migrated is a store on a migrated database of the test's own.
func migrated(t *testing.T) *store.Store {
	t.Helper()
	db := dbtest.Open(t)
	if _, err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return &store.Store{DB: db}
}

// sessions revoke-all previews without -confirm, changing nothing; with it,
// every app and admin session is gone, and every sign-in waiting for a code
// (so a password typed before can't become a session after), and one
// auth.sessions_revoked_all is recorded with the counts, with no actor.
func TestRevokeAllSessions(t *testing.T) {
	repository := migrated(t)
	ctx := context.Background()
	f := factory.TestFaker(t)
	users := must(factory.Users.Create(ctx, repository.DB, f, 2))(t)
	for _, u := range users {
		must(factory.SessionFor(ctx, repository.DB, f, u.ID))(t)
	}
	must(factory.Admins.State(factory.AdminFor(users[0].ID)).CreateOne(ctx, repository.DB, f))(t)
	must(factory.AdminSessions.State(factory.AdminSessionFor(users[0].ID)).CreateOne(ctx, repository.DB, f))(t)
	must(factory.MFAChallenges.State(factory.ChallengeFor(users[1].ID)).CreateOne(ctx, repository.DB, f))(t)
	must(factory.MFAChallenges.State(factory.ChallengeFor(users[0].ID)).State(factory.Purpose("admin")).CreateOne(ctx, repository.DB, f))(t)
	count := func(table string) int {
		return int(must(repository.DB.NewSelect().TableExpr(table).Count(ctx))(t))
	}
	var out bytes.Buffer
	if err := revokeAllSessions(ctx, repository, false, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "2 app sessions and 1 admin sessions, and end 2 sign-ins") || count("sessions") != 2 || count("admin_sessions") != 1 || count("mfa_challenges") != 2 || count("events") != 0 {
		t.Fatalf("the preview: %s", out.String())
	}
	out.Reset()
	if err := revokeAllSessions(ctx, repository, true, &out); err != nil {
		t.Fatal(err)
	}
	if count("sessions") != 0 || count("admin_sessions") != 0 || count("mfa_challenges") != 0 || !strings.Contains(out.String(), "Signed everyone out") {
		t.Fatalf("after: %s", out.String())
	}
	var payload string
	var actor *int64
	err := repository.DB.NewSelect().TableExpr("events").Column("payload", "actor_id").Where("name = 'auth.sessions_revoked_all'").Scan(ctx, &payload, &actor)
	if err != nil || actor != nil || !strings.Contains(payload, `"sessions": 2`) || !strings.Contains(payload, `"adminSessions": 1`) || !strings.Contains(payload, `"challenges": 2`) || count("events") != 1 {
		t.Fatalf("the event: %s %v %v", payload, actor, err)
	}
}

// suspend previews, then suspends (by: cli, the reason in auth.suspended);
// it refuses an admin (revoke first). unsuspend lifts it.
func TestSuspendFromTheCommandLine(t *testing.T) {
	repository := migrated(t)
	ctx := context.Background()
	f := factory.TestFaker(t)
	user := must(factory.Users.CreateOne(ctx, repository.DB, f))(t)
	must(factory.SessionFor(ctx, repository.DB, f, user.ID))(t)
	const why = "Spam from this account, ticket 4521."
	var out bytes.Buffer
	if err := suspendAccount(ctx, repository, user.Email, why, false, &out); err != nil || !strings.Contains(out.String(), "Would suspend") {
		t.Fatalf("preview: %s %v", out.String(), err)
	}
	if found, _, _ := repository.FindUser(ctx, user.Email); found.Suspended {
		t.Fatal("the preview suspended")
	}
	if err := suspendAccount(ctx, repository, user.Email, why, true, &out); err != nil {
		t.Fatal(err)
	}
	if found, _, _ := repository.FindUser(ctx, user.Email); !found.Suspended {
		t.Fatal("not suspended")
	}
	var payload string
	must(0, repository.DB.NewSelect().TableExpr("events").Column("payload").Where("name = 'auth.suspended'").Scan(ctx, &payload))(t)
	if !strings.Contains(payload, `"by": "cli"`) || !strings.Contains(payload, why) || !strings.Contains(payload, `"sessionsRevoked": 1`) {
		t.Fatalf("auth.suspended: %s", payload)
	}
	if err := suspendAccount(ctx, repository, user.Email, why, true, &out); err == nil || !strings.Contains(err.Error(), "already suspended") {
		t.Fatalf("again: %v", err)
	}
	admin := must(factory.Users.CreateOne(ctx, repository.DB, f))(t)
	must(factory.Admins.State(factory.AdminFor(admin.ID)).CreateOne(ctx, repository.DB, f))(t)
	if err := suspendAccount(ctx, repository, admin.Email, why, true, &out); err == nil || !strings.Contains(err.Error(), "revoke admin first") {
		t.Fatalf("an admin: %v", err)
	}
	if err := unsuspendAccount(ctx, repository, user.Email, why, true, &out); err != nil {
		t.Fatal(err)
	}
	if found, _, _ := repository.FindUser(ctx, user.Email); found.Suspended {
		t.Fatal("still suspended")
	}
	if err := unsuspendAccount(ctx, repository, user.Email, why, true, &out); err == nil {
		t.Fatal("unsuspended twice")
	}
	if _, _, _, err := suspensionFlags("suspend", []string{"-email", user.Email, "-reason", "short"}); err == nil {
		t.Fatal("a short reason")
	}
}

// must is apitest.Must, which this package can't import (it isn't an
// httpapi test).
func must[T any](v T, err error) func(testing.TB) T {
	return func(t testing.TB) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}
