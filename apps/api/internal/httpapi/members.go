package httpapi

import (
	"database/sql"
	"errors"
	"lewcontext/api/internal/auth"
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
		Fail(w, 404, "Member not found.")
	case errors.Is(err, store.ErrNotAssignable):
		Fail(w, 403, "You don’t have permission to do that in this workspace.")
	case errors.Is(err, store.ErrLastOwner):
		Fail(w, 409, "A workspace needs at least one owner.")
	default:
		InternalError(w, err)
	}
}

// Members are the …/members routes inside a workspace (router/routes.go).
type Members struct{ *Server }

func (h Members) Index(w http.ResponseWriter, r *http.Request) {
	params, ok := input.Query(r, "page", "perPage", "q")
	if !ok {
		Fail(w, 400, "Invalid query parameters.")
		return
	}
	req, ok := input.Page(params)
	if !ok {
		Fail(w, 400, "Invalid page.")
		return
	}
	filter := store.MemberFilter{Search: strings.TrimSpace(params["q"])}
	if !input.Search(filter.Search) {
		Fail(w, 400, "Check the search.")
		return
	}
	page, err := h.Repo.IndexMembers(r.Context(), Workspace(r).ID, filter, req)
	if err != nil {
		InternalError(w, err)
		return
	}
	JSON(w, 200, page)
}
func (h Members) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := memberID(r)
	if !ok {
		Fail(w, 404, "Member not found.")
		return
	}
	in, ok := DecodeValid(w, r, validMemberInput, "Choose owner, admin, member or viewer.")
	if !ok {
		return
	}
	member, err := h.Repo.UpdateMemberRole(r.Context(), Workspace(r), Session(r).User.ID, id, in.Role)
	if err != nil {
		failMembership(w, err)
		return
	}
	JSON(w, 200, member)
}
func (h Members) Destroy(w http.ResponseWriter, r *http.Request) {
	id, ok := memberID(r)
	if !ok {
		Fail(w, 404, "Member not found.")
		return
	}
	if id == Session(r).User.PublicID {
		Fail(w, 422, "To leave, use Leave workspace.")
		return
	}
	if err := h.Repo.RemoveMember(r.Context(), Workspace(r), Session(r).User.ID, id); err != nil {
		failMembership(w, err)
		return
	}
	w.WriteHeader(204)
}

// Membership is the signed-in user's own membership of the workspace
// (…/membership), a singleton resource: destroy is leaving.
type Membership struct{ *Server }

// Destroy removes the signed-in user's own membership: they leave.
func (h Membership) Destroy(w http.ResponseWriter, r *http.Request) {
	if err := h.Repo.LeaveWorkspace(r.Context(), Workspace(r), Session(r).User.ID); err != nil {
		failMembership(w, err)
		return
	}
	w.WriteHeader(204)
}
