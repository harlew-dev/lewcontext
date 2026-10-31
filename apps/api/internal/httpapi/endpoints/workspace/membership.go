package workspace

import (
	"lewcontext/api/internal/httpapi"
	"net/http"
)

// Membership is the signed-in user's own membership of the workspace
// (…/membership), a singleton resource: destroy is leaving.
type Membership struct{ *httpapi.Server }

// Destroy removes the signed-in user's own membership: they leave.
func (h Membership) Destroy(w http.ResponseWriter, r *http.Request) {
	if err := h.Repo.LeaveWorkspace(r.Context(), httpapi.Workspace(r), httpapi.Session(r).User.ID); err != nil {
		failMembership(w, err)
		return
	}
	w.WriteHeader(204)
}
