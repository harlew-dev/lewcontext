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
// (PasswordConfirmed).

const WrongCurrentPassword = "Current password is incorrect."

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

// Password is the signed-in account's password, /api/auth/password
// (router/routes.go).
type Password struct{ *Server }

func (h Password) Verify(w http.ResponseWriter, r *http.Request) {
	var in passwordCheck
	if !Decode(w, r, &in) {
		return
	}
	if h.PasswordConfirmed(w, r, in.Password, events.PasswordStepVerify) {
		w.WriteHeader(204)
	}
}

// PasswordConfirmed checks that password is the signed-in user's, for the
// request step, counting against the account's sign-in limit and recording
// auth.password_checked. When it isn't, it answers 422 with field
// "password"; it returns false whenever it has answered. Routes using it run
// behind LimitAuth.
func (s *Server) PasswordConfirmed(w http.ResponseWriter, r *http.Request, password, step string) bool {
	if !PasswordInBounds(password) {
		FailField(w, 422, "password", WrongCurrentPassword)
		return false
	}
	user := Session(r).User
	if !s.AccountLimit(w, r, user.Email) {
		return false
	}
	hash, err := s.Repo.FindPasswordHash(r.Context(), user.ID)
	if err != nil {
		InternalError(w, err)
		return false
	}
	ok := auth.CheckPassword(password, hash)
	if err := s.RecordPasswordCheck(r, user, ok, step); err != nil {
		InternalError(w, err)
		return false
	}
	if !ok {
		FailField(w, 422, "password", WrongCurrentPassword)
	}
	return ok
}

// RecordPasswordCheck records auth.password_checked.
func (s *Server) RecordPasswordCheck(r *http.Request, user store.User, ok bool, step string) error {
	e := events.PasswordChecked{User: user.PublicID, OK: ok, Step: step}
	return s.Repo.Record(r.Context(), e, store.EventScope{UserID: user.ID})
}

// Update saves the new password, signs out every session of the
// user's, and starts a new one for this browser.
func (h Password) Update(w http.ResponseWriter, r *http.Request) {
	var in passwordChange
	if !Decode(w, r, &in) {
		return
	}
	if !PasswordInBounds(in.CurrentPassword) {
		FailField(w, 422, "currentPassword", WrongCurrentPassword)
		return
	}
	if !PasswordInBounds(in.NewPassword) || !StrongPassword(in.NewPassword) {
		FailField(w, 422, "newPassword", "Use a new password of 12–128 characters.")
		return
	}
	user := Session(r).User
	if !h.AccountLimit(w, r, user.Email) {
		return
	}
	err := h.Repo.ChangePassword(r.Context(), user, h.SessionDigest(r), func(hash string) (string, error) {
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
		if recordErr := h.RecordPasswordCheck(r, user, !errors.Is(err, errWrongPassword), events.PasswordStepUpdate); recordErr != nil {
			InternalError(w, recordErr)
			return
		}
	}
	switch {
	case errors.Is(err, errWrongPassword):
		FailField(w, 422, "currentPassword", WrongCurrentPassword)
	case errors.Is(err, errSamePassword):
		FailField(w, 422, "newPassword", "Choose a password different from your current one.")
	case err != nil:
		InternalError(w, err)
	default:
		h.StartSession(w, r, user, 200, nil)
	}
}
