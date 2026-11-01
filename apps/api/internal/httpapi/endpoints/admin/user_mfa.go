package admin

import (
	"errors"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/store"
	"net/http"
)

// UserMFA is an account's two-factor authentication, as the admin
// dashboard acts on it: resetting it for someone who lost their
// authenticator and recovery codes (admin reset-mfa, from the dashboard).
type UserMFA struct{ *httpapi.Server }

// resetMFAInput is Destroy's body: why, and that whoever asked was
// confirmed to be the account's owner outside the app, since this is a way
// around their second factor.
type resetMFAInput struct {
	Reason    string `json:"reason"`
	Confirmed bool   `json:"confirmed"`
}

// Destroy turns the account's MFA off and signs every session of it out,
// recording auth.mfa.disabled (reason reset) and admin.user.mfa_reset.
// 422 on confirmed without the out-of-band confirmation; 409 when MFA is
// off. Never an admin's (403): admins must keep MFA, and only the command
// line resets it, reopening their setup window at the door.
// DELETE /api/admin/users/{user}/mfa.
func (h UserMFA) Destroy(w http.ResponseWriter, r *http.Request) {
	user, ok := targetUser(w, r)
	if !ok {
		return
	}
	var in resetMFAInput
	if !httpapi.Decode(w, r, &in) {
		return
	}
	action, ok := adminAction(w, r, in.Reason)
	if !ok {
		return
	}
	if !in.Confirmed {
		httpapi.FailField(w, 422, "confirmed", "Confirm who is asking, outside the app, first.")
		return
	}
	_, err := h.Repo.ResetUserMFA(r.Context(), user, action)
	switch {
	case errors.Is(err, store.ErrMFANotEnabled):
		httpapi.Fail(w, 409, "Two-factor authentication is already off for this account.")
	case err != nil:
		if !failAccountAction(w, err) {
			httpapi.InternalError(w, r, err)
		}
	default:
		w.WriteHeader(204)
	}
}
