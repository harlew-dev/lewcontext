package database_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/database/dbtest"
	"lewcontext/api/internal/observe"
	"lewcontext/api/internal/store"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// The rollup's lock is exclusive: while one transaction holds it, another
// connection's try fails at once (it never waits), and it can be taken again
// once the holder commits. Servers share it through their schema, so a test
// in another schema doesn't contend.
func TestRollupLockIsExclusive(t *testing.T) {
	db := dbtest.Open(t)
	ctx := context.Background()
	holder, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback()
	if locked, err := database.TryLockMetricsRollup(ctx, holder); err != nil || !locked {
		t.Fatalf("first: %v %v", locked, err)
	}
	try := func() bool {
		t.Helper()
		var locked bool
		err := db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) (err error) {
			locked, err = database.TryLockMetricsRollup(ctx, tx)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return locked
	}
	start := time.Now()
	if try() {
		t.Fatal("a second server took the lock while the first held it")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("the try waited for the lock")
	}
	if err := holder.Commit(); err != nil {
		t.Fatal(err)
	}
	if !try() {
		t.Fatal("the lock wasn't released on commit")
	}
}

// A slow query is logged at warn with its operation, table, duration and
// store method, never its text or values; its error is scrubbed. Every
// query is counted in db_query_duration_ms by operation.
func TestSlowQueriesLogNoValues(t *testing.T) {
	db := dbtest.Open(t)
	ctx := context.Background()
	if _, err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(observe.NewLogger(&out, observe.FormatJSON, slog.LevelInfo))
	t.Cleanup(func() { slog.SetDefault(previous) })
	db.AddQueryHook(database.QueryHook{Slow: time.Nanosecond})

	s := &store.Store{DB: db}
	if _, _, err := s.FindUser(ctx, "ada.lovelace@example.com"); err == nil {
		t.Fatal("found a user that doesn't exist")
	}
	// A failing query whose error echoes its value.
	var n int
	_ = db.NewSelect().ColumnExpr("?::int", "secret-value-42 ada@example.org").Scan(ctx, &n)

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("lines:\n%s", out.String())
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if first["msg"] != "slow query" || first["level"] != "WARN" || first["operation"] != "select" || first["table"] != "users" ||
		first["method"] != "FindUser" || first["durationMs"] == nil {
		t.Fatalf("slow query line: %s", lines[0])
	}
	for _, value := range []string{"ada.lovelace", "ada@", "example.", "secret-value", "SELECT", "WHERE"} {
		if strings.Contains(out.String(), value) {
			t.Errorf("the log holds %q:\n%s", value, out.String())
		}
	}
	if !strings.Contains(lines[len(lines)-1], `"error":`) {
		t.Errorf("the failed query's line has no error: %s", lines[len(lines)-1])
	}
	var metrics bytes.Buffer
	if err := observe.Metrics.WritePrometheus(&metrics); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(metrics.String(), `db_query_duration_ms_count{operation="select"}`) || !strings.Contains(metrics.String(), `db_slow_queries_total{table="users"}`) {
		t.Fatalf("metrics:\n%s", metrics.String())
	}
}

// CountMigrations counts what's applied and embedded, without creating
// anything in a database that has never been migrated.
func TestCountMigrations(t *testing.T) {
	db := dbtest.Open(t)
	ctx := context.Background()
	applied, embedded, err := database.CountMigrations(ctx, db)
	if err != nil || applied != 0 || embedded < 25 {
		t.Fatalf("before: %d %d %v", applied, embedded, err)
	}
	if exists := tableExists(t, db, "bun_migrations"); exists {
		t.Fatal("counting created bun_migrations")
	}
	if _, err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if applied, embedded2, err := database.CountMigrations(ctx, db); err != nil || applied != embedded || embedded2 != embedded {
		t.Fatalf("after: %d of %d %v", applied, embedded, err)
	}
}
