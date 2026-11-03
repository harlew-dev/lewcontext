package prune

import "lewcontext/api/internal/store"

func init() {
	register(Pruner{
		Name:        "alerts",
		Description: "alerts resolved before the cutoff",
		DefaultAge:  365 * day,
		Table:       store.PrunableAlerts,
	})
}
