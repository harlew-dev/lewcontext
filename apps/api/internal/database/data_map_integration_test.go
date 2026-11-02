package database_test

import (
	"context"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/database/dbtest"
	"lewcontext/api/internal/store"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// dataMapRow is a row of docs/compliance.md's data map: its first cell is
// a table's name in backquotes.
var dataMapRow = regexp.MustCompile("^\\| `([a-z_]+)` \\|")

// dataMapTables reads the tables docs/compliance.md's "Data map" section
// lists.
func dataMapTables(t *testing.T) []string {
	t.Helper()
	text, err := os.ReadFile("../../../../docs/compliance.md")
	if err != nil {
		t.Fatal(err)
	}
	_, section, found := strings.Cut(string(text), "\n## Data map\n")
	if !found {
		t.Fatal("docs/compliance.md has no ## Data map section")
	}
	section, _, _ = strings.Cut(section, "\n## ")
	var tables []string
	for _, line := range strings.Split(section, "\n") {
		if m := dataMapRow.FindStringSubmatch(line); m != nil {
			tables = append(tables, m[1])
		}
	}
	return tables
}

// Every table the migrations create is in the data map (what personal data
// it holds, why, for how long, who sees it), and the map names no table
// that's gone, as TestEveryTableHasAFactory does for factories. The known
// tables the Monitoring Data page shows (store.DataTables) are the same
// list: a table name from PostgreSQL's catalog is shown only when it's
// there.
func TestEveryTableIsInTheDataMap(t *testing.T) {
	db := dbtest.Open(t)
	ctx := context.Background()
	if _, err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var tables []string
	err := db.NewSelect().TableExpr("information_schema.tables").Column("table_name").
		Where("table_schema = current_schema()").
		Where("table_name NOT LIKE 'bun_migration%'").
		Scan(ctx, &tables)
	if err != nil {
		t.Fatal(err)
	}
	mapped := dataMapTables(t)
	if len(tables) < 20 || len(mapped) < 20 {
		t.Fatalf("found %d tables and %d in the data map: too few to trust", len(tables), len(mapped))
	}
	for _, table := range tables {
		if !slices.Contains(mapped, table) {
			t.Errorf("table %q isn't in docs/compliance.md's data map: add a row (its personal data, why, how long, who sees it)", table)
		}
	}
	for _, table := range mapped {
		if !slices.Contains(tables, table) {
			t.Errorf("docs/compliance.md's data map lists %q, which no migration creates", table)
		}
	}
	for _, table := range tables {
		if !slices.Contains(store.DataTables, table) {
			t.Errorf("table %q isn't in store.DataTables (data_tables.go): the Monitoring Data page won't show it", table)
		}
	}
	for _, table := range store.DataTables {
		if !slices.Contains(tables, table) {
			t.Errorf("store.DataTables lists %q, which no migration creates", table)
		}
	}
}
