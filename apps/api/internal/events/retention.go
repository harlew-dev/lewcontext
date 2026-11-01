package events

import "strings"

// Retention classes (docs/compliance.md#retention): audit events, the
// record of who got in and what admins did, are kept a year (the
// audit_events prune target); every other event 90 days (events).

// AuditAreas are the areas whose events are audit events: accounts and
// sign-in (auth), the admin dashboard and its CLI (admin), alerts (alert,
// to come), servers starting and stopping (server) and retention itself
// (prune). Each audit event is also written as a log line (msg "audit"),
// for the operator to ship off the server.
var AuditAreas = []string{"auth", "admin", "alert", "server", "prune"}

// Audit reports whether the event named name is an audit event.
func Audit(name string) bool {
	area, _, _ := strings.Cut(name, ".")
	for _, a := range AuditAreas {
		if area == a {
			return true
		}
	}
	return false
}
