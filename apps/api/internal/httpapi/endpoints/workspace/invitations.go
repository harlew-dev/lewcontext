package workspace

import (
	"database/sql"
	"errors"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/input"
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
	return id, input.PublicID("inv", id)
}

// Invitations are a workspace's …/invitations routes and the
// account-level /api/invitations/check and /accept (router/routes.go).
type Invitations struct{ *httpapi.Server }

func (h Invitations) Index(w http.ResponseWriter, r *http.Request) {
	params, ok := input.Query(r, "page", "perPage")
	if !ok {
		httpapi.Fail(w, 400, "Invalid query parameters.")
		return
	}
	req, ok := input.Page(params)
	if !ok {
		httpapi.Fail(w, 400, "Invalid page.")
		return
	}
	page, err := h.Repo.IndexInvitations(r.Context(), httpapi.Workspace(r).ID, req)
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	httpapi.JSON(w, 200, page)
}

// Store invites someone. Without email delivery there's nobody to send the
// link, so it answers 503 before counting anything. The answer is the same
// whether or not the email has an account.
func (h Invitations) Store(w http.ResponseWriter, r *http.Request) {
	if h.Config.MailDriver == config.MailOff {
		httpapi.Fail(w, 503, "Invitations aren’t available on this server.")
		return
	}
	var in invitationInput
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if !httpapi.ValidEmail(&in.Email) {
		httpapi.FailField(w, 422, "email", "Enter a valid email address.")
		return
	}
	role, ok := auth.ParseRole(in.Role)
	if !ok || role == auth.RoleOwner {
		httpapi.FailField(w, 422, "role", invitationRole)
		return
	}
	if !httpapi.Workspace(r).Role.Manages(role) {
		httpapi.Fail(w, 403, "You don’t have permission to do that in this workspace.")
		return
	}
	// Only valid requests spend the invited address's limit and the one
	// shared by everyone, in that order (invitation:per-user came first).
	if !h.Allow(w, r, httpapi.InvitationPerEmail, httpapi.InvitationPerEmail.Key(in.Email)) {
		return
	}
	if !h.Allow(w, r, httpapi.InvitationGlobal, httpapi.InvitationGlobal.Everyone()) {
		return
	}
	inv, err := h.Repo.CreateInvitation(r.Context(), httpapi.Workspace(r), httpapi.Session(r).User.ID, store.InvitationInput{Email: in.Email, Role: role})
	if err != nil {
		failInvitation(w, r, err)
		return
	}
	httpapi.JSON(w, 201, inv)
}

// failInvitation answers the errors creating an invitation can end in.
func failInvitation(w http.ResponseWriter, r *http.Request, err error) {
	var quota store.QuotaError
	switch {
	case errors.Is(err, store.ErrAlreadyMember):
		httpapi.FailField(w, 422, "email", "Already a member of this workspace.")
	case errors.Is(err, store.ErrAlreadyInvited):
		httpapi.FailField(w, 422, "email", "Already invited. Revoke it to send a new one.")
	case errors.Is(err, store.ErrTooManyInvitations):
		httpapi.Fail(w, 422, "A workspace can have up to "+strconv.Itoa(store.MaxPendingInvitations)+" pending invitations. Revoke some to send more.")
	case errors.As(err, &quota):
		httpapi.Fail(w, 422, "A workspace can have up to "+thousands(quota.Max)+" members, counting pending invitations.")
	default:
		failMembership(w, r, err)
	}
}

// Destroy revokes an invitation, so its link stops working.
func (h Invitations) Destroy(w http.ResponseWriter, r *http.Request) {
	id, ok := invitationID(r)
	if !ok {
		httpapi.Fail(w, 404, "Invitation not found.")
		return
	}
	err := h.Repo.RevokeInvitation(r.Context(), httpapi.Workspace(r), httpapi.Workspace(r).Role, id)
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.Fail(w, 404, "Invitation not found.")
		return
	}
	if err != nil {
		failMembership(w, r, err)
		return
	}
	w.WriteHeader(204)
}

// Check lets the link's page show the invitation before anyone signs in. It
// works signed out, so it shares sign-in's IP limit, as the reset check does.
func (h Invitations) Check(w http.ResponseWriter, r *http.Request) {
	var in invitationToken
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if !h.Allow(w, r, httpapi.AuthPerIP, httpapi.AuthPerIP.IPKey(httpapi.ClientIP(r))) {
		return
	}
	if !input.Token(in.Token) {
		httpapi.FailField(w, 422, "token", invitationInvalid)
		return
	}
	preview, err := h.Repo.CheckInvitation(r.Context(), auth.Digest(in.Token))
	if errors.Is(err, store.ErrInvitationInvalid) {
		httpapi.FailField(w, 422, "token", invitationInvalid)
		return
	}
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	httpapi.JSON(w, 200, preview)
}

// Accept adds the signed-in user to the invitation's workspace. It needs the
// token, which only the invited inbox has, and the session's email must be
// the invited one: registering or signing in never joins a workspace alone.
func (h Invitations) Accept(w http.ResponseWriter, r *http.Request) {
	var in invitationToken
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if !input.Token(in.Token) {
		httpapi.FailField(w, 422, "token", invitationInvalid)
		return
	}
	workspace, err := h.Repo.AcceptInvitation(r.Context(), auth.Digest(in.Token), httpapi.Session(r).User)
	var quota store.QuotaError
	switch {
	case errors.Is(err, store.ErrInvitationInvalid):
		httpapi.FailField(w, 422, "token", invitationInvalid)
	case errors.Is(err, store.ErrWrongInvitee):
		httpapi.Fail(w, 403, "This invitation was sent to a different email address.")
	case errors.As(err, &quota):
		httpapi.Fail(w, 422, "This workspace is full. Ask whoever invited you to make room.")
	case err != nil:
		httpapi.InternalError(w, r, err)
	default:
		httpapi.JSON(w, 200, workspace)
	}
}
