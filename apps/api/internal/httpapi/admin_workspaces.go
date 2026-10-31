package httpapi

import (
	"database/sql"
	"errors"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/store"
	"net/http"
)

// adminWorkspaceHandlers are the admin dashboard's /api/admin/workspaces
// routes. Every read is recorded (admin.workspace.listed, .viewed).
type adminWorkspaceHandlers struct{ *Server }

func (h adminWorkspaceHandlers) index(w http.ResponseWriter, r *http.Request) {
	f, req, ok := adminListQuery(w, r, store.DefaultAdminWorkspaceSort, store.AdminWorkspaceSortable)
	if !ok {
		return
	}
	page, err := h.Store.IndexAdminWorkspaces(r.Context(), f, req)
	if err != nil {
		internalError(w, err)
		return
	}
	e := events.AdminWorkspaceListed{Admin: currentAdmin(r).User.PublicID, Q: f.Search, Sort: f.Sort, Page: req.Page}
	if err := h.Store.Record(r.Context(), e, store.EventScope{}); err != nil {
		internalError(w, err)
		return
	}
	jsonResponse(w, 200, page)
}

func (h adminWorkspaceHandlers) show(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("workspace")
	if !validPublicID("wsp", id) {
		fail(w, 404, "Workspace not found.")
		return
	}
	workspace, err := h.Store.FindAdminWorkspace(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "Workspace not found.")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	// No workspace scope: the record of an admin looking is the admin's,
	// and never shows among the workspace's own events.
	e := events.AdminWorkspaceViewed{Admin: currentAdmin(r).User.PublicID, Workspace: workspace.PublicID}
	if err := h.Store.Record(r.Context(), e, store.EventScope{}); err != nil {
		internalError(w, err)
		return
	}
	jsonResponse(w, 200, workspace)
}
