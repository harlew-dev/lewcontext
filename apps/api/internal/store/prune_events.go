package store

import "github.com/uptrace/bun"

// PrunableEvents is the events table for the prune command. It keeps events
// whose listeners haven't run yet; delivered and failed deliveries go with
// their event (ON DELETE CASCADE).
var PrunableEvents = Prunable{
	Model: (*eventRecord)(nil),
	Age:   "created_at",
	Keep: func(q *bun.SelectQuery) *bun.SelectQuery {
		pending := q.NewSelect().Model((*deliveryRecord)(nil)).
			ColumnExpr("1").
			Where("d.event_id = e.id").
			Where("d.delivered_at IS NULL AND d.failed_at IS NULL")
		return q.Where("EXISTS (?)", pending)
	},
}
