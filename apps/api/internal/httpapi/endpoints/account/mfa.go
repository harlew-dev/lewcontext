package account

import (
	"errors"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/store"
	"net/http"
)

// Two-factor authentication (plans/features/mfa.md): an authenticator app's
// codes (TOTP) plus single-use recovery codes. Checking codes and setting MFA
// up are the core's (httpapi/second_factor.go), shared with the admin door.

type MFA struct{ *httpapi.Server }

type mfaConfirmation struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

// Setup starts setting up MFA once the password is confirmed: a new secret,
// returned once for the QR code. POST /api/auth/mfa/setup.
func (h MFA) Setup(w http.ResponseWriter, r *http.Request) {
	var in passwordCheck
	if !httpapi.Decode(w, r, &in) {
		return
	}
	user := httpapi.Session(r).User
	if user.MFAEnabledAt != nil {
		httpapi.Fail(w, 409, "Two-factor authentication is already on.")
		return
	}
	if h.PasswordConfirmed(w, r, in.Password, events.PasswordStepMFASetup) {
		h.BeginMFASetup(w, r, user)
	}
}

// Store turns MFA on with the first code from the authenticator, and returns
// the recovery codes, once. POST /api/auth/mfa.
func (h MFA) Store(w http.ResponseWriter, r *http.Request) {
	var in httpapi.MFACode
	if !httpapi.Decode(w, r, &in) {
		return
	}
	user := httpapi.Session(r).User
	if !h.Allow(w, r, httpapi.MFAPerAccount, httpapi.MFAPerAccount.Key(user.PublicID)) {
		return
	}
	if codes, ok := h.ConfirmMFASetup(w, r, user, h.SessionDigest(r), in.Code); ok {
		httpapi.JSON(w, 201, map[string]any{"recoveryCodes": codes})
	}
}

// Destroy turns MFA off, after the password and a second factor.
// DELETE /api/auth/mfa.
func (h MFA) Destroy(w http.ResponseWriter, r *http.Request) {
	var in mfaConfirmation
	if !httpapi.Decode(w, r, &in) {
		return
	}
	user, ok := mfaConfirmed(h.Server, w, r, in, events.PasswordStepMFADisable)
	if !ok {
		return
	}
	factor := h.SecondFactor(r.Context(), user, in.Code)
	used, err := h.Repo.DisableMFA(r.Context(), user, factor)
	if answerFactor(h.Server, w, r, user, factor, events.MFAStepDisable, used, err) {
		w.WriteHeader(204)
	}
}

// mfaConfirmed checks what turning MFA off and renewing recovery codes need
// before the code: MFA on, the password, and the account's code limit. It
// returns false once it has answered.
func mfaConfirmed(s *httpapi.Server, w http.ResponseWriter, r *http.Request, in mfaConfirmation, step string) (store.User, bool) {
	user := httpapi.Session(r).User
	if user.MFAEnabledAt == nil {
		httpapi.Fail(w, 409, "Two-factor authentication is off.")
		return user, false
	}
	if !s.PasswordConfirmed(w, r, in.Password, step) {
		return user, false
	}
	return user, s.Allow(w, r, httpapi.MFAPerAccount, httpapi.MFAPerAccount.Key(user.PublicID))
}

// answerFactor records the second-factor check a store call made and answers
// its refusal: 409 when MFA turned off meanwhile or the user is an admin
// (who can't turn it off), 422 with field "code" for a wrong code. It returns true when the factor was right.
func answerFactor(s *httpapi.Server, w http.ResponseWriter, r *http.Request, user store.User, f store.SecondFactor, step string, used store.FactorUsed, err error) bool {
	if errors.Is(err, store.ErrMFANotEnabled) {
		httpapi.Fail(w, 409, "Two-factor authentication is off.")
		return false
	}
	if errors.Is(err, store.ErrAdminNeedsMFA) {
		httpapi.Fail(w, 409, "Admins must keep two-factor authentication on.")
		return false
	}
	if err != nil && !errors.Is(err, store.ErrWrongCode) {
		httpapi.InternalError(w, r, err)
		return false
	}
	if err := s.RecordMFACheck(r, user, f, step, err == nil, used); err != nil {
		httpapi.InternalError(w, r, err)
		return false
	}
	if err != nil {
		httpapi.FailField(w, 422, "code", httpapi.WrongMFACode)
		return false
	}
	return true
}
