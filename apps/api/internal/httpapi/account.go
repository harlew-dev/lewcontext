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

// destroyAccount deletes the signed-in account once its password is
// confirmed, and clears the session cookie. The only owner of a workspace
// gets 409 with those workspaces, so the web app can list them.
func (s *Server) destroyAccount(w http.ResponseWriter, r *http.Request) {
	var in accountDeletion
	if !decode(w, r, &in) {
		return
	}
	if !s.passwordConfirmed(w, r, in.Password, events.PasswordStepDestroyAccount) {
		return
	}
	err := s.Store.DeleteAccount(r.Context(), current(r).User)
	var lastOwner store.LastOwnerError
	if errors.As(err, &lastOwner) {
		jsonResponse(w, 409, map[string]any{
			"error":      "Delete the workspaces you own first, or make someone else an owner.",
			"workspaces": lastOwner.Workspaces,
		})
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	s.clearSessionCookie(w)
	w.WriteHeader(204)
}
