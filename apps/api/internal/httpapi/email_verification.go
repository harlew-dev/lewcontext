package httpapi

import (
	"errors"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/store"
	"net/http"
)

// A signed-in account verifying the email it already has: from Account
// settings, or the page EMAIL_VERIFICATION_EXISTING=required sends it to.
// Asking records auth.email_verification.requested, whose listener sends the
// code; the account knows its own email, so nothing here needs hiding.

const (
	alreadyVerified  = "Your email is already verified."
	emailCodeEnded   = "This code has expired. Send a new one."
	emailCodeTooMany = "Too many wrong codes. Send a new one."
)

type emailVerificationHandlers struct{ *Server }

// store asks for a code. POST /api/auth/email-verification.
func (h emailVerificationHandlers) store(w http.ResponseWriter, r *http.Request) {
	user := current(r).User
	if user.EmailVerifiedAt != nil {
		fail(w, 409, alreadyVerified)
		return
	}
	if !h.allow(w, r, emailVerificationPerEmail, emailVerificationPerEmail.Key(user.Email)) ||
		!h.allow(w, r, emailVerificationGlobal, emailVerificationGlobal.Everyone()) {
		return
	}
	if err := h.Store.Record(r.Context(), events.EmailVerificationRequested{User: user.PublicID}, store.EventScope{UserID: user.ID}); err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(202)
}

// update verifies the email with the code. PATCH
// /api/auth/email-verification.
func (h emailVerificationHandlers) update(w http.ResponseWriter, r *http.Request) {
	var in mfaCode
	if !decode(w, r, &in) {
		return
	}
	user := current(r).User
	if !h.allow(w, r, emailCodePerEmail, emailCodePerEmail.Key(user.Email)) {
		return
	}
	result, err := h.Store.VerifyEmail(r.Context(), user.ID, h.emailCodeDigest(in.Code))
	switch {
	case errors.Is(err, store.ErrAlreadyVerified):
		fail(w, 409, alreadyVerified)
	case errors.Is(err, store.ErrNoEmailVerification):
		fail(w, 410, emailCodeEnded)
	case err != nil:
		internalError(w, err)
	case result.Ended:
		fail(w, 410, emailCodeTooMany)
	case !result.OK:
		failField(w, 422, "code", wrongEmailCode)
	default:
		jsonResponse(w, 200, map[string]any{"user": result.User})
	}
}
