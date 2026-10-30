package prune

import "forma/api/internal/store"

func init() {
	register(Pruner{
		Name:        "events",
		Description: "the audit trail, keeping events whose listeners haven't run yet",
		DefaultAge:  90 * day,
		Table:       store.PrunableEvents,
		KeptReason:  "with pending deliveries",
	})
}
