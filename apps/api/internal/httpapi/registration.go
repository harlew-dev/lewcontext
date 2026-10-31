package httpapi

import (
	"errors"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/store"
	"net/http"
	"time"
)

// Sign-up with EMAIL_VERIFICATION on (plans/features/email-verification.md):
// register saves the sign-up and answers 202 with a cookie; the
// mail.registration listener sends a code, or "you already have an account"
// to an email that has one; the code page checks, resends and verifies with
// the cookie. Nothing here reads users before a right code, so every request
// does the same work for a taken email as for a new one, and answers the
// same: only the inbox can tell them apart.

const (
	registrationEnded  = "This sign-up has expired. Start again."
	registrationCodes  = "Too many wrong codes. Start again."
	wrongEmailCode     = "That code isn’t right. Check the email we sent and try again."
	registrationExists = "You already have an account. Sign in instead."
)

type Registrations struct{ *Server }

// beginRegistration saves a valid sign-up until its code is entered, ties it
// to this browser with the registration cookie, and answers 202 the same for
// every email.
func (s *Server) beginRegistration(w http.ResponseWriter, r *http.Request, in Credentials, hash string) {
	token, err := auth.Token()
	if err != nil {
		InternalError(w, err)
		return
	}
	pending := store.NewRegistration{Email: in.Email, Name: in.Name, PasswordHash: hash, TokenHash: auth.Digest(token)}
	if err := s.Repo.CreatePendingRegistration(r.Context(), pending); err != nil {
		InternalError(w, err)
		return
	}
	http.SetCookie(w, auth.RegistrationCookie(token, s.Config.Production))
	waitingForCode(w, in.Email)
}

// waitingForCode is the answer to a sign-up, or a new code, for email.
func waitingForCode(w http.ResponseWriter, email string) {
	JSON(w, 202, map[string]any{"verificationRequired": true, "email": email})
}

// Check tells the code page which email its sign-up is waiting on, so a
// reload (a phone switching to the mail app) resumes it. POST
// /api/auth/register/check.
func (h Registrations) Check(w http.ResponseWriter, r *http.Request) {
	if !h.Allow(w, r, AuthPerIP, AuthPerIP.IPKey(ClientIP(r))) {
		return
	}
	pending, _, ok := h.pendingRegistration(w, r)
	if ok {
		JSON(w, 200, pending)
	}
}

// Resend gives the browser's sign-up a new code, counted like a sign-up.
// POST /api/auth/register/resend.
func (h Registrations) Resend(w http.ResponseWriter, r *http.Request) {
	if !h.Allow(w, r, AuthPerIP, AuthPerIP.IPKey(ClientIP(r))) {
		return
	}
	pending, digest, ok := h.pendingRegistration(w, r)
	if !ok {
		return
	}
	if !h.Allow(w, r, RegisterPerEmail, RegisterPerEmail.Key(pending.Email)) || !h.Allow(w, r, RegisterGlobal, RegisterGlobal.Everyone()) {
		return
	}
	pending, err := h.Repo.ResendPendingRegistration(r.Context(), digest)
	if errors.Is(err, store.ErrNoRegistration) {
		h.endRegistration(w, registrationEnded)
		return
	}
	if err != nil {
		InternalError(w, err)
		return
	}
	token, _ := r.Cookie(auth.RegistrationCookieName(h.Config.Production))
	http.SetCookie(w, auth.RegistrationCookie(token.Value, h.Config.Production))
	waitingForCode(w, pending.Email)
}

// Verify finishes the browser's sign-up with its code: the right one
// creates the account and signs it in. POST /api/auth/register/verify.
func (h Registrations) Verify(w http.ResponseWriter, r *http.Request) {
	var in MFACode
	if !Decode(w, r, &in) {
		return
	}
	if !h.Allow(w, r, AuthPerIP, AuthPerIP.IPKey(ClientIP(r))) {
		return
	}
	pending, digest, ok := h.pendingRegistration(w, r)
	if !ok || !h.Allow(w, r, EmailCodePerEmail, EmailCodePerEmail.Key(pending.Email)) {
		return
	}
	result, err := h.Repo.CompleteRegistration(r.Context(), digest, h.EmailCodeDigest(in.Code))
	switch {
	case errors.Is(err, store.ErrNoRegistration):
		h.endRegistration(w, registrationEnded)
	case err != nil:
		InternalError(w, err)
	case result.Ended:
		h.endRegistration(w, registrationCodes)
	case !result.OK:
		FailField(w, 422, "code", wrongEmailCode)
	case result.Exists:
		h.clearRegistrationCookie(w)
		Fail(w, 409, registrationExists)
	default:
		h.clearRegistrationCookie(w)
		h.StartSession(w, r, result.User, 201, nil)
	}
}

// pendingRegistration is the browser's live sign-up and its cookie's digest.
// Without one it answers 401 and clears the cookie.
func (s *Server) pendingRegistration(w http.ResponseWriter, r *http.Request) (store.PendingRegistration, string, bool) {
	cookie, err := r.Cookie(auth.RegistrationCookieName(s.Config.Production))
	if err != nil || !input.Token(cookie.Value) {
		s.endRegistration(w, registrationEnded)
		return store.PendingRegistration{}, "", false
	}
	digest := auth.Digest(cookie.Value)
	pending, err := s.Repo.FindPendingRegistration(r.Context(), digest)
	if errors.Is(err, store.ErrNoRegistration) {
		s.endRegistration(w, registrationEnded)
		return pending, "", false
	}
	if err != nil {
		InternalError(w, err)
		return pending, "", false
	}
	return pending, digest, true
}

// EmailCodeDigest is the HMAC of a code as typed. Anything that can't be a
// code still gets a digest, of a value no code has, so a malformed code is
// compared like a wrong one.
func (s *Server) EmailCodeDigest(typed string) string {
	code, ok := auth.NormalizeEmailCode(typed)
	if !ok {
		code = "not a code"
	}
	return s.keys.EmailCodeDigest(code)
}

// endRegistration sends the browser back to the sign-up form: 401, without
// the registration cookie.
func (s *Server) endRegistration(w http.ResponseWriter, message string) {
	s.clearRegistrationCookie(w)
	Fail(w, 401, message)
}

func (s *Server) clearRegistrationCookie(w http.ResponseWriter) {
	expired := auth.RegistrationCookie("", s.Config.Production)
	expired.MaxAge = -1
	expired.Expires = time.Unix(0, 0)
	http.SetCookie(w, expired)
}
