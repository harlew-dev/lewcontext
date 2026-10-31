package httpapi

import (
	"database/sql"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/store"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

type Credentials struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// ValidCredentials normalises the email and checks the bounds login and
// register share. The 12-character minimum is a sign-up rule (StrongPassword),
// so accounts created another way, such as the development seed, can sign in.
func ValidCredentials(in *Credentials) bool {
	in.Name = strings.TrimSpace(in.Name)
	return ValidEmail(&in.Email) && PasswordInBounds(in.Password)
}

// ValidEmail trims and lowercases an email, then checks it is a bare address
// of at most 254 bytes.
func ValidEmail(email *string) bool {
	*email = strings.ToLower(strings.TrimSpace(*email))
	address, err := mail.ParseAddress(*email)
	return err == nil && address.Address == *email && len(*email) <= 254
}

// PasswordInBounds is the shape any password a request sends must have: not
// empty and at most 128 bytes, which also caps the hashing work.
func PasswordInBounds(password string) bool {
	return password != "" && len(password) <= 128
}
func StrongPassword(password string) bool {
	return utf8.RuneCountInString(password) >= 12
}
func (s *Server) AccountLimit(w http.ResponseWriter, r *http.Request, email string) bool {
	return s.Allow(w, r, AuthPerAccount, AuthPerAccount.Key(email))
}

// registrationLimit counts a valid sign-up against the account creation
// limits: per IP first, so a client refused there doesn't spend the limit
// shared by everyone. With verification on each sign-up emails its address,
// so it counts per email too, for every address alike.
func (s *Server) registrationLimit(w http.ResponseWriter, r *http.Request, email string) bool {
	return s.Allow(w, r, RegisterPerIP, RegisterPerIP.IPKey(ClientIP(r))) &&
		(!s.Config.Verify || s.Allow(w, r, RegisterPerEmail, RegisterPerEmail.Key(email))) &&
		s.Allow(w, r, RegisterGlobal, RegisterGlobal.Everyone())
}
func (h Registrations) Register(w http.ResponseWriter, r *http.Request) {
	var in Credentials
	if !Decode(w, r, &in) {
		return
	}
	if !ValidCredentials(&in) || !StrongPassword(in.Password) || utf8.RuneCountInString(in.Name) < 1 || utf8.RuneCountInString(in.Name) > 80 {
		Fail(w, 422, "Use a name, a valid email, and a password of 12–128 characters.")
		return
	}
	if !h.AccountLimit(w, r, in.Email) || !h.registrationLimit(w, r, in.Email) {
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		InternalError(w, err)
		return
	}
	if h.Config.Verify {
		h.beginRegistration(w, r, in, hash)
		return
	}
	user, err := h.Repo.CreateUser(r.Context(), in.Name, in.Email, hash)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		Fail(w, 409, "Unable to create this account. Try signing in or use a different email.")
		return
	}
	if err != nil {
		InternalError(w, err)
		return
	}
	h.StartSession(w, r, user, 201, nil)
}

// Sessions are signing in and out: /api/auth/login, /login/mfa, /logout and
// /sessions (router/routes.go).
type Sessions struct{ *Server }

func (h Sessions) Login(w http.ResponseWriter, r *http.Request) {
	var in Credentials
	if !Decode(w, r, &in) {
		return
	}
	if !ValidCredentials(&in) {
		Fail(w, 401, "Email or password is incorrect.")
		return
	}
	if !h.AccountLimit(w, r, in.Email) {
		return
	}
	user, hash, err := h.Repo.FindUser(r.Context(), in.Email)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		InternalError(w, err)
		return
	}
	known := err == nil
	if !known {
		hash = h.dummyHash
	}
	valid := auth.CheckPassword(in.Password, hash)
	needsCode := valid && known && user.MFAEnabledAt != nil
	if err := h.recordLoginAttempt(r, in.Email, user, known, valid, needsCode); err != nil {
		InternalError(w, err)
		return
	}
	if !valid || !known {
		Fail(w, 401, "Email or password is incorrect.")
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
func (s *Server) recordLoginAttempt(r *http.Request, email string, user store.User, known, valid, needsCode bool) error {
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
	return s.Repo.Record(r.Context(), e, subject)
}

// StartSession signs u in on this browser, replacing the session its cookie
// carried, records e (if any) with the new session, and responds with the
// session in the shape of /api/auth/me.
func (s *Server) StartSession(w http.ResponseWriter, r *http.Request, u store.User, status int, e events.Event) {
	token, err := auth.Token()
	if err != nil {
		InternalError(w, err)
		return
	}
	csrf, err := auth.Token()
	if err != nil {
		InternalError(w, err)
		return
	}
	old := s.SessionDigest(r)
	signedIn := time.Now()
	expires := signedIn.Add(auth.SessionLifetime)
	if err = s.Repo.CreateSession(r.Context(), u.ID, auth.Digest(token), csrf, old, expires, e); err != nil {
		InternalError(w, err)
		return
	}
	http.SetCookie(w, auth.Cookie(token, s.Config.Production))
	JSON(w, status, map[string]any{"user": u, "csrfToken": csrf, "expiresAt": expires, "signedInAt": signedIn, "emailVerification": s.Config.VerificationMode()})
}

// Me is the signed-in account, /api/auth/me (router/routes.go): reading it, its
// profile and deleting it.
type Me struct{ *Server }

func (h Me) Show(w http.ResponseWriter, r *http.Request) {
	session := Session(r)
	JSON(w, 200, map[string]any{"user": session.User, "csrfToken": session.CSRF, "expiresAt": session.ExpiresAt, "signedInAt": session.CreatedAt, "emailVerification": h.Config.VerificationMode()})
}
func (h Sessions) Logout(w http.ResponseWriter, r *http.Request) {
	if err := h.Repo.DeleteSession(r.Context(), Session(r).User, h.SessionDigest(r)); err != nil {
		InternalError(w, err)
		return
	}
	h.ClearSessionCookie(w)
	w.WriteHeader(204)
}

// ClearSessionCookie tells the browser to drop its session cookie.
func (s *Server) ClearSessionCookie(w http.ResponseWriter) {
	expired := auth.Cookie("", s.Config.Production)
	expired.MaxAge = -1
	expired.Expires = time.Unix(0, 0)
	http.SetCookie(w, expired)
}
func (h Sessions) RevokeOthers(w http.ResponseWriter, r *http.Request) {
	if err := h.Repo.DeleteOtherSessions(r.Context(), Session(r).User, h.SessionDigest(r)); err != nil {
		InternalError(w, err)
		return
	}
	w.WriteHeader(204)
}
