package store

import (
	"github.com/uptrace/bun"
	"lewcontext/api/internal/events"
)

// PrunableEvents is the events table for the prune command, apart from the
// audit events (PrunableAuditEvents keeps those a year). It keeps events
// whose listeners haven't run yet; delivered and failed deliveries go with
// their event (ON DELETE CASCADE).
var PrunableEvents = Prunable{
	Model: (*eventRecord)(nil),
	Age:   "created_at",
	Scope: func(q *bun.SelectQuery) *bun.SelectQuery { return q.Where("NOT ?", auditEvent) },
	Keep:  withPendingDeliveries,
}

// PrunableAuditEvents is the audit events (events.Audit: accounts and
// sign-in, admins, servers, retention) for the prune command, kept longer
// than the rest (a year by default), with the same pending deliveries kept.
var PrunableAuditEvents = Prunable{
	Model: (*eventRecord)(nil),
	Age:   "created_at",
	Scope: func(q *bun.SelectQuery) *bun.SelectQuery { return q.Where("?", auditEvent) },
	Keep:  withPendingDeliveries,
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
