package admin

import (
	"database/sql"
	"errors"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/store"
	"net/http"
)

// AdminSessions are another admin's sessions, as the admin dashboard acts
// on them: signing them out (a lost laptop, a session that moved). The one
// action on another admin (decision 8): suspending one or resetting one's
// MFA stays on the command line, so one compromised admin account can't
// lock the others out.
type AdminSessions struct{ *httpapi.Server }

// Destroy signs the admin out: every admin session and app session of
// theirs, recording admin.admin.signed_out. 404 when the account isn't an
// admin; 403 for yourself (sign your other sessions out instead,
// DELETE /api/admin/sessions). DELETE /api/admin/admins/{user}/sessions.
func (h AdminSessions) Destroy(w http.ResponseWriter, r *http.Request) {
	user, ok := targetUser(w, r)
	if !ok {
		return
	}
	var in destroyInput
	if !httpapi.Decode(w, r, &in) {
		return
	}
	action, ok := adminAction(w, r, in.Reason)
	if !ok {
		return
	}
	_, _, err := h.Repo.RevokeAdminSessions(r.Context(), user, action)
	switch {
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, store.ErrNotAdmin):
		httpapi.Fail(w, 404, "Admin not found.")
	case errors.Is(err, store.ErrActionOnSelf):
		httpapi.Fail(w, 403, "To sign your own sessions out, use Sign out my other sessions, or sign out.")
	case err != nil:
		httpapi.InternalError(w, r, err)
	default:
		w.WriteHeader(204)
	}
}
