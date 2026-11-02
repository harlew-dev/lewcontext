package store

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/uptrace/bun"
)

// strayTable is a table the migrations don't make: one made by hand, which
// the Data page and the daily sizes must never name.
type strayTable struct {
	bun.BaseModel `bun:"table:stray_things"`
	ID            int64 `bun:",pk"`
	Secret        string
}

// The daily sizes are written once a day, by one server at a time, for
// the known tables only: two runs at once write one set, a later run on
// the same day none, and a table made by hand isn't recorded or listed.
func TestRecordTableStats(t *testing.T) {
	s, ctx := queueStore(t, nil)
	if _, err := s.DB.NewCreateTable().Model((*strayTable)(nil)).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.NewInsert().Model(&strayTable{ID: 1, Secret: "hand-made"}).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	runs := make([]TableStatsRun, 2)
	errs := make([]error, 2)
	for i := range runs {
		wg.Go(func() { runs[i], errs[i] = s.RecordTableStats(ctx) })
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil || runs[0].Rows+runs[1].Rows != len(DataTables) || min(runs[0].Rows, runs[1].Rows) != 0 {
		t.Fatalf("two runs at once: %+v %v", runs, errs)
	}
	again, err := s.RecordTableStats(ctx)
	if err != nil || !again.Locked || again.Rows != 0 {
		t.Fatalf("a second run the same day: %+v %v", again, err)
	}
	var rows []tableStatRecord
	if err := s.DB.NewSelect().Model(&rows).Scan(ctx); err != nil {
		t.Fatal(err)
	}
	today := time.Now().UTC().Format(time.DateOnly)
	for _, r := range rows {
		if !slices.Contains(DataTables, r.TableName) || r.Day.UTC().Format(time.DateOnly) != today || r.TotalBytes <= 0 {
			t.Fatalf("recorded %+v", r)
		}
	}
	tables, err := s.IndexTableStats(ctx)
	if err != nil || len(tables) != len(DataTables) {
		t.Fatalf("%d tables listed (%v)", len(tables), err)
	}
	for _, table := range tables {
		if table.Name == "stray_things" {
			t.Fatal("a table made by hand is listed")
		}
	}
}
