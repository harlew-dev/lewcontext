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

// Projects are the …/projects routes inside a workspace (router/routes.go).
type Projects struct{ *Server }

func (h Projects) Index(w http.ResponseWriter, r *http.Request) {
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
	page, err := h.Repo.IndexProjects(r.Context(), Workspace(r).ID, filter, req)
	if err != nil {
		InternalError(w, err)
		return
	}
	JSON(w, 200, page)
}
func (h Projects) Show(w http.ResponseWriter, r *http.Request) {
	id, ok := projectID(r)
	if !ok {
		Fail(w, 404, "Project not found.")
		return
	}
	p, err := h.Repo.FindProject(r.Context(), Workspace(r).ID, id)
	if errors.Is(err, sql.ErrNoRows) {
		Fail(w, 404, "Project not found.")
		return
	}
	if err != nil {
		InternalError(w, err)
		return
	}
	JSON(w, 200, p)
}
func (h Projects) Store(w http.ResponseWriter, r *http.Request) {
	in, ok := DecodeValid(w, r, validProject, invalidProject)
	if !ok {
		return
	}
	p, err := h.Repo.CreateProject(r.Context(), Workspace(r), in)
	var quota store.QuotaError
	if errors.As(err, &quota) {
		Fail(w, 422, "A workspace can have up to "+thousands(quota.Max)+" projects.")
		return
	}
	if err != nil {
		InternalError(w, err)
		return
	}
	JSON(w, 201, p)
}
func (h Projects) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := projectID(r)
	if !ok {
		Fail(w, 404, "Project not found.")
		return
	}
	in, ok := DecodeValid(w, r, validProject, invalidProject)
	if !ok {
		return
	}
	p, err := h.Repo.UpdateProject(r.Context(), Workspace(r), id, in)
	if errors.Is(err, sql.ErrNoRows) {
		Fail(w, 404, "Project not found.")
		return
	}
	if err != nil {
		InternalError(w, err)
		return
	}
	JSON(w, 200, p)
}
func (h Projects) Destroy(w http.ResponseWriter, r *http.Request) {
	id, ok := projectID(r)
	if !ok {
		Fail(w, 404, "Project not found.")
		return
	}
	err := h.Repo.DeleteProject(r.Context(), Workspace(r), id)
	if errors.Is(err, sql.ErrNoRows) {
		Fail(w, 404, "Project not found.")
		return
	}
	if err != nil {
		InternalError(w, err)
		return
	}
	w.WriteHeader(204)
}
