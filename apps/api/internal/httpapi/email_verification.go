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

type EmailVerification struct{ *Server }

// Store asks for a code. POST /api/auth/email-verification.
func (h EmailVerification) Store(w http.ResponseWriter, r *http.Request) {
	user := Session(r).User
	if user.EmailVerifiedAt != nil {
		Fail(w, 409, alreadyVerified)
		return
	}
	if !h.Allow(w, r, EmailVerificationPerEmail, EmailVerificationPerEmail.Key(user.Email)) ||
		!h.Allow(w, r, EmailVerificationGlobal, EmailVerificationGlobal.Everyone()) {
		return
	}
	if err := h.Repo.Record(r.Context(), events.EmailVerificationRequested{User: user.PublicID}, store.EventScope{UserID: user.ID}); err != nil {
		InternalError(w, err)
		return
	}
	w.WriteHeader(202)
}

// Update verifies the email with the code. PATCH
// /api/auth/email-verification.
func (h EmailVerification) Update(w http.ResponseWriter, r *http.Request) {
	var in MFACode
	if !Decode(w, r, &in) {
		return
	}
	user := Session(r).User
	if !h.Allow(w, r, EmailCodePerEmail, EmailCodePerEmail.Key(user.Email)) {
		return
	}
	result, err := h.Repo.VerifyEmail(r.Context(), user.ID, h.EmailCodeDigest(in.Code))
	switch {
	case errors.Is(err, store.ErrAlreadyVerified):
		Fail(w, 409, alreadyVerified)
	case errors.Is(err, store.ErrNoEmailVerification):
		Fail(w, 410, emailCodeEnded)
	case err != nil:
		InternalError(w, err)
	case result.Ended:
		Fail(w, 410, emailCodeTooMany)
	case !result.OK:
		FailField(w, 422, "code", wrongEmailCode)
	default:
		JSON(w, 200, map[string]any{"user": result.User})
	}
}
