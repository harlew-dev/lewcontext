// Package dbtest gives PostgreSQL integration tests a private schema.
package dbtest

import (
	"context"
	"fmt"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/database"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Open connects to the DATABASE_URL in .env.testing with search_path pinned to a new unique
// schema that is dropped at cleanup. Existing application data is never touched. The
// database must be running: the test fails rather than skips without it.
func Open(t *testing.T) *bun.DB {
	t.Helper()
	raw := testEnv(t)["DATABASE_URL"]
	if err := localTestDatabase(raw); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	admin, err := database.Open(ctx, raw)
	if err != nil {
		t.Fatalf("the test database isn't reachable (start it with make db): %v", err)
	}
	schema := fmt.Sprintf("lewcontext_test_%d", time.Now().UnixNano())
	if _, err = admin.ExecContext(ctx, "CREATE SCHEMA ?", bun.Ident(schema)); err != nil {
		t.Fatal(err)
	}
	scoped, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	query := scoped.Query()
	query.Set("search_path", schema)
	scoped.RawQuery = query.Encode()
	db, err := database.Open(ctx, scoped.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		_, _ = admin.ExecContext(ctx, "DROP SCHEMA ? CASCADE", bun.Ident(schema))
		admin.Close()
	})
	// Tests run down migrations, so refuse anything but the temporary schema.
	var current string
	if err = db.NewSelect().ColumnExpr("current_schema()").Scan(ctx, &current); err != nil || current != schema {
		t.Fatalf("test schema not active: got %q (%v)", current, err)
	}
	return db
}

// Explain is PostgreSQL's plan for q (a Bun query, written out by its
// String), one line per node, with sequential scans discouraged as on a
// table big enough to matter (a test's few rows would otherwise be read
// whole), so a test can check that a query finds its rows through an
// index. Nothing runs: EXPLAIN without ANALYZE only plans, inside a
// transaction rolled back.
func Explain(t *testing.T, db *bun.DB, q fmt.Stringer) string {
	t.Helper()
	ctx := context.Background()
	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.QueryContext(ctx, "EXPLAIN "+q.String())
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(plan, "\n")
}
