package admin

import (
	"database/sql"
	"errors"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/store"
	"net/http"
)

// WorkspaceInvitations are a workspace's invitations, as the admin
// dashboard acts on them: revoking every live one, to stop an
// invitation-spam run without touching the workspace's data.
type WorkspaceInvitations struct{ *httpapi.Server }

// Destroy revokes every live invitation of the workspace, so their links
// stop working, recording workspace.invitation.revoked for each and
// admin.workspace.invitations_revoked. 409 when none is live.
// DELETE /api/admin/workspaces/{workspace}/invitations.
func (h WorkspaceInvitations) Destroy(w http.ResponseWriter, r *http.Request) {
	workspace := r.PathValue("workspace")
	if !input.PublicID("wsp", workspace) {
		httpapi.Fail(w, 404, "Workspace not found.")
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
	_, err := h.Repo.RevokeWorkspaceInvitations(r.Context(), workspace, action)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		httpapi.Fail(w, 404, "Workspace not found.")
	case errors.Is(err, store.ErrNothingToRevoke):
		httpapi.Fail(w, 409, "This workspace has no pending invitations.")
	case err != nil:
		httpapi.InternalError(w, r, err)
	default:
		w.WriteHeader(204)
	}
}
