package store

import (
	"context"
	"database/sql"
	"github.com/uptrace/bun"
	"time"
)

// Prunable is a table the prune command (internal/prune) can delete old rows
// from. Each target declares one in its own prune_<target>.go; the queries
// below serve them all. The table needs a bigint id primary key.
type Prunable struct {
	Model any    // the table's model, e.g. (*eventRecord)(nil)
	Age   string // the column whose age counts, e.g. "created_at"
	// AgeExpression is fixed SQL for activity-based retention, when a parent
	// has discussion. Recheck serialises deletion with new parent activity.
	AgeQuery func(bun.IDB) *bun.SelectQuery
	Recheck  bool
	// Scope narrows the table to the rows this target looks after
	// (optional): the events target the events that aren't audit events,
	// audit_events the others.
	Scope func(q *bun.SelectQuery) *bun.SelectQuery
	// Keep narrows a query to old rows that must stay (optional).
	Keep func(q *bun.SelectQuery) *bun.SelectQuery
	// Oldest finds the oldest row the target would prune (optional), for
	// the Monitoring Data page's retention: without it, the oldest Age in
	// Scope minus Keep, read through an index leading with Age.
	Oldest func(ctx context.Context, s *Store) (*time.Time, error)
}

// old selects the target's rows older than before.
func (s *Store) old(p Prunable, before time.Time) *bun.SelectQuery {
	return oldPrunable(s.DB, p, before)
}

func oldPrunable(db bun.IDB, p Prunable, before time.Time) *bun.SelectQuery {
	q := db.NewSelect().Model(p.Model)
	if p.AgeQuery != nil {
		q = q.Where("(?) < ?", p.AgeQuery(db), before)
	} else {
		q = q.Where("?TableAlias.? < ?", bun.Ident(p.Age), before)
	}
	if p.Scope != nil {
		q = q.WhereGroup(" AND ", p.Scope)
	}
	return q
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
	if p.Recheck {
		return s.pruneIncidentParents(ctx, p, before, limit)
	}
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

// A candidate query's snapshot can predate a comment's commit while waiting
// for its lock. Re-evaluate in a fresh statement after locking the parent.
func (s *Store) pruneIncidentParents(ctx context.Context, p Prunable, before time.Time, limit int) (int, error) {
	deleted := 0
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		q := oldPrunable(tx, p, before)
		if p.Keep != nil {
			q = q.WhereGroup(" AND NOT ", p.Keep)
		}
		var ids []int64
		if err := q.ColumnExpr("?TableAlias.id").OrderExpr("?TableAlias.id").Limit(int64(limit)).For("UPDATE SKIP LOCKED").Scan(ctx, &ids); err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		recheck := oldPrunable(tx, p, before).ColumnExpr("?TableAlias.id").Where("?TableAlias.id IN (?)", bun.In(ids))
		if p.Keep != nil {
			recheck = recheck.WhereGroup(" AND NOT ", p.Keep)
		}
		result, err := tx.NewDelete().Model(p.Model).Where("?TableAlias.id IN (?)", recheck).Exec(ctx)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		deleted = int(n)
		return err
	})
	return deleted, err
}

// FindOldestPrunable is when the oldest row p would prune was made (by its
// Age column), or nil when it has none: how far behind its retention it
// is.
func (s *Store) FindOldestPrunable(ctx context.Context, p Prunable) (*time.Time, error) {
	if p.Oldest != nil {
		return p.Oldest(ctx, s)
	}
	var oldest sql.NullTime
	q := s.DB.NewSelect().Model(p.Model)
	if p.AgeQuery != nil {
		q = q.ColumnExpr("min((?))", p.AgeQuery(s.DB))
	} else {
		q = q.ColumnExpr("min(?TableAlias.?)", bun.Ident(p.Age))
	}
	// Bun drops a group's "AND NOT" when it is the query's first condition,
	// which would turn Keep (what to keep) into what to look at.
	q = q.Where("TRUE")
	if p.Scope != nil {
		q = q.WhereGroup(" AND ", p.Scope)
	}
	if p.Keep != nil {
		q = q.WhereGroup(" AND NOT ", p.Keep)
	}
	if err := q.Scan(ctx, &oldest); err != nil || !oldest.Valid {
		return nil, err
	}
	at := oldest.Time.UTC()
	return &at, nil
}
