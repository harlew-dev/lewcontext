package httpapi

import (
	"errors"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/store"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	invalidWorkspace   = "Use a workspace name of 1–80 characters."
	wrongWorkspaceName = "Type the workspace’s name to confirm."
)

// validWorkspace trims the name and requires 1–80 characters without control
// characters, matching the column's CHECK.
func validWorkspace(in *store.WorkspaceInput) bool {
	in.Name = strings.TrimSpace(in.Name)
	n := utf8.RuneCountInString(in.Name)
	return n >= 1 && n <= 80 && !strings.ContainsFunc(in.Name, unicode.IsControl)
}

// workspaceHandlers are the /api/workspaces routes (routes.go).
type workspaceHandlers struct{ *Server }

// workspaceFilter reads the listing's "canLeave" parameter. Only "false" (the
// workspaces you're the only owner of, which block deleting your account) has
// a use, so it's the only value accepted.
func workspaceFilter(params map[string]string) (store.WorkspaceFilter, bool) {
	value, ok := params["canLeave"]
	if !ok {
		return store.WorkspaceFilter{}, true
	}
	if value != "false" {
		return store.WorkspaceFilter{}, false
	}
	canLeave := false
	return store.WorkspaceFilter{CanLeave: &canLeave}, true
}
func (h workspaceHandlers) index(w http.ResponseWriter, r *http.Request) {
	params, ok := input.Query(r, "page", "perPage", "canLeave")
	if !ok {
		fail(w, 400, "Invalid query parameters.")
		return
	}
	req, ok := input.Page(params)
	if !ok {
		fail(w, 400, "Invalid page.")
		return
	}
	filter, ok := workspaceFilter(params)
	if !ok {
		fail(w, 400, "Invalid canLeave filter.")
		return
	}
	page, err := h.Store.IndexWorkspaces(r.Context(), current(r).User.ID, filter, req)
	if err != nil {
		internalError(w, err)
		return
	}
	jsonResponse(w, 200, page)
}
func (h workspaceHandlers) show(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, 200, currentWorkspace(r))
}
func (h workspaceHandlers) store(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeValid(w, r, validWorkspace, invalidWorkspace)
	if !ok {
		return
	}
	workspace, err := h.Store.CreateWorkspace(r.Context(), current(r).User.ID, in)
	var quota store.QuotaError
	if errors.As(err, &quota) {
		fail(w, 422, "You can own up to "+thousands(quota.Max)+" workspaces.")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	jsonResponse(w, 201, workspace)
}
func (h workspaceHandlers) update(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeValid(w, r, validWorkspace, invalidWorkspace)
	if !ok {
		return
	}
	current := currentWorkspace(r)
	workspace, err := h.Store.RenameWorkspace(r.Context(), current.ID, in)
	if err != nil {
		internalError(w, err)
		return
	}
	workspace.Role, workspace.CanLeave = current.Role, current.CanLeave
	jsonResponse(w, 200, workspace)
}

// workspaceDeletion confirms deleting a workspace: its name, typed, and the
// owner's password.
type workspaceDeletion struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

// destroy deletes the workspace, with its projects and memberships. The name
// is compared with the current one, here and again under the workspace lock,
// so a tab opened before a rename can't delete it under its old name.
func (h workspaceHandlers) destroy(w http.ResponseWriter, r *http.Request) {
	var in workspaceDeletion
	if !decode(w, r, &in) {
		return
	}
	workspace := currentWorkspace(r)
	name := strings.TrimSpace(in.Name)
	if name != workspace.Name {
		failField(w, 422, "name", wrongWorkspaceName)
		return
	}
	if !h.passwordConfirmed(w, r, in.Password, events.PasswordStepDestroyWorkspace) {
		return
	}
	err := h.Store.DeleteWorkspace(r.Context(), workspace, name)
	if errors.Is(err, store.ErrWrongName) {
		failField(w, 422, "name", wrongWorkspaceName)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(204)
}
