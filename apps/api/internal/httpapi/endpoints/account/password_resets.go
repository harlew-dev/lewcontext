package account

import (
	"errors"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
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

// PasswordResets are reset links, /api/auth/password-reset (router/routes.go).
type PasswordResets struct{ *httpapi.Server }

func (h PasswordResets) Request(w http.ResponseWriter, r *http.Request) {
	if h.Config.MailDriver == config.MailOff {
		httpapi.Fail(w, 503, "Password reset isn’t available yet.")
		return
	}
	var in resetRequest
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if !httpapi.ValidEmail(&in.Email) {
		httpapi.FailField(w, 422, "email", "Enter a valid email address.")
		return
	}
	if !h.allowPasswordResetRequest(w, r, in.Email) {
		return
	}
	e := events.PasswordResetRequested{Email: in.Email}
	if err := h.Repo.Record(r.Context(), e, store.EventScope{}); err != nil {
		httpapi.InternalError(w, err)
		return
	}
	w.WriteHeader(202)
}

// allowPasswordResetRequest counts a request against each limit in turn, so
// one refused early doesn't spend the later ones: a single client can't use
// up the limit shared by everyone. Unknown emails count like real ones.
func (h PasswordResets) allowPasswordResetRequest(w http.ResponseWriter, r *http.Request, email string) bool {
	return h.Allow(w, r, httpapi.AuthPerIP, httpapi.AuthPerIP.IPKey(httpapi.ClientIP(r))) &&
		h.Allow(w, r, httpapi.PasswordResetPerIP, httpapi.PasswordResetPerIP.IPKey(httpapi.ClientIP(r))) &&
		h.Allow(w, r, httpapi.PasswordResetPerEmail, httpapi.PasswordResetPerEmail.Key(email)) &&
		h.Allow(w, r, httpapi.PasswordResetGlobal, httpapi.PasswordResetGlobal.Everyone())
}

// Check lets the link's page say straight away that a link has
// expired, before the user types a new password.
func (h PasswordResets) Check(w http.ResponseWriter, r *http.Request) {
	var in resetCheck
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if !h.Allow(w, r, httpapi.AuthPerIP, httpapi.AuthPerIP.IPKey(httpapi.ClientIP(r))) {
		return
	}
	if !input.Token(in.Token) {
		httpapi.FailField(w, 422, "token", resetLinkInvalid)
		return
	}
	live, err := h.Repo.CheckPasswordReset(r.Context(), auth.Digest(in.Token))
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	if !live {
		httpapi.FailField(w, 422, "token", resetLinkInvalid)
		return
	}
	w.WriteHeader(204)
}

// Reset sets the new password with a live link. The account isn't
// signed in afterwards: every session ends, and the user signs in with the
// new password (which also lets a password manager save it).
func (h PasswordResets) Reset(w http.ResponseWriter, r *http.Request) {
	var in resetPassword
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if !input.Token(in.Token) {
		httpapi.FailField(w, 422, "token", resetLinkInvalid)
		return
	}
	if !httpapi.PasswordInBounds(in.NewPassword) || !httpapi.StrongPassword(in.NewPassword) {
		httpapi.FailField(w, 422, "newPassword", "Use a new password of 12–128 characters.")
		return
	}
	digest := auth.Digest(in.Token)
	// A cheap check first, so a dead link costs no hashing. ResetPassword
	// checks again under a lock.
	live, err := h.Repo.CheckPasswordReset(r.Context(), digest)
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	if !live {
		httpapi.FailField(w, 422, "token", resetLinkInvalid)
		return
	}
	hash, err := auth.HashPassword(in.NewPassword)
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	user, err := h.Repo.ResetPassword(r.Context(), digest, hash)
	if errors.Is(err, store.ErrResetInvalid) {
		httpapi.FailField(w, 422, "token", resetLinkInvalid)
		return
	}
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	// The email lets the login page fill it in.
	httpapi.JSON(w, 200, map[string]string{"email": user.Email})
}
