package admin

import (
	"errors"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/store"
	"net/http"
)

// UserSuspension is an account's suspension, a singleton
// (docs/api/admin.md#suspending-an-account): Store suspends, Destroy lifts
// it.
type UserSuspension struct{ *httpapi.Server }

// suspendInput is Store's body: why, and whether to revoke the live
// invitations the account sent (true when left out, the dialog's default).
type suspendInput struct {
	Reason            string `json:"reason"`
	RevokeInvitations *bool  `json:"revokeInvitations"`
}

// Store suspends the account: every session of it ends, it can't sign in
// (an honest 403, but only after its password and code) and gets no reset
// link; its workspaces keep working. 201 with what it ended. 409 when it's
// already suspended; never your own account nor an admin's (403: only the
// command line suspends an admin, after revoking admin).
// POST /api/admin/users/{user}/suspension.
func (h UserSuspension) Store(w http.ResponseWriter, r *http.Request) {
	user, ok := targetUser(w, r)
	if !ok {
		return
	}
	var in suspendInput
	if !httpapi.Decode(w, r, &in) {
		return
	}
	action, ok := adminAction(w, r, in.Reason)
	if !ok {
		return
	}
	suspension := store.Suspension{Reason: action.Reason, RevokeInvitations: in.RevokeInvitations == nil || *in.RevokeInvitations}
	done, err := h.Repo.SuspendUser(r.Context(), user, suspension, &action)
	switch {
	case errors.Is(err, store.ErrAlreadySuspended):
		httpapi.Fail(w, 409, "This account is already suspended.")
	case err != nil:
		if !failAccountAction(w, err) {
			httpapi.InternalError(w, r, err)
		}
	default:
		httpapi.JSON(w, 201, map[string]int{"sessionsRevoked": done.SessionsRevoked, "invitationsRevoked": done.InvitationsRevoked})
	}
}

// Destroy lifts the account's suspension: it can sign in again. 409 when
// it isn't suspended. DELETE /api/admin/users/{user}/suspension.
func (h UserSuspension) Destroy(w http.ResponseWriter, r *http.Request) {
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
	_, err := h.Repo.UnsuspendUser(r.Context(), user, action.Reason, &action)
	switch {
	case errors.Is(err, store.ErrNotSuspended):
		httpapi.Fail(w, 409, "This account isn't suspended.")
	case err != nil:
		if !failAccountAction(w, err) {
			httpapi.InternalError(w, r, err)
		}
	default:
		w.WriteHeader(204)
	}
}
