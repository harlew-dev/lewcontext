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
		fail(w, 404, "Member not found.")
	case errors.Is(err, store.ErrNotAssignable):
		fail(w, 403, "You don’t have permission to do that in this workspace.")
	case errors.Is(err, store.ErrLastOwner):
		fail(w, 409, "A workspace needs at least one owner.")
	default:
		internalError(w, err)
	}
}

// memberHandlers are the …/members routes inside a workspace (routes.go).
type memberHandlers struct{ *Server }

func (h memberHandlers) index(w http.ResponseWriter, r *http.Request) {
	params, ok := input.Query(r, "page", "perPage", "q")
	if !ok {
		fail(w, 400, "Invalid query parameters.")
		return
	}
	req, ok := input.Page(params)
	if !ok {
		fail(w, 400, "Invalid page.")
		return
	}
	filter := store.MemberFilter{Search: strings.TrimSpace(params["q"])}
	if !input.Search(filter.Search) {
		fail(w, 400, "Check the search.")
		return
	}
	page, err := h.Store.IndexMembers(r.Context(), currentWorkspace(r).ID, filter, req)
	if err != nil {
		internalError(w, err)
		return
	}
	jsonResponse(w, 200, page)
}
func (h memberHandlers) update(w http.ResponseWriter, r *http.Request) {
	id, ok := memberID(r)
	if !ok {
		fail(w, 404, "Member not found.")
		return
	}
	in, ok := decodeValid(w, r, validMemberInput, "Choose owner, admin, member or viewer.")
	if !ok {
		return
	}
	member, err := h.Store.UpdateMemberRole(r.Context(), currentWorkspace(r), current(r).User.ID, id, in.Role)
	if err != nil {
		failMembership(w, err)
		return
	}
	jsonResponse(w, 200, member)
}
func (h memberHandlers) destroy(w http.ResponseWriter, r *http.Request) {
	id, ok := memberID(r)
	if !ok {
		fail(w, 404, "Member not found.")
		return
	}
	if id == current(r).User.PublicID {
		fail(w, 422, "To leave, use Leave workspace.")
		return
	}
	if err := h.Store.RemoveMember(r.Context(), currentWorkspace(r), current(r).User.ID, id); err != nil {
		failMembership(w, err)
		return
	}
	w.WriteHeader(204)
}

// membershipHandlers are the signed-in user's own membership of the workspace
// (…/membership), a singleton resource: destroy is leaving.
type membershipHandlers struct{ *Server }

// destroy removes the signed-in user's own membership: they leave.
func (h membershipHandlers) destroy(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.LeaveWorkspace(r.Context(), currentWorkspace(r), current(r).User.ID); err != nil {
		failMembership(w, err)
		return
	}
	w.WriteHeader(204)
}
