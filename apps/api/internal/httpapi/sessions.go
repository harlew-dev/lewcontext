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

// ClearSessionCookie tells the browser to drop its session cookie.
func (s *Server) ClearSessionCookie(w http.ResponseWriter) {
	expired := auth.Cookie("", s.Config.Production)
	expired.MaxAge = -1
	expired.Expires = time.Unix(0, 0)
	http.SetCookie(w, expired)
}
