package store

// PrunableAlerts is the alerts table for the prune command: a resolved
// alert goes a year after it resolved (the evidence of when something was
// detected and how fast it was answered); an open one never.
var PrunableAlerts = Prunable{
	Model: (*alertRecord)(nil),
	Age:   "resolved_at",
}
