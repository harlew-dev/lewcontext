package store

import "github.com/uptrace/bun"

// Discussion shifts the 30-day target's effective age by 335 days, giving
// discussed groups 365 days after their latest occurrence/resolution/comment.
// A redaction deliberately does not move that activity timestamp.
func incidentActivity(db bun.IDB, parentColumn string) *bun.SelectQuery {
	return db.NewSelect().Model((*IncidentMessage)(nil)).ColumnExpr("max(im.created_at)").Join("JOIN incident_cycles AS ic ON ic.id = im.cycle_id").Where("? = ?", bun.Ident("ic."+parentColumn), bun.Ident(map[string]string{"app_error_id": "ae.id", "alert_id": "alt.id"}[parentColumn]))
}
func errorDiscussion(db bun.IDB) *bun.SelectQuery {
	return db.NewSelect().Model((*IncidentMessage)(nil)).ColumnExpr("1").Join("JOIN incident_cycles AS ic ON ic.id = im.cycle_id").Where("ic.app_error_id = ae.id")
}

// PrunableAppErrors is the grouped errors table for the prune command: a
// group goes once it hasn't been seen for the age given (30 days by
// default); one that recurs later starts a new group.
var PrunableAppErrors = Prunable{
	Model: (*appErrorRecord)(nil),
	Age:   "last_seen_at",
	AgeQuery: func(db bun.IDB) *bun.SelectQuery {
		return db.NewSelect().ColumnExpr("CASE WHEN EXISTS (?) THEN greatest(ae.last_seen_at, ae.resolved_at, (?)) + interval '8040 hours' ELSE ae.last_seen_at END", errorDiscussion(db), incidentActivity(db, "app_error_id"))
	},
	Recheck: true,
	Keep: func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Where("ae.resolved_at IS NULL AND EXISTS (?)", errorDiscussion(q.DB()))
	},
}
