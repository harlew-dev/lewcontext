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

// Workspaces are the admin dashboard's /api/admin/workspaces
// routes. Every read is recorded (admin.workspace.listed, .viewed).
type Workspaces struct{ *httpapi.Server }

func (h Workspaces) Index(w http.ResponseWriter, r *http.Request) {
	f, req, ok := adminListQuery(w, r, store.DefaultAdminWorkspaceSort, store.AdminWorkspaceSortable)
	if !ok {
		return
	}
	page, err := h.Repo.IndexAdminWorkspaces(r.Context(), f, req)
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	e := events.AdminWorkspaceListed{Admin: httpapi.Admin(r).User.PublicID, Q: f.Search, Sort: f.Sort, Page: req.Page}
	if err := h.Repo.Record(r.Context(), e, store.EventScope{}); err != nil {
		httpapi.InternalError(w, err)
		return
	}
	httpapi.JSON(w, 200, page)
}

func (h Workspaces) Show(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("workspace")
	if !input.PublicID("wsp", id) {
		httpapi.Fail(w, 404, "Workspace not found.")
		return
	}
	workspace, err := h.Repo.FindAdminWorkspace(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.Fail(w, 404, "Workspace not found.")
		return
	}
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	// No workspace scope: the record of an admin looking is the admin's,
	// and never shows among the workspace's own events.
	e := events.AdminWorkspaceViewed{Admin: httpapi.Admin(r).User.PublicID, Workspace: workspace.PublicID}
	if err := h.Repo.Record(r.Context(), e, store.EventScope{}); err != nil {
		httpapi.InternalError(w, err)
		return
	}
	httpapi.JSON(w, 200, workspace)
}
