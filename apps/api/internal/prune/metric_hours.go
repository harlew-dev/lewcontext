package prune

import "lewcontext/api/internal/store"

func init() {
	register(Pruner{
		Name:        "request_hours",
		Description: "hourly request sums older than the cutoff",
		DefaultAge:  90 * day,
		Table:       store.PrunableRequestHours,
	})
	register(Pruner{
		Name:        "metric_hours",
		Description: "hourly sums of the other metrics older than the cutoff",
		DefaultAge:  90 * day,
		Table:       store.PrunableMetricHours,
	})
}
