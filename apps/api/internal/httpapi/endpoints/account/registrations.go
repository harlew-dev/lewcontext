package account

import (
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/store"
	"net/http"
	"time"
	"unicode/utf8"
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
	// registrationBlocked reaches only someone with a code from the inbox:
	// the address was blocked after its code was sent.
	registrationBlocked = "This address has been blocked. To use it, unblock it from the link in an email we sent, then sign up again."
	registrationRefused = "Unable to create this account. Try signing in or use a different email."
)

type Registrations struct{ *httpapi.Server }

// beginRegistration saves a valid sign-up until its code is entered, ties it
// to this browser with the registration cookie, and answers 202 the same for
// every email.
func (h Registrations) beginRegistration(w http.ResponseWriter, r *http.Request, in httpapi.Credentials, hash string) {
	token, err := auth.Token()
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	pending := store.NewRegistration{Email: in.Email, Name: in.Name, PasswordHash: hash, TokenHash: auth.Digest(token)}
	if err := h.Repo.CreatePendingRegistration(r.Context(), pending); err != nil {
		httpapi.InternalError(w, err)
		return
	}
	http.SetCookie(w, auth.RegistrationCookie(token, h.Config.Production))
	waitingForCode(w, in.Email)
}

// waitingForCode is the answer to a sign-up, or a new code, for email.
func waitingForCode(w http.ResponseWriter, email string) {
	httpapi.JSON(w, 202, map[string]any{"verificationRequired": true, "email": email})
}

// Check tells the code page which email its sign-up is waiting on, so a
// reload (a phone switching to the mail app) resumes it. POST
// /api/auth/register/check.
func (h Registrations) Check(w http.ResponseWriter, r *http.Request) {
	if !h.Allow(w, r, httpapi.AuthPerIP, httpapi.AuthPerIP.IPKey(httpapi.ClientIP(r))) {
		return
	}
	pending, _, ok := h.pendingRegistration(w, r)
	if ok {
		httpapi.JSON(w, 200, pending)
	}
}

// Resend gives the browser's sign-up a new code, counted like a sign-up.
// POST /api/auth/register/resend.
func (h Registrations) Resend(w http.ResponseWriter, r *http.Request) {
	if !h.Allow(w, r, httpapi.AuthPerIP, httpapi.AuthPerIP.IPKey(httpapi.ClientIP(r))) {
		return
	}
	pending, digest, ok := h.pendingRegistration(w, r)
	if !ok {
		return
	}
	if !h.Allow(w, r, httpapi.RegisterPerEmail, httpapi.RegisterPerEmail.Key(pending.Email)) || !h.Allow(w, r, httpapi.RegisterGlobal, httpapi.RegisterGlobal.Everyone()) {
		return
	}
	pending, err := h.Repo.ResendPendingRegistration(r.Context(), digest)
	if errors.Is(err, store.ErrNoRegistration) {
		h.endRegistration(w, registrationEnded)
		return
	}
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	token, _ := r.Cookie(auth.RegistrationCookieName(h.Config.Production))
	http.SetCookie(w, auth.RegistrationCookie(token.Value, h.Config.Production))
	waitingForCode(w, pending.Email)
}

// Verify finishes the browser's sign-up with its code: the right one
// creates the account and signs it in. POST /api/auth/register/verify.
func (h Registrations) Verify(w http.ResponseWriter, r *http.Request) {
	var in httpapi.MFACode
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if !h.Allow(w, r, httpapi.AuthPerIP, httpapi.AuthPerIP.IPKey(httpapi.ClientIP(r))) {
		return
	}
	pending, digest, ok := h.pendingRegistration(w, r)
	if !ok || !h.Allow(w, r, httpapi.EmailCodePerEmail, httpapi.EmailCodePerEmail.Key(pending.Email)) {
		return
	}
	result, err := h.Repo.CompleteRegistration(r.Context(), digest, h.EmailCodeDigest(in.Code))
	switch {
	case errors.Is(err, store.ErrNoRegistration):
		h.endRegistration(w, registrationEnded)
	case err != nil:
		httpapi.InternalError(w, err)
	case result.Ended:
		h.endRegistration(w, registrationCodes)
	case !result.OK:
		httpapi.FailField(w, 422, "code", wrongEmailCode)
	case result.Exists:
		h.clearRegistrationCookie(w)
		httpapi.Fail(w, 409, registrationExists)
	case result.Blocked:
		h.endRegistration(w, registrationBlocked)
	default:
		h.clearRegistrationCookie(w)
		h.StartSession(w, r, result.User, 201, nil)
	}
}

