package account

import (
	"errors"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/store"
	"net/http"
	"strings"
)

// Changing the password takes two requests without MFA: verify checks the
// current password so the web app can move on to the new one, and change
// checks it again, because nothing remembers that verify passed. With MFA on,
// change takes a code instead of the current password, and the web app skips
// verify. Every check counts against the account's sign-in or code limit, so
// a stolen session can't guess either. Deleting a workspace or the account
// checks the password as verify does (PasswordConfirmed).

var (
	errWrongPassword = errors.New("current password is incorrect")
	errSamePassword  = errors.New("new password equals the current one")
)

const missingCode = "Enter the code from your authenticator app, or a recovery code."

type passwordCheck struct {
	Password string `json:"password"`
}
type passwordChange struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
	Code            string `json:"code"`
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

// Update saves the new password, once a code (MFA on) or the current
// password (MFA off) confirms it, signs out every session of the user's, and
// starts a new one for this browser.
func (h Password) Update(w http.ResponseWriter, r *http.Request) {
	var in passwordChange
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if !httpapi.PasswordInBounds(in.NewPassword) || !httpapi.StrongPassword(in.NewPassword) {
		httpapi.FailField(w, 422, "newPassword", "Use a new password of 12–128 characters.")
		return
	}
	user := httpapi.Session(r).User
	if !h.confirmationAllowed(w, r, user, in) {
		return
	}
	// The store decides which check runs, from the locked rows: the code
	// with MFA on (the current password is ignored), the password with it off.
	confirm := store.PasswordConfirmation{
		Factor: h.SecondFactor(r.Context(), user, in.Code),
		Password: func(hash string) error {
			if !httpapi.PasswordInBounds(in.CurrentPassword) || !auth.CheckPassword(in.CurrentPassword, hash) {
				return errWrongPassword
			}
			return nil
		},
	}
	confirmed, err := h.Repo.ChangePassword(r.Context(), user, h.SessionDigest(r), confirm, func(hash string) (string, error) {
		if auth.CheckPassword(in.NewPassword, hash) {
			return "", errSamePassword
		}
		return auth.HashPassword(in.NewPassword)
	})
	if recordErr := h.recordConfirmation(r, user, confirm.Factor, confirmed, err); recordErr != nil {
		httpapi.InternalError(w, r, recordErr)
		return
	}
	switch {
	case errors.Is(err, errWrongPassword):
		httpapi.FailField(w, 422, "currentPassword", httpapi.WrongCurrentPassword)
	case errors.Is(err, store.ErrWrongCode):
		httpapi.FailField(w, 422, "code", httpapi.WrongMFACode)
	case errors.Is(err, errSamePassword):
		httpapi.FailField(w, 422, "newPassword", "Choose a password different from your current one.")
	case err != nil:
		httpapi.InternalError(w, r, err)
	default:
		h.StartSession(w, r, user, 200, nil)
	}
}

// confirmationAllowed checks what the session's user needs before the
// transaction: with MFA on a code, counted against the account's code limit;
// with it off the current password, counted against its sign-in limit. It
// returns false once it has answered.
func (h Password) confirmationAllowed(w http.ResponseWriter, r *http.Request, user store.User, in passwordChange) bool {
	if user.MFAEnabledAt != nil {
		if strings.TrimSpace(in.Code) == "" {
			httpapi.FailField(w, 422, "code", missingCode)
			return false
		}
		return h.Allow(w, r, httpapi.MFAPerAccount, httpapi.MFAPerAccount.Key(user.PublicID))
	}
	if !httpapi.PasswordInBounds(in.CurrentPassword) {
		httpapi.FailField(w, 422, "currentPassword", httpapi.WrongCurrentPassword)
		return false
	}
	return h.AccountLimit(w, r, user.Email)
}

// recordConfirmation records the check ChangePassword ran, once the change
// has committed or rolled back: auth.mfa.checked for a code,
// auth.password_checked for the password. Only a wrong code or password
// counts as failed; refusing the new password comes after the check passed.
func (h Password) recordConfirmation(r *http.Request, user store.User, factor store.SecondFactor, confirmed store.PasswordConfirmed, err error) error {
	if err != nil && !errors.Is(err, errWrongPassword) && !errors.Is(err, store.ErrWrongCode) && !errors.Is(err, errSamePassword) {
		return nil
	}
	if confirmed.MFA {
		return h.RecordMFACheck(r, user, factor, events.MFAStepPasswordUpdate, !errors.Is(err, store.ErrWrongCode), confirmed.FactorUsed)
	}
	return h.RecordPasswordCheck(r, user, !errors.Is(err, errWrongPassword), events.PasswordStepUpdate)
}
