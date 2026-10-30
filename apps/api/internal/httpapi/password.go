package httpapi

import (
	"errors"
	"forma/api/internal/auth"
	"net/http"
)

// Changing the password takes two requests: verify checks the current
// password so the web app can move on to the new one, and change checks it
// again, because nothing remembers that verify passed. Both count against the
// account's sign-in rate limit, so a stolen session can't guess the password.

const wrongCurrentPassword = "Current password is incorrect."

var (
	errWrongPassword = errors.New("current password is incorrect")
	errSamePassword  = errors.New("new password equals the current one")
)

type passwordCheck struct {
	Password string `json:"password"`
}
type passwordChange struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

func (s *Server) verifyPassword(w http.ResponseWriter, r *http.Request) {
	var in passwordCheck
	if !decode(w, r, &in) {
		return
	}
	if !passwordInBounds(in.Password) {
		failField(w, 422, "password", wrongCurrentPassword)
		return
	}
	user := current(r).User
	if !s.accountLimit(w, r, user.Email) {
		return
	}
	hash, err := s.Store.PasswordHash(r.Context(), user.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	if !auth.CheckPassword(in.Password, hash) {
		failField(w, 422, "password", wrongCurrentPassword)
		return
	}
	w.WriteHeader(204)
}

// changePassword saves the new password, signs out every session of the
// user's, and starts a new one for this browser.
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var in passwordChange
	if !decode(w, r, &in) {
		return
	}
	if !passwordInBounds(in.CurrentPassword) {
		failField(w, 422, "currentPassword", wrongCurrentPassword)
		return
	}
	if !passwordInBounds(in.NewPassword) || !strongPassword(in.NewPassword) {
		failField(w, 422, "newPassword", "Use a new password of 12–128 characters.")
		return
	}
	user := current(r).User
	if !s.accountLimit(w, r, user.Email) {
		return
	}
	err := s.Store.ChangePassword(r.Context(), user.ID, func(hash string) (string, error) {
		if !auth.CheckPassword(in.CurrentPassword, hash) {
			return "", errWrongPassword
		}
		if in.NewPassword == in.CurrentPassword {
			return "", errSamePassword
		}
		return auth.HashPassword(in.NewPassword)
	})
	switch {
	case errors.Is(err, errWrongPassword):
		failField(w, 422, "currentPassword", wrongCurrentPassword)
	case errors.Is(err, errSamePassword):
		failField(w, 422, "newPassword", "Choose a password different from your current one.")
	case err != nil:
		internalError(w, err)
	default:
		s.startSession(w, r, user, 200)
	}
}
