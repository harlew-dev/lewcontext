package httpapi

import (
	"errors"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/store"
	"net/http"
)

// Changing the password takes two requests: verify checks the current
// password so the web app can move on to the new one, and change checks it
// again, because nothing remembers that verify passed. Both count against the
// account's sign-in rate limit, so a stolen session can't guess the password.
// Deleting a workspace or the account checks it the same way
// (passwordConfirmed).

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
	if s.passwordConfirmed(w, r, in.Password, events.PasswordStepVerify) {
		w.WriteHeader(204)
	}
}

// passwordConfirmed checks that password is the signed-in user's, for the
// request step, counting against the account's sign-in limit and recording
// auth.password_checked. When it isn't, it answers 422 with field
// "password"; it returns false whenever it has answered. Routes using it run
// behind limitAuth.
func (s *Server) passwordConfirmed(w http.ResponseWriter, r *http.Request, password, step string) bool {
	if !passwordInBounds(password) {
		failField(w, 422, "password", wrongCurrentPassword)
		return false
	}
	user := current(r).User
	if !s.accountLimit(w, r, user.Email) {
		return false
	}
	hash, err := s.Store.FindPasswordHash(r.Context(), user.ID)
	if err != nil {
		internalError(w, err)
		return false
	}
	ok := auth.CheckPassword(password, hash)
	if err := s.recordPasswordCheck(r, user, ok, step); err != nil {
		internalError(w, err)
		return false
	}
	if !ok {
		failField(w, 422, "password", wrongCurrentPassword)
	}
	return ok
}

// recordPasswordCheck records auth.password_checked.
func (s *Server) recordPasswordCheck(r *http.Request, user store.User, ok bool, step string) error {
	e := events.PasswordChecked{User: user.PublicID, OK: ok, Step: step}
	return s.Store.Record(r.Context(), e, store.EventScope{UserID: user.ID})
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
	err := s.Store.ChangePassword(r.Context(), user, s.sessionDigest(r), func(hash string) (string, error) {
		if !auth.CheckPassword(in.CurrentPassword, hash) {
			return "", errWrongPassword
		}
		if in.NewPassword == in.CurrentPassword {
			return "", errSamePassword
		}
		return auth.HashPassword(in.NewPassword)
	})
	// The check passed unless the current password was wrong; record it once
	// the change has committed or rolled back.
	if err == nil || errors.Is(err, errWrongPassword) || errors.Is(err, errSamePassword) {
		if recordErr := s.recordPasswordCheck(r, user, !errors.Is(err, errWrongPassword), events.PasswordStepUpdate); recordErr != nil {
			internalError(w, recordErr)
			return
		}
	}
	switch {
	case errors.Is(err, errWrongPassword):
		failField(w, 422, "currentPassword", wrongCurrentPassword)
	case errors.Is(err, errSamePassword):
		failField(w, 422, "newPassword", "Choose a password different from your current one.")
	case err != nil:
		internalError(w, err)
	default:
		s.startSession(w, r, user, 200, nil)
	}
}