// pendingRegistration is the browser's live sign-up and its cookie's digest.
// Without one it answers 401 and clears the cookie.
func (h Registrations) pendingRegistration(w http.ResponseWriter, r *http.Request) (store.PendingRegistration, string, bool) {
	cookie, err := r.Cookie(auth.RegistrationCookieName(h.Config.Production))
	if err != nil || !input.Token(cookie.Value) {
		h.endRegistration(w, registrationEnded)
		return store.PendingRegistration{}, "", false
	}
	digest := auth.Digest(cookie.Value)
	pending, err := h.Repo.FindPendingRegistration(r.Context(), digest)
	if errors.Is(err, store.ErrNoRegistration) {
		h.endRegistration(w, registrationEnded)
		return pending, "", false
	}
	if err != nil {
		httpapi.InternalError(w, err)
		return pending, "", false
	}
	return pending, digest, true
}

// endRegistration sends the browser back to the sign-up form: 401, without
// the registration cookie.
func (h Registrations) endRegistration(w http.ResponseWriter, message string) {
	h.clearRegistrationCookie(w)
	httpapi.Fail(w, 401, message)
}

func (h Registrations) clearRegistrationCookie(w http.ResponseWriter) {
	expired := auth.RegistrationCookie("", h.Config.Production)
	expired.MaxAge = -1
	expired.Expires = time.Unix(0, 0)
	http.SetCookie(w, expired)
}

// registrationLimit counts a valid sign-up against the account creation
// limits: per IP first, so a client refused there doesn't spend the limit
// shared by everyone. With verification on each sign-up emails its address,
// so it counts per email too, for every address alike.
func (h Registrations) registrationLimit(w http.ResponseWriter, r *http.Request, email string) bool {
	return h.Allow(w, r, httpapi.RegisterPerIP, httpapi.RegisterPerIP.IPKey(httpapi.ClientIP(r))) &&
		(!h.Config.Verify || h.Allow(w, r, httpapi.RegisterPerEmail, httpapi.RegisterPerEmail.Key(email))) &&
		h.Allow(w, r, httpapi.RegisterGlobal, httpapi.RegisterGlobal.Everyone())
}

func (h Registrations) Register(w http.ResponseWriter, r *http.Request) {
	var in httpapi.Credentials
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if !httpapi.ValidCredentials(&in) || !httpapi.StrongPassword(in.Password) || utf8.RuneCountInString(in.Name) < 1 || utf8.RuneCountInString(in.Name) > 80 {
		httpapi.Fail(w, 422, "Use a name, a valid email, and a password of 12–128 characters.")
		return
	}
	if !h.AccountLimit(w, r, in.Email) || !h.registrationLimit(w, r, in.Email) {
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	if h.Config.Verify {
		h.beginRegistration(w, r, in, hash)
		return
	}
	// With verification off a taken email is refused, and a blocked one
	// looks taken: that says no more than this mode already does.
	user, err := h.Repo.CreateUser(r.Context(), in.Name, in.Email, hash)
	var pgErr *pgconn.PgError
	if errors.Is(err, store.ErrEmailBlocked) || errors.As(err, &pgErr) && pgErr.Code == "23505" {
		httpapi.Fail(w, 409, registrationRefused)
		return
	}
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	h.StartSession(w, r, user, 201, nil)
}
