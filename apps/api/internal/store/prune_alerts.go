package store

import "github.com/uptrace/bun"

// PrunableAlerts is the alerts table for the prune command: a resolved
// alert goes a year after it resolved (the evidence of when something was
// detected and how fast it was answered); an open one never.
var PrunableAlerts = Prunable{
	Model: (*alertRecord)(nil),
	Age:   "resolved_at",
	AgeQuery: func(db bun.IDB) *bun.SelectQuery {
		return db.NewSelect().ColumnExpr("greatest(alt.resolved_at, (?))", incidentActivity(db, "alert_id"))
	},
	Recheck: true,
	Scope:   func(q *bun.SelectQuery) *bun.SelectQuery { return q.Where("alt.resolved_at IS NOT NULL") },
}
