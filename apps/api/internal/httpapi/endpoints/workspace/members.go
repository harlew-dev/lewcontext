package workspace

import (
	"database/sql"
	"errors"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/store"
	"net/http"
	"strings"
)

// memberInput is the body of a role change.
type memberInput struct {
	Role auth.Role `json:"role"`
}

func validMemberInput(in *memberInput) bool {
	_, ok := auth.ParseRole(string(in.Role))
	return ok
}

// memberID reads the {member} path value, the user's usr_ public ID.
func memberID(r *http.Request) (string, bool) {
	id := r.PathValue("member")
	return id, input.PublicID("usr", id)
}

// failMembership answers the errors a membership change can end in.
func failMembership(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		httpapi.Fail(w, 404, "Member not found.")
	case errors.Is(err, store.ErrNotAssignable):
		httpapi.Fail(w, 403, "You don’t have permission to do that in this workspace.")
	case errors.Is(err, store.ErrLastOwner):
		httpapi.Fail(w, 409, "A workspace needs at least one owner.")
	default:
		httpapi.InternalError(w, err)
	}
}

// Members are the …/members routes inside a workspace (router/routes.go).
type Members struct{ *httpapi.Server }

func (h Members) Index(w http.ResponseWriter, r *http.Request) {
	params, ok := input.Query(r, "page", "perPage", "q")
	if !ok {
		httpapi.Fail(w, 400, "Invalid query parameters.")
		return
	}
	req, ok := input.Page(params)
	if !ok {
		httpapi.Fail(w, 400, "Invalid page.")
		return
	}
	filter := store.MemberFilter{Search: strings.TrimSpace(params["q"])}
	if !input.Search(filter.Search) {
		httpapi.Fail(w, 400, "Check the search.")
		return
	}
	page, err := h.Repo.IndexMembers(r.Context(), httpapi.Workspace(r).ID, filter, req)
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	httpapi.JSON(w, 200, page)
}
func (h Members) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := memberID(r)
	if !ok {
		httpapi.Fail(w, 404, "Member not found.")
		return
	}
	in, ok := httpapi.DecodeValid(w, r, validMemberInput, "Choose owner, admin, member or viewer.")
	if !ok {
		return
	}
	member, err := h.Repo.UpdateMemberRole(r.Context(), httpapi.Workspace(r), httpapi.Session(r).User.ID, id, in.Role)
	if err != nil {
		failMembership(w, err)
		return
	}
	httpapi.JSON(w, 200, member)
}
func (h Members) Destroy(w http.ResponseWriter, r *http.Request) {
	id, ok := memberID(r)
	if !ok {
		httpapi.Fail(w, 404, "Member not found.")
		return
	}
	if id == httpapi.Session(r).User.PublicID {
		httpapi.Fail(w, 422, "To leave, use Leave workspace.")
		return
	}
	if err := h.Repo.RemoveMember(r.Context(), httpapi.Workspace(r), httpapi.Session(r).User.ID, id); err != nil {
		failMembership(w, err)
		return
	}
	w.WriteHeader(204)
}
