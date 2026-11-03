package database_test

import (
	"context"
	"lewcontext/api/internal/database/dbtest"
	"testing"
)

// Every connection is UTC (database.Open), even when the environment asks
// pgx for another TimeZone (PGTZ, or timezone in DATABASE_URL), so date
// arithmetic and printed times don't depend on where the server runs.
func TestConnectionsAreUTC(t *testing.T) {
	t.Setenv("PGTZ", "America/New_York")
	db := dbtest.Open(t)
	var zone string
	if err := db.NewSelect().ColumnExpr("current_setting('TimeZone')").Scan(context.Background(), &zone); err != nil || zone != "UTC" {
		t.Fatalf("TimeZone %q (%v)", zone, err)
	}
}
