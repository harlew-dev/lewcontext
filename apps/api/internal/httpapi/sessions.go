package httpapi

// The app's sessions: the cookie, looking one up, starting and ending one.

import (
	"errors"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/store"
	"net/http"
	"time"
)

// errNoSession means the request has no well-formed session cookie.
var errNoSession = errors.New("no session cookie")

// session looks up the request's session, refreshing its idle timer. It
// returns errNoSession without a cookie, and sql.ErrNoRows when the session is
// unknown, expired or idle.
func (s *Server) session(r *http.Request) (store.Session, error) {
	token, ok := s.sessionToken(r)
	if !ok {
		return store.Session{}, errNoSession
	}
	return s.Repo.FindSession(r.Context(), auth.Digest(token))
}

// sessionToken is the request's session cookie, if it has a well-formed one
// (43 base64url characters).
func (s *Server) sessionToken(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(auth.CookieName(s.Config.Production))
	if err != nil || len(cookie.Value) != 43 {
		return "", false
	}
	return cookie.Value, true
}

// SessionDigest is what the sessions table stores for the request's session
// cookie, or "" without one.
func (s *Server) SessionDigest(r *http.Request) string {
	token, ok := s.sessionToken(r)
	if !ok {
		return ""
	}
	return auth.Digest(token)
}

// StartSession signs u in on this browser, replacing the session its cookie
// carried, records e (if any) with the new session, and responds with the
// session in the shape of /api/auth/me. A suspended account gets no session
// (store.ErrSuspended, checked under its lock): Suspended answers it, so a
// sign-in that reached here only after its password and code learns why.
func (s *Server) StartSession(w http.ResponseWriter, r *http.Request, u store.User, status int, e events.Event) {
	token, err := auth.Token()
	if err != nil {
		InternalError(w, r, err)
		return
	}
	csrf, err := auth.Token()
	if err != nil {
		InternalError(w, r, err)
		return
	}
	old := s.SessionDigest(r)
	signedIn := time.Now()
	expires := signedIn.Add(auth.SessionLifetime)
	err = s.Repo.CreateSession(r.Context(), u.ID, auth.Digest(token), csrf, old, expires, e)
	if errors.Is(err, store.ErrSuspended) {
		s.Suspended(w, r, u)
		return
	}
	if err != nil {
		InternalError(w, r, err)
		return
	}
	http.SetCookie(w, auth.Cookie(token, s.Config.Production))
	JSON(w, status, map[string]any{"user": u, "csrfToken": csrf, "expiresAt": expires, "signedInAt": signedIn, "emailVerification": s.Config.VerificationMode()})
}

// ClearSessionCookie tells the browser to drop its session cookie.
func (s *Server) ClearSessionCookie(w http.ResponseWriter) {
	expired := auth.Cookie("", s.Config.Production)
	expired.MaxAge = -1
	expired.Expires = time.Unix(0, 0)
	http.SetCookie(w, expired)
}

// AccountSuspended is what a suspended account hears at sign-in, once its
// password (and code) were right: someone who doesn't know the password
// only ever gets the usual 401, so a suspension can't be probed.
const AccountSuspended = "This account has been suspended. Contact support."

// Suspended refuses a sign-in of the suspended account u whose password
// (and code, with MFA on) were right: 403 {error, suspended: true}, and
// auth.login_attempted with result suspended.
func (s *Server) Suspended(w http.ResponseWriter, r *http.Request, u store.User) {
	e := events.LoginAttempted{Email: u.Email, User: u.PublicID, Result: events.LoginSuspended}
	if err := s.Repo.Record(r.Context(), e, store.EventScope{UserID: u.ID}); err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, 403, map[string]any{"error": AccountSuspended, "suspended": true})
}
