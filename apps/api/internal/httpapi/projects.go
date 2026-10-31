package httpapi

import (
	"database/sql"
	"errors"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/store"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"
)

var projectColors = []string{"violet", "blue", "amber", "emerald", "rose"}

const invalidProject = "Check the project name, description, status, and color."

func validProject(in *store.ProjectInput) bool {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if utf8.RuneCountInString(in.Name) < 1 || utf8.RuneCountInString(in.Name) > 100 || utf8.RuneCountInString(in.Description) > 500 {
		return false
	}
	return slices.Contains(input.ProjectStatuses, in.Status) && slices.Contains(projectColors, in.Color)
}

// projectID reads the {project} path value, a prj_ public ID.
func projectID(r *http.Request) (string, bool) {
	id := r.PathValue("project")
	return id, input.PublicID("prj", id)
}

// projectHandlers are the …/projects routes inside a workspace (routes.go).
type projectHandlers struct{ *Server }

func (h projectHandlers) index(w http.ResponseWriter, r *http.Request) {
	params, ok := input.Query(r, "page", "perPage", "q", "status", "sort")
	if !ok {
		fail(w, 400, "Invalid query parameters.")
		return
	}
	req, ok := input.Page(params)
	if !ok {
		fail(w, 400, "Invalid page.")
		return
	}
	filter, ok := input.ProjectFilter(params)
	if !ok {
		fail(w, 400, "Check the search, status, and sort.")
		return
	}
	page, err := h.Store.IndexProjects(r.Context(), currentWorkspace(r).ID, filter, req)
	if err != nil {
		internalError(w, err)
		return
	}
	jsonResponse(w, 200, page)
}
func (h projectHandlers) show(w http.ResponseWriter, r *http.Request) {
	id, ok := projectID(r)
	if !ok {
		fail(w, 404, "Project not found.")
		return
	}
	p, err := h.Store.FindProject(r.Context(), currentWorkspace(r).ID, id)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "Project not found.")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	jsonResponse(w, 200, p)
}
func (h projectHandlers) store(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeValid(w, r, validProject, invalidProject)
	if !ok {
		return
	}
	p, err := h.Store.CreateProject(r.Context(), currentWorkspace(r), in)
	var quota store.QuotaError
	if errors.As(err, &quota) {
		fail(w, 422, "A workspace can have up to "+thousands(quota.Max)+" projects.")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	jsonResponse(w, 201, p)
}
func (h projectHandlers) update(w http.ResponseWriter, r *http.Request) {
	id, ok := projectID(r)
	if !ok {
		fail(w, 404, "Project not found.")
		return
	}
	in, ok := decodeValid(w, r, validProject, invalidProject)
	if !ok {
		return
	}
	p, err := h.Store.UpdateProject(r.Context(), currentWorkspace(r), id, in)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "Project not found.")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	jsonResponse(w, 200, p)
}
func (h projectHandlers) destroy(w http.ResponseWriter, r *http.Request) {
	id, ok := projectID(r)
	if !ok {
		fail(w, 404, "Project not found.")
		return
	}
	err := h.Store.DeleteProject(r.Context(), currentWorkspace(r), id)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "Project not found.")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(204)
}
