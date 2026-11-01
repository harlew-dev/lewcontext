package admin

import (
	"database/sql"
	"errors"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/store"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

// What every account-and-access action shares (plans/features/admin/actions.md):
// the reason, the admin acting, and the answers to the refusals the store
// returns. The router registers each with Action (the operator role, a
// fresh code, then admin-action:per-user), so by the time a controller
// runs, those have passed.

// Bounds of an action's reason, in characters, after trimming.
const (
	minReason = 10
	maxReason = 500
)

// validReason trims the reason and checks it is 10 to 500 characters
// (counted as Unicode characters, as sign-up counts a password's), with no
// character that could hide or rearrange what an audit record says: line
// breaks and tabs are allowed, but no other control character (and no NUL,
// which PostgreSQL's jsonb can't hold), no format character (bidi
// overrides such as U+202E, zero-width characters, U+FEFF) and no Unicode
// line or paragraph separator.
func validReason(reason *string) bool {
	*reason = strings.TrimSpace(*reason)
	n := utf8.RuneCountInString(*reason)
	return n >= minReason && n <= maxReason && !strings.ContainsFunc(*reason, hiddenInReason)
}

// hiddenInReason reports whether c may not appear in a reason.
func hiddenInReason(c rune) bool {
	switch {
	case c == '\n' || c == '\r' || c == '\t':
		return false
	case c == ' ' || c == ' ':
		return true
	}
	return unicode.IsControl(c) || unicode.Is(unicode.Cf, c)
}

// adminAction checks the reason and returns the action of the signed-in
// admin, answering 422 on reason when it isn't valid. The reason goes only
// into the action's event: never a log line (the body is never logged).
func adminAction(w http.ResponseWriter, r *http.Request, reason string) (store.AdminAction, bool) {
	if !validReason(&reason) {
		httpapi.FailField(w, 422, "reason", "Give a reason of 10 to 500 characters.")
		return store.AdminAction{}, false
	}
	return store.AdminAction{Admin: httpapi.Admin(r).User, Reason: reason}, true
}

// targetUser is the {user} in the path, answering 404 unless it's shaped
// like a public ID.
func targetUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("user")
	if !input.PublicID("usr", id) {
		httpapi.Fail(w, 404, "User not found.")
		return "", false
	}
	return id, true
}

// failAccountAction answers the refusals an action on an account shares,
// or reports false for anything else (the caller's own refusals, or a 500).
func failAccountAction(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		httpapi.Fail(w, 404, "User not found.")
	case errors.Is(err, store.ErrActionOnSelf):
		httpapi.Fail(w, 403, "You can’t do this to your own account.")
	case errors.Is(err, store.ErrActionOnAdmin):
		httpapi.Fail(w, 403, "This account is an admin: only the command line can do this to an admin.")
	default:
		return false
	}
	return true
}
