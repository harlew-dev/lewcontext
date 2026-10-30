package seed

import (
	"context"
	"forma/api/internal/database"
	"forma/api/internal/database/dbtest"
	"forma/api/internal/factory"
	"github.com/uptrace/bun"
	"strings"
	"testing"
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
		Join("JOIN users AS u ON u.id = p.user_id").
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
}
func TestVolumeAddsToOneAccount(t *testing.T) {
	db := migrated(t)
	ctx := context.Background()
	f := factory.TestFaker(t)
	users, err := factory.Users.Create(ctx, db, f, 2)
	if err != nil {
		t.Fatal(err)
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
}
