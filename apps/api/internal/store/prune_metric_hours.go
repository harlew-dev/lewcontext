package store

// PrunableRequestHours is the hourly request sums for the prune command:
// an hour goes once it's older than the age given (90 days by default). The
// minute tables need no target: the rollup keeps them to 48 hours.
var PrunableRequestHours = Prunable{
	Model: (*requestHour)(nil),
	Age:   "hour",
}

// PrunableMetricHours is the hourly sums of the other metrics, pruned
// like request_hours.
var PrunableMetricHours = Prunable{
	Model: (*metricHour)(nil),
	Age:   "hour",
}
