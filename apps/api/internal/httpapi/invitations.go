package httpapi

import (
	"database/sql"
	"errors"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/store"
	"net/http"
	"strconv"
)

const (
	invitationInvalid = "This invitation has expired or was revoked."
	invitationRole    = "Choose admin, member or viewer."
)

// invitationInput is the body of an invitation: who, and as what.
type invitationInput struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

// invitationToken is the body of check and accept: the token from the link.
// It travels only in JSON bodies, never in an API URL.
type invitationToken struct {
	Token string `json:"token"`
}

// invitationID reads the {invitation} path value, an inv_ public ID.
func invitationID(r *http.Request) (string, bool) {
	id := r.PathValue("invitation")
	return id, validPublicID("inv", id)
}

// invitationHandlers are a workspace's …/invitations routes and the
// account-level /api/invitations/check and /accept (routes.go).
type invitationHandlers struct{ *Server }

func (h invitationHandlers) index(w http.ResponseWriter, r *http.Request) {
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
	page, err := h.Store.IndexInvitations(r.Context(), currentWorkspace(r).ID, req)
	if err != nil {
		internalError(w, err)
		return
	}
	jsonResponse(w, 200, page)
}

// store invites someone. Without email delivery there's nobody to send the
// link, so it answers 503 before counting anything. The answer is the same
// whether or not the email has an account.
func (h invitationHandlers) store(w http.ResponseWriter, r *http.Request) {
	if h.Config.MailDriver == config.MailOff {
		fail(w, 503, "Invitations aren’t available on this server.")
		return
	}
	var in invitationInput
	if !decode(w, r, &in) {
		return
	}
	if !validEmail(&in.Email) {
		failField(w, 422, "email", "Enter a valid email address.")
		return
	}
	role, ok := auth.ParseRole(in.Role)
	if !ok || role == auth.RoleOwner {
		failField(w, 422, "role", invitationRole)
		return
	}
	if !currentWorkspace(r).Role.Manages(role) {
		fail(w, 403, "You don’t have permission to do that in this workspace.")
		return
	}
	// Only valid requests spend the invited address's limit and the one
	// shared by everyone, in that order (invitation:per-user came first).
	if !h.allow(w, r, invitationPerEmail, invitationPerEmail.key(in.Email)) {
		return
	}
	if !h.allow(w, r, invitationGlobal, invitationGlobal.everyone()) {
		return
	}
	inv, err := h.Store.CreateInvitation(r.Context(), currentWorkspace(r), current(r).User.ID, store.InvitationInput{Email: in.Email, Role: role})
	if err != nil {
		failInvitation(w, err)
		return
	}
	jsonResponse(w, 201, inv)
}

// failInvitation answers the errors creating an invitation can end in.
func failInvitation(w http.ResponseWriter, err error) {
	var quota store.QuotaError
	switch {
	case errors.Is(err, store.ErrAlreadyMember):
		failField(w, 422, "email", "Already a member of this workspace.")
	case errors.Is(err, store.ErrAlreadyInvited):
		failField(w, 422, "email", "Already invited. Revoke it to send a new one.")
	case errors.Is(err, store.ErrTooManyInvitations):
		fail(w, 422, "A workspace can have up to "+strconv.Itoa(store.MaxPendingInvitations)+" pending invitations. Revoke some to send more.")
	case errors.As(err, &quota):
		fail(w, 422, "A workspace can have up to "+thousands(quota.Max)+" members, counting pending invitations.")
	default:
		failMembership(w, err)
	}
}

// destroy revokes an invitation, so its link stops working.
func (h invitationHandlers) destroy(w http.ResponseWriter, r *http.Request) {
	id, ok := invitationID(r)
	if !ok {
		fail(w, 404, "Invitation not found.")
		return
	}
	err := h.Store.RevokeInvitation(r.Context(), currentWorkspace(r), currentWorkspace(r).Role, id)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "Invitation not found.")
		return
	}
	if err != nil {
		failMembership(w, err)
		return
	}
	w.WriteHeader(204)
}

// check lets the link's page show the invitation before anyone signs in. It
// works signed out, so it shares sign-in's IP limit, as the reset check does.
func (h invitationHandlers) check(w http.ResponseWriter, r *http.Request) {
	var in invitationToken
	if !decode(w, r, &in) {
		return
	}
	if !h.allow(w, r, authPerIP, authPerIP.ipKey(r)) {
		return
	}
	if !validResetToken(in.Token) {
		failField(w, 422, "token", invitationInvalid)
		return
	}
	preview, err := h.Store.CheckInvitation(r.Context(), auth.Digest(in.Token))
	if errors.Is(err, store.ErrInvitationInvalid) {
		failField(w, 422, "token", invitationInvalid)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	jsonResponse(w, 200, preview)
}

// accept adds the signed-in user to the invitation's workspace. It needs the
// token, which only the invited inbox has, and the session's email must be
// the invited one: registering or signing in never joins a workspace alone.
func (h invitationHandlers) accept(w http.ResponseWriter, r *http.Request) {
	var in invitationToken
	if !decode(w, r, &in) {
		return
	}
	if !validResetToken(in.Token) {
		failField(w, 422, "token", invitationInvalid)
		return
	}
	workspace, err := h.Store.AcceptInvitation(r.Context(), auth.Digest(in.Token), current(r).User)
	var quota store.QuotaError
	switch {
	case errors.Is(err, store.ErrInvitationInvalid):
		failField(w, 422, "token", invitationInvalid)
	case errors.Is(err, store.ErrWrongInvitee):
		fail(w, 403, "This invitation was sent to a different email address.")
	case errors.As(err, &quota):
		fail(w, 422, "This workspace is full. Ask whoever invited you to make room.")
	case err != nil:
		internalError(w, err)
	default:
		jsonResponse(w, 200, workspace)
	}
}
