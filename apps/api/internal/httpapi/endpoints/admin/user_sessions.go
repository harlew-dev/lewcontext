package admin

import (
	"lewcontext/api/internal/httpapi"
	"net/http"
)

// UserSessions are an account's sessions, as the admin dashboard acts on
// them: Sign out everywhere.
type UserSessions struct{ *httpapi.Server }

// destroyInput is an action's body with nothing to say but why.
type destroyInput struct {
	Reason string `json:"reason"`
}

// Destroy signs every session of the account out, recording
// admin.user.signed_out. Never your own account (403: sign your other
// sessions out instead) nor another admin's (403: Sign out on the Admins
// page). DELETE /api/admin/users/{user}/sessions.
func (h UserSessions) Destroy(w http.ResponseWriter, r *http.Request) {
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
	if _, err := h.Repo.RevokeUserSessions(r.Context(), user, action); err != nil {
		if !failAccountAction(w, err) {
			httpapi.InternalError(w, r, err)
		}
		return
	}
	w.WriteHeader(204)
}
