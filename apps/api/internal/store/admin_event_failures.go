package store

import (
	"lewcontext/api/internal/events"
	"strings"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/schema"
)

// Failed checks, as the admin dashboard's Events page reads them: one fixed
// table behind the rows' Failed badge, the "Failed checks only" filter and
// the failures chart, so the three never disagree. Each row is an event
// name and fixed SQL over its payload; nothing in it comes from input.

// Kinds of failed check, in the failures chart's order.
const (
	FailureSignIn          = "sign_in"
	FailureSecondFactor    = "second_factor"
	FailurePassword        = "password"
	FailureSignUpCode      = "sign_up_code"
	FailureAdminDoor       = "admin_door"
	FailureEmailChangeCode = "email_change_code"
)

// FailureKinds are the kinds, in the chart's order.
var FailureKinds = []string{FailureSignIn, FailureSecondFactor, FailurePassword, FailureSignUpCode, FailureAdminDoor, FailureEmailChangeCode}

// eventFailure is when an event named Name records a failed check of Kind:
// Condition (fixed SQL over e.payload, its values as ? placeholders in
// Args) holds.
type eventFailure struct {
	Kind      string
	Name      string
	Condition string
	Args      []any
}

// adminEventFailures is the table (docs/api/admin.md#failed-checks).
var adminEventFailures = []eventFailure{
	{FailureSignIn, "auth.login_attempted", "e.payload->>'result' IN (?)",
		[]any{bun.In([]string{events.LoginWrongPassword, events.LoginUnknownAccount})}},
	{FailureSecondFactor, "auth.mfa.checked", "e.payload->>'ok' = ?", []any{"false"}},
	{FailurePassword, "auth.password_checked", "e.payload->>'ok' = ?", []any{"false"}},
	{FailureSignUpCode, "auth.registration.checked", "e.payload->>'ok' = ?", []any{"false"}},
	{FailureEmailChangeCode, "auth.email_change.checked", "e.payload->>'ok' = ?", []any{"false"}},
	{FailureAdminDoor, "admin.login_attempted", "e.payload->>'result' IN (?)",
		[]any{bun.In([]string{events.AdminLoginWrongPassword, events.AdminLoginUnknownAccount, events.AdminLoginNotAdmin, events.AdminLoginMFAOff})}},
	{FailureAdminDoor, "admin.access_refused", "TRUE", nil},
}

// FailureNames are the names that can record a failed check, in the
// table's order.
func FailureNames() []string {
	names := make([]string, len(adminEventFailures))
	for i, f := range adminEventFailures {
		names[i] = f.Name
	}
	return names
}

// failedCondition is true for an event recording a failed check:
// (e.name = ? AND <condition>) OR … over the table, never null.
func failedCondition() schema.QueryWithArgs {
	parts := make([]string, len(adminEventFailures))
	var args []any
	for i, f := range adminEventFailures {
		parts[i] = "(e.name = ? AND " + f.Condition + ")"
		args = append(append(args, f.Name), f.Args...)
	}
	return bun.SafeQuery("COALESCE("+strings.Join(parts, " OR ")+", FALSE)", args...)
}

// failureKind names the kind of failed check an event records, or null:
// CASE WHEN e.name = ? AND <condition> THEN ? … END.
func failureKind() schema.QueryWithArgs {
	var sql strings.Builder
	var args []any
	sql.WriteString("CASE")
	for _, f := range adminEventFailures {
		sql.WriteString(" WHEN e.name = ? AND " + f.Condition + " THEN ?")
		args = append(append(append(args, f.Name), f.Args...), f.Kind)
	}
	sql.WriteString(" END")
	return bun.SafeQuery(sql.String(), args...)
}
