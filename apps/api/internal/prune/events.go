package prune

import "lewcontext/api/internal/store"

func init() {
	register(Pruner{
		Name:        "events",
		Description: "the audit trail apart from its audit events, keeping events whose listeners haven't run yet",
		DefaultAge:  90 * day,
		Table:       store.PrunableEvents,
		KeptReason:  "with pending deliveries",
	})
	register(Pruner{
		Name:        "audit_events",
		Description: "the audit events (accounts and sign-in, admins, servers, retention: events.Audit), kept a year",
		DefaultAge:  365 * day,
		Table:       store.PrunableAuditEvents,
		KeptReason:  "with pending deliveries",
	})
	register(Pruner{
		Name:        "pseudonymise",
		Description: "minimises events: IPs cut to their /24 or /48, emails replaced by keyed pseudonyms (rewrites, never deletes)",
		DefaultAge:  30 * day,
		Table:       store.PrunablePseudonymise,
		KeptReason:  "with pending deliveries",
		Minimises:   true,
	})
}
