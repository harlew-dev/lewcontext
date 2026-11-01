package admin

import (
	"database/sql"
	"errors"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"net/http"
)

// StepUp is the admin session's step-up window
// (plans/features/implemented/admin/email-blocks.md): a fresh code opens it
// for auth.AdminStepUpWindow, and the dashboard's account-and-access
// actions (httpapi.RequireAdminStepUp) need it open. A singleton of the
// session, as EmailVerification is the account's.
type StepUp struct{ *httpapi.Server }

// stepUpEnded is the answer to the wrong code that ends the session.
const stepUpEnded = "Too many wrong codes. Sign in again."

// Store checks an authenticator or recovery code exactly as the door's
// second step does (the same factor, used up the same way, so a code works
// once), counting against mfa:per-account and recording auth.mfa.checked
// (step admin_step_up), right or wrong. The right one opens this session's
// window: 204. A wrong one is 422 on "code"; the session's fifth ends it
// (store.MaxStepUpFailures): 401, the cookie cleared, so a stolen admin
// cookie gets one burst of guesses. POST /api/admin/step-up.
func (h StepUp) Store(w http.ResponseWriter, r *http.Request) {
	var in httpapi.MFACode
	if !httpapi.Decode(w, r, &in) {
		return
	}
	admin := httpapi.Admin(r).User
	if !h.Allow(w, r, httpapi.MFAPerAccount, httpapi.MFAPerAccount.Key(admin.PublicID)) {
		return
	}
	factor := h.SecondFactor(admin, in.Code)
	result, err := h.Repo.StepUpAdminSession(r.Context(), h.AdminSessionDigest(r), admin, factor)
	if errors.Is(err, sql.ErrNoRows) {
		// Signed out (or ended) between the session check and here.
		httpapi.Fail(w, 401, "Your admin session has ended. Please sign in again.")
		return
	}
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	if err := h.RecordMFACheck(r, admin, factor, events.MFAStepAdminStepUp, result.OK, result.FactorUsed); err != nil {
		httpapi.InternalError(w, err)
		return
	}
	switch {
	case result.Ended:
		clearCookie(w, auth.AdminCookie("", h.Config.Production))
		httpapi.Fail(w, 401, stepUpEnded)
	case !result.OK:
		httpapi.FailField(w, 422, "code", httpapi.WrongMFACode)
	default:
		w.WriteHeader(204)
	}
}
