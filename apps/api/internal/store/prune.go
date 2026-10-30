package store

import (
	"context"
	"github.com/uptrace/bun"
	"time"
)

// Prunable is a table the prune command (internal/prune) can delete old rows
// from. Each target declares one in its own prune_<target>.go; the queries
// below serve them all. The table needs a bigint id primary key.
type Prunable struct {
	Model any    // the table's model, e.g. (*eventRecord)(nil)
	Age   string // the column whose age counts, e.g. "created_at"
	// Keep narrows a query to old rows that must stay (optional).
	Keep func(q *bun.SelectQuery) *bun.SelectQuery
}

// old selects the target's rows older than before.
func (s *Store) old(p Prunable, before time.Time) *bun.SelectQuery {
	return s.DB.NewSelect().Model(p.Model).Where("?TableAlias.? < ?", bun.Ident(p.Age), before)
}

// prunable is old minus the rows Keep holds back.
func (s *Store) prunable(p Prunable, before time.Time) *bun.SelectQuery {
	q := s.old(p, before)
	if p.Keep != nil {
		q = q.WhereGroup(" AND NOT ", p.Keep)
	}
	return q
}

// CountPrunable counts the old rows a run would delete, and those it keeps.
func (s *Store) CountPrunable(ctx context.Context, p Prunable, before time.Time) (prunable, kept int, err error) {
	n, err := s.prunable(p, before).Count(ctx)
	if err != nil || p.Keep == nil {
		return int(n), 0, err
	}
	k, err := s.old(p, before).WhereGroup(" AND ", p.Keep).Count(ctx)
	return int(n), int(k), err
}

// Prune deletes up to limit prunable rows in one short statement and says how
// many it deleted. SKIP LOCKED leaves rows another run holds rather than
// waiting for them.
func (s *Store) Prune(ctx context.Context, p Prunable, before time.Time, limit int) (int, error) {
	batch := s.prunable(p, before).
		ColumnExpr("?TableAlias.id").
		OrderExpr("?TableAlias.id").
		Limit(int64(limit)).
		For("UPDATE SKIP LOCKED")
	result, err := s.DB.NewDelete().Model(p.Model).Where("?TableAlias.id IN (?)", batch).Exec(ctx)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	return int(n), err
}
