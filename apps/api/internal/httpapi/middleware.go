package httpapi

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/store"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"time"
)

type sessionKey struct{}
type workspaceKey struct{}

func current(r *http.Request) store.Session { return r.Context().Value(sessionKey{}).(store.Session) }

// optionalSession is the request's session on routes that may run without
// one (the rate limiters).
func optionalSession(r *http.Request) (store.Session, bool) {
	session, ok := r.Context().Value(sessionKey{}).(store.Session)
	return session, ok
}

// clientIP is the connection's address. Forwarded headers are not trusted.
func clientIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// currentWorkspace is the workspace in the path, set by requireWorkspace.
func currentWorkspace(r *http.Request) store.Workspace {
	return r.Context().Value(workspaceKey{}).(store.Workspace)
}
func unsafeMethod(method string) bool {
	return method != "GET" && method != "HEAD" && method != "OPTIONS"
}
func (s *Server) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if unsafeMethod(r.Method) {
			if r.Header.Get("Origin") != s.Config.Origin || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				fail(w, 403, "Request origin is not allowed.")
				return
			}
			if r.Method != "DELETE" {
				media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
				if err != nil || media != "application/json" {
					fail(w, 415, "Content-Type must be application/json.")
					return
				}
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		ctx = events.WithUserAgent(ctx, r.UserAgent())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

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
	return s.Store.Session(r.Context(), auth.Digest(token))
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

// sessionDigest is what the sessions table stores for the request's session
// cookie, or "" without one.
func (s *Server) sessionDigest(r *http.Request) string {
	token, ok := s.sessionToken(r)
	if !ok {
		return ""
	}
	return auth.Digest(token)
}
func (s *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := s.session(r)
		if errors.Is(err, errNoSession) {
			fail(w, 401, "Please sign in to continue.")
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 401, "Your session has expired. Please sign in again.")
			return
		}
		if err != nil {
			internalError(w, err)
			return
		}
		if unsafeMethod(r.Method) && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRF)) != 1 {
			fail(w, 403, "Invalid CSRF token. Refresh and try again.")
			return
		}
		ctx := context.WithValue(r.Context(), sessionKey{}, session)
		next.ServeHTTP(w, r.WithContext(events.WithActor(ctx, session.User.ID)))
	})
}

// requirePageSession sends visitors without a valid session from a signed-in
// page (/app/*) to login, returning there afterwards, before any HTML is
// served. If the database can't be reached the page is served anyway; its
// own session check then shows the outage page.
func (s *Server) requirePageSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		_, err := s.session(r.WithContext(ctx))
		if errors.Is(err, errNoSession) || errors.Is(err, sql.ErrNoRows) {
			w.Header().Set("Cache-Control", "no-store")
			login := "/auth/login?next=" + url.QueryEscape(r.URL.RequestURI())
			http.Redirect(w, r, login, http.StatusFound)
			return
		}
		if err != nil {
			slog.Error("page session check failed", "error", err)
		}
		next.ServeHTTP(w, r)
	})
}

// requireWorkspace runs after requireSession. It loads the {workspace} in the
// path through the signed-in user's membership; a malformed ID, a missing
// workspace and someone else's workspace all get the same 404.
func (s *Server) requireWorkspace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("workspace")
		if !validPublicID("wsp", id) {
			fail(w, 404, "Workspace not found.")
			return
		}
		workspace, err := s.Store.MemberWorkspace(r.Context(), current(r).User.ID, id)
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 404, "Workspace not found.")
			return
		}
		if err != nil {
			internalError(w, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), workspaceKey{}, workspace)))
	})
}

// requireRole runs after requireWorkspace and refuses members whose role is
// below min.
func requireRole(min string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !store.RoleAtLeast(currentWorkspace(r).Role, min) {
			fail(w, 403, "You don’t have permission to do that in this workspace.")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) limitAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.allowAuth(w, r, "ip", "ip:"+auth.Digest(clientIP(r)), 60) {
			return
		}
		select {
		case s.authSlots <- struct{}{}:
			defer func() { <-s.authSlots }()
		default:
			fail(w, 503, "Sign-in is busy. Please try again shortly.")
			return
		}
		next.ServeHTTP(w, r)
	})
}
