package database_test

import (
	"context"
	"errors"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/database/dbtest"
	"lewcontext/api/internal/factory"
	"testing"
)

var appTables = []string{"users", "sessions", "workspaces", "workspace_user", "projects", "rate_limits", "events", "event_deliveries", "workspace_invitations"}

func tableExists(t *testing.T, db *bun.DB, table string) bool {
	t.Helper()
	var exists bool
	if err := db.NewSelect().ColumnExpr("to_regclass(?) IS NOT NULL", table).Scan(context.Background(), &exists); err != nil {
		t.Fatal(err)
	}
	return exists
}
func TestMigrateRollbackRoundTrip(t *testing.T) {
	db := dbtest.Open(t)
	ctx := context.Background()
	applied, err := database.Migrate(ctx, db)
	if err != nil || applied.IsZero() {
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
func TestPublicIDDefaults(t *testing.T) {
	db := dbtest.Open(t)
	ctx := context.Background()
	if _, err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var total, distinct int
	var formatted bool
	ids := db.NewSelect().ColumnExpr("public_id('prj') AS id").TableExpr("generate_series(1, 5000)")
	err := db.NewSelect().
		ColumnExpr("count(*), count(DISTINCT id), bool_and(id ~ ?)", "^prj_[0-9A-Za-z]{14}$").
		TableExpr("(?) AS ids", ids).
		Scan(ctx, &total, &distinct, &formatted)
	if err != nil || total != 5000 || distinct != 5000 || !formatted {
		t.Fatalf("public_id: %d generated, %d distinct, formatted %v (%v)", total, distinct, formatted, err)
	}
	short := map[string]any{"public_id": "usr_short", "name": "A", "email": "a@example.com", "password_hash": "x"}
	if _, err := db.NewInsert().Model(&short).TableExpr("users").Exec(ctx); err == nil {
		t.Fatal("malformed public_id accepted")
	}
}

// Event names are <resource>.<fact>: two or three snake_case segments.
func TestEventNameCheck(t *testing.T) {
	db := dbtest.Open(t)
	ctx := context.Background()
	if _, err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	f := factory.TestFaker(t)
	named := func(name string) error {
		_, err := factory.Events.State(func(e *factory.Event) { e.Name = name }).CreateOne(ctx, db, f)
		return err
	}
	for _, name := range []string{"auth.logged_in", "workspace.member.removed"} {
		if err := named(name); err != nil {
			t.Errorf("%q refused: %v", name, err)
		}
	}
	for _, name := range []string{"workspace.member.removed.again", "workspace", "Workspace.created", "workspace..created", "workspace.1created"} {
		if err := named(name); err == nil {
			t.Errorf("%q accepted", name)
		}
	}
}
func TestFreshRebuildsTheSchema(t *testing.T) {
	db := dbtest.Open(t)
	ctx := context.Background()
	if _, err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	gone := map[string]any{"name": "Gone", "email": "gone@example.com", "password_hash": "x"}
	if _, err := db.NewInsert().Model(&gone).TableExpr("users").Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Fresh(ctx, db, true); !errors.Is(err, database.ErrFreshInProduction) {
		t.Fatalf("fresh ran in production: %v", err)
	}
	preview, err := database.FreshPreview(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]int64{}
	for _, table := range preview {
		rows[table.Name] = table.Rows
	}
	if rows["users"] != 1 || len(preview) < len(appTables) {
		t.Fatalf("preview: %+v", preview)
	}
	group, err := database.Fresh(ctx, db, false)
	if err != nil || group.IsZero() {
		t.Fatalf("fresh: %s %v", group, err)
	}
	users, err := db.NewSelect().Table("users").Count(ctx)
	if err != nil || users != 0 {
		t.Fatalf("fresh kept %d users (%v)", users, err)
	}
	for _, table := range appTables {
		if !tableExists(t, db, table) {
			t.Fatalf("%s missing after fresh", table)
		}
	}
}
