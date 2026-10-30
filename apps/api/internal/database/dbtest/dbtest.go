// Package dbtest gives PostgreSQL integration tests a private schema.
package dbtest

import (
	"context"
	"fmt"
	"forma/api/internal/database"
	"github.com/uptrace/bun"
	"net/url"
	"os"
	"testing"
	"time"
)

// Open connects to TEST_DATABASE_URL with search_path pinned to a new unique schema that is
// dropped at cleanup. Existing application data is never touched. Skips when the URL is unset.
func Open(t *testing.T) *bun.DB {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx := context.Background()
	admin, err := database.Open(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("forma_test_%d", time.Now().UnixNano())
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
