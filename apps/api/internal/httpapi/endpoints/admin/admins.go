package admin

import (
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/store"
	"net/http"
)

// Admins is the Admins page's route: who holds admin, with what role and
// MFA, when they last signed in, and their live admin sessions, for an
// access review. Recorded as admin.admin.listed. Roles change only from the
// command line, never here.
type Admins struct{ *httpapi.Server }

// Index lists every admin. GET /api/admin/admins.
func (h Admins) Index(w http.ResponseWriter, r *http.Request) {
	if _, ok := input.Query(r); !ok {
		httpapi.Fail(w, 400, "Invalid query parameters.")
		return
	}
	admins, err := h.Repo.IndexAdminAdmins(r.Context(), h.AdminSessionDigest(r), h.AdminKeys())
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	e := events.AdminAdminListed{Admin: httpapi.Admin(r).User.PublicID}
	if err := h.Repo.Record(r.Context(), e, store.EventScope{}); err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"data": admins})
}
