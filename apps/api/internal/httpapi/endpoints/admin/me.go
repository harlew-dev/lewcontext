package admin

import (
	"errors"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/store"
	"net/http"
)

// Me is the signed-in admin's own settings, /api/admin/me: which alerts
// they're emailed (docs/api/monitoring.md#alerts). Reading the session is
// Sessions.Me. Changing it touches only their own inbox, so any admin may
// (Role viewer), with no code: the one write on the Admins page without
// one.
type Me struct{ *httpapi.Server }

// meInput is Update's body.
type meInput struct {
	AlertEmail string `json:"alertEmail"`
}

// Update sets which alerts the admin is emailed: all, critical or none,
// recording admin.updated (changes.alertEmail) when it changed. 204; 422
// on alertEmail. PATCH /api/admin/me.
func (h Me) Update(w http.ResponseWriter, r *http.Request) {
	var in meInput
	if !httpapi.Decode(w, r, &in) {
		return
	}
	switch in.AlertEmail {
	case store.AlertEmailAll, store.AlertEmailCritical, store.AlertEmailNone:
	default:
		httpapi.FailField(w, 422, "alertEmail", "Pick all, critical or none.")
		return
	}
	err := h.Repo.UpdateAdminAlertEmail(r.Context(), httpapi.Admin(r).User, in.AlertEmail)
	if errors.Is(err, store.ErrNotAdmin) {
		httpapi.Fail(w, 401, "Sign in again.")
		return
	}
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	w.WriteHeader(204)
}
