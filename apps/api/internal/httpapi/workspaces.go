package httpapi

import (
	"lewcontext/api/internal/store"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

const invalidWorkspace = "Use a workspace name of 1–80 characters."

// validWorkspace trims the name and requires 1–80 characters without control
// characters, matching the column's CHECK.
func validWorkspace(in *store.WorkspaceInput) bool {
	in.Name = strings.TrimSpace(in.Name)
	n := utf8.RuneCountInString(in.Name)
	return n >= 1 && n <= 80 && !strings.ContainsFunc(in.Name, unicode.IsControl)
}
func (s *Server) workspaces(w http.ResponseWriter, r *http.Request) {
	params, ok := queryParams(r, "page", "perPage")
	if !ok {
		fail(w, 400, "Invalid query parameters.")
		return
	}
	req, ok := pageRequest(params)
	if !ok {
		fail(w, 400, "Invalid page.")
		return
	}
	page, err := s.Store.WorkspacePage(r.Context(), current(r).User.ID, req)
	if err != nil {
		internalError(w, err)
		return
	}
	jsonResponse(w, 200, page)
}
func (s *Server) workspace(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, 200, currentWorkspace(r))
}
func (s *Server) createWorkspace(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeValid(w, r, validWorkspace, invalidWorkspace)
	if !ok {
		return
	}
	workspace, err := s.Store.CreateWorkspace(r.Context(), current(r).User.ID, in)
	if err != nil {
		internalError(w, err)
		return
	}
	jsonResponse(w, 201, workspace)
}
func (s *Server) updateWorkspace(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeValid(w, r, validWorkspace, invalidWorkspace)
	if !ok {
		return
	}
	current := currentWorkspace(r)
	workspace, err := s.Store.RenameWorkspace(r.Context(), current.ID, in)
	if err != nil {
		internalError(w, err)
		return
	}
	workspace.Role = current.Role
	jsonResponse(w, 200, workspace)
}
