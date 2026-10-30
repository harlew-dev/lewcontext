package httpapi

import (
	"database/sql"
	"errors"
	"forma/api/internal/store"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

func validProject(in *store.ProjectInput) bool {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if utf8.RuneCountInString(in.Name) < 1 || utf8.RuneCountInString(in.Name) > 100 || utf8.RuneCountInString(in.Description) > 500 {
		return false
	}
	switch in.Status {
	case "Planned", "In progress", "Completed":
	default:
		return false
	}
	switch in.Color {
	case "violet", "blue", "amber", "emerald", "rose":
	default:
		return false
	}
	return true
}
func (s *Server) projects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.Store.Projects(r.Context(), current(r).User.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	jsonResponse(w, 200, projects)
}
func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var in store.ProjectInput
	if !decode(w, r, &in) {
		return
	}
	if !validProject(&in) {
		fail(w, 422, "Check the project name, description, status, and color.")
		return
	}
	p, err := s.Store.CreateProject(r.Context(), current(r).User.ID, in)
	if err != nil {
		internalError(w, err)
		return
	}
	jsonResponse(w, 201, p)
}
func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		fail(w, 400, "Invalid project ID.")
		return
	}
	var in store.ProjectInput
	if !decode(w, r, &in) {
		return
	}
	if !validProject(&in) {
		fail(w, 422, "Check the project name, description, status, and color.")
		return
	}
	p, err := s.Store.UpdateProject(r.Context(), current(r).User.ID, id, in)
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
