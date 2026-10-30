package database_test

import (
	"context"
	"forma/api/internal/database"
	"forma/api/internal/database/dbtest"
	"forma/api/migrations"
	"github.com/uptrace/bun"
	"io/fs"
	"testing"
)

var appTables = []string{"users", "sessions", "projects", "auth_attempts"}

func tableExists(t *testing.T, db *bun.DB, table string) bool {
	t.Helper()
	var exists bool
	if err := db.NewRaw("SELECT to_regclass(?) IS NOT NULL", table).Scan(context.Background(), &exists); err != nil {
		t.Fatal(err)
	}
	return exists
}
func TestMigrateRollbackRoundTrip(t *testing.T) {
	db := dbtest.Open(t)
	ctx := context.Background()
	applied, err := database.Migrate(ctx, db)
	if err != nil || len(applied.Migrations) != len(appTables) {
		t.Fatalf("migrate: %s %v", applied, err)
	}
	rolled, err := database.Rollback(ctx, db)
	if err != nil || rolled.ID != applied.ID {
		t.Fatalf("rollback: %s %v", rolled, err)
	}
	for _, table := range appTables {
		if tableExists(t, db, table) {
			t.Fatalf("%s survived rollback", table)
		}
	}
	status, err := database.Status(ctx, db)
	if err != nil || len(status.Applied()) != 0 {
		t.Fatalf("status after rollback: %s %v", status, err)
	}
	if _, err = database.Migrate(ctx, db); err != nil {
		t.Fatal("re-apply after rollback:", err)
	}
	for _, table := range appTables {
		if !tableExists(t, db, table) {
			t.Fatalf("%s missing after re-apply", table)
		}
	}
}
func TestMigrateAdoptsLegacyRunnerHistory(t *testing.T) {
	db := dbtest.Open(t)
	ctx := context.Background()
	// Recreate what the previous runner left behind, including real data. Its single
	// 001_initial.up.sql held exactly the SQL of today's per-table up migrations.
	statements := []string{"CREATE TABLE schema_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())"}
	ups, err := fs.Glob(migrations.Files, "00[1-4]_*.tx.up.sql")
	if err != nil || len(ups) != len(appTables) {
		t.Fatalf("initial migrations: %v %v", ups, err)
	}
	for _, file := range ups {
		sql, err := migrations.Files.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		statements = append(statements, string(sql))
	}
	statements = append(statements,
		"INSERT INTO schema_migrations(version) VALUES('001_initial.up.sql')",
		"INSERT INTO users(name,email,password_hash) VALUES('Kept','kept@example.com','hash')",
	)
	for _, statement := range statements {
		if _, err = db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if group, err := database.Migrate(ctx, db); err != nil || !group.IsZero() {
			t.Fatalf("run %d: legacy migration re-applied: %s %v", i+1, group, err)
		}
	}
	status, err := database.Status(ctx, db)
	if err != nil || len(status) != len(appTables) || len(status.Applied()) != len(appTables) || status.LastGroupID() != 1 {
		t.Fatalf("legacy history not adopted: %s %v", status, err)
	}
	if tableExists(t, db, "schema_migrations") || !tableExists(t, db, "schema_migrations_legacy") {
		t.Fatal("legacy table not renamed")
	}
	users, err := db.NewSelect().Table("users").Count(ctx)
	if err != nil || users != 1 {
		t.Fatalf("existing data changed: %d users (%v)", users, err)
	}
}
