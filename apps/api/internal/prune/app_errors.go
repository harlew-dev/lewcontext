package prune

import "lewcontext/api/internal/store"

func init() {
	register(Pruner{
		Name:        "app_errors",
		Description: "grouped errors not seen since the cutoff",
		DefaultAge:  30 * day,
		Table:       store.PrunableAppErrors,
	})
}
