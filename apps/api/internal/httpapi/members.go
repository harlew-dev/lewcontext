package httpapi

import (
	"database/sql"
	"errors"
	"lewcontext/api/internal/auth"
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
	return id, validPublicID("usr", id)
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
func (s *Server) members(w http.ResponseWriter, r *http.Request) {
	params, ok := queryParams(r, "page", "perPage", "q")
	if !ok {
		fail(w, 400, "Invalid query parameters.")
		return
	}
	req, ok := pageRequest(params)
	if !ok {
		fail(w, 400, "Invalid page.")
		return
	}
	filter := store.MemberFilter{Search: strings.TrimSpace(params["q"])}
	if !validSearch(filter.Search) {
		fail(w, 400, "Check the search.")
		return
	}
	page, err := s.Store.IndexMembers(r.Context(), currentWorkspace(r).ID, filter, req)
	if err != nil {
		internalError(w, err)
		return
	}
	jsonResponse(w, 200, page)
}
func (s *Server) updateMember(w http.ResponseWriter, r *http.Request) {
	id, ok := memberID(r)
	if !ok {
		fail(w, 404, "Member not found.")
		return
	}
	in, ok := decodeValid(w, r, validMemberInput, "Choose owner, admin, member or viewer.")
	if !ok {
		return
	}
	member, err := s.Store.UpdateMemberRole(r.Context(), currentWorkspace(r), current(r).User.ID, id, in.Role)
	if err != nil {
		failMembership(w, err)
		return
	}
	jsonResponse(w, 200, member)
}
func (s *Server) removeMember(w http.ResponseWriter, r *http.Request) {
	id, ok := memberID(r)
	if !ok {
		fail(w, 404, "Member not found.")
		return
	}
	if id == current(r).User.PublicID {
		fail(w, 422, "To leave, use Leave workspace.")
		return
	}
	if err := s.Store.RemoveMember(r.Context(), currentWorkspace(r), current(r).User.ID, id); err != nil {
		failMembership(w, err)
		return
	}
	w.WriteHeader(204)
}

// leaveWorkspace removes the signed-in user's own membership.
func (s *Server) leaveWorkspace(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.LeaveWorkspace(r.Context(), currentWorkspace(r), current(r).User.ID); err != nil {
		failMembership(w, err)
		return
	}
	w.WriteHeader(204)
}
