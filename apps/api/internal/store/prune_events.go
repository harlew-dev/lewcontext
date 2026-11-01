package store

import (
	"context"
	"database/sql"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"lewcontext/api/internal/events"
	"time"
)

// PrunableEvents is the events table for the prune command, apart from the
// audit events (PrunableAuditEvents keeps those a year). It keeps events
// whose listeners haven't run yet; delivered and failed deliveries go with
// their event (ON DELETE CASCADE).
var PrunableEvents = Prunable{
	Model:  (*eventRecord)(nil),
	Age:    "created_at",
	Scope:  func(q *bun.SelectQuery) *bun.SelectQuery { return q.Where("NOT ?", auditEvent) },
	Keep:   withPendingDeliveries,
	Oldest: oldestEvents(false),
}

// PrunableAuditEvents is the audit events (events.Audit: accounts and
// sign-in, admins, servers, retention) for the prune command, kept longer
// than the rest (a year by default), with the same pending deliveries kept.
var PrunableAuditEvents = Prunable{
	Model:  (*eventRecord)(nil),
	Age:    "created_at",
	Scope:  func(q *bun.SelectQuery) *bun.SelectQuery { return q.Where("?", auditEvent) },
	Keep:   withPendingDeliveries,
	Oldest: oldestEvents(true),
}

// auditEvent is true for an audit event's row (events.Audit, in SQL).
var auditEvent = bun.SafeQuery("split_part(e.name, '.', 1) IN (?)", bun.In(events.AuditAreas))

// withPendingDeliveries narrows q to events a listener hasn't run for yet.
func withPendingDeliveries(q *bun.SelectQuery) *bun.SelectQuery {
	pending := q.NewSelect().Model((*deliveryRecord)(nil)).
		ColumnExpr("1").
		Where("d.event_id = e.id").
		Where("d.delivered_at IS NULL AND d.failed_at IS NULL")
	return q.Where("EXISTS (?)", pending)
}

// oldestEvents finds the oldest event of one retention class (audit
// events, or the others) for the Data page, one catalogue name at a time:
// each is a probe of one end of its name's range in
// events_name_created_idx, where reading events_created_idx from its start
// for the oldest event that isn't an audit event would pass every audit
// event older than it (a year's sign-ins). A name outside the catalogue
// isn't looked at (the Integrity page's unknown-events check finds those),
// and an event kept for its pending delivery counts like any other.
func oldestEvents(audit bool) func(ctx context.Context, s *Store) (*time.Time, error) {
	return func(ctx context.Context, s *Store) (*time.Time, error) {
		var names []string
		for _, name := range events.Names() {
			if events.Audit(name) == audit {
				names = append(names, name)
			}
		}
		oldest := s.DB.NewSelect().Model((*eventRecord)(nil)).
			ColumnExpr("min(e.created_at) AS oldest").
			Where("e.name = n.name")
		var at sql.NullTime
		err := s.DB.NewSelect().TableExpr("unnest(?::text[]) AS n(name)", pgdialect.Array(names)).
			Join("CROSS JOIN LATERAL (?) AS o", oldest).
			ColumnExpr("min(o.oldest)").
			Scan(ctx, &at)
		if err != nil || !at.Valid {
			return nil, err
		}
		t := at.Time.UTC()
		return &t, nil
	}
}
