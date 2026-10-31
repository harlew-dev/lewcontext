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

// adminWorkspaceProjectHandlers are /api/admin/workspaces/{workspace}/projects:
// a workspace's projects, with their names and descriptions, recorded
// (admin.project.listed) like every admin read.
type adminWorkspaceProjectHandlers struct{ *Server }

func (h adminWorkspaceProjectHandlers) index(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("workspace")
	if !validPublicID("wsp", id) {
		fail(w, 404, "Workspace not found.")
		return
	}
	params, ok := queryParams(r, "page", "perPage", "q", "status", "sort")
	if !ok {
		fail(w, 400, "Invalid query parameters.")
		return
	}
	req, ok := pageRequest(params)
	if !ok {
		fail(w, 400, "Invalid page.")
		return
	}
	filter, ok := projectFilter(params)
	if !ok {
		fail(w, 400, "Check the search, status, and sort.")
		return
	}
	page, err := h.Store.IndexAdminProjects(r.Context(), id, filter, req)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "Workspace not found.")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	e := events.AdminProjectListed{
		Admin: currentAdmin(r).User.PublicID, Workspace: id,
		Q: filter.Search, Status: filter.Status, Sort: filter.Sort, Page: req.Page,
	}
	if err := h.Store.Record(r.Context(), e, store.EventScope{}); err != nil {
		internalError(w, err)
		return
	}
	jsonResponse(w, 200, page)
}
