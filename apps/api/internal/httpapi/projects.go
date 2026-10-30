package httpapi

import (
	"database/sql"
	"errors"
	"forma/api/internal/store"
	"net/http"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	projectStatuses = []string{"Planned", "In progress", "Completed"}
	projectColors   = []string{"violet", "blue", "amber", "emerald", "rose"}
)

// maxSearchLength bounds the project search term in Unicode characters.
const maxSearchLength = 100

const invalidProject = "Check the project name, description, status, and color."

func validProject(in *store.ProjectInput) bool {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if utf8.RuneCountInString(in.Name) < 1 || utf8.RuneCountInString(in.Name) > 100 || utf8.RuneCountInString(in.Description) > 500 {
		return false
	}
	return slices.Contains(projectStatuses, in.Status) && slices.Contains(projectColors, in.Color)
}

// projectFilter validates the listing's "q", "status" and "sort" parameters.
func projectFilter(params map[string]string) (store.ProjectFilter, bool) {
	f := store.ProjectFilter{
		Search: strings.TrimSpace(params["q"]),
		Status: params["status"],
		Sort:   params["sort"],
	}
	if !validSearch(f.Search) {
		return store.ProjectFilter{}, false
	}
	if f.Status != "" && !slices.Contains(projectStatuses, f.Status) {
		return store.ProjectFilter{}, false
	}
	if f.Sort == "" {
		f.Sort = store.DefaultProjectSort
	}
	if !store.ProjectSortable(f.Sort) {
		return store.ProjectFilter{}, false
	}
	return f, true
}

// validSearch rejects invalid UTF-8 and control characters (PostgreSQL text
// cannot hold NUL) and bounds the length.
func validSearch(term string) bool {
	if !utf8.ValidString(term) || utf8.RuneCountInString(term) > maxSearchLength {
		return false
	}
	return !strings.ContainsFunc(term, unicode.IsControl)
}

// projectID reads the {project} path value, a prj_ public ID.
func projectID(r *http.Request) (string, bool) {
	id := r.PathValue("project")
	return id, validPublicID("prj", id)
}
func (s *Server) projects(w http.ResponseWriter, r *http.Request) {
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
	page, err := s.Store.ProjectPage(r.Context(), currentWorkspace(r).ID, filter, req)
	if err != nil {
		internalError(w, err)
		return
	}
	jsonResponse(w, 200, page)
}
func (s *Server) project(w http.ResponseWriter, r *http.Request) {
	id, ok := projectID(r)
	if !ok {
		fail(w, 404, "Project not found.")
		return
	}
	p, err := s.Store.Project(r.Context(), currentWorkspace(r).ID, id)
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
func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeValid(w, r, validProject, invalidProject)
	if !ok {
		return
	}
	p, err := s.Store.CreateProject(r.Context(), currentWorkspace(r), in)
	if err != nil {
		internalError(w, err)
		return
	}
	jsonResponse(w, 201, p)
}
func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	id, ok := projectID(r)
	if !ok {
		fail(w, 404, "Project not found.")
		return
	}
	in, ok := decodeValid(w, r, validProject, invalidProject)
	if !ok {
		return
	}
	p, err := s.Store.UpdateProject(r.Context(), currentWorkspace(r), id, in)
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
