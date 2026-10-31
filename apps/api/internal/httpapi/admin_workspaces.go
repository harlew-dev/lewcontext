package httpapi

import (
	"database/sql"
	"errors"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/store"
	"net/http"
)

// AdminWorkspaces are the admin dashboard's /api/admin/workspaces
// routes. Every read is recorded (admin.workspace.listed, .viewed).
type AdminWorkspaces struct{ *Server }

func (h AdminWorkspaces) Index(w http.ResponseWriter, r *http.Request) {
	f, req, ok := adminListQuery(w, r, store.DefaultAdminWorkspaceSort, store.AdminWorkspaceSortable)
	if !ok {
		return
	}
	page, err := h.Repo.IndexAdminWorkspaces(r.Context(), f, req)
	if err != nil {
		InternalError(w, err)
		return
	}
	e := events.AdminWorkspaceListed{Admin: Admin(r).User.PublicID, Q: f.Search, Sort: f.Sort, Page: req.Page}
	if err := h.Repo.Record(r.Context(), e, store.EventScope{}); err != nil {
		InternalError(w, err)
		return
	}
	JSON(w, 200, page)
}

func (h AdminWorkspaces) Show(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("workspace")
	if !input.PublicID("wsp", id) {
		Fail(w, 404, "Workspace not found.")
		return
	}
	workspace, err := h.Repo.FindAdminWorkspace(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		Fail(w, 404, "Workspace not found.")
		return
	}
	if err != nil {
		InternalError(w, err)
		return
	}
	// No workspace scope: the record of an admin looking is the admin's,
	// and never shows among the workspace's own events.
	e := events.AdminWorkspaceViewed{Admin: Admin(r).User.PublicID, Workspace: workspace.PublicID}
	if err := h.Repo.Record(r.Context(), e, store.EventScope{}); err != nil {
		InternalError(w, err)
		return
	}
	JSON(w, 200, workspace)
}

// WorkspaceProjects are /api/admin/workspaces/{workspace}/projects:
// a workspace's projects, with their names and descriptions, recorded
// (admin.project.listed) like every admin read.
type WorkspaceProjects struct{ *Server }

func (h WorkspaceProjects) Index(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("workspace")
	if !input.PublicID("wsp", id) {
		Fail(w, 404, "Workspace not found.")
		return
	}
	params, ok := input.Query(r, "page", "perPage", "q", "status", "sort")
	if !ok {
		Fail(w, 400, "Invalid query parameters.")
		return
	}
	req, ok := input.Page(params)
	if !ok {
		Fail(w, 400, "Invalid page.")
		return
	}
	filter, ok := input.ProjectFilter(params)
	if !ok {
		Fail(w, 400, "Check the search, status, and sort.")
		return
	}
	page, err := h.Repo.IndexAdminProjects(r.Context(), id, filter, req)
	if errors.Is(err, sql.ErrNoRows) {
		Fail(w, 404, "Workspace not found.")
		return
	}
	if err != nil {
		InternalError(w, err)
		return
	}
	e := events.AdminProjectListed{
		Admin: Admin(r).User.PublicID, Workspace: id,
		Q: filter.Search, Status: filter.Status, Sort: filter.Sort, Page: req.Page,
	}
	if err := h.Repo.Record(r.Context(), e, store.EventScope{}); err != nil {
		InternalError(w, err)
		return
	}
	JSON(w, 200, page)
}
