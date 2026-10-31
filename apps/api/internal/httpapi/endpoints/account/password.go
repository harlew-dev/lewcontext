package account

import (
	"errors"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"net/http"
)

// Changing the password takes two requests: verify checks the current
// password so the web app can move on to the new one, and change checks it
// again, because nothing remembers that verify passed. Both count against the
// account's sign-in rate limit, so a stolen session can't guess the password.
// Deleting a workspace or the account checks it the same way
// (PasswordConfirmed).

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
type Password struct{ *httpapi.Server }

func (h Password) Verify(w http.ResponseWriter, r *http.Request) {
	var in passwordCheck
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if h.PasswordConfirmed(w, r, in.Password, events.PasswordStepVerify) {
		w.WriteHeader(204)
	}
}

// Update saves the new password, signs out every session of the
// user's, and starts a new one for this browser.
func (h Password) Update(w http.ResponseWriter, r *http.Request) {
	var in passwordChange
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if !httpapi.PasswordInBounds(in.CurrentPassword) {
		httpapi.FailField(w, 422, "currentPassword", httpapi.WrongCurrentPassword)
		return
	}
	if !httpapi.PasswordInBounds(in.NewPassword) || !httpapi.StrongPassword(in.NewPassword) {
		httpapi.FailField(w, 422, "newPassword", "Use a new password of 12–128 characters.")
		return
	}
	user := httpapi.Session(r).User
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
			httpapi.InternalError(w, recordErr)
			return
		}
	}
	switch {
	case errors.Is(err, errWrongPassword):
		httpapi.FailField(w, 422, "currentPassword", httpapi.WrongCurrentPassword)
	case errors.Is(err, errSamePassword):
		httpapi.FailField(w, 422, "newPassword", "Choose a password different from your current one.")
	case err != nil:
		httpapi.InternalError(w, err)
	default:
		h.StartSession(w, r, user, 200, nil)
	}
}
