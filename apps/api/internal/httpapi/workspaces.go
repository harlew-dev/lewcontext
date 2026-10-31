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

// Workspaces are the /api/workspaces routes (router/routes.go).
type Workspaces struct{ *Server }

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
func (h Workspaces) Index(w http.ResponseWriter, r *http.Request) {
	params, ok := input.Query(r, "page", "perPage", "canLeave")
	if !ok {
		Fail(w, 400, "Invalid query parameters.")
		return
	}
	req, ok := input.Page(params)
	if !ok {
		Fail(w, 400, "Invalid page.")
		return
	}
	filter, ok := workspaceFilter(params)
	if !ok {
		Fail(w, 400, "Invalid canLeave filter.")
		return
	}
	page, err := h.Repo.IndexWorkspaces(r.Context(), Session(r).User.ID, filter, req)
	if err != nil {
		InternalError(w, err)
		return
	}
	JSON(w, 200, page)
}
func (h Workspaces) Show(w http.ResponseWriter, r *http.Request) {
	JSON(w, 200, Workspace(r))
}
func (h Workspaces) Store(w http.ResponseWriter, r *http.Request) {
	in, ok := DecodeValid(w, r, validWorkspace, invalidWorkspace)
	if !ok {
		return
	}
	workspace, err := h.Repo.CreateWorkspace(r.Context(), Session(r).User.ID, in)
	var quota store.QuotaError
	if errors.As(err, &quota) {
		Fail(w, 422, "You can own up to "+thousands(quota.Max)+" workspaces.")
		return
	}
	if err != nil {
		InternalError(w, err)
		return
	}
	JSON(w, 201, workspace)
}
func (h Workspaces) Update(w http.ResponseWriter, r *http.Request) {
	in, ok := DecodeValid(w, r, validWorkspace, invalidWorkspace)
	if !ok {
		return
	}
	current := Workspace(r)
	workspace, err := h.Repo.RenameWorkspace(r.Context(), current.ID, in)
	if err != nil {
		InternalError(w, err)
		return
	}
	workspace.Role, workspace.CanLeave = current.Role, current.CanLeave
	JSON(w, 200, workspace)
}

// workspaceDeletion confirms deleting a workspace: its name, typed, and the
// owner's password.
type workspaceDeletion struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

// Destroy deletes the workspace, with its projects and memberships. The name
// is compared with the current one, here and again under the workspace lock,
// so a tab opened before a rename can't delete it under its old name.
func (h Workspaces) Destroy(w http.ResponseWriter, r *http.Request) {
	var in workspaceDeletion
	if !Decode(w, r, &in) {
		return
	}
	workspace := Workspace(r)
	name := strings.TrimSpace(in.Name)
	if name != workspace.Name {
		FailField(w, 422, "name", wrongWorkspaceName)
		return
	}
	if !h.PasswordConfirmed(w, r, in.Password, events.PasswordStepDestroyWorkspace) {
		return
	}
	err := h.Repo.DeleteWorkspace(r.Context(), workspace, name)
	if errors.Is(err, store.ErrWrongName) {
		FailField(w, 422, "name", wrongWorkspaceName)
		return
	}
	if err != nil {
		InternalError(w, err)
		return
	}
	w.WriteHeader(204)
}
