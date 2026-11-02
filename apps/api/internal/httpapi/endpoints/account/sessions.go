// Package account answers /api/auth/*: signing up, in and out, the account
// and its password, email verification and two-factor authentication. Its
// actions are named (Login, Register) as docs/naming.md allows for the
// account and its session; its singletons use the CRUD actions.
package account

import (
	"database/sql"
	"errors"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/store"
	"net/http"
)

// Sessions are signing in and out: /api/auth/login, /login/mfa, /logout and
// /sessions (router/routes.go).
type Sessions struct{ *httpapi.Server }

func (h Sessions) Login(w http.ResponseWriter, r *http.Request) {
	var in httpapi.Credentials
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if !httpapi.ValidCredentials(&in) {
		httpapi.Fail(w, 401, "Email or password is incorrect.")
		return
	}
	if !h.AccountLimit(w, r, in.Email) {
		return
	}
	user, hash, err := h.Repo.FindUser(r.Context(), in.Email)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		httpapi.InternalError(w, r, err)
		return
	}
	known := err == nil
	if !known {
		hash = h.DummyHash()
	}
	valid := auth.CheckPassword(in.Password, hash)
	needsCode := valid && known && user.MFAEnabledAt != nil
	// A suspended account hears why only once its password is right, and
	// with MFA on only after its code too (StartSession refuses it then):
	// anyone else gets the usual 401, so a suspension can't be probed.
	if valid && known && user.Suspended && !needsCode {
		h.Suspended(w, r, user)
		return
	}
	if err := h.recordLoginAttempt(r, in.Email, user, known, valid, needsCode); err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	if !valid || !known {
		httpapi.Fail(w, 401, "Email or password is incorrect.")
		return
	}
	if needsCode {
		h.StartMFAChallenge(w, r, user, store.ChallengeLogin)
		return
	}
	h.StartSession(w, r, user, 200, events.LoggedIn{User: user.PublicID})
}

// recordLoginAttempt records auth.login_attempted for every result, so the
// write takes the same time whether or not the account exists. A right
// password that still needs a second factor is mfa_required.
func (h Sessions) recordLoginAttempt(r *http.Request, email string, user store.User, known, valid, needsCode bool) error {
	e := events.LoginAttempted{Email: email, Result: events.LoginUnknownAccount}
	var subject store.EventScope
	if known {
		e.User, subject.UserID = user.PublicID, user.ID
		e.Result = events.LoginWrongPassword
		if needsCode {
			e.Result = events.LoginMFARequired
		} else if valid {
			e.Result = events.LoginSucceeded
		}
	}
	return h.Repo.Record(r.Context(), e, subject)
}

// LoginMFA finishes a sign-in whose password was right with its second
// factor: the challenge in the MFA cookie (login) and a code. POST
// /api/auth/login/mfa.
func (h Sessions) LoginMFA(w http.ResponseWriter, r *http.Request) {
	var in httpapi.MFACode
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if !h.Allow(w, r, httpapi.AuthPerIP, httpapi.AuthPerIP.IPKey(httpapi.ClientIP(r))) {
		return
	}
	hash, ok := h.MFAChallengeDigest(r)
	if !ok {
		h.EndMFAChallenge(w, httpapi.MFASignInAgain)
		return
	}
	user, err := h.Repo.FindMFAChallenge(r.Context(), hash, store.ChallengeLogin)
	if errors.Is(err, store.ErrNoChallenge) {
		h.EndMFAChallenge(w, httpapi.MFASignInAgain)
		return
	}
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	if !h.Allow(w, r, httpapi.MFAPerAccount, httpapi.MFAPerAccount.Key(user.PublicID)) {
		return
	}
	factor := h.SecondFactor(r.Context(), user, in.Code)
	result, err := h.Repo.AnswerMFAChallenge(r.Context(), hash, store.ChallengeLogin, user.ID, factor)
	if errors.Is(err, store.ErrNoChallenge) {
		h.EndMFAChallenge(w, httpapi.MFASignInAgain)
		return
	}
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	if err := h.RecordMFACheck(r, user, factor, events.MFAStepLogin, result.OK, result.FactorUsed); err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	switch {
	case result.Ended:
		h.EndMFAChallenge(w, httpapi.MFATooManyCodes)
	case !result.OK:
		httpapi.FailField(w, 422, "code", httpapi.WrongMFACode)
	default:
		h.ClearMFACookie(w)
		h.StartSession(w, r, result.User, 200, events.LoggedIn{User: result.User.PublicID})
	}
}

func (h Sessions) Logout(w http.ResponseWriter, r *http.Request) {
	if err := h.Repo.DeleteSession(r.Context(), httpapi.Session(r).User, h.SessionDigest(r)); err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	h.ClearSessionCookie(w)
	w.WriteHeader(204)
}

func (h Sessions) RevokeOthers(w http.ResponseWriter, r *http.Request) {
	if err := h.Repo.DeleteOtherSessions(r.Context(), httpapi.Session(r).User, h.SessionDigest(r)); err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	w.WriteHeader(204)
}
