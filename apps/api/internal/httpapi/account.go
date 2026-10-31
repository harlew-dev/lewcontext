package httpapi

import (
	"errors"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/store"
	"net/http"
)

// accountDeletion confirms deleting the signed-in account.
type accountDeletion struct {
	Password string `json:"password"`
}

// Destroy deletes the signed-in account once its password is
// confirmed, and clears the session cookie. The only owner of a workspace
// gets 409 with those workspaces, so the web app can list them.
func (h Me) Destroy(w http.ResponseWriter, r *http.Request) {
	var in accountDeletion
	if !Decode(w, r, &in) {
		return
	}
	if !h.PasswordConfirmed(w, r, in.Password, events.PasswordStepDestroyAccount) {
		return
	}
	err := h.Repo.DeleteAccount(r.Context(), Session(r).User)
	var lastOwner store.LastOwnerError
	if errors.As(err, &lastOwner) {
		JSON(w, 409, map[string]any{
			"error":      "Delete the workspaces you own first, or make someone else an owner.",
			"workspaces": lastOwner.Workspaces,
		})
		return
	}
	if err != nil {
		InternalError(w, err)
		return
	}
	h.ClearSessionCookie(w)
	w.WriteHeader(204)
}
