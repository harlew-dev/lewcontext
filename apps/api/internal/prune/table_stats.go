package prune

import "lewcontext/api/internal/store"

func init() {
	register(Pruner{
		Name:        "table_stats_daily",
		Description: "the daily table sizes the Monitoring Data page's growth reads, kept 400 days",
		DefaultAge:  400 * day,
		Table:       store.PrunableTableStats,
	})
}
