package admin

import (
	"database/sql"
	"errors"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/store"
	"net/http"
)

// WorkspaceProjects are /api/admin/workspaces/{workspace}/projects:
// a workspace's projects, with their names and descriptions, recorded
// (admin.project.listed) like every admin read.
type WorkspaceProjects struct{ *httpapi.Server }

func (h WorkspaceProjects) Index(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("workspace")
	if !input.PublicID("wsp", id) {
		httpapi.Fail(w, 404, "Workspace not found.")
		return
	}
	params, ok := input.Query(r, "page", "perPage", "q", "status", "sort")
	if !ok {
		httpapi.Fail(w, 400, "Invalid query parameters.")
		return
	}
	req, ok := input.Page(params)
	if !ok {
		httpapi.Fail(w, 400, "Invalid page.")
		return
	}
	filter, ok := input.ProjectFilter(params)
	if !ok {
		httpapi.Fail(w, 400, "Check the search, status, and sort.")
		return
	}
	page, err := h.Repo.IndexAdminProjects(r.Context(), id, filter, req)
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.Fail(w, 404, "Workspace not found.")
		return
	}
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	e := events.AdminProjectListed{
		Admin: httpapi.Admin(r).User.PublicID, Workspace: id,
		Q: filter.Search, Status: filter.Status, Sort: filter.Sort, Page: req.Page,
	}
	if err := h.Repo.Record(r.Context(), e, store.EventScope{}); err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	httpapi.JSON(w, 200, page)
}
