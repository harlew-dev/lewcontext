package workspace

import (
	"database/sql"
	"errors"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/store"
	"net/http"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

var projectColors = []string{"violet", "blue", "amber", "emerald", "rose"}

const (
	invalidProject            = "Check the project name, description, status, and color."
	invalidProjectName        = "Use a project name of 1–100 characters, without control characters."
	invalidProjectDescription = "Use a description of up to 500 characters, without control characters other than line breaks and tabs."
)

// validProject trims the name and description and checks them, the status
// and the colour. When something is wrong it returns the field to blame
// ("name" or "description") with why, or no field for a status or colour
// the API doesn't offer. A name is 1–100 characters with no control
// character, as a workspace name; a description is up to 500, where line
// breaks and tabs are fine but no other control character (NUL among them,
// which PostgreSQL can't store), format character or line separator is
// (hiddenInDescription).
func validProject(in *store.ProjectInput) (field, message string, ok bool) {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	n := utf8.RuneCountInString(in.Name)
	switch {
	case n < 1 || n > 100 || strings.ContainsFunc(in.Name, unicode.IsControl):
		return "name", invalidProjectName, false
	case utf8.RuneCountInString(in.Description) > 500 || strings.ContainsFunc(in.Description, hiddenInDescription):
		return "description", invalidProjectDescription, false
	case !slices.Contains(input.ProjectStatuses, in.Status) || !slices.Contains(projectColors, in.Color):
		return "", invalidProject, false
	}
	return "", "", true
}

// hiddenInDescription reports whether c may not appear in a description:
// any control character but a line break or tab, a format character (bidi
// overrides such as U+202E, zero-width characters, U+FEFF), and the Unicode
// line and paragraph separators. The admin dashboard's reasons follow the
// same rule.
func hiddenInDescription(c rune) bool {
	switch {
	case c == '\n' || c == '\r' || c == '\t':
		return false
	case c == '\u2028' || c == '\u2029':
		return true
	}
	return unicode.IsControl(c) || unicode.Is(unicode.Cf, c)
}

// decodeProject reads a project from the body: 400 for a body that isn't
// one, 422 (on the field when there is one) for one validProject refuses.
func decodeProject(w http.ResponseWriter, r *http.Request) (store.ProjectInput, bool) {
	var in store.ProjectInput
	if !httpapi.Decode(w, r, &in) {
		return in, false
	}
	field, message, ok := validProject(&in)
	switch {
	case ok:
		return in, true
	case field == "":
		httpapi.Fail(w, 422, message)
	default:
		httpapi.FailField(w, 422, field, message)
	}
	return in, false
}

// projectID reads the {project} path value, a prj_ public ID.
func projectID(r *http.Request) (string, bool) {
	id := r.PathValue("project")
	return id, input.PublicID("prj", id)
}

// Projects are the …/projects routes inside a workspace (router/routes.go).
type Projects struct{ *httpapi.Server }

func (h Projects) Index(w http.ResponseWriter, r *http.Request) {
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
	page, err := h.Repo.IndexProjects(r.Context(), httpapi.Workspace(r).ID, filter, req)
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	httpapi.JSON(w, 200, page)
}
func (h Projects) Show(w http.ResponseWriter, r *http.Request) {
	id, ok := projectID(r)
	if !ok {
		httpapi.Fail(w, 404, "Project not found.")
		return
	}
	p, err := h.Repo.FindProject(r.Context(), httpapi.Workspace(r).ID, id)
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.Fail(w, 404, "Project not found.")
		return
	}
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	httpapi.JSON(w, 200, p)
}
func (h Projects) Store(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeProject(w, r)
	if !ok {
		return
	}
	p, err := h.Repo.CreateProject(r.Context(), httpapi.Workspace(r), in)
	var quota store.QuotaError
	if errors.As(err, &quota) {
		httpapi.Fail(w, 422, "A workspace can have up to "+thousands(quota.Max)+" projects.")
		return
	}
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	httpapi.JSON(w, 201, p)
}
func (h Projects) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := projectID(r)
	if !ok {
		httpapi.Fail(w, 404, "Project not found.")
		return
	}
	in, ok := decodeProject(w, r)
	if !ok {
		return
	}
	p, err := h.Repo.UpdateProject(r.Context(), httpapi.Workspace(r), id, in)
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.Fail(w, 404, "Project not found.")
		return
	}
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	httpapi.JSON(w, 200, p)
}
func (h Projects) Destroy(w http.ResponseWriter, r *http.Request) {
	id, ok := projectID(r)
	if !ok {
		httpapi.Fail(w, 404, "Project not found.")
		return
	}
	err := h.Repo.DeleteProject(r.Context(), httpapi.Workspace(r), id)
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.Fail(w, 404, "Project not found.")
		return
	}
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	w.WriteHeader(204)
}
