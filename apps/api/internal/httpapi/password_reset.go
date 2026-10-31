package httpapi

import (
	"errors"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/store"
	"net/http"
)

// Password reset by link, for someone who can't sign in. Requesting a link
// only records auth.password_reset.requested and answers the same for every
// email, without looking the account up; the mail.password_reset listener
// (internal/mail) issues and sends the link. The link's page checks it, then
// sets a new password with it, which signs out every session.

const resetLinkInvalid = "This link has expired or was already used. Request a new one."

type resetRequest struct {
	Email string `json:"email"`
}
type resetCheck struct {
	Token string `json:"token"`
}
type resetPassword struct {
	Token       string `json:"token"`
	NewPassword string `json:"newPassword"`
}

func (s *Server) requestPasswordReset(w http.ResponseWriter, r *http.Request) {
	if s.Config.MailDriver == config.MailOff {
		fail(w, 503, "Password reset isn’t available yet.")
		return
	}
	var in resetRequest
	if !decode(w, r, &in) {
		return
	}
	if !validEmail(&in.Email) {
		failField(w, 422, "email", "Enter a valid email address.")
		return
	}
	if !s.allowPasswordResetRequest(w, r, in.Email) {
		return
	}
	e := events.PasswordResetRequested{Email: in.Email}
	if err := s.Store.Record(r.Context(), e, store.EventScope{}); err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(202)
}

// allowPasswordResetRequest counts a request against each limit in turn, so
// one refused early doesn't spend the later ones: a single client can't use
// up the limit shared by everyone. Unknown emails count like real ones.
func (s *Server) allowPasswordResetRequest(w http.ResponseWriter, r *http.Request, email string) bool {
	return s.allow(w, r, authPerIP, authPerIP.IPKey(ClientIP(r))) &&
		s.allow(w, r, passwordResetPerIP, passwordResetPerIP.IPKey(ClientIP(r))) &&
		s.allow(w, r, passwordResetPerEmail, passwordResetPerEmail.Key(email)) &&
		s.allow(w, r, passwordResetGlobal, passwordResetGlobal.Everyone())
}

// checkPasswordReset lets the link's page say straight away that a link has
// expired, before the user types a new password.
func (s *Server) checkPasswordReset(w http.ResponseWriter, r *http.Request) {
	var in resetCheck
	if !decode(w, r, &in) {
		return
	}
	if !s.allow(w, r, authPerIP, authPerIP.IPKey(ClientIP(r))) {
		return
	}
	if !input.Token(in.Token) {
		failField(w, 422, "token", resetLinkInvalid)
		return
	}
	live, err := s.Store.CheckPasswordReset(r.Context(), auth.Digest(in.Token))
	if err != nil {
		internalError(w, err)
		return
	}
	if !live {
		failField(w, 422, "token", resetLinkInvalid)
		return
	}
	w.WriteHeader(204)
}

// resetPassword sets the new password with a live link. The account isn't
// signed in afterwards: every session ends, and the user signs in with the
// new password (which also lets a password manager save it).
func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request) {
	var in resetPassword
	if !decode(w, r, &in) {
		return
	}
	if !input.Token(in.Token) {
		failField(w, 422, "token", resetLinkInvalid)
		return
	}
	if !passwordInBounds(in.NewPassword) || !strongPassword(in.NewPassword) {
		failField(w, 422, "newPassword", "Use a new password of 12–128 characters.")
		return
	}
	digest := auth.Digest(in.Token)
	// A cheap check first, so a dead link costs no hashing. ResetPassword
	// checks again under a lock.
	live, err := s.Store.CheckPasswordReset(r.Context(), digest)
	if err != nil {
		internalError(w, err)
		return
	}
	if !live {
		failField(w, 422, "token", resetLinkInvalid)
		return
	}
	hash, err := auth.HashPassword(in.NewPassword)
	if err != nil {
		internalError(w, err)
		return
	}
	user, err := s.Store.ResetPassword(r.Context(), digest, hash)
	if errors.Is(err, store.ErrResetInvalid) {
		failField(w, 422, "token", resetLinkInvalid)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	// The email lets the login page fill it in.
	jsonResponse(w, 200, map[string]string{"email": user.Email})
}
