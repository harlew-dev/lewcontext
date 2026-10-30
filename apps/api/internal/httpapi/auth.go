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

type credentials struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// validCredentials normalises the email and checks the bounds login and
// register share. The 12-character minimum is a sign-up rule (strongPassword),
// so accounts created another way, such as the development seed, can sign in.
func validCredentials(in *credentials) bool {
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Name = strings.TrimSpace(in.Name)
	address, err := mail.ParseAddress(in.Email)
	return err == nil && address.Address == in.Email && len(in.Email) <= 254 && passwordInBounds(in.Password)
}

// passwordInBounds is the shape any password a request sends must have: not
// empty and at most 128 bytes, which also caps the hashing work.
func passwordInBounds(password string) bool {
	return password != "" && len(password) <= 128
}
func strongPassword(password string) bool {
	return utf8.RuneCountInString(password) >= 12
}
func (s *Server) accountLimit(w http.ResponseWriter, r *http.Request, email string) bool {
	return s.allowAuth(w, r, "account", "account:"+auth.Digest(email), 10)
}

// allowAuth counts an attempt against a rate limit and answers 429 when it is
// used up. The first refusal in a window records auth.rate_limited; the ones
// after it don't, so a flood writes one event.
func (s *Server) allowAuth(w http.ResponseWriter, r *http.Request, scope, key string, limit int) bool {
	allowed, firstRefusal, err := s.Store.AllowAuth(r.Context(), key, limit)
	if err == nil && firstRefusal {
		e := events.RateLimited{Scope: scope}
		var subject store.EventScope
		if session, ok := optionalSession(r); ok {
			e.User, subject.UserID = session.User.PublicID, session.User.ID
		}
		err = s.Store.Record(r.Context(), e, subject)
	}
	if err != nil {
		internalError(w, err)
		return false
	}
	if !allowed {
		w.Header().Set("Retry-After", "900")
		fail(w, 429, "Too many attempts. Try again in 15 minutes.")
		return false
	}
	return true
}
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !decode(w, r, &in) {
		return
	}
	if !validCredentials(&in) || !strongPassword(in.Password) || utf8.RuneCountInString(in.Name) < 1 || utf8.RuneCountInString(in.Name) > 80 {
		fail(w, 422, "Use a name, a valid email, and a password of 12–128 characters.")
		return
	}
	if !s.accountLimit(w, r, in.Email) {
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		internalError(w, err)
		return
	}
	user, err := s.Store.CreateUser(r.Context(), in.Name, in.Email, hash)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		fail(w, 409, "Unable to create this account. Try signing in or use a different email.")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	s.startSession(w, r, user, 201, nil)
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !decode(w, r, &in) {
		return
	}
	if !validCredentials(&in) {
		fail(w, 401, "Email or password is incorrect.")
		return
	}
	if !s.accountLimit(w, r, in.Email) {
		return
	}
	user, hash, err := s.Store.FindUser(r.Context(), in.Email)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		internalError(w, err)
		return
	}
	known := err == nil
	if !known {
		hash = s.dummyHash
	}
	valid := auth.CheckPassword(in.Password, hash)
	if err := s.recordLoginAttempt(r, in.Email, user, known, valid); err != nil {
		internalError(w, err)
		return
	}
	if !valid || !known {
		fail(w, 401, "Email or password is incorrect.")
		return
	}
	s.startSession(w, r, user, 200, events.LoggedIn{User: user.PublicID})
}

// recordLoginAttempt records auth.login_attempted for every result, so the
// write takes the same time whether or not the account exists.
func (s *Server) recordLoginAttempt(r *http.Request, email string, user store.User, known, valid bool) error {
	e := events.LoginAttempted{Email: email, IP: clientIP(r), Result: events.LoginUnknownAccount}
	var subject store.EventScope
	if known {
		e.User, subject.UserID = user.PublicID, user.ID
		e.Result = events.LoginWrongPassword
		if valid {
			e.Result = events.LoginSucceeded
		}
	}
	return s.Store.Record(r.Context(), e, subject)
}

// startSession signs u in on this browser, replacing the session its cookie
// carried, records e (if any) with the new session, and responds with the
// session in the shape of /api/auth/me.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, u store.User, status int, e events.Event) {
	token, err := auth.Token()
	if err != nil {
		internalError(w, err)
		return
	}
	csrf, err := auth.Token()
	if err != nil {
		internalError(w, err)
		return
	}
	old := s.sessionDigest(r)
	signedIn := time.Now()
	expires := signedIn.Add(auth.SessionLifetime)
	if err = s.Store.CreateSession(r.Context(), u.ID, auth.Digest(token), csrf, old, expires, e); err != nil {
		internalError(w, err)
		return
	}
	http.SetCookie(w, auth.Cookie(token, s.Config.Production))
	jsonResponse(w, status, map[string]any{"user": u, "csrfToken": csrf, "expiresAt": expires, "signedInAt": signedIn})
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	session := current(r)
	jsonResponse(w, 200, map[string]any{"user": session.User, "csrfToken": session.CSRF, "expiresAt": session.ExpiresAt, "signedInAt": session.CreatedAt})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteSession(r.Context(), current(r).User, s.sessionDigest(r)); err != nil {
		internalError(w, err)
		return
	}
	expired := auth.Cookie("", s.Config.Production)
	expired.MaxAge = -1
	expired.Expires = time.Unix(0, 0)
	http.SetCookie(w, expired)
	w.WriteHeader(204)
}
func (s *Server) revokeOthers(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DeleteOtherSessions(r.Context(), current(r).User, s.sessionDigest(r)); err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(204)
}
