package workspace

import (
	"lewcontext/api/internal/httpapi"
	"net/http"
)

// Events are the …/events routes inside a workspace: its log of changes,
// for owners and admins (workspace.event.index, .show). The work is
// httpapi.IndexWorkspaceLog, which a personal API key's own log shares.
type Events struct{ *httpapi.Server }

func (h Events) Index(w http.ResponseWriter, r *http.Request) {
	h.IndexWorkspaceLog(w, r, httpapi.Workspace(r).ID, "")
}
func (h Events) Show(w http.ResponseWriter, r *http.Request) {
	h.ShowWorkspaceLogEvent(w, r, httpapi.Workspace(r).ID)
}
