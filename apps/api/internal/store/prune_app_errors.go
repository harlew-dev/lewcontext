package store

// PrunableAppErrors is the grouped errors table for the prune command: a
// group goes once it hasn't been seen for the age given (30 days by
// default); one that recurs later starts a new group.
var PrunableAppErrors = Prunable{
	Model: (*appErrorRecord)(nil),
	Age:   "last_seen_at",
}
