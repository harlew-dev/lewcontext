package store

import (
	"slices"
	"testing"
)

// Every table with a workspace_id column is in WorkspaceTables (monitoring
// decision 9), so the Workspaces page counts a new table's rows without
// anyone remembering; every one listed exists, and has an index leading
// with workspace_id, so a workspace's rows are counted through it.
func TestEveryWorkspaceTableIsMonitored(t *testing.T) {
	s, ctx := queueStore(t, nil)
	var tables []string
	err := s.DB.NewSelect().TableExpr("information_schema.columns").Column("table_name").
		Where("table_schema = current_schema()").
		Where("column_name = 'workspace_id'").
		OrderExpr("table_name").
		Scan(ctx, &tables)
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) < 4 {
		t.Fatalf("found only %v", tables)
	}
	var listed []string
	for _, wt := range WorkspaceTables {
		listed = append(listed, wt.Name)
		if wt.Label == "" {
			t.Errorf("%s has no label", wt.Name)
		}
	}
	for _, table := range tables {
		if !slices.Contains(listed, table) {
			t.Errorf("table %q has a workspace_id but isn't in store.WorkspaceTables: list it there", table)
		}
	}
	for _, name := range listed {
		if !slices.Contains(tables, name) {
			t.Errorf("store.WorkspaceTables lists %q, which has no workspace_id column", name)
		}
		var indexes []string
		err := s.DB.NewSelect().TableExpr("pg_indexes").Column("indexname").
			Where("schemaname = current_schema()").
			Where("tablename = ?", name).
			Where(`indexdef ~ '\(workspace_id[,)]'`).
			Scan(ctx, &indexes)
		if err != nil {
			t.Fatal(err)
		}
		if len(indexes) == 0 {
			t.Errorf("%s has no index leading with workspace_id: counting a workspace's rows would read the whole table", name)
		}
	}
}
