package seed

import (
	"context"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/database/dbtest"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/monitor"
	"lewcontext/api/internal/store"
	"strings"
	"testing"
	"time"
)

func migrated(t *testing.T) *bun.DB {
	t.Helper()
	db := dbtest.Open(t)
	if _, err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}
func run(t *testing.T, db *bun.DB, name string, o Options) string {
	t.Helper()
	seeder, ok := Find(name)
	if !ok {
		t.Fatalf("no seeder %q", name)
	}
	if o.PasswordHash == "" {
		o.PasswordHash = "fake-hash"
	}
	summary, err := seeder.Run(context.Background(), db, factory.TestFaker(t), o)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return summary
}
func projectsOf(t *testing.T, db *bun.DB, email string) int {
	t.Helper()
	count, err := db.NewSelect().Model((*factory.Project)(nil)).
		Join("JOIN workspace_user AS m ON m.workspace_id = p.workspace_id").
		Join("JOIN users AS u ON u.id = m.user_id").
		Where("u.email = ?", email).
		Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return int(count)
}
func TestDemoRunsOnce(t *testing.T) {
	db := migrated(t)
	run(t, db, "demo", Options{})
	if summary := run(t, db, "demo", Options{}); !strings.Contains(summary, "already exists") {
		t.Fatalf("second demo run changed data: %s", summary)
	}
	if n := projectsOf(t, db, DemoEmail); n != len(demoProjects) {
		t.Fatalf("demo projects: want %d got %d", len(demoProjects), n)
	}
	var names []string
	err := db.NewSelect().Model((*factory.Workspace)(nil)).Column("w.name").
		Join("JOIN workspace_user AS m ON m.workspace_id = w.id").
		Join("JOIN users AS u ON u.id = m.user_id").
		Where("u.email = ? AND m.role = 'owner'", DemoEmail).
		Scan(context.Background(), &names)
	if err != nil || len(names) != 1 || names[0] != DemoWorkspace {
		t.Fatalf("demo workspace: %v (%v)", names, err)
	}
	var roles []string
	var hashes []string
	err = db.NewSelect().Model((*factory.WorkspaceUser)(nil)).ColumnExpr("m.role, u.password_hash").
		Join("JOIN users AS u ON u.id = m.user_id").
		Where("u.email <> ?", DemoEmail).
		OrderExpr("m.created_at").
		Scan(context.Background(), &roles, &hashes)
	if err != nil || strings.Join(roles, ",") != "admin,member,member,viewer" {
		t.Fatalf("demo members: %v (%v)", roles, err)
	}
	for _, hash := range hashes {
		if auth.CheckPassword(factory.DefaultPassword, hash) || auth.CheckPassword("fake-hash", hash) || !strings.HasPrefix(hash, "unusable:") {
			t.Fatalf("a demo member can sign in: %q", hash)
		}
	}
}
func TestVolumeAddsToOneAccount(t *testing.T) {
	db := migrated(t)
	ctx := context.Background()
	f := factory.TestFaker(t)
	users, err := factory.Users.Create(ctx, db, f, 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range users[:2] {
		if _, err := factory.WorkspaceFor(ctx, db, f, user.ID); err != nil {
			t.Fatal(err)
		}
	}
	run(t, db, "volume", Options{Email: strings.ToUpper(users[0].Email), Count: 37})
	if n := projectsOf(t, db, users[0].Email); n != 37 {
		t.Fatalf("target account: want 37 projects got %d", n)
	}
	if n := projectsOf(t, db, users[1].Email); n != 0 {
		t.Fatalf("volume touched another account: %d projects", n)
	}
	seeder, _ := Find("volume")
	if _, err := seeder.Run(ctx, db, f, Options{Email: "nobody@example.test"}); err == nil {
		t.Fatal("volume seeded an unknown account")
	}
	if _, err := seeder.Run(ctx, db, f, Options{Email: users[2].Email}); err == nil {
		t.Fatal("volume seeded an account without a workspace")
	}
}
func TestUsersCreatesAccountsWithProjects(t *testing.T) {
	db := migrated(t)
	run(t, db, "users", Options{Count: 4, Projects: 3})
	ctx := context.Background()
	users, err := db.NewSelect().Model((*factory.User)(nil)).Count(ctx)
	if err != nil || users != 4 {
		t.Fatalf("want 4 users got %d (%v)", users, err)
	}
	projects, err := db.NewSelect().Model((*factory.Project)(nil)).Count(ctx)
	if err != nil || projects > 12 {
		t.Fatalf("want at most 12 projects got %d (%v)", projects, err)
	}
	owners, err := db.NewSelect().Model((*factory.WorkspaceUser)(nil)).Where("role = 'owner'").Count(ctx)
	if err != nil || owners != 4 {
		t.Fatalf("want a workspace per user, got %d owners (%v)", owners, err)
	}
}

// The events seeder inserts what it says: catalogue names only, never in
// the future, mostly in the last week, with failed sign-ins among them.
func TestEventsInsertFakeHistory(t *testing.T) {
	db := migrated(t)
	ctx := context.Background()
	if summary := run(t, db, "events", Options{Count: 1500}); !strings.Contains(summary, "1500 fake events") {
		t.Fatalf("summary: %s", summary)
	}
	var rows []factory.Event
	if err := db.NewSelect().Model(&rows).Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1500 {
		t.Fatalf("%d events", len(rows))
	}
	names := map[string]bool{}
	for _, name := range events.Names() {
		names[name] = true
	}
	recent, failed := 0, 0
	for _, e := range rows {
		if !names[e.Name] || e.CreatedAt.After(time.Now()) || e.CreatedAt.Before(time.Now().AddDate(0, 0, -91)) {
			t.Fatalf("event %s at %v", e.Name, e.CreatedAt)
		}
		if e.Payload["seeded"] != true {
			t.Fatalf("event %s not marked seeded: %v", e.Name, e.Payload)
		}
		if e.CreatedAt.After(time.Now().AddDate(0, 0, -7)) {
			recent++
		}
		if e.Name == "auth.login_attempted" && e.Payload["result"] == events.LoginWrongPassword {
			failed++
		}
	}
	if recent < 1300 || failed < 50 {
		t.Fatalf("%d in the last week, %d failed sign-ins", recent, failed)
	}
	if seeder, _ := Find("events"); seeder.MaxFor() != 2_000_000 {
		t.Fatalf("events max: %d", seeder.MaxFor())
	}
}

// The monitoring seeder plants what the Security and Rate limits pages
// look for: each of their main signals is at its level.
func TestMonitoringSeeder(t *testing.T) {
	db := migrated(t)
	if summary := run(t, db, "monitoring", Options{}); !strings.Contains(summary, SeedStuffingIP) {
		t.Fatalf("summary: %s", summary)
	}
	env := monitor.Env{Store: &store.Store{DB: db}, Limits: httpapi.RateLimits, Now: time.Now()}
	results, err := monitor.ReadAll(context.Background(), env, monitor.Signals())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]monitor.Level{
		"auth.stuffing_addresses":        monitor.LevelWarn,
		"auth.stuffing_successes":        monitor.LevelCritical,
		"auth.distributed_guessing":      monitor.LevelWarn,
		"auth.success_after_failures":    monitor.LevelCritical,
		"auth.possible_takeovers":        monitor.LevelWarn,
		"auth.second_factor_failures":    monitor.LevelWarn,
		"admin.door_failures":            monitor.LevelWarn,
		"admin.door_refusals":            monitor.LevelCritical,
		"limits.shared_reached":          monitor.LevelCritical,
		"auth.unknown_account_share":     monitor.LevelOK,
		"auth.reauthentication_failures": monitor.LevelOK,
		// Part 2.
		"ips.sign_up_cluster":           monitor.LevelWarn,
		"ips.busiest_share":             monitor.LevelOK,
		"accounts.workspace_sprees":     monitor.LevelWarn,
		"accounts.invitation_sprees":    monitor.LevelWarn,
		"accounts.low_acceptance":       monitor.LevelWarn,
		"accounts.project_sprees":       monitor.LevelWarn,
		"accounts.links_in_names":       monitor.LevelWarn,
		"accounts.new_and_busy":         monitor.LevelWarn,
		"workspaces.quota_used":         monitor.LevelWarn,
		"workspaces.throwaway_accounts": monitor.LevelWarn,
	}
	values := map[string]float64{"usage.inactive_accounts": 2, "workspaces.quota_used": 90, "ips.sign_up_cluster": 9}
	for _, r := range results {
		if level, ok := want[r.Name]; ok && r.Level != level {
			t.Errorf("%s: %v (%v), want %v", r.Name, r.Level, r.Value, level)
		}
		if value, ok := values[r.Name]; ok && r.Value != value {
			t.Errorf("%s: %v, want %v", r.Name, r.Value, value)
		}
	}
	var unseeded int
	if err := db.NewSelect().Model((*factory.Event)(nil)).ColumnExpr("count(*)").Where("NOT (payload->>'seeded')::boolean").Scan(context.Background(), &unseeded); err != nil || unseeded != 0 {
		t.Fatalf("%d events not marked seeded (%v)", unseeded, err)
	}
}
