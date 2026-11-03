package store

// PrunableTableStats is the daily table sizes for the prune command: a
// day goes once it's older than the age given (400 days by default, so a
// year's growth can still be compared).
var PrunableTableStats = Prunable{
	Model: (*tableStatRecord)(nil),
	Age:   "day",
}
